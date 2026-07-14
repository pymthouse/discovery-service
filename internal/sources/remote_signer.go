package sources

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/livepeer/discovery-service/internal/config"
)

// RemoteSignerAdapter fetches webhook-compatible discovery from a remote signer.
type RemoteSignerAdapter struct {
	cfg config.Config
}

func NewRemoteSigner(cfg config.Config) *RemoteSignerAdapter {
	return &RemoteSignerAdapter{cfg: cfg}
}

func (a *RemoteSignerAdapter) Kind() Kind { return KindRemoteSigner }

type remoteSignerOrchestrator struct {
	Address      string                `json:"address"`
	Score        float32               `json:"score"`
	Capabilities CapabilityList        `json:"capabilities"`
	Runners      []orchDiscoveryRunner `json:"runners"`
}

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

	var raw []remoteSignerOrchestrator
	if err := json.Unmarshal(body, &raw); err != nil {
		return FetchResult{Stats: Stats{OK: false, DurationMs: elapsedMs(start), ErrorMessage: err.Error()}}, err
	}

	rows := make([]NormalizedOrch, 0, len(raw))
	for _, r := range raw {
		apps := liveRunnerAppsFromRunners(r.Runners)
		grouped := GroupCapabilitiesByServiceType(r.Capabilities)
		if len(grouped) == 0 && len(apps) == 0 {
			continue
		}
		if len(apps) > 0 {
			rows = append(rows, NormalizedOrch{
				ServiceType:    ServiceTypeLiveRunner,
				OrchURI:        r.Address,
				LiveRunnerApps: apps,
				Score:          float64(r.Score),
			})
		}

		for st, caps := range grouped {
			if len(caps) == 0 {
				continue
			}
			row := NormalizedOrch{
				ServiceType:  st,
				OrchURI:      r.Address,
				Capabilities: caps,
				Score:        float64(r.Score),
			}
			rows = append(rows, row)
		}
	}

	return FetchResult{
		Rows:  rows,
		Raw:   raw,
		Stats: Stats{OK: true, Fetched: len(rows), DurationMs: elapsedMs(start)},
	}, nil
}

func liveRunnerAppsFromRunners(runners []orchDiscoveryRunner) []string {
	out := make([]string, 0, len(runners))
	seen := make(map[string]struct{}, len(runners))
	for _, runner := range runners {
		app := strings.TrimSpace(runner.App)
		url := strings.TrimSpace(runner.URL)
		if app == "" || url == "" {
			continue
		}
		if _, ok := seen[app]; ok {
			continue
		}
		seen[app] = struct{}{}
		out = append(out, app)
	}
	return out
}
