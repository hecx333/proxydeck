package adminapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"proxydeck/backend/internal/auth"
	"proxydeck/backend/internal/db"
	"proxydeck/backend/internal/model"
	"proxydeck/backend/internal/quota"
	"proxydeck/backend/internal/redisstore"
	"proxydeck/backend/internal/retry"
	"proxydeck/backend/internal/subscription"

	"github.com/gin-gonic/gin"
)

func TestListSubscriptionsIncludesLastSyncStatus(t *testing.T) {
	gin.SetMode(gin.TestMode)
	sqliteDB, err := db.Open(t.TempDir() + "/app.db")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}

	sub := model.Subscription{
		Name:                "live-sub",
		Type:                "singbox",
		URL:                 "https://example.com/sub.json",
		Enabled:             true,
		SyncIntervalSeconds: 3600,
	}
	if err := sqliteDB.Create(&sub).Error; err != nil {
		t.Fatalf("create subscription: %v", err)
	}
	node := model.ProxyNode{
		NodeKey:        "http://1.1.1.1:80",
		Protocol:       "http",
		Host:           "1.1.1.1",
		Port:           80,
		Healthy:        true,
		SubscriptionID: sub.ID,
	}
	if err := sqliteDB.Create(&node).Error; err != nil {
		t.Fatalf("create node: %v", err)
	}
	if err := sqliteDB.Create(&model.SubscriptionNode{
		SubscriptionID: sub.ID,
		NodeID:         node.ID,
		RawTag:         "raw-tag",
		AliasTag:       "raw-tag",
	}).Error; err != nil {
		t.Fatalf("create subscription node: %v", err)
	}

	now := time.Now().UTC()
	logs := []model.AuditLog{
		{Operator: "admin", Action: "sync_subscription", TargetType: "subscription", TargetID: "1", Detail: "", CreatedAt: now.Add(-time.Minute)},
		{Operator: "admin", Action: "sync_subscription_error", TargetType: "subscription", TargetID: "1", Detail: "bad gateway", CreatedAt: now},
	}
	for _, log := range logs {
		if err := sqliteDB.Create(&log).Error; err != nil {
			t.Fatalf("create audit log: %v", err)
		}
	}

	server := &Server{db: sqliteDB}
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest("GET", "/api/subscriptions", nil)

	server.listSubscriptions(ctx)

	if recorder.Code != 200 {
		t.Fatalf("status = %d, want 200", recorder.Code)
	}

	var payload struct {
		Items []struct {
			ID             uint   `json:"id"`
			NodeCount      int64  `json:"node_count"`
			LastSyncStatus string `json:"last_sync_status"`
			LastSyncDetail string `json:"last_sync_detail"`
		} `json:"items"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(payload.Items) != 1 {
		t.Fatalf("items len = %d, want 1", len(payload.Items))
	}
	item := payload.Items[0]
	if item.NodeCount != 1 {
		t.Fatalf("node_count = %d, want 1", item.NodeCount)
	}
	if item.LastSyncStatus != "error" {
		t.Fatalf("last_sync_status = %q, want error", item.LastSyncStatus)
	}
	if item.LastSyncDetail != "bad gateway" {
		t.Fatalf("last_sync_detail = %q, want bad gateway", item.LastSyncDetail)
	}
}

func TestNodePayloadSanitizesRawJSONCredentials(t *testing.T) {
	payload := nodePayload(model.ProxyNode{
		RawJSON: `{"type":"http","server":"1.1.1.1","server_port":80,"username":"upuser","password":"uppass"}`,
	})
	raw, ok := payload["raw_json"].(map[string]any)
	if !ok {
		t.Fatalf("raw_json type = %T", payload["raw_json"])
	}
	if _, exists := raw["username"]; exists {
		t.Fatal("expected username to be removed from raw_json")
	}
	if _, exists := raw["password"]; exists {
		t.Fatal("expected password to be removed from raw_json")
	}
	if raw["server"] != "1.1.1.1" {
		t.Fatalf("server = %#v", raw["server"])
	}
}

func TestHealthzReturnsOK(t *testing.T) {
	gin.SetMode(gin.TestMode)
	sqliteDB, err := db.Open(t.TempDir() + "/app.db")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}

	server := &Server{db: sqliteDB}
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest("GET", "/healthz", nil)

	server.healthz(ctx)

	if recorder.Code != 200 {
		t.Fatalf("status = %d, want 200", recorder.Code)
	}

	var payload map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if payload["ok"] != true {
		t.Fatalf("ok = %#v, want true", payload["ok"])
	}
}

func TestGetUserReturnsSanitizedPayload(t *testing.T) {
	gin.SetMode(gin.TestMode)
	sqliteDB, err := db.Open(t.TempDir() + "/app.db")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	store := redisstore.New("127.0.0.1:6379", "", 0)
	server := &Server{db: sqliteDB, quota: quota.NewService(sqliteDB, store)}
	user := model.User{
		UID:            "user001",
		PasswordHash:   "hashed-secret",
		Enabled:        true,
		QuotaBytes:     1024,
		UsedBytes:      128,
		TotalRequests:  3,
		MaxConcurrency: 5,
		Remark:         "demo",
	}
	if err := sqliteDB.Create(&user).Error; err != nil {
		t.Fatalf("create user: %v", err)
	}

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest("GET", "/api/users/1", nil)
	ctx.Params = gin.Params{{Key: "id", Value: "1"}}

	server.getUser(ctx)

	if recorder.Code != 200 {
		t.Fatalf("status = %d, want 200", recorder.Code)
	}

	var payload map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if _, exists := payload["password_hash"]; exists {
		t.Fatal("password_hash should not be exposed")
	}
	if payload["uid"] != "user001" {
		t.Fatalf("uid = %#v, want user001", payload["uid"])
	}
	if payload["current_concurrency"] != float64(0) {
		t.Fatalf("current_concurrency = %#v, want 0", payload["current_concurrency"])
	}
}

func TestUserPayloadIncludesCurrentConcurrency(t *testing.T) {
	user := model.User{UID: "user001", Enabled: true}
	got := userPayload(context.Background(), nil, user)
	if got["current_concurrency"] != int64(0) {
		t.Fatalf("current_concurrency = %#v, want 0", got["current_concurrency"])
	}
}

func TestManagedUserByIDHidesAdminUsers(t *testing.T) {
	sqliteDB, err := db.Open(t.TempDir() + "/app.db")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	admin := model.User{
		UID:          model.AdminUIDPrefix + "admin",
		PasswordHash: "secret",
		Enabled:      true,
	}
	if err := sqliteDB.Create(&admin).Error; err != nil {
		t.Fatalf("create admin user: %v", err)
	}
	server := &Server{db: sqliteDB}
	if _, err := server.managedUserByID("1"); err == nil {
		t.Fatal("expected admin user to be hidden")
	}
}

func TestUpdateUserRejectsReservedAdminPrefix(t *testing.T) {
	gin.SetMode(gin.TestMode)
	sqliteDB, err := db.Open(t.TempDir() + "/app.db")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	user := model.User{
		UID:          "user001",
		PasswordHash: "secret",
		Enabled:      true,
	}
	if err := sqliteDB.Create(&user).Error; err != nil {
		t.Fatalf("create user: %v", err)
	}
	server := &Server{db: sqliteDB}
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPut, "/api/users/1", strings.NewReader(`{"uid":"__admin__:oops"}`))
	ctx.Request.Header.Set("Content-Type", "application/json")
	ctx.Params = gin.Params{{Key: "id", Value: "1"}}

	server.updateUser(ctx)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", recorder.Code)
	}
}

func TestUpdateUserRejectsEmptyUIDAndShortPassword(t *testing.T) {
	gin.SetMode(gin.TestMode)
	sqliteDB, err := db.Open(t.TempDir() + "/app.db")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	user := model.User{
		UID:          "user001",
		PasswordHash: "secret",
		Enabled:      true,
	}
	if err := sqliteDB.Create(&user).Error; err != nil {
		t.Fatalf("create user: %v", err)
	}
	server := &Server{db: sqliteDB}

	for _, body := range []string{`{"uid":"   "}`, `{"password":"123"}`, `{"quota_bytes":-1}`, `{"max_concurrency":-1}`} {
		recorder := httptest.NewRecorder()
		ctx, _ := gin.CreateTestContext(recorder)
		ctx.Request = httptest.NewRequest(http.MethodPut, "/api/users/1", strings.NewReader(body))
		ctx.Request.Header.Set("Content-Type", "application/json")
		ctx.Params = gin.Params{{Key: "id", Value: "1"}}

		server.updateUser(ctx)

		if recorder.Code != http.StatusBadRequest {
			t.Fatalf("body %s: status = %d, want 400", body, recorder.Code)
		}
	}
}

func TestCreateSubscriptionRejectsUnsupportedType(t *testing.T) {
	gin.SetMode(gin.TestMode)
	sqliteDB, err := db.Open(t.TempDir() + "/app.db")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	server := &Server{db: sqliteDB}
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/api/subscriptions", strings.NewReader(`{"name":"bad","type":"v2rayn","url":"https://example.com/sub.json"}`))
	ctx.Request.Header.Set("Content-Type", "application/json")

	server.createSubscription(ctx)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", recorder.Code)
	}
}

func TestUpdateSubscriptionRejectsUnsupportedType(t *testing.T) {
	gin.SetMode(gin.TestMode)
	sqliteDB, err := db.Open(t.TempDir() + "/app.db")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	sub := model.Subscription{
		Name: "live-sub",
		Type: "singbox",
		URL:  "https://example.com/sub.json",
	}
	if err := sqliteDB.Create(&sub).Error; err != nil {
		t.Fatalf("create subscription: %v", err)
	}
	server := &Server{db: sqliteDB}
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPut, "/api/subscriptions/1", strings.NewReader(`{"type":"v2rayn"}`))
	ctx.Request.Header.Set("Content-Type", "application/json")
	ctx.Params = gin.Params{{Key: "id", Value: "1"}}

	server.updateSubscription(ctx)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", recorder.Code)
	}
}

func TestUpdateSubscriptionRejectsEmptyNameOrURLAndNormalizesType(t *testing.T) {
	gin.SetMode(gin.TestMode)
	sqliteDB, err := db.Open(t.TempDir() + "/app.db")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	sub := model.Subscription{
		Name: "live-sub",
		Type: "singbox",
		URL:  "https://example.com/sub.json",
	}
	if err := sqliteDB.Create(&sub).Error; err != nil {
		t.Fatalf("create subscription: %v", err)
	}
	server := &Server{db: sqliteDB}

	for _, body := range []string{`{"name":"   "}`, `{"url":"   "}`, `{"sync_interval_seconds":-1}`} {
		recorder := httptest.NewRecorder()
		ctx, _ := gin.CreateTestContext(recorder)
		ctx.Request = httptest.NewRequest(http.MethodPut, "/api/subscriptions/1", strings.NewReader(body))
		ctx.Request.Header.Set("Content-Type", "application/json")
		ctx.Params = gin.Params{{Key: "id", Value: "1"}}

		server.updateSubscription(ctx)

		if recorder.Code != http.StatusBadRequest {
			t.Fatalf("body %s: status = %d, want 400", body, recorder.Code)
		}
	}

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPut, "/api/subscriptions/1", strings.NewReader(`{"type":"SINGBOX"}`))
	ctx.Request.Header.Set("Content-Type", "application/json")
	ctx.Params = gin.Params{{Key: "id", Value: "1"}}

	server.updateSubscription(ctx)

	if recorder.Code != http.StatusOK {
		t.Fatalf("normalize type status = %d, want 200", recorder.Code)
	}
	var updated model.Subscription
	if err := sqliteDB.First(&updated, sub.ID).Error; err != nil {
		t.Fatalf("reload subscription: %v", err)
	}
	if updated.Type != "singbox" {
		t.Fatalf("type = %q, want singbox", updated.Type)
	}
}

func TestSessionCookieMaxAgeUsesConfiguredTTL(t *testing.T) {
	server := &Server{}
	if got := server.sessionCookieMaxAge(); got != 86400 {
		t.Fatalf("default session cookie max age = %d, want 86400", got)
	}

	server.cfg.Security.SessionTTL = 7200
	if got := server.sessionCookieMaxAge(); got != 7200 {
		t.Fatalf("configured session cookie max age = %d, want 7200", got)
	}
}

func TestCreateUserDefaultsEnabledToTrue(t *testing.T) {
	gin.SetMode(gin.TestMode)
	sqliteDB, err := db.Open(t.TempDir() + "/app.db")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	server := &Server{db: sqliteDB}
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/api/users", strings.NewReader(`{"uid":"user001","password":"pass123","quota_bytes":1024}`))
	ctx.Request.Header.Set("Content-Type", "application/json")

	server.createUser(ctx)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", recorder.Code)
	}
	var user model.User
	if err := sqliteDB.Where("uid = ?", "user001").First(&user).Error; err != nil {
		t.Fatalf("load user: %v", err)
	}
	if !user.Enabled {
		t.Fatal("expected enabled to default to true")
	}
}

func TestCreateUserRejectsMissingRequiredFields(t *testing.T) {
	gin.SetMode(gin.TestMode)
	sqliteDB, err := db.Open(t.TempDir() + "/app.db")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	server := &Server{db: sqliteDB}
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/api/users", strings.NewReader(`{"uid":"","password":"","quota_bytes":-1,"max_concurrency":-1}`))
	ctx.Request.Header.Set("Content-Type", "application/json")

	server.createUser(ctx)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", recorder.Code)
	}
}

func TestCreateSubscriptionDefaultsEnabledToTrueAndNormalizesType(t *testing.T) {
	gin.SetMode(gin.TestMode)
	sqliteDB, err := db.Open(t.TempDir() + "/app.db")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	server := &Server{db: sqliteDB}
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/api/subscriptions", strings.NewReader(`{"name":"live-sub","type":"SINGBOX","url":"https://example.com/sub.json"}`))
	ctx.Request.Header.Set("Content-Type", "application/json")

	server.createSubscription(ctx)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", recorder.Code)
	}
	var sub model.Subscription
	if err := sqliteDB.First(&sub, 1).Error; err != nil {
		t.Fatalf("load subscription: %v", err)
	}
	if !sub.Enabled {
		t.Fatal("expected enabled to default to true")
	}
	if sub.Type != "singbox" {
		t.Fatalf("type = %q, want singbox", sub.Type)
	}
}

func TestCreateSubscriptionAcceptsManualType(t *testing.T) {
	gin.SetMode(gin.TestMode)
	sqliteDB, err := db.Open(t.TempDir() + "/app.db")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	server := &Server{db: sqliteDB}
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/api/subscriptions", strings.NewReader(`{"name":"webshare","type":"manual","url":"https://example.com/proxies.txt"}`))
	ctx.Request.Header.Set("Content-Type", "application/json")

	server.createSubscription(ctx)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", recorder.Code)
	}
	var sub model.Subscription
	if err := sqliteDB.First(&sub, 1).Error; err != nil {
		t.Fatalf("load subscription: %v", err)
	}
	if sub.Type != "manual" {
		t.Fatalf("type = %q, want manual", sub.Type)
	}
}

func TestImportNodesCreatesManualHTTPNodes(t *testing.T) {
	gin.SetMode(gin.TestMode)
	sqliteDB, err := db.Open(t.TempDir() + "/app.db")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	store := redisstore.New("127.0.0.1:6379", "", 0)
	cipher, err := auth.NewCipher("test-secret")
	if err != nil {
		t.Fatalf("new cipher: %v", err)
	}
	server := &Server{
		db:           sqliteDB,
		subscription: subscription.NewService(sqliteDB, store, cipher, nil, retry.DefaultConfig(), nil),
	}
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/api/nodes/import", strings.NewReader(`{"protocol":"http","nodes":[{"host":"38.154.203.95","port":5863,"username":"gjpumdzo","password":"fiva3njr8qhu"},{"host":"198.105.121.200","port":6462}]}`))
	ctx.Request.Header.Set("Content-Type", "application/json")
	ctx.Set("admin_username", "admin")

	server.importNodes(ctx)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body=%s", recorder.Code, recorder.Body.String())
	}
	var count int64
	if err := sqliteDB.Model(&model.ProxyNode{}).Count(&count).Error; err != nil {
		t.Fatalf("count nodes: %v", err)
	}
	if count != 2 {
		t.Fatalf("node count = %d, want 2", count)
	}
}

func TestCreateSubscriptionRejectsMissingRequiredFields(t *testing.T) {
	gin.SetMode(gin.TestMode)
	sqliteDB, err := db.Open(t.TempDir() + "/app.db")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	server := &Server{db: sqliteDB}
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/api/subscriptions", strings.NewReader(`{"name":"","url":"","sync_interval_seconds":-1}`))
	ctx.Request.Header.Set("Content-Type", "application/json")

	server.createSubscription(ctx)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", recorder.Code)
	}
}
