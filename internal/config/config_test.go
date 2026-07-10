package config

import (
	"net/url"
	"testing"
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
