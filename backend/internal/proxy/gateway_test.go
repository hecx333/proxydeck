package proxy

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"proxydeck/backend/internal/auth"
	"proxydeck/backend/internal/db"
	"proxydeck/backend/internal/metrics"
	"proxydeck/backend/internal/model"
	"proxydeck/backend/internal/quota"
	"proxydeck/backend/internal/redisstore"
	"proxydeck/backend/internal/selector"
	"proxydeck/backend/internal/stats"

	"github.com/alicebob/miniredis/v2"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

type trackingConn struct {
	readDeadlineSet  int
	writeDeadlineSet int
	readErr          error
	writeErr         error
}

type stubReadCloser struct {
	readErr error
	closed  bool
}

func (r *stubReadCloser) Read(_ []byte) (int, error) {
	if r.readErr != nil {
		return 0, r.readErr
	}
	return 0, io.EOF
}

func (r *stubReadCloser) Close() error {
	r.closed = true
	return nil
}

func (c *trackingConn) Read(_ []byte) (int, error) {
	if c.readErr != nil {
		return 0, c.readErr
	}
	return 0, io.EOF
}

func (c *trackingConn) Write(p []byte) (int, error) {
	if c.writeErr != nil {
		return 0, c.writeErr
	}
	return len(p), nil
}

func (c *trackingConn) Close() error                     { return nil }
func (c *trackingConn) LocalAddr() net.Addr              { return dummyAddr("local") }
func (c *trackingConn) RemoteAddr() net.Addr             { return dummyAddr("remote") }
func (c *trackingConn) SetDeadline(time.Time) error      { return nil }
func (c *trackingConn) SetReadDeadline(time.Time) error  { c.readDeadlineSet++; return nil }
func (c *trackingConn) SetWriteDeadline(time.Time) error { c.writeDeadlineSet++; return nil }

type dummyAddr string

func (a dummyAddr) Network() string { return "tcp" }
func (a dummyAddr) String() string  { return string(a) }

func newTCPListener(t *testing.T) net.Listener {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Skipf("tcp listen unavailable in this environment: %v", err)
	}
	return listener
}

func newHTTPTestServer(t *testing.T, handler http.Handler) *httptest.Server {
	t.Helper()
	listener := newTCPListener(t)
	server := httptest.NewUnstartedServer(handler)
	server.Listener = listener
	server.Start()
	return server
}

func TestParseBasicAuth(t *testing.T) {
	header := "Basic " + base64.StdEncoding.EncodeToString([]byte("user:pass"))
	user, pass, ok := parseBasicAuth(header)
	if !ok || user != "user" || pass != "pass" {
		t.Fatalf("unexpected parse result: %q %q %v", user, pass, ok)
	}
}

func TestParseBasicAuthRejectsInvalid(t *testing.T) {
	if _, _, ok := parseBasicAuth("Bearer abc"); ok {
		t.Fatal("expected invalid header to fail")
	}
}

func TestWriteProxyAuthRequiredAddsChallengeHeader(t *testing.T) {
	recorder := httptest.NewRecorder()
	writeProxyAuthRequired(recorder, errors.New("missing auth"))
	if recorder.Code != http.StatusProxyAuthRequired {
		t.Fatalf("status = %d, want 407", recorder.Code)
	}
	if got := recorder.Header().Get("Proxy-Authenticate"); got != `Basic realm="ProxyDeck"` {
		t.Fatalf("Proxy-Authenticate = %q", got)
	}
}

func TestWriteProxyAccessErrorMapsQuotaAndConcurrency(t *testing.T) {
	tests := []struct {
		name          string
		err           error
		wantStatus    int
		wantChallenge bool
	}{
		{name: "quota", err: quota.ErrQuotaExceeded, wantStatus: http.StatusForbidden},
		{name: "disabled", err: quota.ErrUserDisabled, wantStatus: http.StatusForbidden},
		{name: "expired", err: quota.ErrUserExpired, wantStatus: http.StatusForbidden},
		{name: "concurrency", err: quota.ErrConcurrencyExceeded, wantStatus: http.StatusTooManyRequests},
		{name: "auth", err: errors.New("missing auth"), wantStatus: http.StatusProxyAuthRequired, wantChallenge: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			writeProxyAccessError(recorder, tc.err)
			if recorder.Code != tc.wantStatus {
				t.Fatalf("status = %d, want %d", recorder.Code, tc.wantStatus)
			}
			gotChallenge := recorder.Header().Get("Proxy-Authenticate") != ""
			if gotChallenge != tc.wantChallenge {
				t.Fatalf("challenge present = %v, want %v", gotChallenge, tc.wantChallenge)
			}
		})
	}
}

func TestSafeContentLength(t *testing.T) {
	if got := safeContentLength(-1); got != 0 {
		t.Fatalf("safeContentLength(-1) = %d", got)
	}
	if got := safeContentLength(42); got != 42 {
		t.Fatalf("safeContentLength(42) = %d", got)
	}
}

func TestIdleDeadlineConnRefreshesDeadlines(t *testing.T) {
	base := &trackingConn{}
	conn := &idleDeadlineConn{Conn: base, timeout: time.Second}
	if _, err := conn.Write([]byte("hello")); err != nil {
		t.Fatalf("write: %v", err)
	}
	base.readErr = errors.New("stop")
	if _, err := conn.Read(make([]byte, 1)); err == nil {
		t.Fatal("expected read error")
	}
	if base.writeDeadlineSet == 0 {
		t.Fatal("expected write deadline to be set")
	}
	if base.readDeadlineSet == 0 {
		t.Fatal("expected read deadline to be set")
	}
}

func TestIdleTimeoutBodyRefreshesReadDeadline(t *testing.T) {
	baseConn := &trackingConn{}
	body := &stubReadCloser{readErr: errors.New("stop")}
	wrapped := &idleTimeoutBody{
		ReadCloser: body,
		conn:       baseConn,
		timeout:    time.Second,
	}
	if _, err := wrapped.Read(make([]byte, 1)); err == nil {
		t.Fatal("expected read error")
	}
	if baseConn.readDeadlineSet == 0 {
		t.Fatal("expected body read to refresh read deadline")
	}
	if err := wrapped.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	if !body.closed {
		t.Fatal("expected wrapped body close to propagate")
	}
}

func TestGatewayHTTPProxyFlowAndStats(t *testing.T) {
	upstreamRaw := make(chan string, 1)
	listener := newTCPListener(t)
	defer listener.Close()
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		reader := bufio.NewReader(conn)
		var raw bytes.Buffer
		for {
			line, err := reader.ReadString('\n')
			if err != nil {
				t.Errorf("read proxy request: %v", err)
				return
			}
			raw.WriteString(line)
			if line == "\r\n" {
				break
			}
		}
		upstreamRaw <- raw.String()
		_, _ = io.WriteString(conn, "HTTP/1.1 200 OK\r\nContent-Length: 19\r\n\r\nhello-through-proxy")
	}()

	env := newGatewayTestEnv(t, "http://"+listener.Addr().String(), 2)
	gatewaySrv := newHTTPTestServer(t, env.gateway.Handler())
	defer gatewaySrv.Close()

	resp, body := doProxyRequest(t, gatewaySrv.URL, "user001__region=SG__sid=s1", "pass123", "http://example.com/test")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if body != "hello-through-proxy" {
		t.Fatalf("body = %q", body)
	}
	raw := <-upstreamRaw
	wantAuth := "Proxy-Authorization: Basic " + base64.StdEncoding.EncodeToString([]byte("upuser:uppass"))
	if !strings.Contains(raw, wantAuth) {
		t.Fatalf("upstream raw request missing proxy auth header.\nraw=%s", raw)
	}
	if err := env.stats.FlushUsage(context.Background()); err != nil {
		t.Fatalf("flush usage: %v", err)
	}
	var user model.User
	if err := env.db.Where("uid = ?", "user001").First(&user).Error; err != nil {
		t.Fatalf("reload user: %v", err)
	}
	if user.TotalRequests != 1 {
		t.Fatalf("user total requests = %d, want 1", user.TotalRequests)
	}
	if user.UsedBytes <= 0 {
		t.Fatalf("user used bytes = %d, want > 0", user.UsedBytes)
	}
	var node model.ProxyNode
	if err := env.db.First(&node, env.node.ID).Error; err != nil {
		t.Fatalf("reload node: %v", err)
	}
	if node.TotalRequests != 1 {
		t.Fatalf("node total requests = %d, want 1", node.TotalRequests)
	}
	if node.DownloadBytes <= 0 {
		t.Fatalf("node download bytes = %d, want > 0", node.DownloadBytes)
	}
}

func TestGatewayConcurrencyLimit(t *testing.T) {
	target := newHTTPTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, "ok")
	}))
	defer target.Close()

	started := make(chan struct{}, 1)
	release := make(chan struct{})
	upstream := newHTTPTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case started <- struct{}{}:
		default:
		}
		<-release
		req := r.Clone(r.Context())
		req.RequestURI = ""
		req.Header.Del("Proxy-Authorization")
		resp, err := http.DefaultTransport.RoundTrip(req)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		defer resp.Body.Close()
		w.WriteHeader(resp.StatusCode)
		_, _ = io.Copy(w, resp.Body)
	}))
	defer upstream.Close()

	env := newGatewayTestEnv(t, upstream.URL, 1)
	gatewaySrv := newHTTPTestServer(t, env.gateway.Handler())
	defer gatewaySrv.Close()

	done := make(chan *http.Response, 1)
	go func() {
		resp, _ := doProxyResponse(gatewaySrv.URL, "user001", "pass123", target.URL)
		done <- resp
	}()
	<-started

	secondResp, _ := doProxyResponse(gatewaySrv.URL, "user001", "pass123", target.URL)
	defer secondResp.Body.Close()
	secondStatus := secondResp.StatusCode
	secondChallenge := secondResp.Header.Get("Proxy-Authenticate")
	close(release)
	if secondStatus != http.StatusTooManyRequests {
		t.Fatalf("second status = %d, want 429", secondStatus)
	}
	if secondChallenge != "" {
		t.Fatalf("Proxy-Authenticate = %q, want empty", secondChallenge)
	}
	firstResp := <-done
	if firstResp == nil || firstResp.StatusCode != http.StatusOK {
		t.Fatalf("first response invalid: %#v", firstResp)
	}
	defer firstResp.Body.Close()
}

func TestEstablishTunnelUsesProxyAuth(t *testing.T) {
	cipher, err := auth.NewCipher("test-secret")
	if err != nil {
		t.Fatalf("cipher: %v", err)
	}
	userEnc, _ := cipher.Encrypt("upuser")
	passEnc, _ := cipher.Encrypt("uppass")
	node := model.ProxyNode{
		UpstreamUsernameEnc: userEnc,
		UpstreamPasswordEnc: passEnc,
	}
	listener := newTCPListener(t)
	defer listener.Close()
	done := make(chan string, 1)
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		req, err := http.ReadRequest(bufio.NewReader(conn))
		if err != nil {
			t.Errorf("read request: %v", err)
			return
		}
		done <- req.Header.Get("Proxy-Authorization")
		_, _ = io.WriteString(conn, "HTTP/1.1 200 Connection Established\r\n\r\n")
	}()

	conn, err := net.Dial("tcp", listener.Addr().String())
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()
	tunnel, err := establishTunnel(conn, node, cipher, "example.com:443", time.Second)
	if err != nil {
		t.Fatalf("establish tunnel: %v", err)
	}
	_ = tunnel.Close()
	got := <-done
	want := "Basic " + base64.StdEncoding.EncodeToString([]byte("upuser:uppass"))
	if got != want {
		t.Fatalf("proxy auth = %q, want %q", got, want)
	}
}

type gatewayTestEnv struct {
	db      *gorm.DB
	stats   *stats.Service
	node    model.ProxyNode
	gateway *Gateway
}

func newGatewayTestEnv(t *testing.T, upstreamURL string, maxConcurrency int) gatewayTestEnv {
	t.Helper()
	sqliteDB, err := db.Open(t.TempDir() + "/app.db")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	mini := miniredis.RunT(t)
	store := redisstore.New(mini.Addr(), "", 0)
	cipher, err := auth.NewCipher("test-secret")
	if err != nil {
		t.Fatalf("cipher: %v", err)
	}
	passwordHash, _ := auth.HashPassword("pass123")
	user := model.User{
		UID:            "user001",
		PasswordHash:   passwordHash,
		Enabled:        true,
		QuotaBytes:     1 << 30,
		MaxConcurrency: maxConcurrency,
	}
	if err := sqliteDB.Create(&user).Error; err != nil {
		t.Fatalf("create user: %v", err)
	}
	parsed, err := url.Parse(upstreamURL)
	if err != nil {
		t.Fatalf("parse upstream: %v", err)
	}
	host, portStr, _ := net.SplitHostPort(parsed.Host)
	port, _ := strconv.Atoi(portStr)
	upUser, _ := cipher.Encrypt("upuser")
	upPass, _ := cipher.Encrypt("uppass")
	node := model.ProxyNode{
		NodeKey:             "http://" + parsed.Host,
		Protocol:            "http",
		Host:                host,
		Port:                port,
		UpstreamUsernameEnc: upUser,
		UpstreamPasswordEnc: upPass,
		ExpectedRegion:      "SG",
		DetectedRegion:      "SG",
		Healthy:             true,
	}
	if err := sqliteDB.Create(&node).Error; err != nil {
		t.Fatalf("create node: %v", err)
	}
	if err := store.CacheNode(context.Background(), node); err != nil {
		t.Fatalf("cache node: %v", err)
	}
	quotaSvc := quota.NewService(sqliteDB, store)
	statsSvc := stats.NewService(sqliteDB, store)
	selectorSvc := selector.NewService(sqliteDB, store, time.Hour)
	gateway := NewGateway(quotaSvc, selectorSvc, statsSvc, cipher, metrics.New(), zap.NewNop(), 2*time.Second, 10*time.Second, 2*time.Second, 2*time.Second)
	return gatewayTestEnv{
		db:      sqliteDB,
		stats:   statsSvc,
		node:    node,
		gateway: gateway,
	}
}

func doProxyRequest(t *testing.T, gatewayURL, uid, password, targetURL string) (*http.Response, string) {
	t.Helper()
	resp, err := doProxyResponse(gatewayURL, uid, password, targetURL)
	if err != nil {
		t.Fatalf("proxy request: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	return resp, string(body)
}

func doProxyResponse(gatewayURL, uid, password, targetURL string) (*http.Response, error) {
	proxyURL, err := url.Parse(gatewayURL)
	if err != nil {
		return nil, err
	}
	proxyURL.User = url.UserPassword(uid, password)
	client := &http.Client{
		Transport: &http.Transport{
			Proxy: http.ProxyURL(proxyURL),
		},
	}
	return client.Get(targetURL)
}
