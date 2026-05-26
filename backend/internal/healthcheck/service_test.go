package healthcheck

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"proxydeck/backend/internal/db"
	"proxydeck/backend/internal/model"
	"proxydeck/backend/internal/redisstore"
	"proxydeck/backend/internal/retry"

	"github.com/alicebob/miniredis/v2"
)

func TestParseASNAndISP(t *testing.T) {
	if got := parseASN("AS13335 Cloudflare, Inc."); got != "AS13335" {
		t.Fatalf("parseASN = %q", got)
	}
	if got := parseISP("AS13335 Cloudflare, Inc."); got != "Cloudflare, Inc." {
		t.Fatalf("parseISP = %q", got)
	}
}

func TestHealthcheckUsesIPInfoRegionForDetectedRegion(t *testing.T) {
	payload := struct {
		IP       string `json:"ip"`
		City     string `json:"city"`
		Country  string `json:"country"`
		Region   string `json:"region"`
		Org      string `json:"org"`
		Timezone string `json:"timezone"`
	}{
		IP:      "1.1.1.1",
		City:    "Los Angeles",
		Country: "US",
		Region:  "California",
		Org:     "AS13335 Cloudflare, Inc.",
	}
	node := model.ProxyNode{}
	node.ExitIP = payload.IP
	node.City = payload.City
	node.Country = payload.Country
	node.DetectedRegion = payload.Region
	node.ASN = parseASN(payload.Org)
	node.Org = payload.Org
	node.ISP = parseISP(payload.Org)

	if node.DetectedRegion != "California" {
		t.Fatalf("detected region = %q", node.DetectedRegion)
	}
	if node.ASN != "AS13335" {
		t.Fatalf("asn = %q", node.ASN)
	}
	if node.ISP != "Cloudflare, Inc." {
		t.Fatalf("isp = %q", node.ISP)
	}
}

func TestMarkFailureThreshold(t *testing.T) {
	sqliteDB, err := db.Open(t.TempDir() + "/app.db")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	mini := miniredis.RunT(t)
	store := redisstore.New(mini.Addr(), "", 0)
	node := &model.ProxyNode{NodeKey: "node-1", Protocol: "http", Host: "127.0.0.1", Port: 8080}
	if err := sqliteDB.Create(node).Error; err != nil {
		t.Fatalf("create node: %v", err)
	}
	svc := &Service{maxFailCount: 3, db: sqliteDB, store: store}
	now := time.Now()
	_ = svc.markFailure(context.Background(), node, now, errors.New("boom"))
	if node.Healthy || node.FailCount != 1 {
		t.Fatalf("after first failure from unknown state: healthy=%v failCount=%d", node.Healthy, node.FailCount)
	}
	node.Healthy = true
	_ = svc.markFailure(context.Background(), node, now, errors.New("boom"))
	if !node.Healthy || node.FailCount != 2 {
		t.Fatalf("after first failure: healthy=%v failCount=%d", node.Healthy, node.FailCount)
	}
	_ = svc.markFailure(context.Background(), node, now, errors.New("boom"))
	if node.Healthy || node.FailCount != 3 {
		t.Fatalf("after threshold: healthy=%v failCount=%d", node.Healthy, node.FailCount)
	}
}

func TestFetchProbeInfoFallsBackAfterPrimaryRateLimit(t *testing.T) {
	svc := &Service{
		retry: retry.Config{MaxAttempts: 1, BaseBackoff: time.Millisecond},
		probeSourcesOverride: []probeSource{
			{name: "primary", url: "https://primary.example/json", parse: parseIPInfoProbe},
			{name: "backup", url: "https://backup.example/json", parse: parseIPifyProbe},
		},
	}
	client := &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			switch req.URL.Host {
			case "primary.example":
				return &http.Response{
					StatusCode: http.StatusTooManyRequests,
					Status:     "429 Too Many Requests",
					Body:       io.NopCloser(strings.NewReader(`{"error":"rate limited"}`)),
					Header:     make(http.Header),
					Request:    req,
				}, nil
			case "backup.example":
				return &http.Response{
					StatusCode: http.StatusOK,
					Body:       io.NopCloser(strings.NewReader(`{"ip":"172.236.157.48"}`)),
					Header:     make(http.Header),
					Request:    req,
				}, nil
			default:
				return nil, errors.New("unexpected host")
			}
		}),
	}

	info, err := svc.fetchExitIP(context.Background(), client, model.ProxyNode{})
	if err != nil {
		t.Fatalf("fetchExitIP returned error: %v", err)
	}
	if info.IP != "172.236.157.48" {
		t.Fatalf("ip = %q, want 172.236.157.48", info.IP)
	}
}

func TestEnrichIPInfoAfterIPOnlyProbe(t *testing.T) {
	svc := &Service{
		retry: retry.Config{MaxAttempts: 1, BaseBackoff: time.Millisecond},
		probeSourcesOverride: []probeSource{
			{name: "ipify", url: "https://probe.example/json", parse: parseIPifyProbe},
		},
		enrichSourcesOverride: []enrichSource{
			{name: "ipwho", url: func(ip string) string { return "https://enrich.example/" + ip }, parse: parseIPWhoProbe},
		},
	}
	client := &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			switch req.URL.Host {
			case "probe.example":
				return &http.Response{
					StatusCode: http.StatusOK,
					Body:       io.NopCloser(strings.NewReader(`{"ip":"172.236.157.48"}`)),
					Header:     make(http.Header),
					Request:    req,
				}, nil
			case "enrich.example":
				return &http.Response{
					StatusCode: http.StatusOK,
					Body: io.NopCloser(strings.NewReader(`{
						"success": true,
						"ip": "172.236.157.48",
						"city": "Singapore",
						"region": "Singapore",
						"country_code": "SG",
						"connection": {
							"asn": "63949",
							"isp": "Akamai Connected Cloud"
						}
					}`)),
					Header:  make(http.Header),
					Request: req,
				}, nil
			default:
				return nil, errors.New("unexpected host")
			}
		}),
	}

	probe, err := svc.fetchExitIP(context.Background(), client, model.ProxyNode{})
	if err != nil {
		t.Fatalf("fetchExitIP: %v", err)
	}
	enrich, err := svc.enrichIPInfo(context.Background(), client, probe.IP)
	if err != nil {
		t.Fatalf("enrichIPInfo: %v", err)
	}
	info := mergeProbeInfo(probe, enrich)
	if info.IP != "172.236.157.48" {
		t.Fatalf("ip = %q", info.IP)
	}
	if info.Region != "Singapore" {
		t.Fatalf("region = %q", info.Region)
	}
	if info.Country != "SG" {
		t.Fatalf("country = %q", info.Country)
	}
	if info.Org != "AS63949 Akamai Connected Cloud" {
		t.Fatalf("org = %q", info.Org)
	}
}

func TestParsePlainTextIPProbe(t *testing.T) {
	info, err := parsePlainTextIPProbe([]byte("172.236.157.48\n"))
	if err != nil {
		t.Fatalf("parsePlainTextIPProbe: %v", err)
	}
	if info.IP != "172.236.157.48" {
		t.Fatalf("ip = %q", info.IP)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return fn(req)
}
