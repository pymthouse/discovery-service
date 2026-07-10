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

func TestResolvePublicBaseURLPrefersExplicit(t *testing.T) {
	t.Setenv("PUBLIC_BASE_URL", "https://discovery.example.com/")
	t.Setenv("RAILWAY_PUBLIC_DOMAIN", "ignored.up.railway.app")

	cfg := Load()
	if cfg.PublicBaseURL != "https://discovery.example.com" {
		t.Fatalf("PublicBaseURL = %q, want https://discovery.example.com", cfg.PublicBaseURL)
	}
}

func TestResolvePublicBaseURLUsesRailwayDomain(t *testing.T) {
	t.Setenv("PUBLIC_BASE_URL", "")
	t.Setenv("RAILWAY_PUBLIC_DOMAIN", "discovery-us.up.railway.app")

	cfg := Load()
	if cfg.PublicBaseURL != "https://discovery-us.up.railway.app" {
		t.Fatalf("PublicBaseURL = %q, want railway https origin", cfg.PublicBaseURL)
	}
}

func TestResolvePublicBaseURLEmptyWhenUnset(t *testing.T) {
	t.Setenv("PUBLIC_BASE_URL", "")
	t.Setenv("RAILWAY_PUBLIC_DOMAIN", "")

	cfg := Load()
	if cfg.PublicBaseURL != "" {
		t.Fatalf("PublicBaseURL = %q, want empty for local default", cfg.PublicBaseURL)
	}
}

func TestNormalizePublicBaseURLRejectsUnsafe(t *testing.T) {
	cases := map[string]string{
		"https://ok.example.com":                "https://ok.example.com",
		"http://localhost:8088":                 "http://localhost:8088",
		"https://user:pass@evil.example.com":    "",
		"ftp://files.example.com":               "",
		"not-a-url":                             "",
		"https://ok.example.com/api/v1/":        "https://ok.example.com/api/v1",
		"  https://ok.example.com  ":            "https://ok.example.com",
	}
	for in, want := range cases {
		if got := normalizePublicBaseURL(in); got != want {
			t.Fatalf("normalizePublicBaseURL(%q) = %q, want %q", in, got, want)
		}
	}
}
