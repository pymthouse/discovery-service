package httpapi

import "testing"

func TestNormalizeLegacyCapsStripsPipelinePrefix(t *testing.T) {
	got := normalizeLegacyCaps(
		[]string{"live-video-to-video/streamdiffusion-sdxl", "streamdiffusion-sdxl"},
		[]string{"legacy"},
	)
	want := []string{"streamdiffusion-sdxl", "streamdiffusion-sdxl"}
	if len(got) != len(want) {
		t.Fatalf("got %d caps, want %d: %#v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("caps[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestNormalizeLegacyCapsLeavesRegistryUntouched(t *testing.T) {
	in := []string{"daydream:scope/v1"}
	got := normalizeLegacyCaps(in, []string{"registry"})
	if len(got) != 1 || got[0] != in[0] {
		t.Fatalf("registry caps were modified: %#v", got)
	}
}

func TestNormalizeLegacyCapsDefaultServiceTypesStrip(t *testing.T) {
	got := normalizeLegacyCaps(
		[]string{"live-video-to-video/streamdiffusion-sdxl"},
		[]string{"legacy", "registry"},
	)
	if len(got) != 1 || got[0] != "streamdiffusion-sdxl" {
		t.Fatalf("default service types did not strip prefix: %#v", got)
	}
}
