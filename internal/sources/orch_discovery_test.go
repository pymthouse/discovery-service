package sources

import (
	"testing"
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
