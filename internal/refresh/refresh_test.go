package refresh

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/livepeer/discovery-service/internal/config"
	"github.com/livepeer/discovery-service/internal/resolver"
	"github.com/livepeer/discovery-service/internal/sources"
)

func TestLiveRunnerClaimsFromRemoteSigner(t *testing.T) {
	got := liveRunnerClaimsFromRemoteSigner([]sources.NormalizedOrch{
		{OrchURI: "", LiveRunnerApps: []string{"ignored"}},
		{OrchURI: "https://a.example/", Score: 0, LiveRunnerApps: []string{"transcode/ffmpeg", "", "transcode/ffmpeg", "vllm/llama"}},
		{OrchURI: "https://a.example", Score: 2, LiveRunnerApps: []string{"transcode/ffmpeg"}},
	})
	if len(got) != 2 {
		t.Fatalf("got %#v", got)
	}
	if got[0].OrchURI != "https://a.example" || got[0].App != "transcode/ffmpeg" || got[0].Score != 1 {
		t.Fatalf("unexpected first claim: %#v", got[0])
	}
	if got[1].App != "vllm/llama" || got[1].Score != 1 {
		t.Fatalf("unexpected second claim: %#v", got[1])
	}
}

func TestLiveRunnerClaimsToFlat(t *testing.T) {
	got := liveRunnerClaimsToFlat([]sources.LiveRunnerAppClaim{
		{OrchURI: "", App: "x"},
		{OrchURI: "https://a", App: ""},
		{OrchURI: "https://a", App: "transcode/ffmpeg", Score: 0},
		{OrchURI: "https://b", App: "vllm/llama", Score: 3},
	})
	if len(got) != 2 {
		t.Fatalf("got %#v", got)
	}
	if got[0].Capability != "transcode/ffmpeg" || got[0].Score != 1 || got[0].ServiceType != "legacy" {
		t.Fatalf("unexpected first row: %#v", got[0])
	}
	if got[1].Score != 3 || got[1].OrchURI != "https://b" {
		t.Fatalf("unexpected second row: %#v", got[1])
	}
}

func TestResolverRowsToFlat(t *testing.T) {
	got := resolverRowsToFlat(map[string][]resolver.DatasetRow{
		"cap-a": {
			{OrchURI: ""},
			{OrchURI: "https://a", ServiceType: "", Score: 2},
			{OrchURI: "https://b", ServiceType: "registry", EthAddress: "0x1", OfferingID: "default"},
		},
	})
	if len(got) != 2 {
		t.Fatalf("got %#v", got)
	}
	if got[0].ServiceType != "legacy" || got[0].Capability != "cap-a" || got[0].Score != 2 {
		t.Fatalf("unexpected first: %#v", got[0])
	}
	if got[1].ServiceType != "registry" || got[1].OfferingID != "default" {
		t.Fatalf("unexpected second: %#v", got[1])
	}
}

func TestCountRegistryOrchestrators(t *testing.T) {
	n := countRegistryOrchestrators([]sources.NormalizedOrch{
		{OrchURI: "https://a"},
		{OrchURI: "https://a"},
		{EthAddress: "0xabc"},
		{},
	})
	if n != 2 {
		t.Fatalf("got %d", n)
	}
}

func TestCollectLiveRunnerAppClaimsDisabledUsesSigner(t *testing.T) {
	claims, stats := collectLiveRunnerAppClaims(context.Background(), config.Config{
		OrchDiscoveryRefreshEnabled: false,
	}, map[sources.Kind][]sources.NormalizedOrch{
		sources.KindRemoteSigner: {
			{OrchURI: "https://signer-orch", LiveRunnerApps: []string{"transcode/ffmpeg"}},
		},
	})
	if !stats.OK || stats.Fetched != 1 || len(claims) != 1 || claims[0].App != "transcode/ffmpeg" {
		t.Fatalf("claims=%#v stats=%#v", claims, stats)
	}
}

func TestCollectLiveRunnerAppClaimsProbePrefersOverSigner(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprintf(w, `[{"address":"http://%s/orch","runners":[{"url":"http://r","app":"transcode/ffmpeg"}]}]`, r.Host)
	}))
	defer srv.Close()

	claims, stats := collectLiveRunnerAppClaims(context.Background(), config.Config{
		OrchDiscoveryRefreshEnabled:   true,
		OrchDiscoveryTimeoutMs:        2000,
		OrchDiscoveryMaxConcurrency:   2,
		OrchDiscoveryMaxOrchestrators: 10,
	}, map[sources.Kind][]sources.NormalizedOrch{
		sources.KindSubgraph: {
			{OrchURI: srv.URL + "/orch"},
		},
		sources.KindRemoteSigner: {
			{OrchURI: srv.URL + "/orch", LiveRunnerApps: []string{"transcode/ffmpeg", "vllm/llama"}},
		},
	})
	if !stats.OK || len(claims) != 2 {
		t.Fatalf("claims=%#v stats=%#v", claims, stats)
	}
	apps := map[string]bool{}
	for _, c := range claims {
		apps[c.App] = true
	}
	if !apps["transcode/ffmpeg"] || !apps["vllm/llama"] {
		t.Fatalf("expected probe+signer apps, got %#v", claims)
	}
}

func TestCollectLiveRunnerAppClaimsExtraURIs(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/orch/discovery" {
			http.NotFound(w, r)
			return
		}
		_, _ = fmt.Fprintf(w, `[{"address":"http://%s/orch","runners":[{"url":"http://r","app":"transcode/ffmpeg"}]}]`, r.Host)
	}))
	defer srv.Close()

	claims, stats := collectLiveRunnerAppClaims(context.Background(), config.Config{
		OrchDiscoveryRefreshEnabled:   true,
		OrchDiscoveryTimeoutMs:        2000,
		OrchDiscoveryMaxConcurrency:   2,
		OrchDiscoveryMaxOrchestrators: 10,
		OrchDiscoveryExtraURIs:        []string{srv.URL + "/orch"},
	}, nil)
	if !stats.OK || len(claims) != 1 || claims[0].App != "transcode/ffmpeg" {
		t.Fatalf("claims=%#v stats=%#v", claims, stats)
	}
}
