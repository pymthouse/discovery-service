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

func main() {
	loadEnvFile()

	cfg := config.Load()
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	store, err := db.New(ctx, cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("database: %v", err)
	}
	defer store.Close()

	registry := sources.NewRegistry(
		sources.NewSubgraph(cfg),
		sources.NewClickHouse(cfg),
		sources.NewDiscover(cfg),
		sources.NewPricing(cfg),
		sources.NewRemoteSigner(cfg),
	)

	ref := refresh.New(cfg, store, registry)
	q := query.New(store, cfg.MaxTopN)

	var versionFn func() int64
	versionFn = func() int64 {
		meta, err := store.GetConfig(context.Background())
		if err != nil || meta.LastRefreshedAt == nil {
			return 0
		}
		return meta.DatasetVersion
	}

	cacheLayer, err := cache.New(cfg.QueryCacheTTL, cfg.RedisURL, versionFn)
	if err != nil {
		log.Fatalf("cache: %v", err)
	}
	defer cacheLayer.Close()

	srv := httpapi.New(cfg, store, ref, q, cacheLayer)

	if os.Getenv("REFRESH_ON_STARTUP") == "true" {
		go func() {
			stats, err := store.GetStats(ctx)
			if err != nil {
				return
			}
			needsRefresh := !stats.Populated
			if stats.RefreshedAt != nil {
				needsRefresh = time.Since(*stats.RefreshedAt) > cfg.RefreshInterval
			}
			if needsRefresh {
				log.Println("startup refresh...")
				if _, err := ref.Run(ctx, "startup"); err != nil {
					log.Printf("startup refresh failed: %v", err)
				} else {
					cacheLayer.InvalidateAll()
				}
			}
		}()
	}

	log.Printf("discovery-service listening on %s", cfg.HTTPAddr)
	if err := httpapi.ListenAndServe(ctx, cfg.HTTPAddr, srv.Handler()); err != nil {
		log.Fatalf("server: %v", err)
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
