package sources

import "testing"

func TestParseServiceTypes(t *testing.T) {
	def := ParseServiceTypes(nil)
	if len(def) != 2 || def[0] != ServiceTypeLiveVideoToVideo || def[1] != ServiceTypeLiveRunner {
		t.Fatalf("default types = %#v", def)
	}

	modulesOnly := ParseServiceTypes([]string{"modules"})
	if len(modulesOnly) != 1 || modulesOnly[0] != ServiceTypeModules {
		t.Fatalf("modules filter = %#v", modulesOnly)
	}

	batchAndLive := ParseServiceTypes([]string{"batch", "live-video-to-video", "batch"})
	if len(batchAndLive) != 2 || batchAndLive[0] != ServiceTypeBatch || batchAndLive[1] != ServiceTypeLiveVideoToVideo {
		t.Fatalf("deduped filter = %#v", batchAndLive)
	}

	invalidFallsBack := ParseServiceTypes([]string{"legacy", "registry", "nope"})
	if len(invalidFallsBack) != 2 || invalidFallsBack[0] != ServiceTypeLiveVideoToVideo {
		t.Fatalf("invalid-only fallback = %#v", invalidFallsBack)
	}
}

func TestClassifyPipelineCapability(t *testing.T) {
	cases := []struct {
		in   string
		want ServiceType
	}{
		{"live-video-to-video/streamdiffusion-sdxl", ServiceTypeLiveVideoToVideo},
		{"streamdiffusion-sdxl", ServiceTypeLiveVideoToVideo},
		{"text-to-image/SG161222/RealVisXL", ServiceTypeBatch},
		{"image-to-video/svd", ServiceTypeBatch},
		{"llm/llama", ServiceTypeBatch},
		{"livepeer/streamdiffusion-sdxl", ServiceTypeLiveVideoToVideo},
	}
	for _, tc := range cases {
		if got := ClassifyPipelineCapability(tc.in); got != tc.want {
			t.Fatalf("ClassifyPipelineCapability(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
