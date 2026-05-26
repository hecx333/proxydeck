package subscription

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"proxydeck/backend/internal/auth"
	"proxydeck/backend/internal/metrics"
	"proxydeck/backend/internal/model"
	"proxydeck/backend/internal/redisstore"
	"proxydeck/backend/internal/retry"

	"gopkg.in/yaml.v3"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type Service struct {
	db      *gorm.DB
	store   *redisstore.Store
	cipher  *auth.Cipher
	client  *http.Client
	retry   retry.Config
	metrics *metrics.Registry
}

type SyncResult struct {
	SubscriptionID  uint   `json:"subscription_id"`
	ImportedNodeIDs []uint `json:"imported_node_ids"`
	ImportedCount   int    `json:"imported_count"`
}

func NewService(db *gorm.DB, store *redisstore.Store, cipher *auth.Cipher, client *http.Client, retryCfg retry.Config, metricRegistry *metrics.Registry) *Service {
	if client == nil {
		client = http.DefaultClient
	}
	return &Service{db: db, store: store, cipher: cipher, client: client, retry: retryCfg, metrics: metricRegistry}
}

func (s *Service) Store() *redisstore.Store {
	return s.store
}

func (s *Service) Start(ctx context.Context, interval time.Duration) {
	if interval <= 0 {
		interval = time.Minute
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			_, _ = s.syncDueSubscriptions(ctx, time.Now())
		}
	}
}

type singBoxDocument struct {
	Outbounds []map[string]any `json:"outbounds"`
}

type clashDocument struct {
	Proxies []map[string]any `yaml:"proxies"`
}

type parsedNode struct {
	Protocol       string
	Host           string
	Port           int
	Username       string
	Password       string
	TLSEnabled     bool
	TLSSkipVerify  bool
	ServerName     string
	Tag            string
	ExpectedRegion string
	RawJSON        string
}

func (s *Service) Sync(ctx context.Context, subID uint) (SyncResult, error) {
	result := SyncResult{SubscriptionID: subID}
	if s.metrics != nil {
		s.metrics.IncSubscriptionSync()
	}
	var sub model.Subscription
	if err := s.db.First(&sub, subID).Error; err != nil {
		return result, err
	}
	body, err := s.fetchSubscription(ctx, sub.URL)
	if err != nil {
		if s.metrics != nil {
			s.metrics.IncSubscriptionSyncFailure()
		}
		return result, err
	}
	nodes, err := parseSubscriptionBody(sub.Type, body)
	if err != nil {
		if s.metrics != nil {
			s.metrics.IncSubscriptionSyncFailure()
		}
		return result, err
	}
	importedIDs := make([]uint, 0, len(nodes))
	for _, item := range nodes {
		userEnc, _ := s.cipher.Encrypt(item.Username)
		passEnc, _ := s.cipher.Encrypt(item.Password)
		node := model.ProxyNode{
			NodeKey:             fmt.Sprintf("%s://%s:%d", item.Protocol, item.Host, item.Port),
			Protocol:            item.Protocol,
			Host:                item.Host,
			Port:                item.Port,
			UpstreamUsernameEnc: userEnc,
			UpstreamPasswordEnc: passEnc,
			TLSEnabled:          item.TLSEnabled,
			TLSSkipVerify:       item.TLSSkipVerify,
			ServerName:          item.ServerName,
			Tag:                 item.Tag,
			ExpectedRegion:      item.ExpectedRegion,
			SubscriptionID:      sub.ID,
			RawJSON:             item.RawJSON,
		}
		if err := s.db.Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "protocol"}, {Name: "host"}, {Name: "port"}},
			DoUpdates: clause.AssignmentColumns([]string{"tag", "expected_region", "subscription_id", "raw_json", "upstream_username_enc", "upstream_password_enc", "tls_enabled", "tls_skip_verify", "server_name", "updated_at"}),
		}).Create(&node).Error; err != nil {
			if s.metrics != nil {
				s.metrics.IncSubscriptionSyncFailure()
			}
			return result, err
		}
		var persisted model.ProxyNode
		if err := s.db.Where("protocol = ? AND host = ? AND port = ?", node.Protocol, node.Host, node.Port).First(&persisted).Error; err == nil {
			_ = s.db.Where("subscription_id = ? AND node_id = ?", sub.ID, persisted.ID).
				FirstOrCreate(&model.SubscriptionNode{SubscriptionID: sub.ID, NodeID: persisted.ID, RawTag: item.Tag, AliasTag: item.Tag}).Error
			_ = s.store.CacheNode(ctx, persisted)
			importedIDs = append(importedIDs, persisted.ID)
		}
	}
	if err := s.pruneSubscriptionNodes(ctx, sub.ID, importedIDs); err != nil {
		if s.metrics != nil {
			s.metrics.IncSubscriptionSyncFailure()
		}
		return result, err
	}
	now := time.Now()
	if err := s.db.Model(&sub).Update("last_sync_at", &now).Error; err != nil {
		return result, err
	}
	result.ImportedNodeIDs = importedIDs
	result.ImportedCount = len(importedIDs)
	return result, nil
}

func isSupportedOutboundProtocol(protocol string) bool {
	switch strings.ToLower(strings.TrimSpace(protocol)) {
	case "http", "socks", "socks5", "ss", "shadowsocks", "trojan", "vless", "vmess":
		return true
	default:
		return false
	}
}

func parseSubscriptionBody(subType string, body []byte) ([]parsedNode, error) {
	switch strings.ToLower(strings.TrimSpace(subType)) {
	case "singbox":
		return parseSingboxSubscription(body)
	case "shadowrocket":
		return parseShadowrocketSubscription(body)
	case "clash", "mihomo":
		return parseClashSubscription(body)
	case "surge", "surfboard":
		return parseSurgeSubscription(body)
	case "quantumultx":
		return parseQuantumultXSubscription(body)
	default:
		return nil, fmt.Errorf("unsupported subscription type: %s", subType)
	}
}

func parseSingboxSubscription(body []byte) ([]parsedNode, error) {
	var doc singBoxDocument
	if err := json.Unmarshal(body, &doc); err != nil {
		return nil, err
	}
	nodes := make([]parsedNode, 0, len(doc.Outbounds))
	for _, item := range doc.Outbounds {
		protocol, _ := item["type"].(string)
		if !isSupportedOutboundProtocol(protocol) {
			continue
		}
		server, _ := item["server"].(string)
		port, _ := item["server_port"].(float64)
		tag, _ := item["tag"].(string)
		if server == "" || port == 0 {
			continue
		}
		if tag == "selector" || tag == "urltest" || tag == "direct" {
			continue
		}
		tlsEnabled := false
		tlsSkipVerify := false
		serverName := ""
		if tlsMap, ok := item["tls"].(map[string]any); ok {
			tlsEnabled, _ = tlsMap["enabled"].(bool)
			tlsSkipVerify, _ = tlsMap["insecure"].(bool)
			serverName, _ = tlsMap["server_name"].(string)
		}
		username := stringValue(item["username"])
		password := stringValue(item["password"])
		if isShadowsocksProtocol(protocol) {
			username = stringValue(item["method"])
		}
		if isTrojanProtocol(protocol) {
			tlsEnabled = true
			password = stringValue(item["password"])
		}
		if isVLESSProtocol(protocol) {
			if item["transport"] != nil {
				continue
			}
			username = stringValue(item["uuid"])
			password = ""
		}
		if isVMessProtocol(protocol) {
			if network := stringValue(item["transport"]); network != "" && !strings.EqualFold(network, "tcp") {
				continue
			}
			username = stringValue(item["uuid"])
			password = ""
		}
		rawJSON, _ := json.Marshal(item)
		nodes = append(nodes, parsedNode{
			Protocol:       protocol,
			Host:           server,
			Port:           int(port),
			Username:       username,
			Password:       password,
			TLSEnabled:     tlsEnabled,
			TLSSkipVerify:  tlsSkipVerify,
			ServerName:     serverName,
			Tag:            tag,
			ExpectedRegion: inferRegion(tag),
			RawJSON:        string(rawJSON),
		})
	}
	return nodes, nil
}

func parseShadowrocketSubscription(body []byte) ([]parsedNode, error) {
	lines := strings.FieldsFunc(strings.TrimSpace(string(body)), func(r rune) bool {
		return r == '\n' || r == '\r'
	})
	nodes := make([]parsedNode, 0, len(lines))
	for _, line := range lines {
		decoded, err := decodeShadowrocketLine(line)
		if err != nil {
			continue
		}
		node, ok := parseShadowrocketNode(decoded)
		if !ok {
			continue
		}
		nodes = append(nodes, node)
	}
	if len(nodes) == 0 {
		return nil, fmt.Errorf("subscription contains no supported shadowrocket nodes")
	}
	return nodes, nil
}

func parseClashSubscription(body []byte) ([]parsedNode, error) {
	var doc clashDocument
	if err := yaml.Unmarshal(body, &doc); err != nil {
		return nil, err
	}
	nodes := make([]parsedNode, 0, len(doc.Proxies))
	for _, item := range doc.Proxies {
		protocol := stringValue(item["type"])
		if !isSupportedOutboundProtocol(protocol) {
			continue
		}
		server := stringValue(item["server"])
		port, ok := parsePortValue(item["port"])
		if server == "" || !ok || port <= 0 {
			continue
		}
		tag := stringValue(item["name"])
		tlsEnabled := boolValue(item["tls"])
		tlsSkipVerify := boolValue(item["skip-cert-verify"]) || boolValue(item["insecure"])
		serverName := stringValue(item["servername"])
		if serverName == "" {
			serverName = stringValue(item["sni"])
		}
		if serverName == "" && tlsEnabled {
			serverName = server
		}
		username := stringValue(item["username"])
		password := stringValue(item["password"])
		if isShadowsocksProtocol(protocol) {
			username = firstNonEmpty(stringValue(item["cipher"]), stringValue(item["method"]))
		}
		if isTrojanProtocol(protocol) {
			tlsEnabled = true
			password = stringValue(item["password"])
			if serverName == "" {
				serverName = server
			}
		}
		if isVLESSProtocol(protocol) {
			network := firstNonEmpty(stringValue(item["network"]), stringValue(item["type"]))
			if network != "" && !strings.EqualFold(network, "tcp") && !strings.EqualFold(network, "raw") {
				continue
			}
			if flow := stringValue(item["flow"]); flow != "" {
				continue
			}
			username = stringValue(item["uuid"])
			password = ""
		}
		if isVMessProtocol(protocol) {
			network := firstNonEmpty(stringValue(item["network"]), stringValue(item["type"]))
			if network != "" && !strings.EqualFold(network, "tcp") {
				continue
			}
			username = stringValue(item["uuid"])
			password = ""
		}
		rawJSON, _ := json.Marshal(item)
		nodes = append(nodes, parsedNode{
			Protocol:       protocol,
			Host:           server,
			Port:           port,
			Username:       username,
			Password:       password,
			TLSEnabled:     tlsEnabled,
			TLSSkipVerify:  tlsSkipVerify,
			ServerName:     serverName,
			Tag:            tag,
			ExpectedRegion: inferRegion(tag),
			RawJSON:        string(rawJSON),
		})
	}
	if len(nodes) == 0 {
		return nil, fmt.Errorf("subscription contains no supported clash/mihomo nodes")
	}
	return nodes, nil
}

func parseSurgeSubscription(body []byte) ([]parsedNode, error) {
	lines := strings.FieldsFunc(string(body), func(r rune) bool {
		return r == '\n' || r == '\r'
	})
	inProxySection := false
	nodes := make([]parsedNode, 0, len(lines))
	for _, rawLine := range lines {
		line := strings.TrimSpace(rawLine)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			inProxySection = strings.EqualFold(line, "[Proxy]")
			continue
		}
		if !inProxySection {
			continue
		}
		node, ok := parseSurgeProxyLine(line)
		if !ok {
			continue
		}
		nodes = append(nodes, node)
	}
	if len(nodes) == 0 {
		return nil, fmt.Errorf("subscription contains no supported surge/surfboard nodes")
	}
	return nodes, nil
}

func parseQuantumultXSubscription(body []byte) ([]parsedNode, error) {
	lines := strings.FieldsFunc(string(body), func(r rune) bool {
		return r == '\n' || r == '\r'
	})
	nodes := make([]parsedNode, 0, len(lines))
	for _, rawLine := range lines {
		line := strings.TrimSpace(rawLine)
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, ";") {
			continue
		}
		node, ok := parseQuantumultXLine(line)
		if !ok {
			continue
		}
		nodes = append(nodes, node)
	}
	if len(nodes) == 0 {
		return nil, fmt.Errorf("subscription contains no supported quantumultx nodes")
	}
	return nodes, nil
}

func decodeShadowrocketLine(line string) (string, error) {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" {
		return "", fmt.Errorf("empty line")
	}
	if strings.Contains(trimmed, "://") {
		return trimmed, nil
	}
	decoded, err := base64.StdEncoding.DecodeString(padBase64(trimmed))
	if err != nil {
		decoded, err = base64.RawStdEncoding.DecodeString(trimmed)
		if err != nil {
			decoded, err = base64.URLEncoding.DecodeString(padBase64(trimmed))
			if err != nil {
				return "", err
			}
		}
	}
	return strings.TrimSpace(string(decoded)), nil
}

func parseShadowrocketNode(raw string) (parsedNode, bool) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return parsedNode{}, false
	}
	scheme := strings.ToLower(strings.TrimSpace(u.Scheme))
	var protocol string
	var tlsEnabled bool
	switch scheme {
	case "http":
		protocol = "http"
	case "https":
		protocol = "http"
		tlsEnabled = true
	case "socks", "socks5":
		protocol = "socks"
	case "ss", "shadowsocks":
		return parseShadowrocketShadowsocksNode(raw)
	case "trojan":
		return parseShadowrocketTrojanNode(raw)
	case "vless":
		return parseShadowrocketVLESSNode(raw)
	case "vmess":
		return parseShadowrocketVMessNode(raw)
	default:
		return parsedNode{}, false
	}
	port, err := strconv.Atoi(u.Port())
	if err != nil || u.Hostname() == "" || port <= 0 {
		return parsedNode{}, false
	}
	username, password := parseShadowrocketCredentials(u)
	tag, _ := url.QueryUnescape(strings.TrimSpace(u.Fragment))
	rawJSON, _ := json.Marshal(map[string]any{
		"source_format": "shadowrocket",
		"type":          protocol,
		"server":        u.Hostname(),
		"server_port":   port,
		"tag":           tag,
		"tls_enabled":   tlsEnabled,
		"server_name":   u.Hostname(),
	})
	return parsedNode{
		Protocol:       protocol,
		Host:           u.Hostname(),
		Port:           port,
		Username:       username,
		Password:       password,
		TLSEnabled:     tlsEnabled,
		ServerName:     u.Hostname(),
		Tag:            tag,
		ExpectedRegion: inferRegion(tag),
		RawJSON:        string(rawJSON),
	}, true
}

func parseSurgeProxyLine(line string) (parsedNode, bool) {
	name, rest, ok := strings.Cut(line, "=")
	if !ok {
		return parsedNode{}, false
	}
	parts := splitCommaSeparated(rest)
	if len(parts) < 5 {
		return parsedNode{}, false
	}
	protocol, tlsEnabled := normalizeProxyProtocol(parts[0])
	if protocol == "" {
		return parsedNode{}, false
	}
	server := parts[1]
	port, err := strconv.Atoi(parts[2])
	if err != nil || server == "" || port <= 0 {
		return parsedNode{}, false
	}
	username := parts[3]
	password := parts[4]
	if isShadowsocksProtocol(protocol) {
		options := parseKeyValueOptions(parts[3:])
		username = firstNonEmpty(options["encrypt-method"], options["method"], options["cipher"])
		password = options["password"]
		if username == "" || password == "" {
			return parsedNode{}, false
		}
	}
	if isTrojanProtocol(protocol) {
		password = parts[3]
		if password == "" {
			return parsedNode{}, false
		}
	}
	serverName := server
	tlsSkipVerify := false
	if len(parts) > 4 {
		options := parseKeyValueOptions(parts[4:])
		serverName = firstNonEmpty(options["sni"], options["peer"], options["servername"], server)
		tlsSkipVerify = strings.EqualFold(options["skip-cert-verify"], "true") || strings.EqualFold(options["allowinsecure"], "true")
	}
	if isTrojanProtocol(protocol) {
		tlsEnabled = true
	}
	rawJSON, _ := json.Marshal(map[string]any{
		"source_format":   "surge",
		"type":            protocol,
		"server":          server,
		"server_port":     port,
		"tag":             strings.TrimSpace(name),
		"tls_enabled":     tlsEnabled,
		"tls_skip_verify": tlsSkipVerify,
		"server_name":     serverName,
	})
	return parsedNode{
		Protocol:       protocol,
		Host:           server,
		Port:           port,
		Username:       username,
		Password:       password,
		TLSEnabled:     tlsEnabled,
		TLSSkipVerify:  tlsSkipVerify,
		ServerName:     serverName,
		Tag:            strings.TrimSpace(name),
		ExpectedRegion: inferRegion(strings.TrimSpace(name)),
		RawJSON:        string(rawJSON),
	}, true
}

func parseQuantumultXLine(line string) (parsedNode, bool) {
	protocolPart, rest, ok := strings.Cut(line, "=")
	if !ok {
		return parsedNode{}, false
	}
	protocol, tlsEnabled := normalizeProxyProtocol(protocolPart)
	if protocol == "" {
		return parsedNode{}, false
	}
	parts := splitCommaSeparated(rest)
	if len(parts) < 2 {
		return parsedNode{}, false
	}
	hostPort := strings.TrimSpace(parts[0])
	host, portText, ok := strings.Cut(hostPort, ":")
	if !ok {
		return parsedNode{}, false
	}
	port, err := strconv.Atoi(strings.TrimSpace(portText))
	if err != nil || host == "" || port <= 0 {
		return parsedNode{}, false
	}
	options := parseKeyValueOptions(parts[1:])
	username := options["username"]
	password := options["password"]
	if isShadowsocksProtocol(protocol) {
		username = firstNonEmpty(options["encrypt-method"], options["method"], options["cipher"])
		if username == "" || password == "" {
			return parsedNode{}, false
		}
	}
	if isTrojanProtocol(protocol) {
		password = options["password"]
		if password == "" {
			password = options["secret"]
		}
		if password == "" {
			return parsedNode{}, false
		}
		tlsEnabled = true
	}
	tag := firstNonEmpty(options["tag"], options["remarks"])
	if tag == "" {
		tag = fmt.Sprintf("%s://%s:%d", protocol, host, port)
	}
	tlsEnabled = tlsEnabled || strings.EqualFold(options["over-tls"], "true")
	serverName := firstNonEmpty(options["tls-host"], options["servername"], options["sni"])
	if serverName == "" && tlsEnabled {
		serverName = host
	}
	tlsSkipVerify := strings.EqualFold(options["tls-verification"], "false") ||
		strings.EqualFold(options["skip-cert-verify"], "true") ||
		strings.EqualFold(options["allowinsecure"], "true")
	rawJSON, _ := json.Marshal(map[string]any{
		"source_format":   "quantumultx",
		"type":            protocol,
		"server":          host,
		"server_port":     port,
		"tag":             tag,
		"tls_enabled":     tlsEnabled,
		"tls_skip_verify": tlsSkipVerify,
		"server_name":     serverName,
	})
	return parsedNode{
		Protocol:       protocol,
		Host:           host,
		Port:           port,
		Username:       username,
		Password:       password,
		TLSEnabled:     tlsEnabled,
		TLSSkipVerify:  tlsSkipVerify,
		ServerName:     serverName,
		Tag:            tag,
		ExpectedRegion: inferRegion(tag),
		RawJSON:        string(rawJSON),
	}, true
}

func parseShadowrocketCredentials(u *url.URL) (string, string) {
	if u == nil || u.User == nil {
		return "", ""
	}
	user := u.User.Username()
	pass, hasPass := u.User.Password()
	if hasPass {
		return user, pass
	}
	decoded, err := base64.StdEncoding.DecodeString(padBase64(user))
	if err != nil {
		decoded, err = base64.RawStdEncoding.DecodeString(user)
		if err != nil {
			return user, ""
		}
	}
	parts := strings.SplitN(string(decoded), ":", 2)
	if len(parts) != 2 {
		return user, ""
	}
	return parts[0], parts[1]
}

func parseShadowrocketShadowsocksNode(raw string) (parsedNode, bool) {
	trimmed := strings.TrimSpace(raw)
	u, err := url.Parse(trimmed)
	if err != nil {
		return parsedNode{}, false
	}
	tag, _ := url.QueryUnescape(strings.TrimSpace(u.Fragment))
	method := ""
	password := ""
	host := u.Hostname()
	portText := u.Port()
	if host != "" && portText != "" {
		user := u.User.Username()
		pass, hasPass := u.User.Password()
		if hasPass {
			method = user
			password = pass
		} else {
			var ok bool
			method, password, ok = decodeShadowsocksUserInfo(user)
			if !ok {
				return parsedNode{}, false
			}
		}
	} else {
		payload := strings.TrimPrefix(trimmed, "ss://")
		payload = strings.SplitN(payload, "#", 2)[0]
		payload = strings.SplitN(payload, "?", 2)[0]
		decoded, err := decodeBase64Text(payload)
		if err != nil {
			return parsedNode{}, false
		}
		userInfo, address, ok := strings.Cut(decoded, "@")
		if !ok {
			return parsedNode{}, false
		}
		method, password, ok = strings.Cut(userInfo, ":")
		if !ok {
			return parsedNode{}, false
		}
		host, portText, ok = strings.Cut(address, ":")
		if !ok {
			return parsedNode{}, false
		}
	}
	port, err := strconv.Atoi(strings.TrimSpace(portText))
	if err != nil || host == "" || port <= 0 || method == "" {
		return parsedNode{}, false
	}
	rawJSON, _ := json.Marshal(map[string]any{
		"source_format": "shadowrocket",
		"type":          "ss",
		"server":        host,
		"server_port":   port,
		"method":        method,
		"tag":           tag,
	})
	return parsedNode{
		Protocol:       "ss",
		Host:           strings.TrimSpace(host),
		Port:           port,
		Username:       method,
		Password:       password,
		Tag:            tag,
		ExpectedRegion: inferRegion(tag),
		RawJSON:        string(rawJSON),
	}, true
}

func parseShadowrocketTrojanNode(raw string) (parsedNode, bool) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return parsedNode{}, false
	}
	host := strings.TrimSpace(u.Hostname())
	port, err := strconv.Atoi(strings.TrimSpace(u.Port()))
	if err != nil || host == "" || port <= 0 {
		return parsedNode{}, false
	}
	password := ""
	if u.User != nil {
		password = u.User.Username()
		if pass, hasPass := u.User.Password(); hasPass && password == "" {
			password = pass
		}
	}
	if password == "" {
		return parsedNode{}, false
	}
	tag, _ := url.QueryUnescape(strings.TrimSpace(u.Fragment))
	values := u.Query()
	serverName := firstNonEmpty(values.Get("peer"), values.Get("sni"), values.Get("servername"), host)
	tlsSkipVerify := strings.EqualFold(values.Get("allowInsecure"), "1") ||
		strings.EqualFold(values.Get("allowInsecure"), "true") ||
		strings.EqualFold(values.Get("insecure"), "1") ||
		strings.EqualFold(values.Get("insecure"), "true")
	rawJSON, _ := json.Marshal(map[string]any{
		"source_format":   "shadowrocket",
		"type":            "trojan",
		"server":          host,
		"server_port":     port,
		"server_name":     serverName,
		"tls_skip_verify": tlsSkipVerify,
		"tag":             tag,
	})
	return parsedNode{
		Protocol:       "trojan",
		Host:           host,
		Port:           port,
		Password:       password,
		TLSEnabled:     true,
		TLSSkipVerify:  tlsSkipVerify,
		ServerName:     serverName,
		Tag:            tag,
		ExpectedRegion: inferRegion(tag),
		RawJSON:        string(rawJSON),
	}, true
}

func parseShadowrocketVLESSNode(raw string) (parsedNode, bool) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return parsedNode{}, false
	}
	host := strings.TrimSpace(u.Hostname())
	port, err := strconv.Atoi(strings.TrimSpace(u.Port()))
	if err != nil || host == "" || port <= 0 {
		return parsedNode{}, false
	}
	uuid := ""
	if u.User != nil {
		uuid = u.User.Username()
	}
	if uuid == "" {
		return parsedNode{}, false
	}
	values := u.Query()
	transport := firstNonEmpty(values.Get("type"), values.Get("transport"))
	if transport != "" && !strings.EqualFold(transport, "tcp") {
		return parsedNode{}, false
	}
	security := strings.ToLower(strings.TrimSpace(values.Get("security")))
	if security != "" && security != "tls" && security != "none" {
		return parsedNode{}, false
	}
	flow := strings.TrimSpace(values.Get("flow"))
	if flow != "" {
		return parsedNode{}, false
	}
	encryption := strings.ToLower(strings.TrimSpace(firstNonEmpty(values.Get("encryption"), "none")))
	if encryption != "none" {
		return parsedNode{}, false
	}
	tag, _ := url.QueryUnescape(strings.TrimSpace(u.Fragment))
	tlsEnabled := security == "tls" || values.Get("sni") != "" || values.Get("peer") != ""
	serverName := firstNonEmpty(values.Get("sni"), values.Get("peer"), values.Get("servername"))
	if serverName == "" && tlsEnabled {
		serverName = host
	}
	tlsSkipVerify := strings.EqualFold(values.Get("allowInsecure"), "1") ||
		strings.EqualFold(values.Get("allowInsecure"), "true") ||
		strings.EqualFold(values.Get("insecure"), "1") ||
		strings.EqualFold(values.Get("insecure"), "true")
	rawJSON, _ := json.Marshal(map[string]any{
		"source_format":   "shadowrocket",
		"type":            "vless",
		"server":          host,
		"server_port":     port,
		"uuid":            uuid,
		"server_name":     serverName,
		"tls_enabled":     tlsEnabled,
		"tls_skip_verify": tlsSkipVerify,
		"tag":             tag,
	})
	return parsedNode{
		Protocol:       "vless",
		Host:           host,
		Port:           port,
		Username:       uuid,
		TLSEnabled:     tlsEnabled,
		TLSSkipVerify:  tlsSkipVerify,
		ServerName:     serverName,
		Tag:            tag,
		ExpectedRegion: inferRegion(tag),
		RawJSON:        string(rawJSON),
	}, true
}

func parseShadowrocketVMessNode(raw string) (parsedNode, bool) {
	payload := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(raw), "vmess://"))
	decoded, err := decodeBase64Text(payload)
	if err != nil {
		return parsedNode{}, false
	}
	var item map[string]any
	if err := json.Unmarshal([]byte(decoded), &item); err != nil {
		return parsedNode{}, false
	}
	protocol := firstNonEmpty(stringValue(item["type"]), "vmess")
	if !isVMessProtocol(protocol) {
		return parsedNode{}, false
	}
	server := firstNonEmpty(stringValue(item["add"]), stringValue(item["server"]), stringValue(item["address"]))
	port, ok := parsePortValue(item["port"])
	if server == "" || !ok || port <= 0 {
		return parsedNode{}, false
	}
	network := firstNonEmpty(stringValue(item["net"]), stringValue(item["network"]), stringValue(item["type"]))
	if network != "" && !strings.EqualFold(network, "tcp") {
		return parsedNode{}, false
	}
	uuidValue := firstNonEmpty(stringValue(item["id"]), stringValue(item["uuid"]))
	if uuidValue == "" {
		return parsedNode{}, false
	}
	tlsEnabled := strings.EqualFold(stringValue(item["tls"]), "tls") || strings.EqualFold(stringValue(item["security"]), "tls")
	serverName := firstNonEmpty(stringValue(item["sni"]), stringValue(item["servername"]), stringValue(item["host"]))
	if serverName == "" && tlsEnabled {
		serverName = server
	}
	tag := firstNonEmpty(stringValue(item["ps"]), stringValue(item["name"]), stringValue(item["remark"]))
	rawJSON, _ := json.Marshal(item)
	return parsedNode{
		Protocol:       "vmess",
		Host:           server,
		Port:           port,
		Username:       uuidValue,
		TLSEnabled:     tlsEnabled,
		ServerName:     serverName,
		Tag:            tag,
		ExpectedRegion: inferRegion(tag),
		RawJSON:        string(rawJSON),
	}, true
}

func padBase64(value string) string {
	if mod := len(value) % 4; mod != 0 {
		return value + strings.Repeat("=", 4-mod)
	}
	return value
}

func stringValue(value any) string {
	v, _ := value.(string)
	return v
}

func boolValue(value any) bool {
	v, _ := value.(bool)
	return v
}

func parsePortValue(value any) (int, bool) {
	switch typed := value.(type) {
	case int:
		return typed, true
	case int64:
		return int(typed), true
	case uint64:
		return int(typed), true
	case float64:
		return int(typed), true
	case string:
		port, err := strconv.Atoi(strings.TrimSpace(typed))
		if err != nil {
			return 0, false
		}
		return port, true
	default:
		return 0, false
	}
}

func splitCommaSeparated(value string) []string {
	rawParts := strings.Split(value, ",")
	parts := make([]string, 0, len(rawParts))
	for _, part := range rawParts {
		trimmed := strings.TrimSpace(part)
		if trimmed != "" {
			parts = append(parts, trimmed)
		}
	}
	return parts
}

func normalizeProxyProtocol(value string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "http":
		return "http", false
	case "https":
		return "http", true
	case "socks", "socks5":
		return "socks", false
	case "ss", "shadowsocks":
		return "ss", false
	case "trojan":
		return "trojan", true
	case "vless":
		return "vless", false
	default:
		return "", false
	}
}

func parseKeyValueOptions(parts []string) map[string]string {
	result := make(map[string]string, len(parts))
	for _, part := range parts {
		key, value, ok := strings.Cut(part, "=")
		if !ok {
			continue
		}
		result[strings.ToLower(strings.TrimSpace(key))] = strings.TrimSpace(value)
	}
	return result
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func isShadowsocksProtocol(protocol string) bool {
	switch strings.ToLower(strings.TrimSpace(protocol)) {
	case "ss", "shadowsocks":
		return true
	default:
		return false
	}
}

func isTrojanProtocol(protocol string) bool {
	return strings.EqualFold(strings.TrimSpace(protocol), "trojan")
}

func isVLESSProtocol(protocol string) bool {
	return strings.EqualFold(strings.TrimSpace(protocol), "vless")
}

func isVMessProtocol(protocol string) bool {
	return strings.EqualFold(strings.TrimSpace(protocol), "vmess")
}

func decodeBase64Text(value string) (string, error) {
	trimmed := strings.TrimSpace(value)
	decoded, err := base64.RawURLEncoding.DecodeString(trimmed)
	if err == nil {
		return string(decoded), nil
	}
	decoded, err = base64.URLEncoding.DecodeString(padBase64(trimmed))
	if err == nil {
		return string(decoded), nil
	}
	decoded, err = base64.RawStdEncoding.DecodeString(trimmed)
	if err == nil {
		return string(decoded), nil
	}
	decoded, err = base64.StdEncoding.DecodeString(padBase64(trimmed))
	if err != nil {
		return "", err
	}
	return string(decoded), nil
}

func decodeShadowsocksUserInfo(value string) (string, string, bool) {
	decoded, err := decodeBase64Text(value)
	if err != nil {
		return "", "", false
	}
	method, password, ok := strings.Cut(decoded, ":")
	if !ok {
		return "", "", false
	}
	return method, password, true
}

func (s *Service) syncDueSubscriptions(ctx context.Context, now time.Time) (int, error) {
	var subs []model.Subscription
	if err := s.db.Where("enabled = ? AND sync_interval_seconds > 0", true).Find(&subs).Error; err != nil {
		return 0, err
	}
	synced := 0
	for _, sub := range subs {
		if !subscriptionDue(sub, now) {
			continue
		}
		if _, err := s.Sync(ctx, sub.ID); err != nil {
			s.writeSyncAuditLog(sub.ID, "sync_subscription_error", err.Error())
			continue
		}
		s.writeSyncAuditLog(sub.ID, "sync_subscription", "")
		synced++
	}
	return synced, nil
}

func subscriptionDue(sub model.Subscription, now time.Time) bool {
	if !sub.Enabled || sub.SyncIntervalSeconds <= 0 {
		return false
	}
	if sub.LastSyncAt == nil {
		return true
	}
	return now.Sub(*sub.LastSyncAt) >= time.Duration(sub.SyncIntervalSeconds)*time.Second
}

func (s *Service) fetchSubscription(ctx context.Context, url string) ([]byte, error) {
	var body []byte
	err := retry.Do(ctx, s.retry, func(callCtx context.Context) error {
		req, err := http.NewRequestWithContext(callCtx, http.MethodGet, url, nil)
		if err != nil {
			return err
		}
		resp, err := s.client.Do(req)
		if err != nil {
			return err
		}
		defer resp.Body.Close()
		if resp.StatusCode >= http.StatusInternalServerError {
			return fmt.Errorf("subscription fetch failed: %s", resp.Status)
		}
		if resp.StatusCode != http.StatusOK {
			return retry.StopError{Err: fmt.Errorf("subscription fetch failed: %s", resp.Status)}
		}
		body, err = io.ReadAll(resp.Body)
		return err
	})
	return body, err
}

func (s *Service) Delete(ctx context.Context, subID uint) error {
	var nodes []model.ProxyNode
	if err := s.db.Model(&model.ProxyNode{}).
		Joins("join subscription_nodes on subscription_nodes.node_id = proxy_nodes.id").
		Where("subscription_nodes.subscription_id = ?", subID).
		Group("proxy_nodes.id").
		Find(&nodes).Error; err != nil {
		return err
	}
	if err := s.db.Where("subscription_id = ?", subID).Delete(&model.SubscriptionNode{}).Error; err != nil {
		return err
	}
	for _, node := range nodes {
		var count int64
		if err := s.db.Model(&model.SubscriptionNode{}).Where("node_id = ?", node.ID).Count(&count).Error; err != nil {
			return err
		}
		if count == 0 {
			if err := s.db.Delete(&model.ProxyNode{}, node.ID).Error; err != nil {
				return err
			}
			_ = s.store.RemoveNode(ctx, node)
		}
	}
	return s.db.Delete(&model.Subscription{}, subID).Error
}

func (s *Service) pruneSubscriptionNodes(ctx context.Context, subID uint, importedIDs []uint) error {
	var links []model.SubscriptionNode
	if err := s.db.Where("subscription_id = ?", subID).Find(&links).Error; err != nil {
		return err
	}
	keep := map[uint]struct{}{}
	for _, id := range importedIDs {
		keep[id] = struct{}{}
	}
	for _, link := range links {
		if _, ok := keep[link.NodeID]; ok {
			continue
		}
		if err := s.db.Delete(&link).Error; err != nil {
			return err
		}
		var count int64
		if err := s.db.Model(&model.SubscriptionNode{}).Where("node_id = ?", link.NodeID).Count(&count).Error; err != nil {
			return err
		}
		if count == 0 {
			var node model.ProxyNode
			if err := s.db.First(&node, link.NodeID).Error; err == nil {
				if err := s.db.Delete(&node).Error; err != nil {
					return err
				}
				_ = s.store.RemoveNode(ctx, node)
			}
		}
	}
	return nil
}

var regionPattern = regexp.MustCompile(`(?i)\b(HK|SG|US|JP|TW|KR|DE|FR|GB|NL|CA|AU)\b`)

var regionHints = []struct {
	Match  string
	Region string
}{
	{"🇭🇰", "HK"},
	{"香港", "HK"},
	{"hong kong", "HK"},
	{"🇸🇬", "SG"},
	{"新加坡", "SG"},
	{"singapore", "SG"},
	{"🇯🇵", "JP"},
	{"日本", "JP"},
	{"东京", "JP"},
	{"tokyo", "JP"},
	{"japan", "JP"},
	{"🇺🇸", "US"},
	{"美国", "US"},
	{"硅谷", "US"},
	{"洛杉矶", "US"},
	{"los angeles", "US"},
	{"silicon valley", "US"},
	{"usa", "US"},
	{"united states", "US"},
	{"🇹🇼", "TW"},
	{"台湾", "TW"},
	{"taiwan", "TW"},
	{"🇰🇷", "KR"},
	{"韩国", "KR"},
	{"seoul", "KR"},
	{"korea", "KR"},
	{"🇩🇪", "DE"},
	{"德国", "DE"},
	{"germany", "DE"},
	{"🇫🇷", "FR"},
	{"法国", "FR"},
	{"france", "FR"},
	{"🇬🇧", "GB"},
	{"英国", "GB"},
	{"london", "GB"},
	{"uk", "GB"},
	{"🇳🇱", "NL"},
	{"荷兰", "NL"},
	{"netherlands", "NL"},
	{"🇨🇦", "CA"},
	{"加拿大", "CA"},
	{"canada", "CA"},
	{"🇦🇺", "AU"},
	{"澳大利亚", "AU"},
	{"悉尼", "AU"},
	{"australia", "AU"},
}

func inferRegion(tag string) string {
	matched := regionPattern.FindStringSubmatch(strings.ToUpper(tag))
	if len(matched) > 0 {
		return matched[1]
	}
	lowered := strings.ToLower(tag)
	for _, hint := range regionHints {
		if strings.Contains(lowered, strings.ToLower(hint.Match)) {
			return hint.Region
		}
	}
	return ""
}

func (s *Service) writeSyncAuditLog(subID uint, action, detail string) {
	_ = s.db.Create(&model.AuditLog{
		Operator:   "system",
		Action:     action,
		TargetType: "subscription",
		TargetID:   fmt.Sprintf("%d", subID),
		Detail:     detail,
	}).Error
}
