package db

import "testing"

func TestBuildCapabilityEntriesGroupsOfferings(t *testing.T) {
	rows := []FlatRow{
		{ServiceType: "modules", Capability: "daydream:scope:v1", OfferingID: "default", OrchURI: "https://a"},
		{ServiceType: "modules", Capability: "daydream:scope:v1", OfferingID: "premium", OrchURI: "https://b"},
		{ServiceType: "live-video-to-video", Capability: "streamdiffusion-sdxl", OrchURI: "https://c"},
	}
	entries := buildCapabilityEntries(rows)
	if len(entries) != 2 {
		t.Fatalf("entries = %#v", entries)
	}
	if entries[0].ServiceType != "live-video-to-video" || entries[0].Capability != "streamdiffusion-sdxl" {
		t.Fatalf("live entry = %#v", entries[0])
	}
	if entries[1].OfferingIDs == nil || len(entries[1].OfferingIDs) != 2 {
		t.Fatalf("modules offerings = %#v", entries[1])
	}
}
