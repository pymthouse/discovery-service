package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/livepeer/discovery-service/internal/cache"
	"github.com/livepeer/discovery-service/internal/config"
	"github.com/livepeer/discovery-service/internal/db"
	"github.com/livepeer/discovery-service/pkg/discotypes"
)

func TestNormalizeLegacyCapsKeepsExactAndStripped(t *testing.T) {
	got := normalizeLegacyCaps(
		[]string{"live-video-to-video/streamdiffusion-sdxl", "streamdiffusion-sdxl"},
		[]string{"live-video-to-video"},
	)
	want := []string{"live-video-to-video/streamdiffusion-sdxl", "streamdiffusion-sdxl"}
	if len(got) != len(want) {
		t.Fatalf("got %d caps, want %d: %#v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("caps[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestNormalizeLegacyCapsKeepsLiveRunnerApp(t *testing.T) {
	got := normalizeLegacyCaps(
		[]string{"transcode/ffmpeg"},
		[]string{"live-runner"},
	)
	want := []string{"transcode/ffmpeg", "ffmpeg"}
	if len(got) != len(want) {
		t.Fatalf("got %#v, want %#v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("caps[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestNormalizeLegacyCapsLeavesModulesUntouched(t *testing.T) {
	in := []string{"daydream:scope/v1"}
	got := normalizeLegacyCaps(in, []string{"modules"})
	if len(got) != 1 || got[0] != in[0] {
		t.Fatalf("modules caps were modified: %#v", got)
	}
}

func TestNormalizeLegacyCapsDefaultServiceTypesKeepExactAndStripped(t *testing.T) {
	got := normalizeLegacyCaps(
		[]string{"live-video-to-video/streamdiffusion-sdxl"},
		nil,
	)
	want := []string{"live-video-to-video/streamdiffusion-sdxl", "streamdiffusion-sdxl"}
	if len(got) != len(want) {
		t.Fatalf("got %#v, want %#v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("caps[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestRewriteOpenAPIServersReplacesBlock(t *testing.T) {
	spec := []byte(`openapi: 3.1.0
info:
  title: Test
servers:
  - url: http://localhost:8088
    description: Local development
  - url: https://discovery.example.com
    description: Production
paths:
  /healthz:
    get:
      summary: ok
`)
	got := string(rewriteOpenAPIServers(spec, "https://discovery-us.up.railway.app"))
	if strings.Contains(got, "localhost") {
		t.Fatalf("localhost still present:\n%s", got)
	}
	if !strings.Contains(got, `url: "https://discovery-us.up.railway.app"`) {
		t.Fatalf("public URL missing:\n%s", got)
	}
	if !strings.Contains(got, "paths:") {
		t.Fatalf("paths section lost:\n%s", got)
	}
}

func TestServeOpenAPIUsesPublicBaseURLNotHostHeader(t *testing.T) {
	s := &Server{cfg: config.Config{PublicBaseURL: "https://trusted.example.com"}}
	req := httptest.NewRequest(http.MethodGet, "/openapi.yaml", nil)
	req.Host = "evil.attacker.example"
	req.Header.Set("X-Forwarded-Host", "evil.attacker.example")
	req.Header.Set("X-Forwarded-Proto", "https")
	rr := httptest.NewRecorder()

	s.serveOpenAPI(rr, req)

	body := rr.Body.String()
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d", rr.Code)
	}
	if strings.Contains(body, "evil.attacker.example") {
		t.Fatalf("request Host leaked into OpenAPI servers:\n%s", body)
	}
	if !strings.Contains(body, `url: "https://trusted.example.com"`) {
		t.Fatalf("trusted PublicBaseURL missing:\n%s", body)
	}
	if strings.Contains(body, "localhost:8088") {
		t.Fatalf("localhost still present when PublicBaseURL set:\n%s", body)
	}
}

func TestServeOpenAPIKeepsLocalhostWhenUnset(t *testing.T) {
	s := &Server{cfg: config.Config{}}
	req := httptest.NewRequest(http.MethodGet, "/openapi.yaml", nil)
	rr := httptest.NewRecorder()

	s.serveOpenAPI(rr, req)

	body := rr.Body.String()
	if !strings.Contains(body, "http://localhost:8088") {
		t.Fatalf("expected embedded localhost server when PublicBaseURL unset:\n%s", body)
	}
}

func TestRawCapabilityFilterOmitsStoreWhenUnfiltered(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/v1/discovery/raw", nil)
	if got := rawCapabilityFilter(req); got != nil {
		t.Fatalf("unfiltered caps = %#v", got)
	}
}

func TestRawCapabilityFilterExpandsLiveRunner(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/v1/discovery/raw?caps=transcode/ffmpeg", nil)
	got := rawCapabilityFilter(req)
	want := []string{"transcode/ffmpeg", "ffmpeg"}
	if len(got) != len(want) {
		t.Fatalf("got %#v, want %#v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("caps[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestGroupRawOrchestratorsMergesCapsAndDefaultScore(t *testing.T) {
	byAddr := groupRawOrchestrators([]db.FlatRow{
		{OrchURI: "https://a.example", Capability: "transcode/ffmpeg", Score: 0},
		{OrchURI: "https://a.example", Capability: "comfystream", Score: 0},
		{OrchURI: "https://a.example", Capability: "transcode/ffmpeg", Score: 9},
		{OrchURI: "https://b.example", Capability: "streamdiffusion", Score: 2.5},
	})
	a := byAddr["https://a.example"]
	if a == nil || a.score != 1 {
		t.Fatalf("zero score should become 1: %#v", a)
	}
	if len(a.caps) != 2 || a.caps[0] != "transcode/ffmpeg" || a.caps[1] != "comfystream" {
		t.Fatalf("merged caps = %#v", a.caps)
	}
	b := byAddr["https://b.example"]
	if b == nil || b.score != 2.5 {
		t.Fatalf("b = %#v", b)
	}
}

func TestWebhookOrchestratorsFromRawSortsByAddress(t *testing.T) {
	out := webhookOrchestratorsFromRaw(map[string]*rawOrchEntry{
		"https://b.example": {address: "https://b.example", score: 1, caps: []string{"x"}},
		"https://a.example": {address: "https://a.example", score: 2, caps: []string{"y"}},
	})
	if len(out) != 2 {
		t.Fatalf("got %#v", out)
	}
	if out[0].Address != "https://a.example" || out[1].Address != "https://b.example" {
		t.Fatalf("unsorted: %#v", out)
	}
}

func TestDiscoveryRawSingleQueryAndCache(t *testing.T) {
	var calls int
	layer, err := cache.New(time.Minute, "", func() int64 { return 1 })
	if err != nil {
		t.Fatal(err)
	}
	s := &Server{
		cache: layer,
		rawRowsQuery: func(_ context.Context, caps []string, _ []string) ([]db.FlatRow, error) {
			calls++
			if len(caps) != 0 {
				t.Fatalf("unfiltered raw should not list capabilities first, got %#v", caps)
			}
			return []db.FlatRow{
				{OrchURI: "https://b.example", Capability: "streamdiffusion", Score: 2},
				{OrchURI: "https://a.example", Capability: "comfystream", Score: 0},
				{OrchURI: "https://a.example", Capability: "transcode/ffmpeg", Score: 0},
			}, nil
		},
	}
	handler := s.Handler()

	req := httptest.NewRequest(http.MethodGet, "/v1/discovery/raw", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	if rr.Header().Get("Cache-Control") != rawCacheControl {
		t.Fatalf("cache-control=%q", rr.Header().Get("Cache-Control"))
	}

	var got []discotypes.WebhookOrchestrator
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("got %#v", got)
	}
	if got[0].Address != "https://a.example" {
		t.Fatalf("expected sorted addresses, got %#v", got)
	}
	if got[0].Score != 1 {
		t.Fatalf("zero score should become 1, got %v", got[0].Score)
	}
	if len(got[0].Capabilities) != 2 {
		t.Fatalf("merged capabilities = %#v", got[0].Capabilities)
	}

	rr2 := httptest.NewRecorder()
	handler.ServeHTTP(rr2, httptest.NewRequest(http.MethodGet, "/v1/discovery/raw", nil))
	if rr2.Code != http.StatusOK {
		t.Fatalf("cached status=%d", rr2.Code)
	}
	if calls != 1 {
		t.Fatalf("expected one DB query, got %d", calls)
	}
}

func TestDiscoveryRawPassesNormalizedCaps(t *testing.T) {
	var gotCaps []string
	s := &Server{
		rawRowsQuery: func(_ context.Context, caps []string, _ []string) ([]db.FlatRow, error) {
			gotCaps = append([]string(nil), caps...)
			return nil, nil
		},
	}
	rr := httptest.NewRecorder()
	s.Handler().ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/v1/discovery/raw?caps=transcode/ffmpeg", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	want := []string{"transcode/ffmpeg", "ffmpeg"}
	if len(gotCaps) != len(want) {
		t.Fatalf("got %#v, want %#v", gotCaps, want)
	}
	for i := range want {
		if gotCaps[i] != want[i] {
			t.Fatalf("caps[%d] = %q, want %q", i, gotCaps[i], want[i])
		}
	}
	if rr.Body.String() != "[]\n" {
		t.Fatalf("empty webhook list = %q", rr.Body.String())
	}
}

func TestDiscoveryRawQueryError(t *testing.T) {
	s := &Server{
		rawRowsQuery: func(_ context.Context, _ []string, _ []string) ([]db.FlatRow, error) {
			return nil, errors.New("db down")
		},
	}
	rr := httptest.NewRecorder()
	s.Handler().ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/v1/discovery/raw", nil))
	if rr.Code != http.StatusInternalServerError {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
}
