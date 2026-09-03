package config

import (
	"net/url"
	"testing"
	"time"
)

func TestLoadUsesExplicitDatabaseURL(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://explicit.example/discovery")
	t.Setenv("DISCOVERY_PG_PASSWORD", "ignored")

	cfg := Load()

	if cfg.DatabaseURL != "postgres://explicit.example/discovery" {
		t.Fatalf("expected explicit DATABASE_URL, got %q", cfg.DatabaseURL)
	}
}

func TestLoadBuildsPostgresURLFromDiscreteEnv(t *testing.T) {
	const password = "pw"

	t.Setenv("DATABASE_URL", "")
	t.Setenv("DISCOVERY_PG_USER", "discovery")
	t.Setenv("DISCOVERY_PG_PASSWORD", password)
	t.Setenv("DISCOVERY_PG_HOST", "postgres")
	t.Setenv("DISCOVERY_PG_PORT", "5432")
	t.Setenv("DISCOVERY_PG_DB", "discovery")

	cfg := Load()
	got, err := url.Parse(cfg.DatabaseURL)
	if err != nil {
		t.Fatalf("expected valid DATABASE_URL, got %q: %v", cfg.DatabaseURL, err)
	}

	gotPassword, _ := got.User.Password()
	if got.Scheme != "postgres" ||
		got.User.Username() != "discovery" ||
		gotPassword != password ||
		got.Host != "postgres:5432" ||
		got.Path != "/discovery" ||
		got.Query().Get("sslmode") != "disable" {
		t.Fatalf("unexpected built DATABASE_URL: %q", cfg.DatabaseURL)
	}
}

func TestLoadUsesRailwayPublicDomain(t *testing.T) {
	t.Setenv("RAILWAY_PUBLIC_DOMAIN", "discovery-us.up.railway.app")

	cfg := Load()
	if cfg.PublicBaseURL != "https://discovery-us.up.railway.app" {
		t.Fatalf("PublicBaseURL = %q, want railway https origin", cfg.PublicBaseURL)
	}
}

func TestLoadLeavesPublicBaseURLEmptyOutsideRailway(t *testing.T) {
	t.Setenv("RAILWAY_PUBLIC_DOMAIN", "")

	cfg := Load()
	if cfg.PublicBaseURL != "" {
		t.Fatalf("PublicBaseURL = %q, want empty for local default", cfg.PublicBaseURL)
	}
}

func TestLoadOrchDiscoveryExtraURIs(t *testing.T) {
	t.Setenv("ORCH_DISCOVERY_EXTRA_URIS", "http://154.61.61.108:8787, https://kiloutcorp.link:11111;,http://154.61.61.108:8787")

	cfg := Load()
	want := []string{"http://154.61.61.108:8787", "https://kiloutcorp.link:11111"}
	if len(cfg.OrchDiscoveryExtraURIs) != len(want) {
		t.Fatalf("OrchDiscoveryExtraURIs = %#v, want %#v", cfg.OrchDiscoveryExtraURIs, want)
	}
	for i := range want {
		if cfg.OrchDiscoveryExtraURIs[i] != want[i] {
			t.Fatalf("OrchDiscoveryExtraURIs[%d] = %q, want %q", i, cfg.OrchDiscoveryExtraURIs[i], want[i])
		}
	}
}

func TestLoadOrchDiscoveryExtraURIsEmpty(t *testing.T) {
	t.Setenv("ORCH_DISCOVERY_EXTRA_URIS", "")

	cfg := Load()
	if cfg.OrchDiscoveryExtraURIs != nil {
		t.Fatalf("OrchDiscoveryExtraURIs = %#v, want nil", cfg.OrchDiscoveryExtraURIs)
	}
}

func TestLoadInternalRefreshIntervalDefaults(t *testing.T) {
	t.Setenv("INTERNAL_REFRESH_INTERVAL_MS", "")

	cfg := Load()
	if cfg.InternalRefreshEvery != time.Hour {
		t.Fatalf("InternalRefreshEvery = %s, want %s", cfg.InternalRefreshEvery, time.Hour)
	}
}

func TestLoadInternalRefreshIntervalOverride(t *testing.T) {
	t.Setenv("INTERNAL_REFRESH_INTERVAL_MS", "1800000")

	cfg := Load()
	if cfg.InternalRefreshEvery != 30*time.Minute {
		t.Fatalf("InternalRefreshEvery = %s, want %s", cfg.InternalRefreshEvery, 30*time.Minute)
	}
}

func TestLoadServiceRegistryDefaults(t *testing.T) {
	t.Setenv("SERVICE_REGISTRY_REFRESH_ENABLED", "")
	t.Setenv("SERVICE_REGISTRY_ADDRESS", "")
	t.Setenv("BONDING_MANAGER_ADDRESS", "")

	cfg := Load()
	if !cfg.ServiceRegistryRefreshEnabled {
		t.Fatal("ServiceRegistryRefreshEnabled should default on")
	}
	if cfg.ServiceRegistryAddress != "0xC92d3A360b8f9e083bA64DE15d95Cf8180897431" {
		t.Fatalf("ServiceRegistryAddress = %q", cfg.ServiceRegistryAddress)
	}
	if cfg.BondingManagerAddress != "0x35Bcf3c30594191d53231E4FF333E8A770453e40" {
		t.Fatalf("BondingManagerAddress = %q", cfg.BondingManagerAddress)
	}
}

func TestLoadServiceRegistryOverride(t *testing.T) {
	t.Setenv("SERVICE_REGISTRY_REFRESH_ENABLED", "false")
	t.Setenv("SERVICE_REGISTRY_ADDRESS", "0xabc")
	t.Setenv("BONDING_MANAGER_ADDRESS", "0xdef")

	cfg := Load()
	if cfg.ServiceRegistryRefreshEnabled {
		t.Fatal("ServiceRegistryRefreshEnabled should be false")
	}
	if cfg.ServiceRegistryAddress != "0xabc" || cfg.BondingManagerAddress != "0xdef" {
		t.Fatalf("unexpected override: %#v %#v", cfg.ServiceRegistryAddress, cfg.BondingManagerAddress)
	}
}
