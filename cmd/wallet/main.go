package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jackie4an/payment/internal/platform/config"
	"github.com/jackie4an/payment/internal/platform/httpx"
	"github.com/jackie4an/payment/internal/platform/idempotency"
	"github.com/jackie4an/payment/internal/platform/logging"
	"github.com/jackie4an/payment/internal/platform/postgres"
	"github.com/jackie4an/payment/internal/platform/redis"
	"github.com/jackie4an/payment/internal/wallet"
)

// version is stamped at build time: go build -ldflags "-X main.version=v1.2.3"
var version = "dev"

func main() {
	cfg, err := loadConfig(config.New(os.LookupEnv))
	if err != nil {
		fmt.Fprintln(os.Stderr, "wallet: ", err)
		os.Exit(2)
	}

	logger := logging.New(os.Stdout, logging.Options{
		Service: "wallet",
		Version: version,
		Level:   cfg.LogLevel,
		Format:  cfg.LogFormat,
	})

	slog.SetDefault(logger)
	logger.Info("starting", "env", cfg.Env, "log_level", cfg.LogLevel.String())

	pool, err := postgres.NewPool(context.Background(), cfg.DatabaseURL, int32(cfg.DBMaxConns))
	if err != nil {
		logger.Error("connect to postgres", "err", err)
		os.Exit(1)
	}
	logger.Info("connected to postgres", "max_conns", pool.Config().MaxConns)

	// Redis is only a cache in front of Postgres. If it's down, we start
	// anyway, without the cache, and use it as soon as it's back
	rdb, err := redis.NewClient(cfg.RedisURL)
	if err != nil {
		logger.Error("connect to redis", "err", err)
		os.Exit(1)
	}
	if err := rdb.Ping(context.Background()).Err(); err != nil {
		logger.Warn("redis unreachable, idempotency keys use postgres only", "err", err)
	} else {
		logger.Info("connected to redis")
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		httpx.WriteJSON(w, r, http.StatusOK, map[string]string{"status": "ok"})
	})
	mux.HandleFunc("GET /readyz", func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		if err := pool.Ping(ctx); err != nil {
			logger.WarnContext(ctx, "not ready: database unreachable", "err", err)
			httpx.WriteJSON(
				w,
				r,
				http.StatusServiceUnavailable,
				map[string]string{"status": "unavailable"},
			)
			return
		}
		httpx.WriteJSON(w, r, http.StatusOK, map[string]string{"status": "ready"})
	})

	// The wallet API, and a JSON 404 for everything else
	svc := wallet.NewService(wallet.NewStore(pool))
	keys := idempotency.NewStore(pool, rdb)
	wallet.NewHandler(svc, keys).Register(mux)
	mux.HandleFunc("/", httpx.NotFound)

	srv := &http.Server{
		Addr: cfg.HTTPAddr,
		Handler: httpx.Chain(mux,
			httpx.RequestID,
			httpx.AccessLog(logger),
			httpx.Recover(logger),
		),
		ReadHeaderTimeout: 5 * time.Second,
		ErrorLog:          slog.NewLogLogger(logger.Handler(), slog.LevelWarn),
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// Forget expired idempotency keys
	go keys.Cleanup(ctx, time.Hour)

	go func() {
		logger.Info("wallet service listening", "addr", srv.Addr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("server failed", "err", err)
			os.Exit(1)
		}
	}()

	<-ctx.Done()
	logger.Info("shutting down gracefully")

	shutCtx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
	defer cancel()

	if err := srv.Shutdown(shutCtx); err != nil {
		logger.Error("shutdown failed", "err", err)
	}
	logger.Info("server stopped")

	pool.Close()
	rdb.Close()
	logger.Info("database pool and redis client closed")
}
