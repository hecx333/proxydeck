package quota

import (
	"context"
	"testing"

	"proxydeck/backend/internal/db"
	"proxydeck/backend/internal/model"
	"proxydeck/backend/internal/redisstore"

	"github.com/alicebob/miniredis/v2"
)

func newMiniRedisForQuota(t *testing.T) *miniredis.Miniredis {
	t.Helper()
	mini, err := miniredis.Run()
	if err != nil {
		t.Skipf("miniredis unavailable in this environment: %v", err)
	}
	return mini
}

func TestCleanupUserRuntimeRemovesConnUsageAndStickyKeys(t *testing.T) {
	sqliteDB, err := db.Open(t.TempDir() + "/app.db")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	mini := newMiniRedisForQuota(t)
	defer mini.Close()
	store := redisstore.New(mini.Addr(), "", 0)
	svc := NewService(sqliteDB, store)
	ctx := context.Background()

	user := model.User{UID: "user001", PasswordHash: "x", Enabled: true}
	if err := sqliteDB.Create(&user).Error; err != nil {
		t.Fatalf("create user: %v", err)
	}

	_ = store.Client.Set(ctx, redisstore.ConnKey("user001"), 2, 0).Err()
	_ = store.Client.Set(ctx, redisstore.UsageKey("user001", "upload"), 128, 0).Err()
	_ = store.Client.Set(ctx, redisstore.UsageKey("user001", "download"), 256, 0).Err()
	_ = store.Client.Set(ctx, redisstore.RequestsKey("user001"), 3, 0).Err()
	_ = store.Client.Set(ctx, redisstore.StickyKey("user001", "hash-a", "sid-1"), 11, 0).Err()
	_ = store.Client.Set(ctx, redisstore.StickyKey("user001", "hash-b", "sid-2"), 12, 0).Err()
	_ = store.Client.Set(ctx, redisstore.StickyKey("user002", "hash-c", "sid-3"), 13, 0).Err()

	if err := svc.CleanupUserRuntime(ctx, "user001"); err != nil {
		t.Fatalf("cleanup runtime: %v", err)
	}

	assertRedisKeyMissing(t, store, ctx, redisstore.ConnKey("user001"))
	assertRedisKeyMissing(t, store, ctx, redisstore.UsageKey("user001", "upload"))
	assertRedisKeyMissing(t, store, ctx, redisstore.UsageKey("user001", "download"))
	assertRedisKeyMissing(t, store, ctx, redisstore.RequestsKey("user001"))
	assertRedisKeyMissing(t, store, ctx, redisstore.StickyKey("user001", "hash-a", "sid-1"))
	assertRedisKeyMissing(t, store, ctx, redisstore.StickyKey("user001", "hash-b", "sid-2"))

	if ok, err := store.Client.Exists(ctx, redisstore.StickyKey("user002", "hash-c", "sid-3")).Result(); err != nil || ok != 1 {
		t.Fatalf("other user sticky key should remain, exists=%d err=%v", ok, err)
	}
}

func assertRedisKeyMissing(t *testing.T, store *redisstore.Store, ctx context.Context, key string) {
	t.Helper()
	exists, err := store.Client.Exists(ctx, key).Result()
	if err != nil {
		t.Fatalf("exists %s: %v", key, err)
	}
	if exists != 0 {
		t.Fatalf("expected key %s to be removed", key)
	}
}
