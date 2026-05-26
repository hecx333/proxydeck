package redisstore

import (
	"context"
	"testing"

	"proxydeck/backend/internal/model"

	"github.com/alicebob/miniredis/v2"
)

func newMiniRedisForStore(t *testing.T) *miniredis.Miniredis {
	t.Helper()
	mini, err := miniredis.Run()
	if err != nil {
		t.Skipf("miniredis unavailable in this environment: %v", err)
	}
	return mini
}

func TestCacheNodeReplacesStaleIndexMembership(t *testing.T) {
	ctx := context.Background()
	mini := newMiniRedisForStore(t)
	defer mini.Close()

	store := New(mini.Addr(), "", 0)
	node := model.ProxyNode{
		ID:             7,
		ExpectedRegion: "SG",
		DetectedRegion: "SG",
		City:           "Singapore",
		ASN:            "AS100",
		ISP:            "ISP One",
		Healthy:        true,
	}
	if err := store.CacheNode(ctx, node); err != nil {
		t.Fatalf("cache initial node: %v", err)
	}

	node.DetectedRegion = "US"
	node.City = "Los Angeles"
	node.ASN = "AS200"
	node.ISP = "ISP Two"
	if err := store.CacheNode(ctx, node); err != nil {
		t.Fatalf("cache updated node: %v", err)
	}

	assertSetMissing(t, store, ctx, RegionKey("SG"), "7")
	assertSetMissing(t, store, ctx, CityKey("Singapore"), "7")
	assertSetMissing(t, store, ctx, ASNKey("AS100"), "7")
	assertSetMissing(t, store, ctx, ISPKey("ISP One"), "7")

	assertSetContains(t, store, ctx, RegionKey("US"), "7")
	assertSetContains(t, store, ctx, CityKey("Los Angeles"), "7")
	assertSetContains(t, store, ctx, ASNKey("AS200"), "7")
	assertSetContains(t, store, ctx, ISPKey("ISP Two"), "7")
}

func TestCacheNodeRemovesStickyKeysWhenNodeBecomesUnavailable(t *testing.T) {
	ctx := context.Background()
	mini := newMiniRedisForStore(t)
	defer mini.Close()

	store := New(mini.Addr(), "", 0)
	node := model.ProxyNode{
		ID:      9,
		Healthy: true,
	}
	if err := store.CacheNode(ctx, node); err != nil {
		t.Fatalf("cache healthy node: %v", err)
	}
	_ = store.Client.Set(ctx, StickyKey("user001", "hash-a", "sid-1"), node.ID, 0).Err()
	_ = store.Client.Set(ctx, StickyKey("user002", "hash-b", "sid-2"), uint(10), 0).Err()

	node.Healthy = false
	if err := store.CacheNode(ctx, node); err != nil {
		t.Fatalf("cache unhealthy node: %v", err)
	}

	assertRedisKeyMissing(t, store, ctx, StickyKey("user001", "hash-a", "sid-1"))
	assertRedisKeyExists(t, store, ctx, StickyKey("user002", "hash-b", "sid-2"))
}

func TestRemoveNodeRemovesStickyKeysPointingToNode(t *testing.T) {
	ctx := context.Background()
	mini := newMiniRedisForStore(t)
	defer mini.Close()

	store := New(mini.Addr(), "", 0)
	node := model.ProxyNode{
		ID:             11,
		DetectedRegion: "SG",
		City:           "Singapore",
		ASN:            "AS100",
		ISP:            "ISP One",
		Healthy:        true,
	}
	if err := store.CacheNode(ctx, node); err != nil {
		t.Fatalf("cache node: %v", err)
	}
	_ = store.Client.Set(ctx, StickyKey("user001", "hash-a", "sid-1"), node.ID, 0).Err()
	_ = store.Client.Set(ctx, StickyKey("user002", "hash-b", "sid-2"), uint(12), 0).Err()

	if err := store.RemoveNode(ctx, node); err != nil {
		t.Fatalf("remove node: %v", err)
	}

	assertRedisKeyMissing(t, store, ctx, StickyKey("user001", "hash-a", "sid-1"))
	assertRedisKeyExists(t, store, ctx, StickyKey("user002", "hash-b", "sid-2"))
}

func assertSetContains(t *testing.T, store *Store, ctx context.Context, key, value string) {
	t.Helper()
	ok, err := store.Client.SIsMember(ctx, key, value).Result()
	if err != nil {
		t.Fatalf("check %s contains %s: %v", key, value, err)
	}
	if !ok {
		t.Fatalf("expected %s to contain %s", key, value)
	}
}

func assertSetMissing(t *testing.T, store *Store, ctx context.Context, key, value string) {
	t.Helper()
	ok, err := store.Client.SIsMember(ctx, key, value).Result()
	if err != nil {
		t.Fatalf("check %s missing %s: %v", key, value, err)
	}
	if ok {
		t.Fatalf("expected %s to not contain %s", key, value)
	}
}

func assertRedisKeyMissing(t *testing.T, store *Store, ctx context.Context, key string) {
	t.Helper()
	exists, err := store.Client.Exists(ctx, key).Result()
	if err != nil {
		t.Fatalf("exists %s: %v", key, err)
	}
	if exists != 0 {
		t.Fatalf("expected key %s to be removed", key)
	}
}

func assertRedisKeyExists(t *testing.T, store *Store, ctx context.Context, key string) {
	t.Helper()
	exists, err := store.Client.Exists(ctx, key).Result()
	if err != nil {
		t.Fatalf("exists %s: %v", key, err)
	}
	if exists != 1 {
		t.Fatalf("expected key %s to exist", key)
	}
}
