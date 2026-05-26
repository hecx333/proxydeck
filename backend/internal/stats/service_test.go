package stats

import (
	"context"
	"testing"
	"time"

	"proxydeck/backend/internal/db"
	"proxydeck/backend/internal/model"
	"proxydeck/backend/internal/redisstore"

	"github.com/alicebob/miniredis/v2"
)

func newMiniRedisForStats(t *testing.T) *miniredis.Miniredis {
	t.Helper()
	mini, err := miniredis.Run()
	if err != nil {
		t.Skipf("miniredis unavailable in this environment: %v", err)
	}
	return mini
}

func TestTrafficNodeTag(t *testing.T) {
	if got := trafficNodeTag("raw-node", "California", "Los Angeles", "AS13335", "1.1.1.1"); got != "California | Los Angeles | AS13335 | 1.1.1.1" {
		t.Fatalf("trafficNodeTag = %q", got)
	}
	if got := trafficNodeTag("raw-node", "", "", "", ""); got != "raw-node" {
		t.Fatalf("trafficNodeTag fallback = %q", got)
	}
}

func TestFlushUsageSkipsNodeUsageKeys(t *testing.T) {
	sqlite, err := db.Open(t.TempDir() + "/app.db")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	ctx := context.Background()

	if err := sqlite.Create(&model.User{UID: "user001", PasswordHash: "x", Enabled: true}).Error; err != nil {
		t.Fatalf("create user: %v", err)
	}
	mini := newMiniRedisForStats(t)
	defer mini.Close()
	store := redisstore.New(mini.Addr(), "", 0)
	svc := NewService(sqlite, store)
	_ = store.Client.Set(ctx, redisstore.UsageKey("user001", "upload"), 100, 0).Err()
	_ = store.Client.Set(ctx, redisstore.UsageKey("user001", "download"), 50, 0).Err()
	_ = store.Client.Set(ctx, redisstore.RequestsKey("user001"), 2, 0).Err()
	_ = store.Client.Set(ctx, redisstore.NodeUsageKey(1, "upload"), 999, 0).Err()

	if err := svc.FlushUsage(ctx); err != nil {
		t.Fatalf("flush usage: %v", err)
	}

	var user model.User
	if err := sqlite.Where("uid = ?", "user001").First(&user).Error; err != nil {
		t.Fatalf("reload user: %v", err)
	}
	if user.UsedBytes != 150 {
		t.Fatalf("used bytes = %d, want 150", user.UsedBytes)
	}
	if user.TotalRequests != 2 {
		t.Fatalf("total requests = %d, want 2", user.TotalRequests)
	}
}

func TestOverviewCountsActiveUsersFromLiveConnections(t *testing.T) {
	sqlite, err := db.Open(t.TempDir() + "/app.db")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	ctx := context.Background()
	mini := newMiniRedisForStats(t)
	defer mini.Close()
	store := redisstore.New(mini.Addr(), "", 0)
	svc := NewService(sqlite, store)

	users := []model.User{
		{UID: "user001", PasswordHash: "x", Enabled: true},
		{UID: "user002", PasswordHash: "x", Enabled: true},
		{UID: "user003", PasswordHash: "x", Enabled: false},
	}
	for _, user := range users {
		if err := sqlite.Create(&user).Error; err != nil {
			t.Fatalf("create user %s: %v", user.UID, err)
		}
	}
	_ = store.Client.Set(ctx, redisstore.ConnKey("user001"), 2, 0).Err()
	_ = store.Client.Set(ctx, redisstore.ConnKey("user002"), 0, 0).Err()
	_ = store.Client.Set(ctx, redisstore.ConnKey("user003"), 1, 0).Err()

	overview, err := svc.Overview(ctx)
	if err != nil {
		t.Fatalf("overview: %v", err)
	}
	if overview.ActiveUsers != 2 {
		t.Fatalf("active users = %d, want 2", overview.ActiveUsers)
	}
	if overview.ActiveConnections != 3 {
		t.Fatalf("active connections = %d, want 3", overview.ActiveConnections)
	}
}

func TestOverviewReturnsStructuredRecentErrors(t *testing.T) {
	sqlite, err := db.Open(t.TempDir() + "/app.db")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	ctx := context.Background()
	mini := newMiniRedisForStats(t)
	defer mini.Close()
	store := redisstore.New(mini.Addr(), "", 0)
	svc := NewService(sqlite, store)

	now := time.Now().UTC().Truncate(time.Second)
	logs := []model.AuditLog{
		{Operator: "admin", Action: "check_node_error", TargetType: "node", TargetID: "12", Detail: "timeout", CreatedAt: now.Add(-time.Minute)},
		{Operator: "admin", Action: "sync_subscription_error", TargetType: "subscription", TargetID: "7", Detail: "bad gateway", CreatedAt: now},
	}
	for _, log := range logs {
		if err := sqlite.Create(&log).Error; err != nil {
			t.Fatalf("create log: %v", err)
		}
	}

	overview, err := svc.Overview(ctx)
	if err != nil {
		t.Fatalf("overview: %v", err)
	}
	if len(overview.RecentErrors) != 2 {
		t.Fatalf("recent errors len = %d, want 2", len(overview.RecentErrors))
	}
	if overview.RecentErrors[0].Action != "sync_subscription_error" {
		t.Fatalf("first action = %q", overview.RecentErrors[0].Action)
	}
	if overview.RecentErrors[0].Target != "subscription#7" {
		t.Fatalf("first target = %q", overview.RecentErrors[0].Target)
	}
	if overview.RecentErrors[0].Detail != "bad gateway" {
		t.Fatalf("first detail = %q", overview.RecentErrors[0].Detail)
	}
	if overview.RecentErrors[0].CreatedAt == "" {
		t.Fatal("first created_at is empty")
	}
}
