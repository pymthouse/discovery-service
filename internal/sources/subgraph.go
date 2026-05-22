package sources

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/livepeer/discovery-service/internal/config"
)

const transcodersQuery = `{
  transcoders(
    first: 1000,
    where: { active: true },
    orderBy: totalStake,
    orderDirection: desc
  ) {
    id
    serviceURI
    activationRound
    deactivationRound
    totalStake
    active
  }
}`

// SubgraphAdapter queries The Graph for active transcoders.
type SubgraphAdapter struct {
	cfg config.Config
}

func NewSubgraph(cfg config.Config) *SubgraphAdapter {
	return &SubgraphAdapter{cfg: cfg}
}

func (a *SubgraphAdapter) Kind() Kind { return KindSubgraph }

func (a *SubgraphAdapter) FetchAll(ctx context.Context) (FetchResult, error) {
	start := time.Now()
	base := a.cfg.SubgraphURL
	if base == "" {
		base = "https://api.thegraph.com"
	}
	queryURL := fmt.Sprintf("%s/subgraphs/id/%s", base, a.cfg.SubgraphID)
	headers := map[string]string{"Content-Type": "application/json"}

	payload, _ := json.Marshal(map[string]string{"query": transcodersQuery})
	body, err := httpPost(ctx, queryURL, headers, payload)
	if err != nil {
		return FetchResult{Stats: Stats{OK: false, DurationMs: elapsedMs(start), ErrorMessage: err.Error()}}, err
	}

	rows, err := parseSubgraphTranscoders(body)
	if err != nil {
		return FetchResult{Stats: Stats{OK: false, DurationMs: elapsedMs(start), ErrorMessage: err.Error()}}, err
	}

	return FetchResult{
		Rows:  rows,
		Raw:   json.RawMessage(body),
		Stats: Stats{OK: true, Fetched: len(rows), DurationMs: elapsedMs(start)},
	}, nil
}

type subgraphTranscoderBlock struct {
	Transcoders []subgraphTranscoder `json:"transcoders"`
}

type subgraphDataPayload struct {
	Data        subgraphTranscoderBlock `json:"data"`
	Transcoders []subgraphTranscoder    `json:"transcoders"`
}

type subgraphResponseEnvelope struct {
	Data subgraphDataPayload `json:"data"`
}

func parseSubgraphTranscoders(body []byte) ([]NormalizedOrch, error) {
	var envelope subgraphResponseEnvelope
	if err := json.Unmarshal(body, &envelope); err != nil {
		return nil, err
	}
	raw := envelope.Data.Data.Transcoders
	if len(raw) == 0 {
		raw = envelope.Data.Transcoders
	}

	out := make([]NormalizedOrch, 0, len(raw))
	for _, t := range raw {
		if !t.Active || t.ServiceURI == "" {
			continue
		}
		out = append(out, NormalizedOrch{
			EthAddress:        strings.ToLower(t.ID),
			OrchURI:           t.ServiceURI,
			ActivationRound:   parseInt(t.ActivationRound),
			DeactivationRound: parseInt(t.DeactivationRound),
		})
	}
	return out, nil
}

type subgraphTranscoder struct {
	ID                string `json:"id"`
	ServiceURI        string `json:"serviceURI"`
	ActivationRound   string `json:"activationRound"`
	DeactivationRound string `json:"deactivationRound"`
	Active            bool   `json:"active"`
}

func parseInt(s string) int {
	var n int
	fmt.Sscanf(s, "%d", &n)
	return n
}
