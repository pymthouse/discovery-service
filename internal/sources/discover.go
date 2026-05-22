package sources

import (
	"context"
	"encoding/json"
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
	Address      string         `json:"address"`
	Score        float64        `json:"score"`
	Capabilities CapabilityList `json:"capabilities"`
	LastSeenMs   int64          `json:"last_seen_ms"`
	RecentWork   bool           `json:"recent_work"`
}

type discoverListResponse struct {
	Data []discoverRow `json:"data"`
}

func parseDiscoverRows(body []byte) ([]discoverRow, any, error) {
	var rawRows []discoverRow
	if err := json.Unmarshal(body, &rawRows); err == nil {
		return rawRows, rawRows, nil
	}

	var wrapped discoverListResponse
	if err := json.Unmarshal(body, &wrapped); err == nil && wrapped.Data != nil {
		return wrapped.Data, wrapped, nil
	}

	var rawRowsErr []discoverRow
	if err := json.Unmarshal(body, &rawRowsErr); err != nil {
		return nil, nil, err
	}
	return rawRowsErr, rawRowsErr, nil
}

func discoverRowsToNormalized(rawRows []discoverRow) []NormalizedOrch {
	rows := make([]NormalizedOrch, 0, len(rawRows))
	for _, r := range rawRows {
		if len(r.Capabilities) == 0 {
			continue
		}
		caps := append([]string(nil), r.Capabilities...)
		rows = append(rows, NormalizedOrch{
			ServiceType:  ServiceTypeLegacy,
			OrchURI:      r.Address,
			Capabilities: caps,
			Score:        r.Score,
			RecentWork:   r.RecentWork,
			LastSeenMs:   r.LastSeenMs,
		})
	}
	return rows
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

	rawRows, raw, err := parseDiscoverRows(body)
	if err != nil {
		return FetchResult{Stats: Stats{OK: false, DurationMs: elapsedMs(start), ErrorMessage: err.Error()}}, err
	}

	rows := discoverRowsToNormalized(rawRows)

	return FetchResult{
		Rows:  rows,
		Raw:   raw,
		Stats: Stats{OK: true, Fetched: len(rows), DurationMs: elapsedMs(start)},
	}, nil
}
