package healthcheck

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"

	"proxydeck/backend/internal/auth"
	"proxydeck/backend/internal/metrics"
	"proxydeck/backend/internal/model"
	"proxydeck/backend/internal/redisstore"
	"proxydeck/backend/internal/retry"
	"proxydeck/backend/internal/upstream"

	"go.uber.org/zap"
	"gorm.io/gorm"
)

type Service struct {
	db                    *gorm.DB
	store                 *redisstore.Store
	cipher                *auth.Cipher
	timeout               time.Duration
	maxFailCount          int
	retry                 retry.Config
	metrics               *metrics.Registry
	logger                *zap.Logger
	probeSourcesOverride  []probeSource
	enrichSourcesOverride []enrichSource
}

type Summary struct {
	Total      int `json:"total"`
	Healthy    int `json:"healthy"`
	Unhealthy  int `json:"unhealthy"`
	Failed     int `json:"failed"`
	CheckedIDs []uint
}

type probeSource struct {
	name  string
	url   string
	parse func([]byte) (probeInfo, error)
}

type enrichSource struct {
	name  string
	url   func(string) string
	parse func([]byte) (probeInfo, error)
}

type probeInfo struct {
	IP      string
	City    string
	Country string
	Region  string
	Org     string
}

func NewService(db *gorm.DB, store *redisstore.Store, cipher *auth.Cipher, timeout time.Duration, maxFailCount int, retryCfg retry.Config, metricRegistry *metrics.Registry, logger *zap.Logger) *Service {
	return &Service{db: db, store: store, cipher: cipher, timeout: timeout, maxFailCount: maxFailCount, retry: retryCfg, metrics: metricRegistry, logger: logger}
}

func (s *Service) Start(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			_ = s.CheckAll(ctx)
		}
	}
}

func (s *Service) CheckAll(ctx context.Context) error {
	var nodes []model.ProxyNode
	if err := s.db.Find(&nodes).Error; err != nil {
		return err
	}
	for _, node := range nodes {
		_ = s.CheckOne(ctx, node.ID)
	}
	return nil
}

func (s *Service) CheckNodes(ctx context.Context, nodeIDs []uint) Summary {
	summary := Summary{Total: len(nodeIDs), CheckedIDs: append([]uint(nil), nodeIDs...)}
	for _, nodeID := range nodeIDs {
		if err := s.CheckOne(ctx, nodeID); err != nil {
			summary.Failed++
		}
		var node model.ProxyNode
		if err := s.db.Select("healthy").First(&node, nodeID).Error; err != nil {
			summary.Unhealthy++
			continue
		}
		if node.Healthy {
			summary.Healthy++
			continue
		}
		summary.Unhealthy++
	}
	return summary
}

func (s *Service) CheckOne(ctx context.Context, nodeID uint) error {
	var node model.ProxyNode
	if err := s.db.First(&node, nodeID).Error; err != nil {
		return err
	}
	transport, err := s.transportForNode(node)
	if err != nil {
		return err
	}
	client := &http.Client{Timeout: s.timeout, Transport: transport}
	directClient := &http.Client{
		Timeout: s.timeout,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12},
		},
	}
	start := time.Now()
	info, err := s.fetchExitIP(ctx, client, node)
	now := time.Now()
	if err != nil {
		if s.metrics != nil {
			s.metrics.IncHealthcheckFailure()
		}
		return s.markFailure(ctx, &node, now, err)
	}
	if strings.TrimSpace(info.IP) == "" {
		if s.metrics != nil {
			s.metrics.IncHealthcheckFailure()
		}
		return s.markFailure(ctx, &node, now, errors.New("probe response missing ip"))
	}
	if enrichInfo, enrichErr := s.enrichIPInfo(ctx, directClient, info.IP); enrichErr == nil {
		info = mergeProbeInfo(info, enrichInfo)
	} else if s.logger != nil {
		s.logger.Debug("healthcheck enrich failed", zap.Uint("node_id", node.ID), zap.String("ip", info.IP), zap.Error(enrichErr))
	}
	node.ExitIP = info.IP
	node.City = strings.TrimSpace(info.City)
	node.Country = strings.TrimSpace(info.Country)
	node.DetectedRegion = strings.TrimSpace(info.Region)
	node.ASN = parseASN(info.Org)
	node.Org = strings.TrimSpace(info.Org)
	node.ISP = parseISP(info.Org)
	node.FailCount = 0
	node.Healthy = true
	node.LatencyMS = time.Since(start).Milliseconds()
	node.LastCheckAt = &now
	if err := s.db.Save(&node).Error; err != nil {
		return err
	}
	if s.metrics != nil {
		s.metrics.IncHealthcheckSuccess()
	}
	return s.store.CacheNode(ctx, node)
}

func (s *Service) transportForNode(node model.ProxyNode) (*http.Transport, error) {
	transport := &http.Transport{
		TLSClientConfig: &tls.Config{
			MinVersion:         tls.VersionTLS12,
			InsecureSkipVerify: node.TLSSkipVerify,
		},
	}
	switch {
	case upstream.IsHTTPProxyProtocol(node.Protocol):
		proxyURL, err := upstream.URLForNode(node, s.cipher)
		if err != nil {
			return nil, err
		}
		transport.Proxy = http.ProxyURL(proxyURL)
	case upstream.IsTargetDialProtocol(node.Protocol):
		transport.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
			return upstream.DialTarget(ctx, node, s.cipher, addr, s.timeout)
		}
	default:
		return nil, fmt.Errorf("unsupported healthcheck protocol: %s", node.Protocol)
	}
	return transport, nil
}

func (s *Service) fetchExitIP(ctx context.Context, client *http.Client, node model.ProxyNode) (probeInfo, error) {
	var errs []string
	for _, source := range s.probeSources() {
		info, err := s.fetchFromSource(ctx, client, source.url, source.parse, func(req *http.Request) {
			if upstream.IsHTTPProxyProtocol(node.Protocol) {
				if authValue, err := upstream.ProxyAuthorization(node, s.cipher); err == nil && authValue != "" {
					req.Header.Set("Proxy-Authorization", authValue)
				}
			}
		})
		if err == nil && strings.TrimSpace(info.IP) != "" {
			return info, nil
		}
		if err == nil {
			err = errors.New("missing ip")
		}
		errs = append(errs, fmt.Sprintf("%s: %v", source.name, err))
	}
	return probeInfo{}, errors.New(strings.Join(errs, "; "))
}

func (s *Service) enrichIPInfo(ctx context.Context, client *http.Client, ip string) (probeInfo, error) {
	var errs []string
	for _, source := range s.enrichSources() {
		info, err := s.fetchFromSource(ctx, client, source.url(ip), source.parse, nil)
		if err == nil && hasProfileInfo(info) {
			return info, nil
		}
		if err == nil {
			err = errors.New("missing profile data")
		}
		errs = append(errs, fmt.Sprintf("%s: %v", source.name, err))
	}
	return probeInfo{}, errors.New(strings.Join(errs, "; "))
}

func (s *Service) fetchFromSource(ctx context.Context, client *http.Client, requestURL string, parse func([]byte) (probeInfo, error), decorate func(*http.Request)) (probeInfo, error) {
	var info probeInfo
	err := retry.Do(ctx, s.retry, func(callCtx context.Context) error {
		req, _ := http.NewRequestWithContext(callCtx, http.MethodGet, requestURL, nil)
		req.Header.Set("User-Agent", "ProxyDeck-Healthcheck/1.0")
		req.Header.Set("Accept", "application/json, text/plain;q=0.9, */*;q=0.8")
		if decorate != nil {
			decorate(req)
		}
		resp, err := client.Do(req)
		if err != nil {
			return err
		}
		defer resp.Body.Close()
		if resp.StatusCode >= http.StatusInternalServerError {
			return errors.New(resp.Status)
		}
		if resp.StatusCode != http.StatusOK {
			return retry.StopError{Err: errors.New(resp.Status)}
		}
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return err
		}
		parsed, err := parse(body)
		if err != nil {
			return retry.StopError{Err: err}
		}
		info = parsed
		return nil
	})
	return info, err
}

func (s *Service) probeSources() []probeSource {
	if len(s.probeSourcesOverride) > 0 {
		return s.probeSourcesOverride
	}
	return []probeSource{
		{name: "ipify", url: "https://api.ipify.org?format=json", parse: parseIPifyProbe},
		{name: "ifconfig", url: "https://ifconfig.me/ip", parse: parsePlainTextIPProbe},
		{name: "ipinfo", url: "https://ipinfo.io/json", parse: parseIPInfoProbe},
	}
}

func (s *Service) enrichSources() []enrichSource {
	if len(s.enrichSourcesOverride) > 0 {
		return s.enrichSourcesOverride
	}
	return []enrichSource{
		{name: "ipinfo", url: func(ip string) string { return "https://ipinfo.io/" + ip + "/json" }, parse: parseIPInfoProbe},
		{name: "ipwho", url: func(ip string) string { return "https://ipwho.is/" + ip }, parse: parseIPWhoProbe},
		{name: "ipapi", url: func(ip string) string { return "https://ipapi.co/" + ip + "/json/" }, parse: parseIPAPICoProbe},
	}
}

func (s *Service) markFailure(ctx context.Context, node *model.ProxyNode, now time.Time, err error) error {
	node.FailCount++
	node.Healthy = node.Healthy && node.FailCount < s.maxFailCount
	node.LastCheckAt = &now
	_ = s.db.Save(node).Error
	_ = s.store.CacheNode(ctx, *node)
	return err
}

func parseASN(org string) string {
	parts := strings.Fields(org)
	if len(parts) > 0 && strings.HasPrefix(strings.ToUpper(parts[0]), "AS") {
		return strings.ToUpper(parts[0])
	}
	return ""
}

func parseISP(org string) string {
	parts := strings.Fields(org)
	if len(parts) <= 1 {
		return org
	}
	return strings.Join(parts[1:], " ")
}

func parseIPInfoProbe(body []byte) (probeInfo, error) {
	payload := struct {
		IP      string `json:"ip"`
		City    string `json:"city"`
		Country string `json:"country"`
		Region  string `json:"region"`
		Org     string `json:"org"`
	}{}
	if err := json.Unmarshal(body, &payload); err != nil {
		return probeInfo{}, err
	}
	return probeInfo{
		IP:      strings.TrimSpace(payload.IP),
		City:    strings.TrimSpace(payload.City),
		Country: strings.TrimSpace(payload.Country),
		Region:  strings.TrimSpace(payload.Region),
		Org:     strings.TrimSpace(payload.Org),
	}, nil
}

func parseIPifyProbe(body []byte) (probeInfo, error) {
	payload := struct {
		IP string `json:"ip"`
	}{}
	if err := json.Unmarshal(body, &payload); err != nil {
		return probeInfo{}, err
	}
	return probeInfo{IP: strings.TrimSpace(payload.IP)}, nil
}

func parsePlainTextIPProbe(body []byte) (probeInfo, error) {
	ip := strings.TrimSpace(string(body))
	if ip == "" {
		return probeInfo{}, errors.New("plain text probe missing ip")
	}
	return probeInfo{IP: ip}, nil
}

func parseIPWhoProbe(body []byte) (probeInfo, error) {
	payload := struct {
		IP         string `json:"ip"`
		City       string `json:"city"`
		Country    string `json:"country_code"`
		Region     string `json:"region"`
		Success    *bool  `json:"success"`
		Connection struct {
			ISP string `json:"isp"`
			Org string `json:"org"`
			ASN string `json:"asn"`
		} `json:"connection"`
	}{}
	if err := json.Unmarshal(body, &payload); err != nil {
		return probeInfo{}, err
	}
	if payload.Success != nil && !*payload.Success {
		return probeInfo{}, errors.New("ipwho unsuccessful response")
	}
	org := strings.TrimSpace(payload.Connection.Org)
	if org == "" {
		org = strings.TrimSpace(payload.Connection.ISP)
	}
	if asn := strings.TrimSpace(payload.Connection.ASN); asn != "" && org != "" && !strings.HasPrefix(strings.ToUpper(org), "AS") {
		org = "AS" + strings.TrimPrefix(strings.ToUpper(asn), "AS") + " " + org
	}
	return probeInfo{
		IP:      strings.TrimSpace(payload.IP),
		City:    strings.TrimSpace(payload.City),
		Country: strings.TrimSpace(payload.Country),
		Region:  strings.TrimSpace(payload.Region),
		Org:     strings.TrimSpace(org),
	}, nil
}

func parseIPAPICoProbe(body []byte) (probeInfo, error) {
	payload := struct {
		IP      string `json:"ip"`
		City    string `json:"city"`
		Country string `json:"country_code"`
		Region  string `json:"region"`
		ASN     string `json:"asn"`
		Org     string `json:"org"`
		Error   bool   `json:"error"`
		Reason  string `json:"reason"`
	}{}
	if err := json.Unmarshal(body, &payload); err != nil {
		return probeInfo{}, err
	}
	if payload.Error {
		if payload.Reason == "" {
			payload.Reason = "ipapi error"
		}
		return probeInfo{}, errors.New(payload.Reason)
	}
	org := strings.TrimSpace(payload.Org)
	if asn := strings.TrimSpace(payload.ASN); asn != "" && org != "" && !strings.HasPrefix(strings.ToUpper(org), "AS") {
		org = strings.TrimSpace(asn) + " " + org
	}
	return probeInfo{
		IP:      strings.TrimSpace(payload.IP),
		City:    strings.TrimSpace(payload.City),
		Country: strings.TrimSpace(payload.Country),
		Region:  strings.TrimSpace(payload.Region),
		Org:     strings.TrimSpace(org),
	}, nil
}

func mergeProbeInfo(base, extra probeInfo) probeInfo {
	if base.IP == "" {
		base.IP = extra.IP
	}
	if base.City == "" {
		base.City = extra.City
	}
	if base.Country == "" {
		base.Country = extra.Country
	}
	if base.Region == "" {
		base.Region = extra.Region
	}
	if base.Org == "" {
		base.Org = extra.Org
	}
	return base
}

func hasProfileInfo(info probeInfo) bool {
	return strings.TrimSpace(info.City) != "" ||
		strings.TrimSpace(info.Country) != "" ||
		strings.TrimSpace(info.Region) != "" ||
		strings.TrimSpace(info.Org) != ""
}
