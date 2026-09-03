package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/joho/godotenv"
	"github.com/livepeer/discovery-service/internal/cache"
	"github.com/livepeer/discovery-service/internal/config"
	"github.com/livepeer/discovery-service/internal/db"
	"github.com/livepeer/discovery-service/internal/httpapi"
	"github.com/livepeer/discovery-service/internal/query"
	"github.com/livepeer/discovery-service/internal/refresh"
	"github.com/livepeer/discovery-service/internal/sources"
)

const internalRefreshAdvisoryLockKey int64 = 78008001

func main() {
	loadEnvFile()

	cfg := config.Load()
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	store, err := openStore(ctx, cfg)
	if err != nil {
		log.Fatalf("database: %v", err)
	}
	defer store.Close()

	registry := buildSourceRegistry(cfg)
	ref := refresh.New(cfg, store, registry)
	q := query.New(store, cfg.MaxTopN)

	versionFn := datasetVersionFn(store)
	cacheLayer, err := openCache(cfg, versionFn)
	if err != nil {
		log.Fatalf("cache: %v", err)
	}
	defer closeCache(cacheLayer)

	srv := httpapi.New(cfg, store, ref, q, cacheLayer)

	if os.Getenv("REFRESH_ON_STARTUP") == "true" {
		go refreshOnStartup(ctx, cfg, store, ref, cacheLayer)
	}
	go internalRefreshLoop(ctx, cfg, store, ref, cacheLayer)

	log.Printf("discovery-service listening on %s", cfg.HTTPAddr)
	if err := httpapi.ListenAndServe(ctx, cfg.HTTPAddr, srv.Handler()); err != nil {
		log.Fatalf("server: %v", err)
	}
}

func openStore(ctx context.Context, cfg config.Config) (*db.Store, error) {
	return db.New(ctx, cfg.DatabaseURL)
}

func buildSourceRegistry(cfg config.Config) *sources.Registry {
	return sources.NewRegistry(
		sources.NewSubgraph(cfg),
		sources.NewServiceRegistry(cfg),
		sources.NewRegistryManifest(cfg),
		sources.NewAIRegistryManifest(cfg),
		sources.NewClickHouse(cfg),
		sources.NewDiscover(cfg),
		sources.NewPricing(cfg),
		sources.NewRemoteSigner(cfg),
	)
}

func datasetVersionFn(store *db.Store) func() int64 {
	return func() int64 {
		meta, err := store.GetConfig(context.Background())
		if err != nil || meta.LastRefreshedAt == nil {
			return 0
		}
		return meta.DatasetVersion
	}
}

func openCache(cfg config.Config, versionFn func() int64) (*cache.Layer, error) {
	return cache.New(cfg.QueryCacheTTL, cfg.RedisURL, versionFn)
}

func closeCache(cacheLayer *cache.Layer) {
	if err := cacheLayer.Close(); err != nil {
		log.Printf("cache close: %v", err)
	}
}

func refreshOnStartup(
	ctx context.Context,
	cfg config.Config,
	store *db.Store,
	ref *refresh.Service,
	cacheLayer *cache.Layer,
) {
	stats, err := store.GetStats(ctx)
	if err != nil {
		return
	}
	needsRefresh := !stats.Populated
	if stats.RefreshedAt != nil {
		needsRefresh = time.Since(*stats.RefreshedAt) > cfg.RefreshInterval
	}
	if !needsRefresh {
		return
	}
	log.Println("startup refresh...")
	if _, err := ref.Run(ctx, "startup"); err != nil {
		log.Printf("startup refresh failed: %v", err)
		return
	}
	cacheLayer.InvalidateAll()
}

func internalRefreshLoop(
	ctx context.Context,
	cfg config.Config,
	store *db.Store,
	ref *refresh.Service,
	cacheLayer *cache.Layer,
) {
	interval := cfg.InternalRefreshEvery
	if interval <= 0 {
		log.Printf("invalid INTERNAL_REFRESH_INTERVAL_MS %s; using 1h", interval)
		interval = time.Hour
	}

	log.Printf("internal refresh scheduler enabled: every %s", interval)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			locked, err := store.WithAdvisoryLock(ctx, internalRefreshAdvisoryLockKey, func(runCtx context.Context) error {
				log.Println("internal scheduled refresh...")
				if _, err := ref.Run(runCtx, "internal-cron"); err != nil {
					return err
				}
				cacheLayer.InvalidateAll()
				return nil
			})
			if err != nil {
				log.Printf("internal scheduled refresh failed: %v", err)
				continue
			}
			if !locked {
				log.Println("internal scheduled refresh skipped: another replica is refreshing")
			}
		}
	}
}

// loadEnvFile loads .env from the current directory (repo root when using go run).
// Existing shell environment variables take precedence over .env values.
func loadEnvFile() {
	candidates := []string{".env"}
	if wd, err := os.Getwd(); err == nil {
		candidates = append(candidates, filepath.Join(wd, ".env"))
	}
	for _, path := range candidates {
		if err := godotenv.Load(path); err == nil {
			return
		}
	}
}
