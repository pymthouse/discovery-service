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
