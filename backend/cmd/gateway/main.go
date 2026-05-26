package main

import (
	"context"
	"flag"
	"net/http"
	"os/signal"
	"syscall"
	"time"

	"proxydeck/backend/internal/adminapi"
	"proxydeck/backend/internal/auth"
	"proxydeck/backend/internal/config"
	"proxydeck/backend/internal/db"
	"proxydeck/backend/internal/healthcheck"
	"proxydeck/backend/internal/logger"
	"proxydeck/backend/internal/metrics"
	"proxydeck/backend/internal/proxy"
	"proxydeck/backend/internal/quota"
	"proxydeck/backend/internal/redisstore"
	"proxydeck/backend/internal/retry"
	"proxydeck/backend/internal/selector"
	"proxydeck/backend/internal/stats"
	"proxydeck/backend/internal/subscription"

	"go.uber.org/zap"
)

func main() {
	configPath := flag.String("config", "./backend/configs/config.yaml", "config path")
	flag.Parse()
	cfg, err := config.Load(*configPath)
	if err != nil {
		panic(err)
	}
	log, err := logger.New()
	if err != nil {
		panic(err)
	}
	defer log.Sync()
	sqliteDB, err := db.Open(cfg.SQLite.Path)
	if err != nil {
		panic(err)
	}
	redis := redisstore.New(cfg.Redis.Addr, cfg.Redis.Password, cfg.Redis.DB)
	if err := redis.Client.Ping(context.Background()).Err(); err != nil {
		panic(err)
	}
	if err := auth.EnsureDefaultAdmin(sqliteDB, cfg.Admin.Username, cfg.Admin.Password); err != nil {
		panic(err)
	}
	cipher, err := auth.NewCipher(cfg.Security.EncryptionKey)
	if err != nil {
		panic(err)
	}
	quotaSvc := quota.NewService(sqliteDB, redis)
	statsSvc := stats.NewService(sqliteDB, redis)
	metricsSvc := metrics.New()
	retryCfg := retry.Config{MaxAttempts: cfg.Retry.MaxAttempts, BaseBackoff: cfg.RetryBaseBackoff()}
	selectorSvc := selector.NewService(sqliteDB, redis, cfg.StickyTTL())
	subscriptionSvc := subscription.NewService(sqliteDB, redis, cipher, &http.Client{Timeout: 20 * time.Second}, retryCfg, metricsSvc)
	healthSvc := healthcheck.NewService(sqliteDB, redis, cipher, cfg.HealthcheckTimeout(), cfg.Healthcheck.MaxFailCount, retryCfg, metricsSvc, log)
	adminServer := adminapi.New(sqliteDB, auth.NewAdminSessionManager(redis, cfg.SessionTTL()), statsSvc, subscriptionSvc, healthSvc, quotaSvc, metricsSvc, cfg)
	gateway := proxy.NewGateway(
		quotaSvc,
		selectorSvc,
		statsSvc,
		cipher,
		metricsSvc,
		log,
		cfg.ProxyDialTimeout(),
		cfg.ProxyIdleTimeout(),
		cfg.ProxyResponseHeaderTimeout(),
		cfg.ProxyConnectTimeout(),
	)

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()
	go statsSvc.StartFlushLoop(ctx)
	go healthSvc.Start(ctx, cfg.HealthcheckInterval())
	go subscriptionSvc.Start(ctx, time.Minute)

	adminHTTP := &http.Server{
		Addr:              cfg.Server.AdminListen,
		Handler:           adminServer.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
	}
	proxyHTTP := &http.Server{
		Addr:              cfg.Server.ProxyListen,
		Handler:           gateway.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
	}
	go serve(log, "admin", adminHTTP)
	go serve(log, "proxy", proxyHTTP)
	<-ctx.Done()
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()
	_ = adminHTTP.Shutdown(shutdownCtx)
	_ = proxyHTTP.Shutdown(shutdownCtx)
}

func serve(log *zap.Logger, name string, server *http.Server) {
	log.Info("server starting", zap.String("name", name), zap.String("addr", server.Addr))
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatal("server failed", zap.String("name", name), zap.Error(err))
	}
}
