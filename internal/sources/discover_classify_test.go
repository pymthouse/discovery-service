package sources

import "testing"

func TestDiscoverRowsToNormalizedSplitsServiceTypes(t *testing.T) {
	body := []byte(`[
		{
			"address":"https://orch.example",
			"score":1,
			"capabilities":[
				"live-video-to-video/streamdiffusion-sdxl",
				"text-to-image/org/model",
				{"name":"daydream:scope:v1","offerings":[{"id":"default"}]}
			]
		}
	]`)
	raw, _, err := parseDiscoverRows(body)
	if err != nil {
		t.Fatal(err)
	}
	rows := discoverRowsToNormalized(raw)
	if len(rows) != 3 {
		t.Fatalf("expected 3 typed rows, got %#v", rows)
	}
	byType := map[ServiceType][]string{}
	for _, r := range rows {
		byType[r.ServiceType] = append(byType[r.ServiceType], r.Capabilities...)
	}
	if got := byType[ServiceTypeLiveVideoToVideo]; len(got) != 1 || got[0] != "streamdiffusion-sdxl" {
		t.Fatalf("live = %#v", got)
	}
	if got := byType[ServiceTypeBatch]; len(got) != 1 || got[0] != "org/model" {
		t.Fatalf("batch = %#v", got)
	}
	if got := byType[ServiceTypeModules]; len(got) != 1 || got[0] != "daydream:scope:v1" {
		t.Fatalf("modules = %#v", got)
	}
}
