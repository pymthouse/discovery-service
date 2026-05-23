package resolver

import (
	"testing"

	"github.com/livepeer/discovery-service/internal/sources"
)

func TestLegacyAndRegistryCapabilitiesDoNotMerge(t *testing.T) {
	legacy := Resolve(map[sources.Kind][]sources.NormalizedOrch{
		sources.KindNaapDiscover: {
			{
				ServiceType:  sources.ServiceTypeLegacy,
				OrchURI:      "https://legacy.example",
				Capabilities: []string{"daydream:scope:v1"},
				Score:        2,
				GPUName:      "A100",
			},
		},
	}, Config{
		MembershipStrategy: "union",
		Sources: []SourceConfig{
			{Kind: sources.KindNaapDiscover, Priority: 1, Enabled: true},
		},
	})

	registry := BuildRegistryDataset([]sources.NormalizedOrch{
		{
			ServiceType:     sources.ServiceTypeRegistry,
			OrchURI:         "https://registry-worker.example",
			EthAddress:      "0xabc",
			Capabilities:    []string{"daydream:scope:v1"},
			OfferingID:      "default",
			PricePerUnitWei: "1000",
		},
	})

	legacyRows := legacy.Capabilities["daydream:scope:v1"]
	registryRows := registry["daydream:scope:v1"]
	if len(legacyRows) != 1 || len(registryRows) != 1 {
		t.Fatalf("legacy=%d registry=%d", len(legacyRows), len(registryRows))
	}
	if legacyRows[0].ServiceType != string(sources.ServiceTypeLegacy) {
		t.Fatalf("legacy service type = %q", legacyRows[0].ServiceType)
	}
	if registryRows[0].ServiceType != string(sources.ServiceTypeRegistry) {
		t.Fatalf("registry service type = %q", registryRows[0].ServiceType)
	}
	if legacyRows[0].GPUName != "A100" {
		t.Fatalf("legacy gpu = %q", legacyRows[0].GPUName)
	}
	if registryRows[0].GPUName != "" {
		t.Fatalf("registry should not inherit legacy gpu, got %q", registryRows[0].GPUName)
	}
	if registryRows[0].OfferingID != "default" {
		t.Fatalf("registry offering = %q", registryRows[0].OfferingID)
	}
}
