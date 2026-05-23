package db

import "testing"

func TestBuildCapabilityEntriesGroupsOfferings(t *testing.T) {
	rows := []FlatRow{
		{ServiceType: "registry", Capability: "daydream:scope:v1", OfferingID: "default", OrchURI: "https://a"},
		{ServiceType: "registry", Capability: "daydream:scope:v1", OfferingID: "premium", OrchURI: "https://b"},
		{ServiceType: "legacy", Capability: "streamdiffusion-sdxl", OrchURI: "https://c"},
	}
	entries := buildCapabilityEntries(rows)
	if len(entries) != 2 {
		t.Fatalf("entries = %#v", entries)
	}
	if entries[0].ServiceType != "legacy" || entries[0].Capability != "streamdiffusion-sdxl" {
		t.Fatalf("legacy entry = %#v", entries[0])
	}
	if entries[1].OfferingIDs == nil || len(entries[1].OfferingIDs) != 2 {
		t.Fatalf("registry offerings = %#v", entries[1])
	}
}
