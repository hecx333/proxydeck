package proxy

import (
	"bufio"
	"bytes"
	"context"
	"crypto/tls"
	"encoding/base64"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"proxydeck/backend/internal/auth"
	"proxydeck/backend/internal/metrics"
	"proxydeck/backend/internal/model"
	"proxydeck/backend/internal/quota"
	"proxydeck/backend/internal/selector"
	"proxydeck/backend/internal/stats"
	"proxydeck/backend/internal/upstream"

	"go.uber.org/zap"
	"gorm.io/gorm"
)

type Gateway struct {
	quota     *quota.Service
	selector  *selector.Service
	stats     *stats.Service
	cipher    *auth.Cipher
	metrics   *metrics.Registry
	logger    *zap.Logger
	dialTO    time.Duration
	idleTO    time.Duration
	respHdrTO time.Duration
	connectTO time.Duration
}

type idleDeadlineConn struct {
	net.Conn
	timeout time.Duration
}

func (c *idleDeadlineConn) Read(p []byte) (int, error) {
	c.bumpReadDeadline()
	return c.Conn.Read(p)
}

func (c *idleDeadlineConn) Write(p []byte) (int, error) {
	c.bumpWriteDeadline()
	return c.Conn.Write(p)
}

func (c *idleDeadlineConn) bumpReadDeadline() {
	if c.timeout <= 0 {
		return
	}
	_ = c.Conn.SetReadDeadline(time.Now().Add(c.timeout))
}

func (c *idleDeadlineConn) bumpWriteDeadline() {
	if c.timeout <= 0 {
		return
	}
	_ = c.Conn.SetWriteDeadline(time.Now().Add(c.timeout))
}

type idleTimeoutBody struct {
	io.ReadCloser
	conn    net.Conn
	timeout time.Duration
}

func (b *idleTimeoutBody) Read(p []byte) (int, error) {
	if b.timeout > 0 {
		_ = b.conn.SetReadDeadline(time.Now().Add(b.timeout))
	}
	return b.ReadCloser.Read(p)
}

func NewGateway(quota *quota.Service, selector *selector.Service, stats *stats.Service, cipher *auth.Cipher, metricRegistry *metrics.Registry, logger *zap.Logger, dialTO, idleTO, respHdrTO, connectTO time.Duration) *Gateway {
	return &Gateway{
		quota: quota, selector: selector, stats: stats, cipher: cipher, metrics: metricRegistry, logger: logger,
		dialTO: dialTO, idleTO: idleTO, respHdrTO: respHdrTO, connectTO: connectTO,
	}
}

func (g *Gateway) Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.EqualFold(r.Method, http.MethodConnect) {
			g.handleConnect(w, r)
			return
		}
		g.handleHTTP(w, r)
	})
}

func (g *Gateway) authenticate(r *http.Request) (*model.User, selector.Filters, error) {
	uid, password, ok := parseBasicAuth(r.Header.Get("Proxy-Authorization"))
	if !ok {
		return nil, selector.Filters{}, fmt.Errorf("missing auth")
	}
	realUID, filters := selector.ParseUsername(uid)
	user, err := g.quota.Authenticate(r.Context(), realUID, password)
	if err != nil {
		return nil, filters, err
	}
	return user, filters, g.quota.Acquire(r.Context(), user)
}

func (g *Gateway) handleHTTP(w http.ResponseWriter, r *http.Request) {
	user, filters, err := g.authenticate(r)
	if err != nil {
		if g.metrics != nil && isAuthFailure(err) {
			g.metrics.IncProxyAuthFailures()
		}
		writeProxyAccessError(w, err)
		return
	}
	if g.metrics != nil {
		g.metrics.IncProxyRequests()
	}
	defer g.quota.Release(r.Context(), user.UID)
	node, err := g.selector.Pick(r.Context(), user.UID, filters)
	if err != nil {
		if g.metrics != nil {
			g.metrics.IncProxySelectionFailures()
		}
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	req := r.Clone(r.Context())
	req.RequestURI = ""
	req.Header.Del("Proxy-Authorization")
	if upstream.IsTargetDialProtocol(node.Protocol) {
		targetAddr := req.URL.Host
		if !strings.Contains(targetAddr, ":") {
			targetAddr += ":80"
		}
		targetConn, err := upstream.DialTarget(r.Context(), *node, g.cipher, targetAddr, g.dialTO)
		if err != nil {
			if g.metrics != nil {
				g.metrics.IncProxyUpstreamFailures()
			}
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		defer targetConn.Close()
		if g.idleTO > 0 {
			targetConn = &idleDeadlineConn{Conn: targetConn, timeout: g.idleTO}
		}
		if err := writeOriginRequest(targetConn, req); err != nil {
			if g.metrics != nil {
				g.metrics.IncProxyUpstreamFailures()
			}
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		resp, err := http.ReadResponse(bufio.NewReader(targetConn), req)
		if err != nil {
			if g.metrics != nil {
				g.metrics.IncProxyUpstreamFailures()
			}
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		defer resp.Body.Close()
		copyHeader(w.Header(), resp.Header)
		w.WriteHeader(resp.StatusCode)
		written, _ := io.Copy(w, resp.Body)
		_ = g.stats.RecordTraffic(r.Context(), stats.TrafficRecord{
			UID:      user.UID,
			NodeID:   node.ID,
			Region:   pickRegion(*node),
			Upload:   safeContentLength(r.ContentLength),
			Download: written,
			Requests: 1,
		})
		return
	}
	upstreamConn, err := dialUpstream(r.Context(), *node, g.cipher, g.dialTO, g.connectTO)
	if err != nil {
		if g.metrics != nil {
			g.metrics.IncProxyUpstreamFailures()
		}
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	defer upstreamConn.Close()
	if upstreamAuth, err := upstream.ProxyAuthorization(*node, g.cipher); err == nil && upstreamAuth != "" {
		req.Header.Set("Proxy-Authorization", upstreamAuth)
	}
	_ = upstreamConn.SetDeadline(time.Now().Add(g.respHdrTO))
	writeConn := net.Conn(upstreamConn)
	if g.idleTO > 0 {
		writeConn = &idleDeadlineConn{Conn: upstreamConn, timeout: g.idleTO}
	}
	if err := writeProxyRequest(writeConn, req); err != nil {
		if g.metrics != nil {
			g.metrics.IncProxyUpstreamFailures()
		}
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	resp, err := http.ReadResponse(bufio.NewReader(upstreamConn), req)
	if err != nil {
		if g.metrics != nil {
			g.metrics.IncProxyUpstreamFailures()
		}
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	_ = upstreamConn.SetDeadline(time.Time{})
	defer resp.Body.Close()
	if g.idleTO > 0 {
		resp.Body = &idleTimeoutBody{ReadCloser: resp.Body, conn: upstreamConn, timeout: g.idleTO}
	}
	copyHeader(w.Header(), resp.Header)
	w.WriteHeader(resp.StatusCode)
	written, _ := io.Copy(w, resp.Body)
	_ = g.stats.RecordTraffic(r.Context(), stats.TrafficRecord{
		UID:      user.UID,
		NodeID:   node.ID,
		Region:   pickRegion(*node),
		Upload:   safeContentLength(r.ContentLength),
		Download: written,
		Requests: 1,
	})
}

func (g *Gateway) handleConnect(w http.ResponseWriter, r *http.Request) {
	user, filters, err := g.authenticate(r)
	if err != nil {
		if g.metrics != nil && isAuthFailure(err) {
			g.metrics.IncProxyAuthFailures()
		}
		writeProxyAccessError(w, err)
		return
	}
	if g.metrics != nil {
		g.metrics.IncProxyConnectRequests()
	}
	defer g.quota.Release(r.Context(), user.UID)
	node, err := g.selector.Pick(r.Context(), user.UID, filters)
	if err != nil {
		if g.metrics != nil {
			g.metrics.IncProxySelectionFailures()
		}
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	clientConn, _, err := w.(http.Hijacker).Hijack()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer clientConn.Close()
	if upstream.IsTargetDialProtocol(node.Protocol) {
		targetConn, err := upstream.DialTarget(r.Context(), *node, g.cipher, r.Host, g.dialTO)
		if err != nil {
			if g.metrics != nil {
				g.metrics.IncProxyUpstreamFailures()
			}
			_, _ = clientConn.Write([]byte("HTTP/1.1 502 Bad Gateway\r\n\r\n"))
			return
		}
		if g.metrics != nil {
			g.metrics.IncActiveTunnels()
			defer g.metrics.DecActiveTunnels()
		}
		_, _ = clientConn.Write([]byte("HTTP/1.1 200 Connection Established\r\n\r\n"))
		clientStream := net.Conn(clientConn)
		targetStream := net.Conn(targetConn)
		if g.idleTO > 0 {
			clientStream = &idleDeadlineConn{Conn: clientConn, timeout: g.idleTO}
			targetStream = &idleDeadlineConn{Conn: targetConn, timeout: g.idleTO}
		}
		var upload, download int64
		var wg sync.WaitGroup
		wg.Add(2)
		go func() {
			defer wg.Done()
			upload, _ = io.Copy(targetStream, clientStream)
			_ = targetStream.Close()
		}()
		go func() {
			defer wg.Done()
			download, _ = io.Copy(clientStream, targetStream)
			_ = clientStream.Close()
		}()
		wg.Wait()
		_ = g.stats.RecordTraffic(context.Background(), stats.TrafficRecord{
			UID:      user.UID,
			NodeID:   node.ID,
			Region:   pickRegion(*node),
			Upload:   upload,
			Download: download,
			Requests: 1,
		})
		return
	}
	upstreamConn, err := dialUpstream(r.Context(), *node, g.cipher, g.dialTO, g.connectTO)
	if err != nil {
		if g.metrics != nil {
			g.metrics.IncProxyUpstreamFailures()
		}
		_, _ = clientConn.Write([]byte("HTTP/1.1 502 Bad Gateway\r\n\r\n"))
		return
	}
	targetConn, err := establishTunnel(upstreamConn, *node, g.cipher, r.Host, g.connectTO)
	if err != nil {
		if g.metrics != nil {
			g.metrics.IncProxyUpstreamFailures()
		}
		_, _ = clientConn.Write([]byte("HTTP/1.1 502 Bad Gateway\r\n\r\n"))
		_ = upstreamConn.Close()
		return
	}
	if g.metrics != nil {
		g.metrics.IncActiveTunnels()
		defer g.metrics.DecActiveTunnels()
	}
	_, _ = clientConn.Write([]byte("HTTP/1.1 200 Connection Established\r\n\r\n"))
	clientStream := net.Conn(clientConn)
	targetStream := net.Conn(targetConn)
	if g.idleTO > 0 {
		clientStream = &idleDeadlineConn{Conn: clientConn, timeout: g.idleTO}
		targetStream = &idleDeadlineConn{Conn: targetConn, timeout: g.idleTO}
	}
	var upload, download int64
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		upload, _ = io.Copy(targetStream, clientStream)
		_ = targetStream.Close()
	}()
	go func() {
		defer wg.Done()
		download, _ = io.Copy(clientStream, targetStream)
		_ = clientStream.Close()
	}()
	wg.Wait()
	_ = g.stats.RecordTraffic(context.Background(), stats.TrafficRecord{
		UID:      user.UID,
		NodeID:   node.ID,
		Region:   pickRegion(*node),
		Upload:   upload,
		Download: download,
		Requests: 1,
	})
}

func parseBasicAuth(header string) (string, string, bool) {
	if !strings.HasPrefix(header, "Basic ") {
		return "", "", false
	}
	decoded, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(header, "Basic "))
	if err != nil {
		return "", "", false
	}
	parts := strings.SplitN(string(decoded), ":", 2)
	if len(parts) != 2 {
		return "", "", false
	}
	return parts[0], parts[1], true
}

func dialUpstream(ctx context.Context, node model.ProxyNode, cipher *auth.Cipher, dialTO, connectTO time.Duration) (net.Conn, error) {
	addr := fmt.Sprintf("%s:%d", node.Host, node.Port)
	if node.TLSEnabled {
		serverName := node.ServerName
		if serverName == "" {
			serverName = node.Host
		}
		return tls.DialWithDialer(&net.Dialer{Timeout: dialTO}, "tcp", addr, &tls.Config{ServerName: serverName, MinVersion: tls.VersionTLS12, InsecureSkipVerify: node.TLSSkipVerify})
	}
	_ = connectTO
	return (&net.Dialer{Timeout: dialTO}).DialContext(ctx, "tcp", addr)
}

func establishTunnel(conn net.Conn, node model.ProxyNode, cipher *auth.Cipher, target string, connectTO time.Duration) (net.Conn, error) {
	authValue, _ := upstream.ProxyAuthorization(node, cipher)
	authHeader := ""
	if authValue != "" {
		authHeader = "Proxy-Authorization: " + authValue + "\r\n"
	}
	req := fmt.Sprintf("CONNECT %s HTTP/1.1\r\nHost: %s\r\n%s\r\n", target, target, authHeader)
	_ = conn.SetDeadline(time.Now().Add(connectTO))
	if _, err := conn.Write([]byte(req)); err != nil {
		return nil, err
	}
	resp, err := http.ReadResponse(bufio.NewReader(conn), nil)
	if err != nil {
		return nil, err
	}
	_ = conn.SetDeadline(time.Time{})
	if resp.StatusCode/100 != 2 {
		return nil, fmt.Errorf("upstream connect failed: %s", resp.Status)
	}
	return conn, nil
}

func copyHeader(dst, src http.Header) {
	for key, values := range src {
		for _, value := range values {
			dst.Add(key, value)
		}
	}
}

func writeProxyRequest(conn net.Conn, req *http.Request) error {
	authValue := req.Header.Get("Proxy-Authorization")
	var buf bytes.Buffer
	if err := req.WriteProxy(&buf); err != nil {
		return err
	}
	if authValue != "" && !bytes.Contains(buf.Bytes(), []byte("\r\nProxy-Authorization: ")) {
		raw := buf.Bytes()
		firstLineEnd := bytes.Index(raw, []byte("\r\n"))
		if firstLineEnd < 0 {
			_, err := conn.Write(raw)
			return err
		}
		var patched bytes.Buffer
		patched.Write(raw[:firstLineEnd+2])
		patched.WriteString("Proxy-Authorization: ")
		patched.WriteString(authValue)
		patched.WriteString("\r\n")
		patched.Write(raw[firstLineEnd+2:])
		_, err := conn.Write(patched.Bytes())
		return err
	}
	_, err := conn.Write(buf.Bytes())
	return err
}

func writeOriginRequest(conn net.Conn, req *http.Request) error {
	var buf bytes.Buffer
	if err := req.Write(&buf); err != nil {
		return err
	}
	_, err := conn.Write(buf.Bytes())
	return err
}

func pickRegion(node model.ProxyNode) string {
	if node.DetectedRegion != "" {
		return node.DetectedRegion
	}
	return node.ExpectedRegion
}

func safeContentLength(length int64) int64 {
	if length < 0 {
		return 0
	}
	return length
}

func writeProxyAuthRequired(w http.ResponseWriter, err error) {
	w.Header().Set("Proxy-Authenticate", `Basic realm="ProxyDeck"`)
	http.Error(w, err.Error(), http.StatusProxyAuthRequired)
}

func writeProxyAccessError(w http.ResponseWriter, err error) {
	switch {
	case errorsIsOneOf(err, quota.ErrQuotaExceeded, quota.ErrUserDisabled, quota.ErrUserExpired):
		http.Error(w, err.Error(), http.StatusForbidden)
	case errorsIsOneOf(err, quota.ErrConcurrencyExceeded):
		http.Error(w, err.Error(), http.StatusTooManyRequests)
	default:
		writeProxyAuthRequired(w, err)
	}
}

func isAuthFailure(err error) bool {
	switch {
	case errorsIsOneOf(err, quota.ErrQuotaExceeded, quota.ErrUserDisabled, quota.ErrUserExpired, quota.ErrConcurrencyExceeded):
		return false
	case err == nil:
		return false
	case err == gorm.ErrRecordNotFound:
		return true
	default:
		return true
	}
}

func errorsIsOneOf(err error, targets ...error) bool {
	for _, target := range targets {
		if err == target {
			return true
		}
	}
	return false
}
