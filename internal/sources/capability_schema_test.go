package sources

import (
	"encoding/json"
	"testing"
)

func TestCapabilityListUnmarshalLegacyAndRegistryShapes(t *testing.T) {
	var caps CapabilityList
	body := []byte(`[
		"livepeer/streamdiffusion-sdxl",
		{"name":"openai:/v1/chat/completions","work_unit":"token","offerings":[{"id":"gpt-oss-20b"}]},
		{"capability_id":"daydream:scope:v1","offering_id":"default","interaction_mode":"http-reqresp@v0"},
		{"capability":"transcoding:h264","work_unit":"frame"}
	]`)
	if err := json.Unmarshal(body, &caps); err != nil {
		t.Fatal(err)
	}

	want := []string{
		"streamdiffusion-sdxl",
		"openai:/v1/chat/completions",
		"daydream:scope:v1",
		"transcoding:h264",
	}
	if len(caps) != len(want) {
		t.Fatalf("got %d capabilities, want %d: %#v", len(caps), len(want), caps)
	}
	for i := range want {
		if caps[i] != want[i] {
			t.Fatalf("caps[%d] = %q, want %q", i, caps[i], want[i])
		}
	}
}
