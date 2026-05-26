package subscription

import (
	"context"
	"encoding/base64"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"proxydeck/backend/internal/auth"
	"proxydeck/backend/internal/db"
	"proxydeck/backend/internal/model"
	"proxydeck/backend/internal/redisstore"
	"proxydeck/backend/internal/retry"

	"github.com/alicebob/miniredis/v2"
)

func TestInferRegion(t *testing.T) {
	if got := inferRegion("my-fast-sg-node"); got != "SG" {
		t.Fatalf("inferRegion = %q, want SG", got)
	}
	if got := inferRegion("🇭🇰 香港 Ultra"); got != "HK" {
		t.Fatalf("inferRegion = %q, want HK", got)
	}
	if got := inferRegion("🇺🇸 直连 | 洛杉矶一号"); got != "US" {
		t.Fatalf("inferRegion = %q, want US", got)
	}
	if got := inferRegion("plain-node"); got != "" {
		t.Fatalf("inferRegion = %q, want empty", got)
	}
}

func TestFetchSubscriptionRetriesTransientFailures(t *testing.T) {
	attempts := 0
	svc := &Service{
		client: &http.Client{
			Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
				attempts++
				status := http.StatusOK
				body := `{"outbounds":[]}`
				if attempts < 3 {
					status = http.StatusBadGateway
					body = "temporary"
				}
				return &http.Response{
					StatusCode: status,
					Body:       io.NopCloser(strings.NewReader(body)),
					Header:     make(http.Header),
				}, nil
			}),
		},
		retry: retry.Config{MaxAttempts: 3, BaseBackoff: time.Millisecond},
	}
	body, err := svc.fetchSubscription(t.Context(), "https://example.com/sub.json")
	if err != nil {
		t.Fatalf("fetchSubscription returned error: %v", err)
	}
	if string(body) != `{"outbounds":[]}` {
		t.Fatalf("unexpected body: %s", string(body))
	}
	if attempts != 3 {
		t.Fatalf("attempts = %d, want 3", attempts)
	}
}

func TestSubscriptionDue(t *testing.T) {
	now := time.Now()
	if !subscriptionDue(model.Subscription{Enabled: true, SyncIntervalSeconds: 60}, now) {
		t.Fatal("expected nil last_sync_at subscription to be due")
	}
	lastSync := now.Add(-61 * time.Second)
	if !subscriptionDue(model.Subscription{Enabled: true, SyncIntervalSeconds: 60, LastSyncAt: &lastSync}, now) {
		t.Fatal("expected expired interval to be due")
	}
	recent := now.Add(-30 * time.Second)
	if subscriptionDue(model.Subscription{Enabled: true, SyncIntervalSeconds: 60, LastSyncAt: &recent}, now) {
		t.Fatal("expected recent sync to be not due")
	}
}

func TestSyncDueSubscriptionsRunsEligibleEntries(t *testing.T) {
	sqliteDB, err := db.Open(t.TempDir() + "/app.db")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	cipher, err := auth.NewCipher("test-secret")
	if err != nil {
		t.Fatalf("cipher: %v", err)
	}
	store := redisstore.New("127.0.0.1:6379", "", 0)
	now := time.Now()
	old := now.Add(-2 * time.Hour)
	subs := []model.Subscription{
		{Name: "due", Type: "singbox", URL: "https://example.com/due.json", Enabled: true, SyncIntervalSeconds: 60, LastSyncAt: &old},
	}
	for _, sub := range subs {
		if err := sqliteDB.Create(&sub).Error; err != nil {
			t.Fatalf("create subscription: %v", err)
		}
	}
	requests := 0
	client := &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			requests++
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader(`{"outbounds":[]}`)),
				Header:     make(http.Header),
			}, nil
		}),
	}
	svc := NewService(sqliteDB, store, cipher, client, retry.Config{MaxAttempts: 1, BaseBackoff: time.Millisecond}, nil)
	synced, err := svc.syncDueSubscriptions(context.Background(), now)
	if err != nil {
		t.Fatalf("sync due subscriptions: %v", err)
	}
	if synced != 1 {
		t.Fatalf("synced = %d, want 1", synced)
	}
	if requests != 1 {
		t.Fatalf("requests = %d, want 1", requests)
	}

	var logs []model.AuditLog
	if err := sqliteDB.Where("target_type = ? AND target_id = ?", "subscription", "1").Order("id asc").Find(&logs).Error; err != nil {
		t.Fatalf("load audit logs: %v", err)
	}
	if len(logs) != 1 {
		t.Fatalf("audit logs len = %d, want 1", len(logs))
	}
	if logs[0].Operator != "system" || logs[0].Action != "sync_subscription" {
		t.Fatalf("unexpected audit log: %+v", logs[0])
	}
}

func TestSyncDueSubscriptionsWritesFailureAuditLog(t *testing.T) {
	sqliteDB, err := db.Open(t.TempDir() + "/app.db")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	cipher, err := auth.NewCipher("test-secret")
	if err != nil {
		t.Fatalf("cipher: %v", err)
	}
	store := redisstore.New("127.0.0.1:6379", "", 0)
	now := time.Now().UTC().Truncate(time.Second)
	lastSync := now.Add(-2 * time.Minute)
	sub := model.Subscription{
		Name:                "due",
		Type:                "singbox",
		URL:                 "https://example.com/due.json",
		Enabled:             true,
		SyncIntervalSeconds: 60,
		LastSyncAt:          &lastSync,
	}
	if err := sqliteDB.Create(&sub).Error; err != nil {
		t.Fatalf("create subscription: %v", err)
	}
	client := &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusBadGateway,
				Status:     "502 Bad Gateway",
				Body:       io.NopCloser(strings.NewReader("bad gateway")),
				Header:     make(http.Header),
				Request:    req,
			}, nil
		}),
	}
	svc := NewService(sqliteDB, store, cipher, client, retry.Config{
		MaxAttempts: 1,
		BaseBackoff: time.Millisecond,
	}, nil)

	synced, err := svc.syncDueSubscriptions(context.Background(), now)
	if err != nil {
		t.Fatalf("sync due subscriptions: %v", err)
	}
	if synced != 0 {
		t.Fatalf("synced = %d, want 0", synced)
	}

	var logs []model.AuditLog
	if err := sqliteDB.Where("target_type = ? AND target_id = ?", "subscription", "1").Order("id asc").Find(&logs).Error; err != nil {
		t.Fatalf("load audit logs: %v", err)
	}
	if len(logs) != 1 {
		t.Fatalf("audit logs len = %d, want 1", len(logs))
	}
	if logs[0].Operator != "system" || logs[0].Action != "sync_subscription_error" {
		t.Fatalf("unexpected audit log: %+v", logs[0])
	}
	if logs[0].Detail == "" {
		t.Fatal("expected failure detail to be recorded")
	}
}

func TestSyncReturnsImportedNodeIDs(t *testing.T) {
	sqliteDB, err := db.Open(t.TempDir() + "/app.db")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	cipher, err := auth.NewCipher("test-secret")
	if err != nil {
		t.Fatalf("cipher: %v", err)
	}
	mini := miniredis.RunT(t)
	store := redisstore.New(mini.Addr(), "", 0)
	sub := model.Subscription{
		Name:    "live",
		Type:    "singbox",
		URL:     "https://example.com/sub.json",
		Enabled: true,
	}
	if err := sqliteDB.Create(&sub).Error; err != nil {
		t.Fatalf("create subscription: %v", err)
	}
	client := &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusOK,
				Body: io.NopCloser(strings.NewReader(`{
					"outbounds":[
						{"type":"http","server":"1.1.1.1","server_port":80,"tag":"sg-a","username":"u","password":"p"},
						{"type":"socks","server":"2.2.2.2","server_port":1080,"tag":"socks-a","username":"u2","password":"p2"}
					]
				}`)),
				Header:  make(http.Header),
				Request: req,
			}, nil
		}),
	}
	svc := NewService(sqliteDB, store, cipher, client, retry.Config{MaxAttempts: 1, BaseBackoff: time.Millisecond}, nil)

	result, err := svc.Sync(context.Background(), sub.ID)
	if err != nil {
		t.Fatalf("sync: %v", err)
	}
	if result.ImportedCount != 2 {
		t.Fatalf("imported_count = %d, want 2", result.ImportedCount)
	}
	if len(result.ImportedNodeIDs) != 2 {
		t.Fatalf("imported_node_ids len = %d, want 2", len(result.ImportedNodeIDs))
	}
	var nodes []model.ProxyNode
	if err := sqliteDB.Order("id asc").Find(&nodes).Error; err != nil {
		t.Fatalf("load nodes: %v", err)
	}
	if len(nodes) != 2 {
		t.Fatalf("nodes len = %d, want 2", len(nodes))
	}
	if nodes[0].Protocol != "http" || nodes[1].Protocol != "socks" {
		t.Fatalf("unexpected protocols: %+v", nodes)
	}
}

func TestSyncParsesShadowrocketSubscription(t *testing.T) {
	sqliteDB, err := db.Open(t.TempDir() + "/app.db")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	cipher, err := auth.NewCipher("test-secret")
	if err != nil {
		t.Fatalf("cipher: %v", err)
	}
	mini := miniredis.RunT(t)
	store := redisstore.New(mini.Addr(), "", 0)
	sub := model.Subscription{
		Name:    "shadowrocket",
		Type:    "shadowrocket",
		URL:     "https://example.com/sub.txt",
		Enabled: true,
	}
	if err := sqliteDB.Create(&sub).Error; err != nil {
		t.Fatalf("create subscription: %v", err)
	}
	httpAuth := base64.StdEncoding.EncodeToString([]byte("alice:secret"))
	lines := []string{
		base64.StdEncoding.EncodeToString([]byte("https://" + httpAuth + "@sg.example.com:443/#%F0%9F%87%B8%F0%9F%87%AC%20SG%20Premium")),
		base64.StdEncoding.EncodeToString([]byte("socks5://bob:pass@us.example.com:1080/#US%20SOCKS")),
	}
	client := &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader(strings.Join(lines, "\n"))),
				Header:     make(http.Header),
				Request:    req,
			}, nil
		}),
	}
	svc := NewService(sqliteDB, store, cipher, client, retry.Config{MaxAttempts: 1, BaseBackoff: time.Millisecond}, nil)

	result, err := svc.Sync(context.Background(), sub.ID)
	if err != nil {
		t.Fatalf("sync: %v", err)
	}
	if result.ImportedCount != 2 {
		t.Fatalf("imported_count = %d, want 2", result.ImportedCount)
	}

	var nodes []model.ProxyNode
	if err := sqliteDB.Order("port asc").Find(&nodes).Error; err != nil {
		t.Fatalf("load nodes: %v", err)
	}
	if len(nodes) != 2 {
		t.Fatalf("nodes len = %d, want 2", len(nodes))
	}
	if nodes[0].Protocol != "http" || nodes[1].Protocol != "socks" {
		t.Fatalf("unexpected protocols: %+v", nodes)
	}
	if nodes[0].Tag != "🇸🇬 SG Premium" {
		t.Fatalf("unexpected shadowrocket tag: %q", nodes[0].Tag)
	}
	if user, err := cipher.Decrypt(nodes[0].UpstreamUsernameEnc); err != nil || user != "alice" {
		t.Fatalf("unexpected decrypted username: %q, err=%v", user, err)
	}
	if pass, err := cipher.Decrypt(nodes[0].UpstreamPasswordEnc); err != nil || pass != "secret" {
		t.Fatalf("unexpected decrypted password: %q, err=%v", pass, err)
	}
}

func TestSyncParsesClashSubscription(t *testing.T) {
	sqliteDB, err := db.Open(t.TempDir() + "/app.db")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	cipher, err := auth.NewCipher("test-secret")
	if err != nil {
		t.Fatalf("cipher: %v", err)
	}
	mini := miniredis.RunT(t)
	store := redisstore.New(mini.Addr(), "", 0)
	sub := model.Subscription{
		Name:    "clash",
		Type:    "clash",
		URL:     "https://example.com/sub.yaml",
		Enabled: true,
	}
	if err := sqliteDB.Create(&sub).Error; err != nil {
		t.Fatalf("create subscription: %v", err)
	}
	body := `
proxies:
  - name: "🇸🇬 SG Premium"
    type: http
    server: sg.example.com
    port: 443
    username: alice
    password: secret
    tls: true
    servername: edge.sg.example.com
  - name: "US SOCKS"
    type: socks5
    server: us.example.com
    port: "1080"
    username: bob
    password: pass
`
	client := &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader(body)),
				Header:     make(http.Header),
				Request:    req,
			}, nil
		}),
	}
	svc := NewService(sqliteDB, store, cipher, client, retry.Config{MaxAttempts: 1, BaseBackoff: time.Millisecond}, nil)

	result, err := svc.Sync(context.Background(), sub.ID)
	if err != nil {
		t.Fatalf("sync: %v", err)
	}
	if result.ImportedCount != 2 {
		t.Fatalf("imported_count = %d, want 2", result.ImportedCount)
	}

	var nodes []model.ProxyNode
	if err := sqliteDB.Order("port asc").Find(&nodes).Error; err != nil {
		t.Fatalf("load nodes: %v", err)
	}
	if len(nodes) != 2 {
		t.Fatalf("nodes len = %d, want 2", len(nodes))
	}
	if nodes[0].Protocol != "http" || nodes[1].Protocol != "socks5" {
		t.Fatalf("unexpected protocols: %+v", nodes)
	}
	if !nodes[0].TLSEnabled || nodes[0].ServerName != "edge.sg.example.com" {
		t.Fatalf("unexpected tls fields: %+v", nodes[0])
	}
	if user, err := cipher.Decrypt(nodes[1].UpstreamUsernameEnc); err != nil || user != "bob" {
		t.Fatalf("unexpected decrypted username: %q, err=%v", user, err)
	}
}

func TestSyncParsesSurgeSubscription(t *testing.T) {
	sqliteDB, err := db.Open(t.TempDir() + "/app.db")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	cipher, err := auth.NewCipher("test-secret")
	if err != nil {
		t.Fatalf("cipher: %v", err)
	}
	mini := miniredis.RunT(t)
	store := redisstore.New(mini.Addr(), "", 0)
	sub := model.Subscription{Name: "surge", Type: "surge", URL: "https://example.com/sub.conf", Enabled: true}
	if err := sqliteDB.Create(&sub).Error; err != nil {
		t.Fatalf("create subscription: %v", err)
	}
	body := `
#!MANAGED-CONFIG https://example.com/sub interval=864000 strict=false
[General]
skip-proxy=127.0.0.1
[Proxy]
🇸🇬 SG Premium=https, sg.example.com, 443, alice, secret, skip-cert-verify=true
US SOCKS=socks5, us.example.com, 1080, bob, pass
`
	client := &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader(body)),
				Header:     make(http.Header),
				Request:    req,
			}, nil
		}),
	}
	svc := NewService(sqliteDB, store, cipher, client, retry.Config{MaxAttempts: 1, BaseBackoff: time.Millisecond}, nil)

	result, err := svc.Sync(context.Background(), sub.ID)
	if err != nil {
		t.Fatalf("sync: %v", err)
	}
	if result.ImportedCount != 2 {
		t.Fatalf("imported_count = %d, want 2", result.ImportedCount)
	}
	var nodes []model.ProxyNode
	if err := sqliteDB.Order("port asc").Find(&nodes).Error; err != nil {
		t.Fatalf("load nodes: %v", err)
	}
	if len(nodes) != 2 {
		t.Fatalf("nodes len = %d, want 2", len(nodes))
	}
	if nodes[0].Protocol != "http" || nodes[0].TLSEnabled != true {
		t.Fatalf("unexpected first node: %+v", nodes[0])
	}
	if nodes[1].Protocol != "socks" {
		t.Fatalf("unexpected second node: %+v", nodes[1])
	}
}

func TestSyncParsesQuantumultXSubscription(t *testing.T) {
	sqliteDB, err := db.Open(t.TempDir() + "/app.db")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	cipher, err := auth.NewCipher("test-secret")
	if err != nil {
		t.Fatalf("cipher: %v", err)
	}
	mini := miniredis.RunT(t)
	store := redisstore.New(mini.Addr(), "", 0)
	sub := model.Subscription{Name: "qtx", Type: "quantumultx", URL: "https://example.com/sub.conf", Enabled: true}
	if err := sqliteDB.Create(&sub).Error; err != nil {
		t.Fatalf("create subscription: %v", err)
	}
	body := strings.Join([]string{
		"http=sg.example.com:443, username=alice, password=secret, over-tls=true, tls-host=edge.sg.example.com, tag=🇸🇬 SG Premium",
		"socks5=us.example.com:1080, username=bob, password=pass, tag=US SOCKS",
	}, "\n")
	client := &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader(body)),
				Header:     make(http.Header),
				Request:    req,
			}, nil
		}),
	}
	svc := NewService(sqliteDB, store, cipher, client, retry.Config{MaxAttempts: 1, BaseBackoff: time.Millisecond}, nil)

	result, err := svc.Sync(context.Background(), sub.ID)
	if err != nil {
		t.Fatalf("sync: %v", err)
	}
	if result.ImportedCount != 2 {
		t.Fatalf("imported_count = %d, want 2", result.ImportedCount)
	}
	var nodes []model.ProxyNode
	if err := sqliteDB.Order("port asc").Find(&nodes).Error; err != nil {
		t.Fatalf("load nodes: %v", err)
	}
	if len(nodes) != 2 {
		t.Fatalf("nodes len = %d, want 2", len(nodes))
	}
	if nodes[0].ServerName != "edge.sg.example.com" || !nodes[0].TLSEnabled {
		t.Fatalf("unexpected first node tls fields: %+v", nodes[0])
	}
	if nodes[1].Protocol != "socks" {
		t.Fatalf("unexpected second node: %+v", nodes[1])
	}
}

func TestSyncParsesShadowrocketShadowsocksSubscription(t *testing.T) {
	sqliteDB, err := db.Open(t.TempDir() + "/app.db")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	cipher, err := auth.NewCipher("test-secret")
	if err != nil {
		t.Fatalf("cipher: %v", err)
	}
	mini := miniredis.RunT(t)
	store := redisstore.New(mini.Addr(), "", 0)
	sub := model.Subscription{Name: "ss", Type: "shadowrocket", URL: "https://example.com/sub.txt", Enabled: true}
	if err := sqliteDB.Create(&sub).Error; err != nil {
		t.Fatalf("create subscription: %v", err)
	}
	ssUserInfo := base64.RawURLEncoding.EncodeToString([]byte("aes-256-gcm:secret"))
	lines := []string{
		base64.StdEncoding.EncodeToString([]byte("ss://" + ssUserInfo + "@ss.example.com:8388/#SS%20Node")),
	}
	client := &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader(strings.Join(lines, "\n"))),
				Header:     make(http.Header),
				Request:    req,
			}, nil
		}),
	}
	svc := NewService(sqliteDB, store, cipher, client, retry.Config{MaxAttempts: 1, BaseBackoff: time.Millisecond}, nil)

	result, err := svc.Sync(context.Background(), sub.ID)
	if err != nil {
		t.Fatalf("sync: %v", err)
	}
	if result.ImportedCount != 1 {
		t.Fatalf("imported_count = %d, want 1", result.ImportedCount)
	}
	var node model.ProxyNode
	if err := sqliteDB.First(&node).Error; err != nil {
		t.Fatalf("load node: %v", err)
	}
	if node.Protocol != "ss" {
		t.Fatalf("protocol = %q, want ss", node.Protocol)
	}
	if method, err := cipher.Decrypt(node.UpstreamUsernameEnc); err != nil || method != "aes-256-gcm" {
		t.Fatalf("unexpected shadowsocks method: %q, err=%v", method, err)
	}
	if secret, err := cipher.Decrypt(node.UpstreamPasswordEnc); err != nil || secret != "secret" {
		t.Fatalf("unexpected shadowsocks password: %q, err=%v", secret, err)
	}
}

func TestSyncParsesShadowrocketTrojanSubscription(t *testing.T) {
	sqliteDB, err := db.Open(t.TempDir() + "/app.db")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	cipher, err := auth.NewCipher("test-secret")
	if err != nil {
		t.Fatalf("cipher: %v", err)
	}
	mini := miniredis.RunT(t)
	store := redisstore.New(mini.Addr(), "", 0)
	sub := model.Subscription{Name: "trojan", Type: "shadowrocket", URL: "https://example.com/sub.txt", Enabled: true}
	if err := sqliteDB.Create(&sub).Error; err != nil {
		t.Fatalf("create subscription: %v", err)
	}
	lines := []string{
		base64.StdEncoding.EncodeToString([]byte("trojan://super-secret@tr.example.com:443?peer=edge.example.com&allowInsecure=1#TR%20Node")),
	}
	client := &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader(strings.Join(lines, "\n"))),
				Header:     make(http.Header),
				Request:    req,
			}, nil
		}),
	}
	svc := NewService(sqliteDB, store, cipher, client, retry.Config{MaxAttempts: 1, BaseBackoff: time.Millisecond}, nil)

	result, err := svc.Sync(context.Background(), sub.ID)
	if err != nil {
		t.Fatalf("sync: %v", err)
	}
	if result.ImportedCount != 1 {
		t.Fatalf("imported_count = %d, want 1", result.ImportedCount)
	}
	var node model.ProxyNode
	if err := sqliteDB.First(&node).Error; err != nil {
		t.Fatalf("load node: %v", err)
	}
	if node.Protocol != "trojan" || !node.TLSEnabled || !node.TLSSkipVerify {
		t.Fatalf("unexpected trojan node: %+v", node)
	}
	if node.ServerName != "edge.example.com" {
		t.Fatalf("server_name = %q, want edge.example.com", node.ServerName)
	}
	if secret, err := cipher.Decrypt(node.UpstreamPasswordEnc); err != nil || secret != "super-secret" {
		t.Fatalf("unexpected trojan password: %q, err=%v", secret, err)
	}
}

func TestSyncParsesShadowrocketVLESSSubscription(t *testing.T) {
	sqliteDB, err := db.Open(t.TempDir() + "/app.db")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	cipher, err := auth.NewCipher("test-secret")
	if err != nil {
		t.Fatalf("cipher: %v", err)
	}
	mini := miniredis.RunT(t)
	store := redisstore.New(mini.Addr(), "", 0)
	sub := model.Subscription{Name: "vless", Type: "shadowrocket", URL: "https://example.com/sub.txt", Enabled: true}
	if err := sqliteDB.Create(&sub).Error; err != nil {
		t.Fatalf("create subscription: %v", err)
	}
	lines := []string{
		base64.StdEncoding.EncodeToString([]byte("vless://123e4567-e89b-12d3-a456-426614174000@vl.example.com:443?security=tls&sni=edge.example.com&allowInsecure=1#VL%20Node")),
		base64.StdEncoding.EncodeToString([]byte("vless://123e4567-e89b-12d3-a456-426614174000@ws.example.com:443?type=ws&security=tls#Skip%20Node")),
	}
	client := &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader(strings.Join(lines, "\n"))),
				Header:     make(http.Header),
				Request:    req,
			}, nil
		}),
	}
	svc := NewService(sqliteDB, store, cipher, client, retry.Config{MaxAttempts: 1, BaseBackoff: time.Millisecond}, nil)

	result, err := svc.Sync(context.Background(), sub.ID)
	if err != nil {
		t.Fatalf("sync: %v", err)
	}
	if result.ImportedCount != 1 {
		t.Fatalf("imported_count = %d, want 1", result.ImportedCount)
	}
	var node model.ProxyNode
	if err := sqliteDB.First(&node).Error; err != nil {
		t.Fatalf("load node: %v", err)
	}
	if node.Protocol != "vless" || !node.TLSEnabled || !node.TLSSkipVerify {
		t.Fatalf("unexpected vless node: %+v", node)
	}
	if node.ServerName != "edge.example.com" {
		t.Fatalf("server_name = %q, want edge.example.com", node.ServerName)
	}
	if userID, err := cipher.Decrypt(node.UpstreamUsernameEnc); err != nil || userID != "123e4567-e89b-12d3-a456-426614174000" {
		t.Fatalf("unexpected vless uuid: %q, err=%v", userID, err)
	}
}

func TestSyncParsesShadowrocketVMessSubscription(t *testing.T) {
	sqliteDB, err := db.Open(t.TempDir() + "/app.db")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	cipher, err := auth.NewCipher("test-secret")
	if err != nil {
		t.Fatalf("cipher: %v", err)
	}
	mini := miniredis.RunT(t)
	store := redisstore.New(mini.Addr(), "", 0)
	sub := model.Subscription{Name: "vmess", Type: "shadowrocket", URL: "https://example.com/sub.txt", Enabled: true}
	if err := sqliteDB.Create(&sub).Error; err != nil {
		t.Fatalf("create subscription: %v", err)
	}
	vmessJSON := `{"v":"2","ps":"VM Node","add":"vm.example.com","port":"443","id":"123e4567-e89b-12d3-a456-426614174000","net":"tcp","tls":"tls","sni":"edge.example.com"}`
	lines := []string{
		base64.StdEncoding.EncodeToString([]byte("vmess://" + base64.StdEncoding.EncodeToString([]byte(vmessJSON)))),
	}
	client := &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader(strings.Join(lines, "\n"))),
				Header:     make(http.Header),
				Request:    req,
			}, nil
		}),
	}
	svc := NewService(sqliteDB, store, cipher, client, retry.Config{MaxAttempts: 1, BaseBackoff: time.Millisecond}, nil)

	result, err := svc.Sync(context.Background(), sub.ID)
	if err != nil {
		t.Fatalf("sync: %v", err)
	}
	if result.ImportedCount != 1 {
		t.Fatalf("imported_count = %d, want 1", result.ImportedCount)
	}
	var node model.ProxyNode
	if err := sqliteDB.First(&node).Error; err != nil {
		t.Fatalf("load node: %v", err)
	}
	if node.Protocol != "vmess" || !node.TLSEnabled {
		t.Fatalf("unexpected vmess node: %+v", node)
	}
	if node.ServerName != "edge.example.com" {
		t.Fatalf("server_name = %q, want edge.example.com", node.ServerName)
	}
	if userID, err := cipher.Decrypt(node.UpstreamUsernameEnc); err != nil || userID != "123e4567-e89b-12d3-a456-426614174000" {
		t.Fatalf("unexpected vmess uuid: %q, err=%v", userID, err)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return fn(req)
}
