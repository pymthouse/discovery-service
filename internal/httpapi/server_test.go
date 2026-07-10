package httpapi

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/livepeer/discovery-service/internal/config"
)

func TestNormalizeLegacyCapsKeepsExactAndStripped(t *testing.T) {
	got := normalizeLegacyCaps(
		[]string{"live-video-to-video/streamdiffusion-sdxl", "streamdiffusion-sdxl"},
		[]string{"legacy"},
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
		[]string{"legacy"},
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

func TestNormalizeLegacyCapsLeavesRegistryUntouched(t *testing.T) {
	in := []string{"daydream:scope/v1"}
	got := normalizeLegacyCaps(in, []string{"registry"})
	if len(got) != 1 || got[0] != in[0] {
		t.Fatalf("registry caps were modified: %#v", got)
	}
}

func TestNormalizeLegacyCapsDefaultServiceTypesKeepExactAndStripped(t *testing.T) {
	got := normalizeLegacyCaps(
		[]string{"live-video-to-video/streamdiffusion-sdxl"},
		[]string{"legacy", "registry"},
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
