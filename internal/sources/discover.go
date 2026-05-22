package sources

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/livepeer/discovery-service/internal/config"
)

// DiscoverAdapter fetches orchestrators from the public discover HTTP API.
type DiscoverAdapter struct {
	cfg config.Config
}

func NewDiscover(cfg config.Config) *DiscoverAdapter {
	return &DiscoverAdapter{cfg: cfg}
}

func (a *DiscoverAdapter) Kind() Kind { return KindNaapDiscover }

type discoverRow struct {
	Address      string   `json:"address"`
	Score        float64  `json:"score"`
	Capabilities []string `json:"capabilities"`
	LastSeenMs   int64    `json:"last_seen_ms"`
	RecentWork   bool     `json:"recent_work"`
}

type discoverListResponse struct {
	Data []discoverRow `json:"data"`
}

func extractCapabilityName(raw string) string {
	if idx := strings.LastIndex(raw, "/"); idx >= 0 {
		return raw[idx+1:]
	}
	return raw
}

func (a *DiscoverAdapter) FetchAll(ctx context.Context) (FetchResult, error) {
	start := time.Now()
	url := a.cfg.DiscoverAPIURL
	if url == "" {
		url = "https://naap-api.cloudspe.com/v1/discover/orchestrators"
	}

	body, err := httpGet(ctx, url, nil)
	if err != nil {
		return FetchResult{Stats: Stats{OK: false, DurationMs: elapsedMs(start), ErrorMessage: err.Error()}}, err
	}

	var rawRows []discoverRow
	if err := json.Unmarshal(body, &rawRows); err != nil {
		var wrapped discoverListResponse
		if err := json.Unmarshal(body, &wrapped); err != nil {
			return FetchResult{Stats: Stats{OK: false, DurationMs: elapsedMs(start), ErrorMessage: err.Error()}}, err
		}
		rawRows = wrapped.Data
	}

	rows := make([]NormalizedOrch, 0, len(rawRows))
	for _, r := range rawRows {
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
			Score:        r.Score,
			RecentWork:   r.RecentWork,
			LastSeenMs:   r.LastSeenMs,
		})
	}

	return FetchResult{
		Rows:  rows,
		Raw:   rawRows,
		Stats: Stats{OK: true, Fetched: len(rows), DurationMs: elapsedMs(start)},
	}, nil
}
