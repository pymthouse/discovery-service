package sources

import (
	"context"
	"encoding/json"
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
