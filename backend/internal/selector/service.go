package selector

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"math/rand"
	"strings"
	"time"

	"proxydeck/backend/internal/model"
	"proxydeck/backend/internal/redisstore"

	"gorm.io/gorm"
)

var ErrNoHealthyNode = errors.New("no healthy node available")

type Filters struct {
	Region string `json:"region"`
	State  string `json:"state"`
	City   string `json:"city"`
	ASN    string `json:"asn"`
	ISP    string `json:"isp"`
	SID    string `json:"sid"`
}

type Service struct {
	db        *gorm.DB
	store     *redisstore.Store
	stickyTTL time.Duration
}

func NewService(db *gorm.DB, store *redisstore.Store, stickyTTL time.Duration) *Service {
	return &Service{db: db, store: store, stickyTTL: stickyTTL}
}

func ParseUsername(raw string) (string, Filters) {
	if strings.Contains(raw, "__") {
		return parseLegacyUsername(raw)
	}
	return parseDashUsername(raw)
}

func parseLegacyUsername(raw string) (string, Filters) {
	parts := strings.Split(raw, "__")
	filters := Filters{}
	if len(parts) == 0 {
		return "", filters
	}
	uid := parts[0]
	for _, part := range parts[1:] {
		kv := strings.SplitN(part, "=", 2)
		if len(kv) != 2 {
			continue
		}
		switch kv[0] {
		case "region":
			filters.Region = kv[1]
		case "st", "state":
			filters.State = kv[1]
		case "city":
			filters.City = kv[1]
		case "asn":
			filters.ASN = kv[1]
		case "isp":
			filters.ISP = kv[1]
		case "sid":
			filters.SID = kv[1]
		}
	}
	return uid, filters
}

func parseDashUsername(raw string) (string, Filters) {
	filters := Filters{}
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", filters
	}
	parts := strings.Split(raw, "-")
	if len(parts) == 0 {
		return "", filters
	}
	uid := strings.TrimSpace(parts[0])
	if len(parts) == 1 {
		return uid, filters
	}
	for i := 1; i < len(parts)-1; i += 2 {
		key := strings.TrimSpace(parts[i])
		value := strings.TrimSpace(parts[i+1])
		switch key {
		case "region":
			filters.Region = value
		case "st", "state":
			filters.State = value
		case "city":
			filters.City = value
		case "asn":
			filters.ASN = value
		case "isp":
			filters.ISP = value
		case "sid":
			filters.SID = value
		}
	}
	return uid, filters
}

func FilterHash(f Filters) string {
	sum := sha1.Sum([]byte(f.Region + "|" + f.State + "|" + f.City + "|" + f.ASN + "|" + f.ISP))
	return hex.EncodeToString(sum[:])
}

func (s *Service) Pick(ctx context.Context, uid string, f Filters) (*model.ProxyNode, error) {
	filterHash := FilterHash(f)
	if f.SID != "" {
		stickyKey := redisstore.StickyKey(uid, filterHash, f.SID)
		if nodeID, err := s.store.Client.Get(ctx, stickyKey).Uint64(); err == nil {
			var node model.ProxyNode
			if err := s.db.Where("id = ? AND healthy = ? AND disabled = ?", nodeID, true, false).First(&node).Error; err == nil {
				return &node, nil
			}
			_ = s.store.Client.Del(ctx, stickyKey).Err()
		}
	}
	nodes, err := s.candidates(ctx, f)
	if err != nil {
		return nil, err
	}
	if len(nodes) == 0 {
		return nil, ErrNoHealthyNode
	}
	node := nodes[rand.Intn(len(nodes))]
	if f.SID != "" {
		_ = s.store.Client.Set(ctx, redisstore.StickyKey(uid, filterHash, f.SID), node.ID, s.stickyTTL).Err()
	}
	return &node, nil
}

func (s *Service) candidates(ctx context.Context, f Filters) ([]model.ProxyNode, error) {
	ids, ok, err := s.matchNodeIDs(ctx, f)
	if err != nil {
		return nil, err
	}
	var nodes []model.ProxyNode
	if ok && len(ids) > 0 {
		for _, id := range ids {
			if node, err := s.store.LoadNode(ctx, id); err == nil && node.Healthy && !node.Disabled {
				if f.ISP == "" || node.ISP == f.ISP || node.Org == f.ISP {
					nodes = append(nodes, *node)
				}
			}
		}
		if len(nodes) > 0 {
			return nodes, nil
		}
	}
	query := s.db.Where("healthy = ? AND disabled = ?", true, false)
	if f.Region != "" {
		query = query.Where("country = ? OR detected_region = ? OR expected_region = ?", f.Region, f.Region, f.Region)
	}
	if f.State != "" {
		query = query.Where("detected_region = ?", f.State)
	}
	if f.City != "" {
		query = query.Where("city = ?", f.City)
	}
	if f.ASN != "" {
		query = query.Where("asn = ?", f.ASN)
	}
	if f.ISP != "" {
		query = query.Where("isp = ? OR org = ?", f.ISP, f.ISP)
	}
	if err := query.Find(&nodes).Error; err != nil {
		return nil, err
	}
	return nodes, nil
}

func (s *Service) matchNodeIDs(ctx context.Context, f Filters) ([]uint, bool, error) {
	sets := []string{"healthy_nodes"}
	if f.Region != "" && f.State == "" {
		sets = append(sets, redisstore.RegionKey(f.Region))
	}
	if f.State != "" {
		return nil, false, nil
	}
	if f.City != "" {
		sets = append(sets, redisstore.CityKey(f.City))
	}
	if f.ASN != "" {
		sets = append(sets, redisstore.ASNKey(f.ASN))
	}
	if f.ISP != "" {
		sets = append(sets, redisstore.ISPKey(f.ISP))
	}
	if len(sets) == 1 && f.ISP == "" {
		members, err := s.store.Client.SMembers(ctx, "healthy_nodes").Result()
		return redisstore.ParseUintMembers(members), true, err
	}
	if len(sets) <= 1 {
		return nil, false, nil
	}
	members, err := s.store.Client.SInter(ctx, sets...).Result()
	if err != nil {
		return nil, false, err
	}
	return redisstore.ParseUintMembers(members), true, nil
}
