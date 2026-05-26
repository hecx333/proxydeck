package selector

import (
	"context"
	"testing"
	"time"

	"proxydeck/backend/internal/db"
	"proxydeck/backend/internal/model"
	"proxydeck/backend/internal/redisstore"

	"github.com/alicebob/miniredis/v2"
)

func TestParseUsername(t *testing.T) {
	uid, filters := ParseUsername("user001__region=SG__city=Singapore__asn=AS20473__isp=Cloudflare__sid=abc123")
	if uid != "user001" {
		t.Fatalf("uid = %q, want user001", uid)
	}
	if filters.Region != "SG" || filters.City != "Singapore" || filters.ASN != "AS20473" || filters.ISP != "Cloudflare" || filters.SID != "abc123" {
		t.Fatalf("unexpected filters: %+v", filters)
	}
}

func TestFilterHashStable(t *testing.T) {
	first := FilterHash(Filters{Region: "SG", City: "Singapore", ASN: "AS1", ISP: "Test"})
	second := FilterHash(Filters{Region: "SG", City: "Singapore", ASN: "AS1", ISP: "Test"})
	if first != second {
		t.Fatalf("hash not stable: %q != %q", first, second)
	}
}

func TestPickRemovesStaleStickyKey(t *testing.T) {
	sqliteDB, err := db.Open(t.TempDir() + "/app.db")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	mini, err := miniredis.Run()
	if err != nil {
		t.Skipf("miniredis unavailable in this environment: %v", err)
	}
	defer mini.Close()

	store := redisstore.New(mini.Addr(), "", 0)
	svc := NewService(sqliteDB, store, time.Hour)
	ctx := context.Background()

	healthy := model.ProxyNode{
		NodeKey:        "http://1.1.1.1:80",
		Protocol:       "http",
		Host:           "1.1.1.1",
		Port:           80,
		ExpectedRegion: "SG",
		Healthy:        true,
	}
	unhealthy := model.ProxyNode{
		NodeKey:        "http://2.2.2.2:80",
		Protocol:       "http",
		Host:           "2.2.2.2",
		Port:           80,
		ExpectedRegion: "SG",
		Healthy:        false,
	}
	if err := sqliteDB.Create(&healthy).Error; err != nil {
		t.Fatalf("create healthy node: %v", err)
	}
	if err := sqliteDB.Create(&unhealthy).Error; err != nil {
		t.Fatalf("create unhealthy node: %v", err)
	}
	if err := store.CacheNode(ctx, healthy); err != nil {
		t.Fatalf("cache healthy node: %v", err)
	}
	if err := store.CacheNode(ctx, unhealthy); err != nil {
		t.Fatalf("cache unhealthy node: %v", err)
	}

	stickyKey := redisstore.StickyKey("user001", FilterHash(Filters{Region: "SG"}), "sid-1")
	if err := store.Client.Set(ctx, stickyKey, unhealthy.ID, 0).Err(); err != nil {
		t.Fatalf("seed stale sticky: %v", err)
	}

	node, err := svc.Pick(ctx, "user001", Filters{Region: "SG", SID: "sid-1"})
	if err != nil {
		t.Fatalf("pick: %v", err)
	}
	if node.ID != healthy.ID {
		t.Fatalf("picked node = %d, want %d", node.ID, healthy.ID)
	}
	value, err := store.Client.Get(ctx, stickyKey).Uint64()
	if err != nil {
		t.Fatalf("reload sticky: %v", err)
	}
	if uint(value) != healthy.ID {
		t.Fatalf("sticky value = %d, want %d", value, healthy.ID)
	}
}

func TestPickDeletesStaleStickyWhenNoCandidateExists(t *testing.T) {
	sqliteDB, err := db.Open(t.TempDir() + "/app.db")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	mini, err := miniredis.Run()
	if err != nil {
		t.Skipf("miniredis unavailable in this environment: %v", err)
	}
	defer mini.Close()

	store := redisstore.New(mini.Addr(), "", 0)
	svc := NewService(sqliteDB, store, time.Hour)
	ctx := context.Background()

	unhealthy := model.ProxyNode{
		NodeKey:        "http://2.2.2.2:80",
		Protocol:       "http",
		Host:           "2.2.2.2",
		Port:           80,
		ExpectedRegion: "SG",
		Healthy:        false,
	}
	if err := sqliteDB.Create(&unhealthy).Error; err != nil {
		t.Fatalf("create unhealthy node: %v", err)
	}
	if err := store.CacheNode(ctx, unhealthy); err != nil {
		t.Fatalf("cache unhealthy node: %v", err)
	}

	stickyKey := redisstore.StickyKey("user001", FilterHash(Filters{Region: "SG"}), "sid-1")
	if err := store.Client.Set(ctx, stickyKey, unhealthy.ID, 0).Err(); err != nil {
		t.Fatalf("seed stale sticky: %v", err)
	}

	if _, err := svc.Pick(ctx, "user001", Filters{Region: "SG", SID: "sid-1"}); err != ErrNoHealthyNode {
		t.Fatalf("pick error = %v, want %v", err, ErrNoHealthyNode)
	}
	exists, err := store.Client.Exists(ctx, stickyKey).Result()
	if err != nil {
		t.Fatalf("check sticky: %v", err)
	}
	if exists != 0 {
		t.Fatal("expected stale sticky key to be deleted")
	}
}
