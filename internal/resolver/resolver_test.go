package resolver

import (
	"testing"

	"github.com/livepeer/discovery-service/internal/sources"
)

func TestResolve_UnionIncludesDiscoverOnly(t *testing.T) {
	perSource := map[sources.Kind][]sources.NormalizedOrch{
		sources.KindClickHouse: {
			{OrchURI: "https://ch.example", Capabilities: []string{"streamdiffusion-sdxl"}},
		},
		sources.KindNaapDiscover: {
			{OrchURI: "https://discover-only.example", Capabilities: []string{"streamdiffusion-sdxl"}, Score: 1},
		},
	}
	cfg := Config{
		MembershipStrategy: "union",
		Sources: []SourceConfig{
			{Kind: sources.KindClickHouse, Priority: 1, Enabled: true},
			{Kind: sources.KindNaapDiscover, Priority: 2, Enabled: true},
		},
	}
	res := Resolve(perSource, cfg)
	rows := res.Capabilities["streamdiffusion-sdxl"]
	if len(rows) < 2 {
		t.Fatalf("expected at least 2 orchs, got %d", len(rows))
	}
	uris := map[string]bool{}
	for _, r := range rows {
		uris[r.OrchURI] = true
	}
	if !uris["https://discover-only.example"] {
		t.Fatal("discovery-only orchestrator missing from union result")
	}
}
