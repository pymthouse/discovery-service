package sources

import (
	"context"
	"encoding/json"
	"time"

	"github.com/livepeer/discovery-service/internal/config"
)

// PricingAdapter fetches orchestrator pricing (optional, default disabled).
type PricingAdapter struct {
	cfg config.Config
}

func NewPricing(cfg config.Config) *PricingAdapter {
	return &PricingAdapter{cfg: cfg}
}

func (a *PricingAdapter) Kind() Kind { return KindNaapPricing }

func (a *PricingAdapter) FetchAll(ctx context.Context) (FetchResult, error) {
	start := time.Now()
	if a.cfg.PricingAPIURL == "" {
		return FetchResult{Stats: Stats{OK: true, Fetched: 0, DurationMs: elapsedMs(start)}}, nil
	}

	body, err := httpGet(ctx, a.cfg.PricingAPIURL, nil)
	if err != nil {
		return FetchResult{Stats: Stats{OK: false, DurationMs: elapsedMs(start), ErrorMessage: err.Error()}}, err
	}

	var raw []map[string]any
	if err := json.Unmarshal(body, &raw); err != nil {
		var wrapped struct {
			Data []map[string]any `json:"data"`
		}
		if err2 := json.Unmarshal(body, &wrapped); err2 != nil {
			return FetchResult{Stats: Stats{OK: false, DurationMs: elapsedMs(start), ErrorMessage: err.Error()}}, err
		}
		raw = wrapped.Data
	}

	rows := make([]NormalizedOrch, 0, len(raw))
	for _, item := range raw {
		addr, _ := item["ethAddress"].(string)
		if addr == "" {
			addr, _ = item["address"].(string)
		}
		uri, _ := item["orchUri"].(string)
		price, _ := item["pricePerUnit"].(float64)
		rows = append(rows, NormalizedOrch{
			EthAddress:   addr,
			OrchURI:      uri,
			PricePerUnit: price,
		})
	}

	return FetchResult{
		Rows:  rows,
		Raw:   raw,
		Stats: Stats{OK: true, Fetched: len(rows), DurationMs: elapsedMs(start)},
	}, nil
}
