package db

import (
	"strings"
	"testing"
)

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

func TestBuildRawRowsQueryUnfiltered(t *testing.T) {
	q, args := buildRawRowsQuery(nil, []string{"live-video-to-video", "live-runner"})
	if !strings.Contains(q, "service_type = ANY($1)") ||
		!strings.Contains(q, "ORDER BY orch_uri, capability") ||
		!strings.Contains(q, "LIMIT $2") {
		t.Fatalf("query = %s", q)
	}
	if len(args) != 2 {
		t.Fatalf("args = %#v", args)
	}
	if args[1] != rawWebhookRowLimit {
		t.Fatalf("limit = %#v", args[1])
	}
}

func TestBuildRawRowsQueryWithCaps(t *testing.T) {
	q, args := buildRawRowsQuery(
		[]string{"transcode/ffmpeg", "ffmpeg"},
		[]string{"live-runner"},
	)
	if !strings.Contains(q, "service_type = ANY($1)") ||
		!strings.Contains(q, "capability = ANY($2)") ||
		!strings.Contains(q, "LIMIT $3") {
		t.Fatalf("query = %s", q)
	}
	if len(args) != 3 {
		t.Fatalf("args = %#v", args)
	}
}

func TestBuildRawRowsQueryCapsOnly(t *testing.T) {
	q, args := buildRawRowsQuery([]string{"comfystream"}, nil)
	if !strings.Contains(q, "WHERE capability = ANY($1)") ||
		!strings.Contains(q, "LIMIT $2") {
		t.Fatalf("query = %s", q)
	}
	if len(args) != 2 {
		t.Fatalf("args = %#v", args)
	}
}
