package sources

import (
	"context"
	"encoding/base64"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/livepeer/discovery-service/internal/config"
)

// ClickHouseAdapter fetches per-capability rows from semantic.network_capabilities.
type ClickHouseAdapter struct {
	cfg config.Config
}

func NewClickHouse(cfg config.Config) *ClickHouseAdapter {
	return &ClickHouseAdapter{cfg: cfg}
}

func (a *ClickHouseAdapter) Kind() Kind { return KindClickHouse }

func (a *ClickHouseAdapter) resolveTarget() (queryURL string, headers map[string]string, err error) {
	headers = map[string]string{"Content-Type": "text/plain"}
	if a.cfg.ClickHouseURL != "" && a.cfg.ClickHouseUser != "" && a.cfg.ClickHousePassword != "" {
		u, err := url.Parse(a.cfg.ClickHouseURL)
		if err != nil {
			return "", nil, err
		}
		u.Path = "/"
		auth := base64.StdEncoding.EncodeToString([]byte(a.cfg.ClickHouseUser + ":" + a.cfg.ClickHousePassword))
		headers["Authorization"] = "Basic " + auth
		return u.String(), headers, nil
	}
	if a.cfg.ClickHouseGatewayURL != "" {
		gw := strings.TrimSuffix(a.cfg.ClickHouseGatewayURL, "/")
		return gw + "/query", headers, nil
	}
	return "", nil, fmt.Errorf("CLICKHOUSE_URL or CLICKHOUSE_GATEWAY_URL required")
}

func (a *ClickHouseAdapter) FetchAll(ctx context.Context) (FetchResult, error) {
	start := time.Now()
	queryURL, headers, err := a.resolveTarget()
	if err != nil {
		return FetchResult{Stats: Stats{OK: false, DurationMs: elapsedMs(start), ErrorMessage: err.Error()}}, err
	}

	capBody, err := httpPost(ctx, queryURL, headers, []byte(capabilitiesSQL))
	caps := fallbackCapabilities
	if err == nil {
		if parsed := parseCapabilityNames(capBody); len(parsed) > 0 {
			caps = parsed
		}
	}

	var all []NormalizedOrch
	raw := make(map[string][]CHRow)

	for _, cap := range caps {
		sql, err := buildLeaderboardSQL(cap, maxQueryRows)
		if err != nil {
			continue
		}
		body, err := httpPost(ctx, queryURL, headers, []byte(sql))
		if err != nil {
			raw[cap] = nil
			continue
		}
		rows, err := parseCHRows(body)
		if err != nil {
			raw[cap] = nil
			continue
		}
		raw[cap] = rows
		for _, r := range rows {
			all = append(all, chRowToNormalized(r, cap))
		}
	}

	return FetchResult{
		Rows:  all,
		Raw:   raw,
		Stats: Stats{OK: true, Fetched: len(all), DurationMs: elapsedMs(start)},
	}, nil
}
