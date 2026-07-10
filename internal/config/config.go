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

	// PublicBaseURL is the externally reachable origin for OpenAPI/Scalar docs
	// derived from Railway's trusted RAILWAY_PUBLIC_DOMAIN setting. It is never
	// derived from request Host headers.
	PublicBaseURL string

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

	RegistryManifestRefreshEnabled   bool
	RegistryManifestTimeoutMs        int64
	RegistryManifestMaxConcurrency   int
	RegistryManifestMaxOrchestrators int

	OrchDiscoveryRefreshEnabled   bool
	OrchDiscoveryTimeoutMs        int64
	OrchDiscoveryMaxConcurrency   int
	OrchDiscoveryMaxOrchestrators int
	OrchDiscoveryExtraURIs        []string

	AIServiceRegistryRPCURL  string
	AIServiceRegistryAddress string
}

// Load reads configuration from environment variables.
func Load() Config {
	refreshMs := envInt64("LEADERBOARD_REFRESH_INTERVAL_MS", 60_000)
	databaseURL := env("DATABASE_URL", "")
	if databaseURL == "" {
		databaseURL = postgresURLFromEnv()
	}

	return Config{
		HTTPAddr:      httpListenAddr(),
		PublicBaseURL: railwayPublicBaseURL(),

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

		RegistryManifestRefreshEnabled:   envBool("REGISTRY_MANIFEST_REFRESH_ENABLED", true),
		RegistryManifestTimeoutMs:        envInt64("REGISTRY_MANIFEST_TIMEOUT_MS", 5000),
		RegistryManifestMaxConcurrency:   envInt("REGISTRY_MANIFEST_MAX_CONCURRENCY", 25),
		RegistryManifestMaxOrchestrators: envInt("REGISTRY_MANIFEST_MAX_ORCHESTRATORS", 1000),

		OrchDiscoveryRefreshEnabled:   envBool("ORCH_DISCOVERY_REFRESH_ENABLED", true),
		OrchDiscoveryTimeoutMs:        envInt64("ORCH_DISCOVERY_TIMEOUT_MS", 5000),
		OrchDiscoveryMaxConcurrency:   envInt("ORCH_DISCOVERY_MAX_CONCURRENCY", 25),
		OrchDiscoveryMaxOrchestrators: envInt("ORCH_DISCOVERY_MAX_ORCHESTRATORS", 1000),
		OrchDiscoveryExtraURIs:        envCSV("ORCH_DISCOVERY_EXTRA_URIS"),

		AIServiceRegistryRPCURL:  env("AI_SERVICE_REGISTRY_RPC_URL", "https://arb1.arbitrum.io/rpc"),
		AIServiceRegistryAddress: env("AI_SERVICE_REGISTRY_ADDRESS", "0x04C0b249740175999E5BF5c9ac1dA92431EF34C5"),
	}
}

func envBool(key string, def bool) bool {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return def
	}
	switch strings.ToLower(v) {
	case "1", "true", "yes", "on":
		return true
	case "0", "false", "no", "off":
		return false
	default:
		return def
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

// railwayPublicBaseURL returns the trusted Railway public origin for API docs.
// Empty string preserves the embedded local-development servers list.
func railwayPublicBaseURL() string {
	domain := strings.TrimSpace(os.Getenv("RAILWAY_PUBLIC_DOMAIN"))
	if domain == "" {
		return ""
	}
	return (&url.URL{Scheme: "https", Host: domain}).String()
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

func envCSV(key string) []string {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return nil
	}
	parts := strings.FieldsFunc(raw, func(r rune) bool {
		return r == ',' || r == '\n' || r == ';'
	})
	out := make([]string, 0, len(parts))
	seen := make(map[string]struct{}, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if _, ok := seen[part]; ok {
			continue
		}
		seen[part] = struct{}{}
		out = append(out, part)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
