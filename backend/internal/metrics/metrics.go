package metrics

import (
	"fmt"
	"net/http"
	"sync/atomic"
)

type Registry struct {
	adminLoginsTotal             atomic.Int64
	proxyRequestsTotal           atomic.Int64
	proxyConnectRequestsTotal    atomic.Int64
	proxyAuthFailuresTotal       atomic.Int64
	proxySelectionFailuresTotal  atomic.Int64
	proxyUpstreamFailuresTotal   atomic.Int64
	activeTunnels                atomic.Int64
	healthcheckSuccessTotal      atomic.Int64
	healthcheckFailureTotal      atomic.Int64
	subscriptionSyncTotal        atomic.Int64
	subscriptionSyncFailureTotal atomic.Int64
}

type Snapshot struct {
	AdminLoginsTotal             int64
	ProxyRequestsTotal           int64
	ProxyConnectRequestsTotal    int64
	ProxyAuthFailuresTotal       int64
	ProxySelectionFailuresTotal  int64
	ProxyUpstreamFailuresTotal   int64
	ActiveTunnels                int64
	HealthcheckSuccessTotal      int64
	HealthcheckFailureTotal      int64
	SubscriptionSyncTotal        int64
	SubscriptionSyncFailureTotal int64
}

func New() *Registry {
	return &Registry{}
}

func (r *Registry) IncAdminLogins()             { r.adminLoginsTotal.Add(1) }
func (r *Registry) IncProxyRequests()           { r.proxyRequestsTotal.Add(1) }
func (r *Registry) IncProxyConnectRequests()    { r.proxyConnectRequestsTotal.Add(1) }
func (r *Registry) IncProxyAuthFailures()       { r.proxyAuthFailuresTotal.Add(1) }
func (r *Registry) IncProxySelectionFailures()  { r.proxySelectionFailuresTotal.Add(1) }
func (r *Registry) IncProxyUpstreamFailures()   { r.proxyUpstreamFailuresTotal.Add(1) }
func (r *Registry) IncHealthcheckSuccess()      { r.healthcheckSuccessTotal.Add(1) }
func (r *Registry) IncHealthcheckFailure()      { r.healthcheckFailureTotal.Add(1) }
func (r *Registry) IncSubscriptionSync()        { r.subscriptionSyncTotal.Add(1) }
func (r *Registry) IncSubscriptionSyncFailure() { r.subscriptionSyncFailureTotal.Add(1) }
func (r *Registry) IncActiveTunnels()           { r.activeTunnels.Add(1) }
func (r *Registry) DecActiveTunnels()           { r.activeTunnels.Add(-1) }

func (r *Registry) Snapshot() Snapshot {
	return Snapshot{
		AdminLoginsTotal:             r.adminLoginsTotal.Load(),
		ProxyRequestsTotal:           r.proxyRequestsTotal.Load(),
		ProxyConnectRequestsTotal:    r.proxyConnectRequestsTotal.Load(),
		ProxyAuthFailuresTotal:       r.proxyAuthFailuresTotal.Load(),
		ProxySelectionFailuresTotal:  r.proxySelectionFailuresTotal.Load(),
		ProxyUpstreamFailuresTotal:   r.proxyUpstreamFailuresTotal.Load(),
		ActiveTunnels:                r.activeTunnels.Load(),
		HealthcheckSuccessTotal:      r.healthcheckSuccessTotal.Load(),
		HealthcheckFailureTotal:      r.healthcheckFailureTotal.Load(),
		SubscriptionSyncTotal:        r.subscriptionSyncTotal.Load(),
		SubscriptionSyncFailureTotal: r.subscriptionSyncFailureTotal.Load(),
	}
}

func (r *Registry) Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		snapshot := r.Snapshot()
		w.Header().Set("Content-Type", "text/plain; version=0.0.4")
		_, _ = fmt.Fprintf(w,
			"proxydeck_admin_logins_total %d\n"+
				"proxydeck_proxy_requests_total %d\n"+
				"proxydeck_proxy_connect_requests_total %d\n"+
				"proxydeck_proxy_auth_failures_total %d\n"+
				"proxydeck_proxy_selection_failures_total %d\n"+
				"proxydeck_proxy_upstream_failures_total %d\n"+
				"proxydeck_active_tunnels %d\n"+
				"proxydeck_healthcheck_success_total %d\n"+
				"proxydeck_healthcheck_failure_total %d\n"+
				"proxydeck_subscription_sync_total %d\n"+
				"proxydeck_subscription_sync_failure_total %d\n",
			snapshot.AdminLoginsTotal,
			snapshot.ProxyRequestsTotal,
			snapshot.ProxyConnectRequestsTotal,
			snapshot.ProxyAuthFailuresTotal,
			snapshot.ProxySelectionFailuresTotal,
			snapshot.ProxyUpstreamFailuresTotal,
			snapshot.ActiveTunnels,
			snapshot.HealthcheckSuccessTotal,
			snapshot.HealthcheckFailureTotal,
			snapshot.SubscriptionSyncTotal,
			snapshot.SubscriptionSyncFailureTotal,
		)
	})
}
