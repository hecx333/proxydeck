package adminapi

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"proxydeck/backend/internal/auth"
	"proxydeck/backend/internal/config"
	"proxydeck/backend/internal/healthcheck"
	"proxydeck/backend/internal/metrics"
	"proxydeck/backend/internal/model"
	"proxydeck/backend/internal/quota"
	"proxydeck/backend/internal/stats"
	"proxydeck/backend/internal/subscription"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type Server struct {
	db           *gorm.DB
	sessions     *auth.AdminSessionManager
	stats        *stats.Service
	subscription *subscription.Service
	healthcheck  *healthcheck.Service
	quota        *quota.Service
	metrics      *metrics.Registry
	cfg          config.Config
}

func New(db *gorm.DB, sessions *auth.AdminSessionManager, stats *stats.Service, subscription *subscription.Service, healthcheck *healthcheck.Service, quota *quota.Service, metricRegistry *metrics.Registry, cfg config.Config) *Server {
	return &Server{db: db, sessions: sessions, stats: stats, subscription: subscription, healthcheck: healthcheck, quota: quota, metrics: metricRegistry, cfg: cfg}
}

func (s *Server) Handler() http.Handler {
	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	r.Use(gin.Recovery())
	r.Use(func(c *gin.Context) {
		c.Header("Access-Control-Allow-Origin", "http://localhost:5173")
		c.Header("Access-Control-Allow-Credentials", "true")
		c.Header("Access-Control-Allow-Headers", "Content-Type")
		c.Header("Access-Control-Allow-Methods", "GET,POST,PUT,DELETE,OPTIONS")
		if c.Request.Method == http.MethodOptions {
			c.AbortWithStatus(http.StatusNoContent)
			return
		}
		c.Next()
	})
	r.GET("/healthz", s.healthz)
	if s.metrics != nil {
		r.GET("/metrics", gin.WrapH(s.metrics.Handler()))
	}
	api := r.Group("/api")
	admin := api.Group("/admin")
	admin.POST("/login", s.login)
	admin.POST("/logout", s.logout)
	admin.GET("/me", s.requireAdmin(), s.me)

	protected := api.Group("/")
	protected.Use(s.requireAdmin())
	protected.GET("/users", s.listUsers)
	protected.POST("/users", s.createUser)
	protected.GET("/users/:id", s.getUser)
	protected.PUT("/users/:id", s.updateUser)
	protected.DELETE("/users/:id", s.deleteUser)

	protected.GET("/subscriptions", s.listSubscriptions)
	protected.POST("/subscriptions", s.createSubscription)
	protected.PUT("/subscriptions/:id", s.updateSubscription)
	protected.DELETE("/subscriptions/:id", s.deleteSubscription)
	protected.POST("/subscriptions/:id/sync", s.syncSubscription)

	protected.GET("/nodes", s.listNodes)
	protected.GET("/nodes/:id", s.getNode)
	protected.POST("/nodes/:id/check", s.checkNode)
	protected.PUT("/nodes/:id/disable", s.toggleNodeDisabled)
	protected.DELETE("/nodes/:id", s.deleteNode)

	protected.GET("/stats/overview", s.statsOverview)
	protected.GET("/stats/users", s.userStats)
	protected.GET("/stats/nodes", s.nodeStats)
	protected.GET("/stats/traffic", s.trafficStats)
	protected.GET("/audit-logs", s.listAuditLogs)
	protected.GET("/settings", s.settings)
	return r
}

func (s *Server) requireAdmin() gin.HandlerFunc {
	return func(c *gin.Context) {
		token, err := c.Cookie("admin_token")
		if err != nil || token == "" {
			c.AbortWithStatus(http.StatusUnauthorized)
			return
		}
		username, err := s.sessions.Get(c.Request.Context(), token)
		if err != nil || username == "" {
			c.AbortWithStatus(http.StatusUnauthorized)
			return
		}
		c.Set("admin_username", username)
		c.Next()
	}
}

func (s *Server) login(c *gin.Context) {
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	var admin model.User
	if err := s.db.Where("uid = ?", model.AdminUIDPrefix+req.Username).First(&admin).Error; err != nil || auth.CheckPassword(admin.PasswordHash, req.Password) != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid credentials"})
		return
	}
	token, err := s.sessions.Create(c.Request.Context(), req.Username)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.SetCookie("admin_token", token, s.sessionCookieMaxAge(), "/", "", false, true)
	if s.metrics != nil {
		s.metrics.IncAdminLogins()
	}
	c.JSON(http.StatusOK, gin.H{"username": req.Username})
}

func (s *Server) logout(c *gin.Context) {
	token, _ := c.Cookie("admin_token")
	if token != "" {
		_ = s.sessions.Delete(c.Request.Context(), token)
	}
	c.SetCookie("admin_token", "", -1, "/", "", false, true)
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func (s *Server) me(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"username": c.GetString("admin_username")})
}

func (s *Server) listUsers(c *gin.Context) {
	var users []model.User
	s.db.Where("uid NOT LIKE ? ESCAPE '\\'", "\\_\\_admin\\_\\_:%").Order("id desc").Find(&users)
	items := make([]gin.H, 0, len(users))
	for _, user := range users {
		items = append(items, userPayload(c.Request.Context(), s.quota, user))
	}
	c.JSON(http.StatusOK, gin.H{"items": items})
}

func (s *Server) createUser(c *gin.Context) {
	var req struct {
		UID            string     `json:"uid" binding:"required"`
		Password       string     `json:"password" binding:"required,min=6"`
		Enabled        *bool      `json:"enabled"`
		QuotaBytes     int64      `json:"quota_bytes"`
		MaxConcurrency int        `json:"max_concurrency"`
		ExpiredAt      *time.Time `json:"expired_at"`
		Remark         string     `json:"remark"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if strings.HasPrefix(req.UID, model.AdminUIDPrefix) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "uid uses reserved admin prefix"})
		return
	}
	req.UID = strings.TrimSpace(req.UID)
	if req.UID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "uid is required"})
		return
	}
	if req.QuotaBytes < 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "quota_bytes must be >= 0"})
		return
	}
	if req.MaxConcurrency < 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "max_concurrency must be >= 0"})
		return
	}
	hash, err := auth.HashPassword(req.Password)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}
	user := model.User{UID: req.UID, PasswordHash: hash, Enabled: enabled, QuotaBytes: req.QuotaBytes, MaxConcurrency: req.MaxConcurrency, ExpiredAt: req.ExpiredAt, Remark: req.Remark}
	if err := s.db.Create(&user).Error; err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	s.audit(c, "create_user", "user", strconv.Itoa(int(user.ID)), user.UID)
	c.JSON(http.StatusOK, user)
}

func (s *Server) getUser(c *gin.Context) {
	user, err := s.managedUserByID(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, userPayload(c.Request.Context(), s.quota, *user))
}

func (s *Server) updateUser(c *gin.Context) {
	user, err := s.managedUserByID(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}
	var req map[string]any
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if uid, ok := req["uid"].(string); ok && strings.HasPrefix(uid, model.AdminUIDPrefix) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "uid uses reserved admin prefix"})
		return
	}
	if uid, ok := req["uid"].(string); ok {
		trimmed := strings.TrimSpace(uid)
		if trimmed == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "uid is required"})
			return
		}
		req["uid"] = trimmed
	}
	if quotaBytes, ok := readInt64(req["quota_bytes"]); ok && quotaBytes < 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "quota_bytes must be >= 0"})
		return
	}
	if maxConcurrency, ok := readInt64(req["max_concurrency"]); ok && maxConcurrency < 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "max_concurrency must be >= 0"})
		return
	}
	if password, ok := req["password"].(string); ok && password != "" {
		if len(password) < 6 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "password must be at least 6 characters"})
			return
		}
		hash, _ := auth.HashPassword(password)
		req["password_hash"] = hash
		delete(req, "password")
	}
	if err := s.db.Model(user).Updates(req).Error; err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	s.audit(c, "update_user", "user", c.Param("id"), user.UID)
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func (s *Server) deleteUser(c *gin.Context) {
	user, err := s.managedUserByID(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}
	if err := s.db.Delete(user).Error; err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if s.quota != nil {
		_ = s.quota.CleanupUserRuntime(c.Request.Context(), user.UID)
	}
	s.audit(c, "delete_user", "user", c.Param("id"), "")
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func (s *Server) listSubscriptions(c *gin.Context) {
	type row struct {
		model.Subscription
		NodeCount      int64  `json:"node_count"`
		LastSyncStatus string `json:"last_sync_status"`
		LastSyncDetail string `json:"last_sync_detail"`
	}
	var items []row
	s.db.Model(&model.Subscription{}).
		Select("subscriptions.*, count(subscription_nodes.id) as node_count").
		Joins("left join subscription_nodes on subscription_nodes.subscription_id = subscriptions.id").
		Group("subscriptions.id").
		Order("subscriptions.id desc").
		Scan(&items)
	for index := range items {
		var audit model.AuditLog
		err := s.db.
			Where("target_type = ? AND target_id = ? AND action IN ?", "subscription", strconv.Itoa(int(items[index].ID)), []string{"sync_subscription", "sync_subscription_error"}).
			Order("id desc").
			First(&audit).Error
		if err != nil {
			continue
		}
		if audit.Action == "sync_subscription_error" {
			items[index].LastSyncStatus = "error"
			items[index].LastSyncDetail = audit.Detail
			continue
		}
		items[index].LastSyncStatus = "ok"
		items[index].LastSyncDetail = audit.Detail
	}
	c.JSON(http.StatusOK, gin.H{"items": items})
}

func (s *Server) createSubscription(c *gin.Context) {
	var req struct {
		Name                string `json:"name" binding:"required"`
		Type                string `json:"type"`
		URL                 string `json:"url" binding:"required"`
		Enabled             *bool  `json:"enabled"`
		SyncIntervalSeconds int    `json:"sync_interval_seconds"`
		Remark              string `json:"remark"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if req.Type == "" {
		req.Type = "singbox"
	}
	if !isSupportedSubscriptionType(req.Type) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "unsupported subscription type"})
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	req.URL = strings.TrimSpace(req.URL)
	if req.Name == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "name is required"})
		return
	}
	if req.URL == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "url is required"})
		return
	}
	if req.SyncIntervalSeconds < 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "sync_interval_seconds must be >= 0"})
		return
	}
	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}
	item := model.Subscription{
		Name:                req.Name,
		Type:                strings.ToLower(strings.TrimSpace(req.Type)),
		URL:                 req.URL,
		Enabled:             enabled,
		SyncIntervalSeconds: req.SyncIntervalSeconds,
		Remark:              req.Remark,
	}
	if err := s.db.Create(&item).Error; err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	s.audit(c, "create_subscription", "subscription", strconv.Itoa(int(item.ID)), item.Name)
	c.JSON(http.StatusOK, item)
}

func (s *Server) updateSubscription(c *gin.Context) {
	var req map[string]any
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if rawType, ok := req["type"]; ok {
		typeValue, _ := rawType.(string)
		if !isSupportedSubscriptionType(typeValue) {
			c.JSON(http.StatusBadRequest, gin.H{"error": "unsupported subscription type"})
			return
		}
		req["type"] = strings.ToLower(strings.TrimSpace(typeValue))
	}
	if name, ok := req["name"].(string); ok {
		trimmed := strings.TrimSpace(name)
		if trimmed == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "name is required"})
			return
		}
		req["name"] = trimmed
	}
	if url, ok := req["url"].(string); ok {
		trimmed := strings.TrimSpace(url)
		if trimmed == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "url is required"})
			return
		}
		req["url"] = trimmed
	}
	if syncIntervalSeconds, ok := readInt64(req["sync_interval_seconds"]); ok && syncIntervalSeconds < 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "sync_interval_seconds must be >= 0"})
		return
	}
	if err := s.db.Model(&model.Subscription{}).Where("id = ?", c.Param("id")).Updates(req).Error; err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	s.audit(c, "update_subscription", "subscription", c.Param("id"), "")
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func (s *Server) deleteSubscription(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	if err := s.subscription.Delete(c.Request.Context(), uint(id)); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	s.audit(c, "delete_subscription", "subscription", c.Param("id"), "")
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func (s *Server) syncSubscription(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	syncResult, err := s.subscription.Sync(c.Request.Context(), uint(id))
	if err != nil {
		s.audit(c, "sync_subscription_error", "subscription", c.Param("id"), err.Error())
		c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
		return
	}
	if len(syncResult.ImportedNodeIDs) > 0 {
		go s.healthcheck.CheckNodes(context.Background(), syncResult.ImportedNodeIDs)
	}
	s.audit(c, "sync_subscription", "subscription", c.Param("id"), "")
	c.JSON(http.StatusOK, gin.H{
		"ok":                  true,
		"imported_count":      syncResult.ImportedCount,
		"healthcheck_started": len(syncResult.ImportedNodeIDs) > 0,
	})
}

func (s *Server) listNodes(c *gin.Context) {
	query := s.db.Model(&model.ProxyNode{})
	if v := c.Query("region"); v != "" {
		query = query.Where("detected_region = ? OR expected_region = ?", v, v)
	}
	if v := c.Query("city"); v != "" {
		query = query.Where("city = ?", v)
	}
	if v := c.Query("asn"); v != "" {
		query = query.Where("asn = ?", v)
	}
	if v := c.Query("isp"); v != "" {
		query = query.Where("isp = ? OR org = ?", v, v)
	}
	if v := c.Query("keyword"); v != "" {
		query = query.Where("host like ? OR tag like ? OR exit_ip like ?", "%"+v+"%", "%"+v+"%", "%"+v+"%")
	}
	var items []model.ProxyNode
	query.Order("id desc").Find(&items)
	resp := make([]gin.H, 0, len(items))
	for _, item := range items {
		resp = append(resp, nodePayload(item))
	}
	c.JSON(http.StatusOK, gin.H{"items": resp})
}

func (s *Server) getNode(c *gin.Context) {
	var item model.ProxyNode
	if err := s.db.First(&item, c.Param("id")).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, nodePayload(item))
}

func (s *Server) checkNode(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	if err := s.healthcheck.CheckOne(c.Request.Context(), uint(id)); err != nil {
		s.audit(c, "check_node_error", "node", c.Param("id"), err.Error())
		c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
		return
	}
	s.audit(c, "check_node", "node", c.Param("id"), "")
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func (s *Server) toggleNodeDisabled(c *gin.Context) {
	var req struct {
		Disabled bool `json:"disabled"`
	}
	_ = c.ShouldBindJSON(&req)
	var node model.ProxyNode
	if err := s.db.First(&node, c.Param("id")).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}
	node.Disabled = req.Disabled
	if err := s.db.Save(&node).Error; err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	_ = s.subscription.Store().CacheNode(c.Request.Context(), node)
	s.audit(c, "toggle_node_disabled", "node", c.Param("id"), "")
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func (s *Server) deleteNode(c *gin.Context) {
	var node model.ProxyNode
	if err := s.db.First(&node, c.Param("id")).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}
	_ = s.db.Where("node_id = ?", node.ID).Delete(&model.SubscriptionNode{}).Error
	if err := s.db.Delete(&node).Error; err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	_ = s.subscription.Store().RemoveNode(c.Request.Context(), node)
	s.audit(c, "delete_node", "node", c.Param("id"), "")
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func (s *Server) statsOverview(c *gin.Context) {
	data, err := s.stats.Overview(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, data)
}

func (s *Server) userStats(c *gin.Context) {
	var items []model.User
	s.db.Where("uid NOT LIKE ? ESCAPE '\\'", "\\_\\_admin\\_\\_:%").Order("used_bytes desc").Limit(100).Find(&items)
	c.JSON(http.StatusOK, gin.H{"items": items})
}

func (s *Server) nodeStats(c *gin.Context) {
	var items []model.ProxyNode
	s.db.Order("upload_bytes + download_bytes desc").Limit(100).Find(&items)
	resp := make([]gin.H, 0, len(items))
	for _, item := range items {
		resp = append(resp, nodePayload(item))
	}
	c.JSON(http.StatusOK, gin.H{"items": resp})
}

func (s *Server) trafficStats(c *gin.Context) {
	data, err := s.stats.TrafficSummary()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, data)
}

func (s *Server) listAuditLogs(c *gin.Context) {
	var items []model.AuditLog
	query := s.db.Model(&model.AuditLog{})
	if v := strings.TrimSpace(c.Query("operator")); v != "" {
		query = query.Where("operator = ?", v)
	}
	if v := strings.TrimSpace(c.Query("action")); v != "" {
		query = query.Where("action = ?", v)
	}
	if v := strings.TrimSpace(c.Query("keyword")); v != "" {
		query = query.Where("target_type LIKE ? OR target_id LIKE ? OR detail LIKE ?", "%"+v+"%", "%"+v+"%", "%"+v+"%")
	}
	query.Order("id desc").Limit(200).Find(&items)
	c.JSON(http.StatusOK, gin.H{"items": items})
}

func (s *Server) audit(c *gin.Context, action, targetType, targetID, detail string) {
	_ = s.db.Create(&model.AuditLog{
		Operator:   c.GetString("admin_username"),
		Action:     action,
		TargetType: targetType,
		TargetID:   targetID,
		Detail:     detail,
	}).Error
}

func (s *Server) SyncNow(ctx context.Context, id uint) error {
	_, err := s.subscription.Sync(ctx, id)
	return err
}

func (s *Server) settings(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"server": gin.H{
			"proxy_listen":                          s.cfg.Server.ProxyListen,
			"admin_listen":                          s.cfg.Server.AdminListen,
			"proxy_dial_timeout_seconds":            s.cfg.Server.ProxyDialTimeoutSec,
			"proxy_idle_timeout_seconds":            s.cfg.Server.ProxyIdleTimeoutSec,
			"proxy_response_header_timeout_seconds": s.cfg.Server.ProxyRespTimeoutSec,
			"proxy_connect_timeout_seconds":         s.cfg.Server.ProxyConnectTimeoutS,
		},
		"sqlite": gin.H{
			"path": s.cfg.SQLite.Path,
		},
		"redis": gin.H{
			"addr": s.cfg.Redis.Addr,
			"db":   s.cfg.Redis.DB,
		},
		"healthcheck": gin.H{
			"interval_seconds": s.cfg.Healthcheck.IntervalSeconds,
			"timeout_seconds":  s.cfg.Healthcheck.TimeoutSeconds,
			"max_fail_count":   s.cfg.Healthcheck.MaxFailCount,
		},
		"retry": gin.H{
			"max_attempts":    s.cfg.Retry.MaxAttempts,
			"base_backoff_ms": s.cfg.Retry.BaseBackoffMS,
		},
		"sticky": gin.H{
			"ttl_seconds": s.cfg.Sticky.TTLSeconds,
		},
	})
}

func (s *Server) healthz(c *gin.Context) {
	sqlDB, err := s.db.DB()
	if err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"ok": false, "error": err.Error()})
		return
	}
	if err := sqlDB.PingContext(c.Request.Context()); err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"ok": false, "error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func (s *Server) sessionCookieMaxAge() int {
	ttl := s.cfg.Security.SessionTTL
	if ttl <= 0 {
		return 86400
	}
	return ttl
}

func nodePayload(node model.ProxyNode) gin.H {
	return gin.H{
		"id":              node.ID,
		"node_key":        node.NodeKey,
		"tag":             exitTag(node),
		"raw_tag":         node.Tag,
		"protocol":        node.Protocol,
		"host":            node.Host,
		"port":            node.Port,
		"tls_enabled":     node.TLSEnabled,
		"tls_skip_verify": node.TLSSkipVerify,
		"server_name":     node.ServerName,
		"expected_region": node.ExpectedRegion,
		"detected_region": node.DetectedRegion,
		"city":            node.City,
		"country":         node.Country,
		"asn":             node.ASN,
		"org":             node.Org,
		"isp":             node.ISP,
		"exit_ip":         node.ExitIP,
		"latency_ms":      node.LatencyMS,
		"healthy":         node.Healthy,
		"disabled":        node.Disabled,
		"fail_count":      node.FailCount,
		"last_check_at":   node.LastCheckAt,
		"subscription_id": node.SubscriptionID,
		"raw_json":        sanitizedRawJSON(node.RawJSON),
		"upload_bytes":    node.UploadBytes,
		"download_bytes":  node.DownloadBytes,
		"total_requests":  node.TotalRequests,
		"created_at":      node.CreatedAt,
		"updated_at":      node.UpdatedAt,
	}
}

func exitTag(node model.ProxyNode) string {
	parts := make([]string, 0, 4)
	if node.DetectedRegion != "" {
		parts = append(parts, node.DetectedRegion)
	}
	if node.City != "" {
		parts = append(parts, node.City)
	}
	if node.ASN != "" {
		parts = append(parts, node.ASN)
	}
	if node.ExitIP != "" {
		parts = append(parts, node.ExitIP)
	}
	if len(parts) == 0 {
		return node.Tag
	}
	return strings.Join(parts, " | ")
}

func sanitizedRawJSON(raw string) any {
	if strings.TrimSpace(raw) == "" {
		return raw
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		return raw
	}
	delete(payload, "username")
	delete(payload, "password")
	return payload
}

func userPayload(ctx context.Context, quotaSvc *quota.Service, user model.User) gin.H {
	currentConcurrency := int64(0)
	if quotaSvc != nil {
		currentConcurrency = quotaSvc.CurrentConcurrency(ctx, user.UID)
	}
	return gin.H{
		"id":                  user.ID,
		"uid":                 user.UID,
		"enabled":             user.Enabled,
		"quota_bytes":         user.QuotaBytes,
		"used_bytes":          user.UsedBytes,
		"total_requests":      user.TotalRequests,
		"max_concurrency":     user.MaxConcurrency,
		"current_concurrency": currentConcurrency,
		"expired_at":          user.ExpiredAt,
		"remark":              user.Remark,
		"created_at":          user.CreatedAt,
		"updated_at":          user.UpdatedAt,
	}
}

func (s *Server) managedUserByID(id string) (*model.User, error) {
	var user model.User
	if err := s.db.First(&user, id).Error; err != nil {
		return nil, err
	}
	if strings.HasPrefix(user.UID, model.AdminUIDPrefix) {
		return nil, gorm.ErrRecordNotFound
	}
	return &user, nil
}

func isSupportedSubscriptionType(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "singbox", "shadowrocket", "clash", "mihomo", "surge", "surfboard", "quantumultx":
		return true
	default:
		return false
	}
}

func readInt64(value any) (int64, bool) {
	switch typed := value.(type) {
	case int:
		return int64(typed), true
	case int8:
		return int64(typed), true
	case int16:
		return int64(typed), true
	case int32:
		return int64(typed), true
	case int64:
		return typed, true
	case float32:
		return int64(typed), true
	case float64:
		return int64(typed), true
	default:
		return 0, false
	}
}
