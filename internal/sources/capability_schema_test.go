package sources

import (
	"encoding/json"
	"testing"
)

func TestCapabilityListUnmarshalLegacyAndRegistryShapes(t *testing.T) {
	var caps CapabilityList
	body := []byte(`[
		"livepeer/streamdiffusion-sdxl",
		"live-video-to-video/streamdiffusion-sdxl-v2v",
		"text-to-image/SG161222/RealVisXL",
		{"name":"openai:/v1/chat/completions","work_unit":"token","offerings":[{"id":"gpt-oss-20b"}]},
		{"capability_id":"daydream:scope:v1","offering_id":"default","interaction_mode":"http-reqresp@v0"},
		{"capability":"transcoding:h264","work_unit":"frame"}
	]`)
	if err := json.Unmarshal(body, &caps); err != nil {
		t.Fatal(err)
	}

	want := []ParsedCapability{
		{Name: "streamdiffusion-sdxl", ServiceType: ServiceTypeBatch},
		{Name: "streamdiffusion-sdxl-v2v", ServiceType: ServiceTypeLiveVideoToVideo},
		{Name: "SG161222/RealVisXL", ServiceType: ServiceTypeBatch},
		{Name: "openai:/v1/chat/completions", ServiceType: ServiceTypeModules},
		{Name: "daydream:scope:v1", ServiceType: ServiceTypeModules},
		{Name: "transcoding:h264", ServiceType: ServiceTypeModules},
	}
	if len(caps) != len(want) {
		t.Fatalf("got %d capabilities, want %d: %#v", len(caps), len(want), caps)
	}
	for i := range want {
		if caps[i] != want[i] {
			t.Fatalf("caps[%d] = %#v, want %#v", i, caps[i], want[i])
		}
	}
}

func TestGroupCapabilitiesByServiceType(t *testing.T) {
	grouped := GroupCapabilitiesByServiceType(CapabilityList{
		{Name: "a", ServiceType: ServiceTypeLiveVideoToVideo},
		{Name: "b", ServiceType: ServiceTypeBatch},
		{Name: "c", ServiceType: ServiceTypeModules},
		{Name: "a2", ServiceType: ServiceTypeLiveVideoToVideo},
	})
	if len(grouped[ServiceTypeLiveVideoToVideo]) != 2 {
		t.Fatalf("live = %#v", grouped[ServiceTypeLiveVideoToVideo])
	}
	if len(grouped[ServiceTypeBatch]) != 1 || grouped[ServiceTypeBatch][0] != "b" {
		t.Fatalf("batch = %#v", grouped[ServiceTypeBatch])
	}
	if len(grouped[ServiceTypeModules]) != 1 {
		t.Fatalf("modules = %#v", grouped[ServiceTypeModules])
	}
}
