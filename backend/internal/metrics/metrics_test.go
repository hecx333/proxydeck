package metrics

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHandlerExposesCounters(t *testing.T) {
	registry := New()
	registry.IncAdminLogins()
	registry.IncProxyRequests()
	registry.IncProxyConnectRequests()
	registry.IncProxyAuthFailures()
	registry.IncProxySelectionFailures()
	registry.IncProxyUpstreamFailures()
	registry.IncActiveTunnels()
	registry.IncHealthcheckSuccess()
	registry.IncHealthcheckFailure()
	registry.IncSubscriptionSync()
	registry.IncSubscriptionSyncFailure()

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	registry.Handler().ServeHTTP(recorder, request)

	body := recorder.Body.String()
	for _, expected := range []string{
		"proxydeck_admin_logins_total 1",
		"proxydeck_proxy_requests_total 1",
		"proxydeck_proxy_connect_requests_total 1",
		"proxydeck_proxy_auth_failures_total 1",
		"proxydeck_proxy_selection_failures_total 1",
		"proxydeck_proxy_upstream_failures_total 1",
		"proxydeck_active_tunnels 1",
		"proxydeck_healthcheck_success_total 1",
		"proxydeck_healthcheck_failure_total 1",
		"proxydeck_subscription_sync_total 1",
		"proxydeck_subscription_sync_failure_total 1",
	} {
		if !strings.Contains(body, expected) {
			t.Fatalf("metrics body missing %q: %s", expected, body)
		}
	}
}
