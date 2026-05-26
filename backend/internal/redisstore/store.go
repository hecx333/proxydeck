package redisstore

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"proxydeck/backend/internal/model"

	"github.com/redis/go-redis/v9"
)

type Store struct {
	Client *redis.Client
}

type cachedNode struct {
	model.ProxyNode
	UpstreamUsernameEnc string `json:"upstream_username_enc"`
	UpstreamPasswordEnc string `json:"upstream_password_enc"`
}

func New(addr, password string, db int) *Store {
	return &Store{
		Client: redis.NewClient(&redis.Options{
			Addr:     addr,
			Password: password,
			DB:       db,
		}),
	}
}

func StickyKey(uid, filterHash, sid string) string {
	return fmt.Sprintf("sticky:%s:%s:%s", uid, filterHash, sid)
}

func UsageKey(uid, direction string) string {
	return fmt.Sprintf("usage:%s:%s", uid, direction)
}

func ConnKey(uid string) string {
	return fmt.Sprintf("conn:%s:current", uid)
}

func RequestsKey(uid string) string {
	return fmt.Sprintf("usage:%s:requests", uid)
}

func NodeUsageKey(nodeID uint, direction string) string {
	return fmt.Sprintf("usage:node:%d:%s", nodeID, direction)
}

func NodeRequestsKey(nodeID uint) string {
	return fmt.Sprintf("usage:node:%d:requests", nodeID)
}

func RegionUsageKey(region string) string {
	return "traffic:region"
}

func HourlyUsageKey(hour string) string {
	return fmt.Sprintf("traffic:hour:%s", hour)
}

func RegionMember(region string) string {
	if region == "" {
		return "__unknown__"
	}
	return region
}

func NodeKey(nodeID uint) string {
	return fmt.Sprintf("node:%d", nodeID)
}

func RegionKey(region string) string {
	return "region:" + region
}

func CityKey(city string) string {
	return "city:" + city
}

func ASNKey(asn string) string {
	return "asn:" + asn
}

func ISPKey(isp string) string {
	return "isp:" + isp
}

func (s *Store) CacheNode(ctx context.Context, node model.ProxyNode) error {
	var previous *model.ProxyNode
	if loaded, err := s.LoadNode(ctx, node.ID); err == nil {
		previous = loaded
	}
	raw, err := json.Marshal(cachedNode{
		ProxyNode:           node,
		UpstreamUsernameEnc: node.UpstreamUsernameEnc,
		UpstreamPasswordEnc: node.UpstreamPasswordEnc,
	})
	if err != nil {
		return err
	}
	pipe := s.Client.TxPipeline()
	pipe.Set(ctx, NodeKey(node.ID), raw, 24*time.Hour)
	if node.Healthy && !node.Disabled {
		pipe.SAdd(ctx, "healthy_nodes", node.ID)
	} else {
		pipe.SRem(ctx, "healthy_nodes", node.ID)
	}
	if previous != nil {
		for _, key := range nodeIndexKeys(*previous) {
			pipe.SRem(ctx, key, node.ID)
		}
	}
	region := node.DetectedRegion
	if region == "" {
		region = node.ExpectedRegion
	}
	replaceSetMembership(pipe, ctx, RegionKey(region), node.ID, region != "")
	replaceSetMembership(pipe, ctx, CityKey(node.City), node.ID, node.City != "")
	replaceSetMembership(pipe, ctx, ASNKey(node.ASN), node.ID, node.ASN != "")
	replaceSetMembership(pipe, ctx, ISPKey(node.ISP), node.ID, node.ISP != "")
	_, err = pipe.Exec(ctx)
	if err != nil {
		return err
	}
	if !node.Healthy || node.Disabled {
		return s.cleanupStickyForNode(ctx, node.ID)
	}
	return nil
}

func (s *Store) LoadNode(ctx context.Context, nodeID uint) (*model.ProxyNode, error) {
	raw, err := s.Client.Get(ctx, NodeKey(nodeID)).Result()
	if err != nil {
		return nil, err
	}
	var payload cachedNode
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		return nil, err
	}
	node := payload.ProxyNode
	node.UpstreamUsernameEnc = payload.UpstreamUsernameEnc
	node.UpstreamPasswordEnc = payload.UpstreamPasswordEnc
	return &node, nil
}

func (s *Store) RemoveNode(ctx context.Context, node model.ProxyNode) error {
	pipe := s.Client.TxPipeline()
	pipe.Del(ctx, NodeKey(node.ID))
	pipe.SRem(ctx, "healthy_nodes", node.ID)
	region := node.DetectedRegion
	if region == "" {
		region = node.ExpectedRegion
	}
	if region != "" {
		pipe.SRem(ctx, RegionKey(region), node.ID)
	}
	if node.City != "" {
		pipe.SRem(ctx, CityKey(node.City), node.ID)
	}
	if node.ASN != "" {
		pipe.SRem(ctx, ASNKey(node.ASN), node.ID)
	}
	if node.ISP != "" {
		pipe.SRem(ctx, ISPKey(node.ISP), node.ID)
	}
	pipe.Del(ctx,
		NodeUsageKey(node.ID, "upload"),
		NodeUsageKey(node.ID, "download"),
		NodeRequestsKey(node.ID),
	)
	_, err := pipe.Exec(ctx)
	if err != nil {
		return err
	}
	return s.cleanupStickyForNode(ctx, node.ID)
}

func (s *Store) UserPendingUsage(ctx context.Context, uid string) int64 {
	upload, _ := s.Client.Get(ctx, UsageKey(uid, "upload")).Int64()
	download, _ := s.Client.Get(ctx, UsageKey(uid, "download")).Int64()
	return upload + download
}

func ParseUintMembers(values []string) []uint {
	out := make([]uint, 0, len(values))
	for _, value := range values {
		id, err := strconv.ParseUint(value, 10, 64)
		if err == nil {
			out = append(out, uint(id))
		}
	}
	return out
}

func replaceSetMembership(pipe redis.Pipeliner, ctx context.Context, key string, id uint, shouldAdd bool) {
	if key == "region:" || key == "city:" || key == "asn:" || key == "isp:" {
		return
	}
	if shouldAdd {
		pipe.SAdd(ctx, key, id)
		return
	}
	pipe.SRem(ctx, key, id)
}

func nodeIndexKeys(node model.ProxyNode) []string {
	keys := make([]string, 0, 4)
	region := node.DetectedRegion
	if region == "" {
		region = node.ExpectedRegion
	}
	if region != "" {
		keys = append(keys, RegionKey(region))
	}
	if node.City != "" {
		keys = append(keys, CityKey(node.City))
	}
	if node.ASN != "" {
		keys = append(keys, ASNKey(node.ASN))
	}
	if node.ISP != "" {
		keys = append(keys, ISPKey(node.ISP))
	}
	return keys
}

func (s *Store) cleanupStickyForNode(ctx context.Context, nodeID uint) error {
	keys, err := s.Client.Keys(ctx, "sticky:*").Result()
	if err != nil {
		return err
	}
	if len(keys) == 0 {
		return nil
	}
	toDelete := make([]string, 0)
	for _, key := range keys {
		value, err := s.Client.Get(ctx, key).Uint64()
		if err != nil {
			continue
		}
		if uint(value) == nodeID {
			toDelete = append(toDelete, key)
		}
	}
	if len(toDelete) == 0 {
		return nil
	}
	return s.Client.Del(ctx, toDelete...).Err()
}
