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

func TestCollectTypedCapabilitiesDeduplicatesWithinServiceType(t *testing.T) {
	got := collectTypedCapabilities([]sources.NormalizedOrch{
		{
			ServiceType: sources.ServiceTypeLiveVideoToVideo,
			Capabilities: []string{
				"same",
				"",
				"__uncategorized",
				"same",
				"z-last",
			},
		},
		{
			ServiceType: sources.ServiceTypeBatch,
			Capabilities: []string{
				"same",
				"a-first",
			},
		},
	})

	want := []typedCapability{
		{
			name:        "a-first",
			serviceType: sources.ServiceTypeBatch,
		},
		{
			name:        "same",
			serviceType: sources.ServiceTypeBatch,
		},
		{
			name:        "same",
			serviceType: sources.ServiceTypeLiveVideoToVideo,
		},
		{
			name:        "z-last",
			serviceType: sources.ServiceTypeLiveVideoToVideo,
		},
	}
	if len(got) != len(want) {
		t.Fatalf("got %#v, want %#v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got[%d] = %#v, want %#v", i, got[i], want[i])
		}
	}
}

func TestMergeCapabilitiesByPriorityKeepsDistinctServiceTypes(t *testing.T) {
	sourceRows := map[sources.Kind][]sources.NormalizedOrch{
		sources.KindClickHouse: {
			{
				ServiceType: sources.ServiceTypeLiveVideoToVideo,
				Capabilities: []string{
					"shared",
				},
			},
		},
		sources.KindNaapDiscover: {
			{
				ServiceType: sources.ServiceTypeLiveVideoToVideo,
				Capabilities: []string{
					"shared",
				},
			},
			{
				ServiceType: sources.ServiceTypeBatch,
				Capabilities: []string{
					"shared",
				},
			},
		},
	}
	got := mergeCapabilitiesByPriority(
		nil,
		sourceRows,
		map[string][]sources.Kind{
			"capabilities": {
				sources.KindRemoteSigner,
				sources.KindClickHouse,
				sources.KindNaapDiscover,
			},
		},
	)

	if len(got) != 2 {
		t.Fatalf("got %#v", got)
	}
	if got[0].serviceType != sources.ServiceTypeBatch || got[1].serviceType != sources.ServiceTypeLiveVideoToVideo {
		t.Fatalf("unexpected service types: %#v", got)
	}
}
