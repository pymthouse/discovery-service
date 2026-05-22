package config

import (
	"net"
	"net/url"
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
	databaseURL := env("DATABASE_URL", "")
	if databaseURL == "" {
		databaseURL = postgresURLFromEnv()
	}

	return Config{
		HTTPAddr: httpListenAddr(),

		DatabaseURL: databaseURL,
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

func postgresURLFromEnv() string {
	password := strings.TrimSpace(os.Getenv("DISCOVERY_PG_PASSWORD"))
	if password == "" {
		return ""
	}

	sslMode := env("DISCOVERY_PG_SSLMODE", "disable")
	u := url.URL{
		Scheme: "postgres",
		User: url.UserPassword(
			env("DISCOVERY_PG_USER", "discovery"),
			password,
		),
		Host:     net.JoinHostPort(env("DISCOVERY_PG_HOST", "localhost"), env("DISCOVERY_PG_PORT", "5432")),
		Path:     "/" + env("DISCOVERY_PG_DB", "discovery"),
		RawQuery: "sslmode=" + url.QueryEscape(sslMode),
	}
	return u.String()
}

// httpListenAddr prefers HTTP_ADDR, then Railway/cloud PORT, then :8088.
func httpListenAddr() string {
	if addr := strings.TrimSpace(os.Getenv("HTTP_ADDR")); addr != "" {
		return addr
	}
	if port := strings.TrimSpace(os.Getenv("PORT")); port != "" {
		return ":" + port
	}
	return ":8088"
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
