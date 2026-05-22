package config

import (
	"os"
	"strconv"
	"strings"
	"time"
)

// Config holds service environment configuration.
type Config struct {
	HTTPAddr string

	DatabaseURL string
	RedisURL    string

	CronSecret string

	RefreshInterval    time.Duration
	MembershipStrategy string

	ClickHouseURL        string
	ClickHouseUser       string
	ClickHousePassword   string
	ClickHouseGatewayURL string

	SubgraphURL string
	SubgraphID  string

	DiscoverAPIURL string
	PricingAPIURL  string

	RemoteSignerURL string

	QueryCacheTTL time.Duration
	MaxTopN       int
}

// Load reads configuration from environment variables.
func Load() Config {
	refreshMs := envInt64("LEADERBOARD_REFRESH_INTERVAL_MS", 60_000)
	return Config{
		HTTPAddr: env("HTTP_ADDR", ":8088"),

		DatabaseURL: env("DATABASE_URL", ""),
		RedisURL:    env("REDIS_URL", ""),

		CronSecret: env("CRON_SECRET", ""),

		RefreshInterval:    time.Duration(refreshMs) * time.Millisecond,
		MembershipStrategy: env("MEMBERSHIP_STRATEGY", "union"),

		ClickHouseURL:        env("CLICKHOUSE_URL", ""),
		ClickHouseUser:       env("CLICKHOUSE_USER", ""),
		ClickHousePassword:   env("CLICKHOUSE_PASSWORD", ""),
		ClickHouseGatewayURL: env("CLICKHOUSE_GATEWAY_URL", ""),

		SubgraphURL: env("SUBGRAPH_URL", "https://api.thegraph.com"),
		SubgraphID:  env("SUBGRAPH_ID", "FE63YgkzcpVocxdCEyEYbvjYqEf2kb1A6daMYRxmejYC"),

		DiscoverAPIURL: env("DISCOVER_API_URL", "https://naap-api.cloudspe.com/v1/discover/orchestrators"),
		PricingAPIURL:  env("PRICING_API_URL", ""),

		RemoteSignerURL: env("REMOTE_SIGNER_URL", ""),

		QueryCacheTTL: time.Duration(envInt64("QUERY_CACHE_TTL_MS", 120_000)) * time.Millisecond,
		MaxTopN:       envInt("MAX_TOP_N", 1000),
	}
}

func env(key, def string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return def
}

func envInt(key string, def int) int {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}

func envInt64(key string, def int64) int64 {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil {
			return n
		}
	}
	return def
}
