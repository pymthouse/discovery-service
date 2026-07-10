package sources

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/livepeer/discovery-service/internal/config"
)

func TestOrchDiscoveryURL(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"https://orch.example:8935", "https://orch.example:8935/discovery"},
		{"https://orch.example:8935/", "https://orch.example:8935/discovery"},
		{"https://orch.example:8935/ai", "https://orch.example:8935/ai/discovery"},
		{"https://orch.example:8935/ai/", "https://orch.example:8935/ai/discovery"},
		{"", ""},
		{"not-a-url", ""},
	}
	for _, tt := range tests {
		got := OrchDiscoveryURL(tt.in)
		if got != tt.want {
			t.Fatalf("OrchDiscoveryURL(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestParseOrchDiscoveryBody(t *testing.T) {
	body := []byte(`[
		{
			"address": "https://orch.example:8935",
			"runners": [
				{"url": "https://runner.example/a", "app": "transcode/ffmpeg"},
				{"url": "https://runner.example/b", "app": "transcode/ffmpeg"},
				{"url": "", "app": "ignored"},
				{"url": "https://runner.example/c", "app": "vllm/llama"}
			]
		}
	]`)
	got := ParseOrchDiscoveryBody(body, "https://fallback.example")
	if len(got) != 2 {
		t.Fatalf("got %d claims, want 2: %#v", len(got), got)
	}
	if got[0].OrchURI != "https://orch.example:8935" || got[0].App != "transcode/ffmpeg" {
		t.Fatalf("unexpected first claim: %#v", got[0])
	}
	if got[1].App != "vllm/llama" {
		t.Fatalf("unexpected second claim: %#v", got[1])
	}
}

func TestParseOrchDiscoveryBodyFallbackAddress(t *testing.T) {
	body := []byte(`[{"runners":[{"url":"https://r","app":"transcode/ffmpeg"}]}]`)
	got := ParseOrchDiscoveryBody(body, "https://fallback.example/")
	if len(got) != 1 || got[0].OrchURI != "https://fallback.example" {
		t.Fatalf("expected fallback orch URI, got %#v", got)
	}
}

func TestCollectOrchURIs(t *testing.T) {
	perSource := map[Kind][]NormalizedOrch{
		KindSubgraph: {
			{OrchURI: "https://a.example/"},
			{OrchURI: "https://a.example"},
			{OrchURI: ""},
		},
		KindClickHouse: {
			{OrchURI: "https://b.example"},
			{OrchURI: "https://c.example"},
		},
	}
	got := CollectOrchURIs(perSource, 2)
	if len(got) != 2 {
		t.Fatalf("expected max 2, got %#v", got)
	}
}

func TestMergeLiveRunnerAppClaimsPrefersProbe(t *testing.T) {
	preferred := []LiveRunnerAppClaim{{OrchURI: "https://a", App: "transcode/ffmpeg", Score: 1}}
	fallback := []LiveRunnerAppClaim{
		{OrchURI: "https://a", App: "transcode/ffmpeg", Score: 2},
		{OrchURI: "https://b", App: "vllm/llama", Score: 1},
	}
	got := MergeLiveRunnerAppClaims(preferred, fallback)
	if len(got) != 2 {
		t.Fatalf("got %#v", got)
	}
	if got[0].Score != 1 {
		t.Fatalf("preferred claim should win: %#v", got[0])
	}
	if got[1].OrchURI != "https://b" {
		t.Fatalf("expected fallback-only claim: %#v", got[1])
	}
}

func TestProbeOptionsFromConfig(t *testing.T) {
	got := ProbeOptionsFromConfig(config.Config{
		OrchDiscoveryTimeoutMs:      1234,
		OrchDiscoveryMaxConcurrency: 7,
	})
	if got.TimeoutMs != 1234 || got.MaxConcurrency != 7 {
		t.Fatalf("unexpected options: %#v", got)
	}
}

func TestProbeOrchDiscoveryCollectsClaimsAndSoftFails(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		switch r.URL.Path {
		case "/ok/discovery":
			_, _ = fmt.Fprintf(w, `[{"address":"http://%s/ok","runners":[{"url":"http://runner/a","app":"transcode/ffmpeg"}]}]`, r.Host)
		case "/empty/discovery":
			_, _ = w.Write([]byte(`[{"address":"http://empty","runners":[]}]`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	claims, stats := ProbeOrchDiscovery(context.Background(), []string{
		srv.URL + "/ok",
		srv.URL + "/empty",
		srv.URL + "/missing",
		"not-a-url",
	}, ProbeOrchDiscoveryOptions{TimeoutMs: 2000, MaxConcurrency: 2})
	if hits.Load() != 3 {
		t.Fatalf("expected 3 HTTP probes, got %d", hits.Load())
	}
	if len(claims) != 1 || claims[0].App != "transcode/ffmpeg" {
		t.Fatalf("unexpected claims: %#v", claims)
	}
	if stats.Fetched != 1 || stats.ErrorMessage != "" {
		t.Fatalf("unexpected stats: %#v", stats)
	}
}

func TestProbeOrchDiscoveryAllFailedMessage(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "down", http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	claims, stats := ProbeOrchDiscovery(context.Background(), []string{
		srv.URL + "/a",
		srv.URL + "/b",
	}, ProbeOrchDiscoveryOptions{TimeoutMs: 1000, MaxConcurrency: 2})
	if len(claims) != 0 {
		t.Fatalf("expected no claims, got %#v", claims)
	}
	if stats.ErrorMessage == "" {
		t.Fatal("expected error message when all probes fail")
	}
}

func TestProbeOrchDiscoveryDefaults(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	claims, stats := ProbeOrchDiscovery(ctx, nil, ProbeOrchDiscoveryOptions{})
	if claims != nil || stats.Fetched != 0 || !stats.OK {
		t.Fatalf("empty input should short-circuit: claims=%#v stats=%#v", claims, stats)
	}
}

func TestProbeOrchDiscoveryAcceptsInvalidTLS(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/discovery" {
			http.NotFound(w, r)
			return
		}
		_, _ = fmt.Fprintf(w, `[{"address":"%s","runners":[{"url":"http://runner/a","app":"transcode/ffmpeg"}]}]`, strings.TrimRight(srvURL(r), "/"))
	}))
	defer srv.Close()

	claims, stats := ProbeOrchDiscovery(context.Background(), []string{
		srv.URL,
	}, ProbeOrchDiscoveryOptions{TimeoutMs: 2000, MaxConcurrency: 1})
	if len(claims) != 1 || claims[0].App != "transcode/ffmpeg" {
		t.Fatalf("expected claim despite invalid TLS, got %#v (stats=%#v)", claims, stats)
	}
	if stats.ErrorMessage != "" {
		t.Fatalf("unexpected probe error: %s", stats.ErrorMessage)
	}
}

// srvURL rebuilds the request's base URL for JSON address fields in TLS tests.
func srvURL(r *http.Request) string {
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	return scheme + "://" + r.Host
}
