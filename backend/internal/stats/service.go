package stats

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"proxydeck/backend/internal/model"
	"proxydeck/backend/internal/redisstore"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type Service struct {
	db    *gorm.DB
	store *redisstore.Store
}

func NewService(db *gorm.DB, store *redisstore.Store) *Service {
	return &Service{db: db, store: store}
}

type TrafficRecord struct {
	UID      string
	NodeID   uint
	Region   string
	Upload   int64
	Download int64
	Requests int64
}

func (s *Service) RecordTraffic(ctx context.Context, record TrafficRecord) error {
	pipe := s.store.Client.TxPipeline()
	pipe.IncrBy(ctx, redisstore.UsageKey(record.UID, "upload"), record.Upload)
	pipe.IncrBy(ctx, redisstore.UsageKey(record.UID, "download"), record.Download)
	pipe.IncrBy(ctx, redisstore.RequestsKey(record.UID), maxInt64(record.Requests, 1))
	if record.NodeID > 0 {
		pipe.IncrBy(ctx, redisstore.NodeUsageKey(record.NodeID, "upload"), record.Upload)
		pipe.IncrBy(ctx, redisstore.NodeUsageKey(record.NodeID, "download"), record.Download)
		pipe.IncrBy(ctx, redisstore.NodeRequestsKey(record.NodeID), maxInt64(record.Requests, 1))
	}
	pipe.ZIncrBy(ctx, redisstore.RegionUsageKey(record.Region), float64(record.Upload+record.Download), redisstore.RegionMember(record.Region))
	hourKey := redisstore.HourlyUsageKey(time.Now().UTC().Format("2006010215"))
	pipe.IncrBy(ctx, hourKey, record.Upload+record.Download)
	_, err := pipe.Exec(ctx)
	return err
}

func (s *Service) FlushUsage(ctx context.Context) error {
	keys, err := s.store.Client.Keys(ctx, "usage:*:upload").Result()
	if err != nil {
		return err
	}
	for _, key := range keys {
		if strings.HasPrefix(key, "usage:node:") {
			continue
		}
		uid := key[len("usage:") : len(key)-len(":upload")]
		up, _ := s.store.Client.Get(ctx, redisstore.UsageKey(uid, "upload")).Int64()
		down, _ := s.store.Client.Get(ctx, redisstore.UsageKey(uid, "download")).Int64()
		total := up + down
		if total == 0 {
			continue
		}
		if err := s.db.Model(&model.User{}).Where("uid = ?", uid).
			Updates(map[string]any{
				"used_bytes":     gorm.Expr("used_bytes + ?", total),
				"total_requests": gorm.Expr("coalesce(total_requests, 0) + ?", requestCount(ctx, s.store, uid)),
			}).Error; err != nil {
			return err
		}
		pipe := s.store.Client.TxPipeline()
		pipe.DecrBy(ctx, redisstore.UsageKey(uid, "upload"), up)
		pipe.DecrBy(ctx, redisstore.UsageKey(uid, "download"), down)
		requests, _ := s.store.Client.Get(ctx, redisstore.RequestsKey(uid)).Int64()
		pipe.DecrBy(ctx, redisstore.RequestsKey(uid), requests)
		if _, err := pipe.Exec(ctx); err != nil {
			return err
		}
	}
	if err := s.flushNodeUsage(ctx); err != nil {
		return err
	}
	return nil
}

func (s *Service) StartFlushLoop(ctx context.Context) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			_ = s.FlushUsage(ctx)
		}
	}
}

type Overview struct {
	TotalUsers        int64            `json:"total_users"`
	ActiveUsers       int64            `json:"active_users"`
	TotalNodes        int64            `json:"total_nodes"`
	HealthyNodes      int64            `json:"healthy_nodes"`
	UnhealthyNodes    int64            `json:"unhealthy_nodes"`
	TotalTraffic      int64            `json:"total_traffic"`
	ActiveConnections int64            `json:"active_connections"`
	TotalRequests     int64            `json:"total_requests"`
	TodayTraffic      int64            `json:"today_traffic"`
	RegionCounts      map[string]int64 `json:"region_counts"`
	RecentErrors      []RecentError    `json:"recent_errors"`
}

type RecentError struct {
	Action    string `json:"action"`
	Target    string `json:"target"`
	Detail    string `json:"detail"`
	CreatedAt string `json:"created_at"`
}

func (s *Service) Overview(ctx context.Context) (Overview, error) {
	var out Overview
	out.RegionCounts = map[string]int64{}
	s.db.Model(&model.User{}).Where("uid NOT LIKE ? ESCAPE '\\'", "\\_\\_admin\\_\\_:%").Count(&out.TotalUsers)
	s.db.Model(&model.ProxyNode{}).Count(&out.TotalNodes)
	s.db.Model(&model.ProxyNode{}).Where("healthy = ?", true).Count(&out.HealthyNodes)
	out.UnhealthyNodes = out.TotalNodes - out.HealthyNodes
	s.db.Model(&model.User{}).Where("uid NOT LIKE ? ESCAPE '\\'", "\\_\\_admin\\_\\_:%").Select("coalesce(sum(used_bytes), 0)").Scan(&out.TotalTraffic)
	s.db.Model(&model.User{}).Where("uid NOT LIKE ? ESCAPE '\\'", "\\_\\_admin\\_\\_:%").Select("coalesce(sum(total_requests), 0)").Scan(&out.TotalRequests)
	keys, _ := s.store.Client.Keys(ctx, "conn:*:current").Result()
	for _, key := range keys {
		n, _ := s.store.Client.Get(ctx, key).Int64()
		if n > 0 {
			out.ActiveUsers++
		}
		out.ActiveConnections += n
	}
	type row struct {
		DetectedRegion string
		Count          int64
	}
	var rows []row
	s.db.Model(&model.ProxyNode{}).Select("detected_region, count(*) as count").
		Group("detected_region").Where("detected_region <> ''").Scan(&rows)
	for _, item := range rows {
		out.RegionCounts[item.DetectedRegion] = item.Count
	}
	out.TodayTraffic = s.todayTraffic(ctx)
	type auditRow struct {
		Action     string
		TargetType string
		TargetID   string
		Detail     string
		CreatedAt  time.Time
	}
	var auditRows []auditRow
	s.db.Model(&model.AuditLog{}).
		Select("action, target_type, target_id, detail, created_at").
		Where("action LIKE ?", "%error%").
		Order("id desc").
		Limit(5).
		Scan(&auditRows)
	for _, row := range auditRows {
		out.RecentErrors = append(out.RecentErrors, RecentError{
			Action:    row.Action,
			Target:    row.TargetType + "#" + row.TargetID,
			Detail:    row.Detail,
			CreatedAt: row.CreatedAt.Format(time.RFC3339),
		})
	}
	return out, nil
}

type TrafficPoint struct {
	Hour  string `json:"hour"`
	Bytes int64  `json:"bytes"`
}

type TrafficSummary struct {
	TopUsers   []map[string]any `json:"top_users"`
	TopNodes   []map[string]any `json:"top_nodes"`
	TopRegions []map[string]any `json:"top_regions"`
	Recent24H  []TrafficPoint   `json:"recent_24h"`
}

func (s *Service) TrafficSummary() (TrafficSummary, error) {
	var out TrafficSummary
	out.TopUsers = make([]map[string]any, 0)
	out.TopNodes = make([]map[string]any, 0)
	out.TopRegions = make([]map[string]any, 0)
	s.db.Model(&model.User{}).Where("uid NOT LIKE ? ESCAPE '\\'", "\\_\\_admin\\_\\_:%").Select("uid, used_bytes, coalesce(total_requests, 0) as total_requests").
		Order("used_bytes desc").Limit(10).Find(&out.TopUsers)
	type trafficNodeRow struct {
		Tag            string
		DetectedRegion string
		City           string
		ASN            string
		ExitIP         string
		LatencyMS      int64
		Bytes          int64
		TotalRequests  int64
	}
	var topNodes []trafficNodeRow
	s.db.Model(&model.ProxyNode{}).
		Select("tag, detected_region, city, asn, exit_ip, latency_ms, upload_bytes + download_bytes as bytes, coalesce(total_requests, 0) as total_requests").
		Order("upload_bytes + download_bytes desc").
		Limit(10).
		Find(&topNodes)
	for _, node := range topNodes {
		out.TopNodes = append(out.TopNodes, map[string]any{
			"tag":            trafficNodeTag(node.Tag, node.DetectedRegion, node.City, node.ASN, node.ExitIP),
			"raw_tag":        node.Tag,
			"exit_ip":        node.ExitIP,
			"latency_ms":     node.LatencyMS,
			"bytes":          node.Bytes,
			"total_requests": node.TotalRequests,
		})
	}
	out.TopRegions = s.topRegions(context.Background())
	now := time.Now().Truncate(time.Hour)
	for i := 23; i >= 0; i-- {
		ts := now.Add(-time.Duration(i) * time.Hour)
		key := redisstore.HourlyUsageKey(ts.UTC().Format("2006010215"))
		bytes, _ := s.store.Client.Get(context.Background(), key).Int64()
		out.Recent24H = append(out.Recent24H, TrafficPoint{Hour: ts.Format("15:00"), Bytes: bytes})
	}
	return out, nil
}

func (s *Service) flushNodeUsage(ctx context.Context) error {
	keys, err := s.store.Client.Keys(ctx, "usage:node:*:upload").Result()
	if err != nil {
		return err
	}
	for _, key := range keys {
		var nodeID uint
		if _, err := fmt.Sscanf(key, "usage:node:%d:upload", &nodeID); err != nil {
			continue
		}
		upload, _ := s.store.Client.Get(ctx, redisstore.NodeUsageKey(nodeID, "upload")).Int64()
		download, _ := s.store.Client.Get(ctx, redisstore.NodeUsageKey(nodeID, "download")).Int64()
		requests, _ := s.store.Client.Get(ctx, redisstore.NodeRequestsKey(nodeID)).Int64()
		if upload == 0 && download == 0 && requests == 0 {
			continue
		}
		if err := s.db.Model(&model.ProxyNode{}).Where("id = ?", nodeID).Updates(map[string]any{
			"upload_bytes":   gorm.Expr("coalesce(upload_bytes, 0) + ?", upload),
			"download_bytes": gorm.Expr("coalesce(download_bytes, 0) + ?", download),
			"total_requests": gorm.Expr("coalesce(total_requests, 0) + ?", requests),
		}).Error; err != nil {
			return err
		}
		pipe := s.store.Client.TxPipeline()
		pipe.DecrBy(ctx, redisstore.NodeUsageKey(nodeID, "upload"), upload)
		pipe.DecrBy(ctx, redisstore.NodeUsageKey(nodeID, "download"), download)
		pipe.DecrBy(ctx, redisstore.NodeRequestsKey(nodeID), requests)
		if _, err := pipe.Exec(ctx); err != nil {
			return err
		}
	}
	return nil
}

func requestCount(ctx context.Context, store *redisstore.Store, uid string) int64 {
	requests, _ := store.Client.Get(ctx, redisstore.RequestsKey(uid)).Int64()
	return requests
}

func (s *Service) todayTraffic(ctx context.Context) int64 {
	total := int64(0)
	now := time.Now().UTC().Truncate(time.Hour)
	for i := 0; i <= now.Hour(); i++ {
		key := redisstore.HourlyUsageKey(now.Add(-time.Duration(i) * time.Hour).Format("2006010215"))
		value, _ := s.store.Client.Get(ctx, key).Int64()
		total += value
	}
	return total
}

func (s *Service) topRegions(ctx context.Context) []map[string]any {
	values, err := s.store.Client.ZRevRangeWithScores(ctx, redisstore.RegionUsageKey(""), 0, 9).Result()
	if err != nil {
		return nil
	}
	items := make([]map[string]any, 0, len(values))
	for _, value := range values {
		region, _ := value.Member.(string)
		items = append(items, map[string]any{
			"region": region,
			"value":  int64(value.Score),
		})
	}
	sort.Slice(items, func(i, j int) bool {
		return items[i]["value"].(int64) > items[j]["value"].(int64)
	})
	return items
}

func maxInt64(value, fallback int64) int64 {
	if value <= 0 {
		return fallback
	}
	return value
}

func trafficNodeTag(rawTag, region, city, asn, exitIP string) string {
	parts := make([]string, 0, 4)
	if region != "" {
		parts = append(parts, region)
	}
	if city != "" {
		parts = append(parts, city)
	}
	if asn != "" {
		parts = append(parts, asn)
	}
	if exitIP != "" {
		parts = append(parts, exitIP)
	}
	if len(parts) == 0 {
		return rawTag
	}
	return strings.Join(parts, " | ")
}

func UpsertAudit(db *gorm.DB, log model.AuditLog) error {
	return db.Clauses(clause.OnConflict{DoNothing: true}).Create(&log).Error
}
