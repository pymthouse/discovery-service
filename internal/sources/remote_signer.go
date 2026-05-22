package sources

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/livepeer/discovery-service/internal/config"
	"github.com/livepeer/discovery-service/pkg/discotypes"
)

// RemoteSignerAdapter fetches webhook-compatible discovery from a remote signer.
type RemoteSignerAdapter struct {
	cfg config.Config
}

func NewRemoteSigner(cfg config.Config) *RemoteSignerAdapter {
	return &RemoteSignerAdapter{cfg: cfg}
}

func (a *RemoteSignerAdapter) Kind() Kind { return KindRemoteSigner }

func (a *RemoteSignerAdapter) FetchAll(ctx context.Context) (FetchResult, error) {
	start := time.Now()
	url := strings.TrimSuffix(a.cfg.RemoteSignerURL, "/") + "/discover-orchestrators"
	if a.cfg.RemoteSignerURL == "" {
		return FetchResult{
			Stats: Stats{OK: true, Fetched: 0, DurationMs: elapsedMs(start)},
		}, nil
	}

	body, err := httpGet(ctx, url, nil)
	if err != nil {
		return FetchResult{Stats: Stats{OK: false, DurationMs: elapsedMs(start), ErrorMessage: err.Error()}}, err
	}

	var raw []discotypes.WebhookOrchestrator
	if err := json.Unmarshal(body, &raw); err != nil {
		return FetchResult{Stats: Stats{OK: false, DurationMs: elapsedMs(start), ErrorMessage: err.Error()}}, err
	}

	rows := make([]NormalizedOrch, 0, len(raw))
	for _, r := range raw {
		if len(r.Capabilities) == 0 {
			continue
		}
		short := make([]string, len(r.Capabilities))
		for i, c := range r.Capabilities {
			short[i] = extractCapabilityName(c)
		}
		rows = append(rows, NormalizedOrch{
			OrchURI:      r.Address,
			Capabilities: short,
			Score:        float64(r.Score),
		})
	}

	return FetchResult{
		Rows:  rows,
		Raw:   raw,
		Stats: Stats{OK: true, Fetched: len(rows), DurationMs: elapsedMs(start)},
	}, nil
}
