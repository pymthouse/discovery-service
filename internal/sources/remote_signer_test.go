package sources

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/livepeer/discovery-service/internal/config"
	"github.com/livepeer/discovery-service/pkg/discotypes"
)

func TestRemoteSigner_ParseFixture(t *testing.T) {
	path := filepath.Join("..", "..", "fixtures", "discover-orchestrators.json")
	body, err := os.ReadFile(path)
	if err != nil {
		t.Skip("fixture not found:", err)
	}
	var raw []discotypes.WebhookOrchestrator
	if err := json.Unmarshal(body, &raw); err != nil {
		t.Fatal(err)
	}
	if len(raw) == 0 {
		t.Fatal("empty fixture")
	}
	if raw[0].Address == "" || len(raw[0].Capabilities) == 0 {
		t.Fatalf("unexpected first row: %+v", raw[0])
	}
}

func TestRemoteSigner_DisabledWithoutURL(t *testing.T) {
	a := NewRemoteSigner(config.Config{})
	res, err := a.FetchAll(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if res.Stats.Fetched != 0 {
		t.Fatalf("expected 0 rows, got %d", res.Stats.Fetched)
	}
}

func TestRemoteSigner_SeparatesRunnerAppsFromCapabilities(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/discover-orchestrators" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(`[
			{
				"address": "https://mixed.example",
				"score": 0.75,
				"capabilities": [
					"live-video-to-video/streamdiffusion-sdxl",
					"text-to-image/org/model"
				],
				"runners": [
					{"url": "https://runner.example", "app": "transcode/ffmpeg"}
				]
			},
			{
				"address": "https://runner-only.example",
				"score": 0.5,
				"capabilities": [],
				"runners": [
					{"url": "https://runner.example", "app": "vllm/llama"}
				]
			},
			{
				"address": "https://empty.example",
				"score": 1,
				"capabilities": [],
				"runners": []
			}
		]`))
	}))
	defer srv.Close()

	a := NewRemoteSigner(config.Config{
		RemoteSignerURL: srv.URL,
	})
	res, err := a.FetchAll(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if res.Stats.Fetched != 4 {
		t.Fatalf("expected 4 rows, got %d: %#v", res.Stats.Fetched, res.Rows)
	}

	rows := make(map[string]map[ServiceType]NormalizedOrch)
	for _, row := range res.Rows {
		if rows[row.OrchURI] == nil {
			rows[row.OrchURI] = make(map[ServiceType]NormalizedOrch)
		}
		rows[row.OrchURI][row.ServiceType] = row
	}

	mixed := rows["https://mixed.example"]
	if len(mixed) != 3 {
		t.Fatalf("expected three mixed rows, got %#v", mixed)
	}
	runner := mixed[ServiceTypeLiveRunner]
	if len(runner.LiveRunnerApps) != 1 || runner.LiveRunnerApps[0] != "transcode/ffmpeg" {
		t.Fatalf("unexpected mixed runner row: %#v", runner)
	}
	if len(mixed[ServiceTypeLiveVideoToVideo].LiveRunnerApps) != 0 {
		t.Fatalf("live capability row received runner apps: %#v", mixed[ServiceTypeLiveVideoToVideo])
	}
	if len(mixed[ServiceTypeBatch].LiveRunnerApps) != 0 {
		t.Fatalf("batch capability row received runner apps: %#v", mixed[ServiceTypeBatch])
	}

	runnerOnly := rows["https://runner-only.example"][ServiceTypeLiveRunner]
	if len(runnerOnly.LiveRunnerApps) != 1 || runnerOnly.LiveRunnerApps[0] != "vllm/llama" {
		t.Fatalf("unexpected runner-only row: %#v", runnerOnly)
	}
	if _, ok := rows["https://empty.example"]; ok {
		t.Fatalf("empty orchestrator emitted rows: %#v", rows["https://empty.example"])
	}
}

func TestLiveRunnerAppsFromRunners(t *testing.T) {
	got := liveRunnerAppsFromRunners([]orchDiscoveryRunner{
		{URL: "https://r1", App: "transcode/ffmpeg"},
		{URL: "https://r2", App: "transcode/ffmpeg"},
		{URL: "", App: "ignored"},
		{URL: "https://r3", App: "vllm/llama"},
	})
	if len(got) != 2 || got[0] != "transcode/ffmpeg" || got[1] != "vllm/llama" {
		t.Fatalf("unexpected apps: %#v", got)
	}
}
