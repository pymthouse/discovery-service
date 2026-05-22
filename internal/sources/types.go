package sources

import (
	"context"
	"time"
)

// Kind identifies a discovery data source adapter.
type Kind string

const (
	KindSubgraph      Kind = "livepeer-subgraph"
	KindClickHouse    Kind = "clickhouse-query"
	KindNaapDiscover  Kind = "naap-discover"
	KindNaapPricing   Kind = "naap-pricing"
	KindRemoteSigner  Kind = "remote-signer"
)

// AllKinds is the default registration order.
var AllKinds = []Kind{
	KindSubgraph,
	KindClickHouse,
	KindNaapDiscover,
	KindNaapPricing,
	KindRemoteSigner,
}

// FetchCtx carries optional auth for gateway-proxied upstreams.
type FetchCtx struct {
	AuthToken string
}

// Stats records per-source fetch metrics.
type Stats struct {
	OK           bool   `json:"ok"`
	Fetched      int    `json:"fetched"`
	DurationMs   int64  `json:"durationMs"`
	ErrorMessage string `json:"errorMessage,omitempty"`
}

// NormalizedOrch is the intermediate row shape before resolver merge.
type NormalizedOrch struct {
	EthAddress          string
	OrchURI             string
	Capabilities        []string
	Score               float64
	RecentWork          bool
	LastSeenMs          int64
	GPUName             string
	GPUGb               float64
	Avail               float64
	TotalCap            float64
	PricePerUnit        float64
	BestLatMs           *float64
	AvgLatMs            *float64
	SwapRatio           *float64
	AvgAvail            *float64
	ActivationRound     int
	DeactivationRound   int
}

// FetchResult is returned by each source adapter.
type FetchResult struct {
	Rows  []NormalizedOrch
	Raw   any
	Stats Stats
}

// Adapter fetches orchestrator data from one upstream source.
type Adapter interface {
	Kind() Kind
	FetchAll(ctx context.Context) (FetchResult, error)
}

// Registry maps source kinds to adapters.
type Registry struct {
	adapters map[Kind]Adapter
}

// NewRegistry builds a registry from enabled adapters.
func NewRegistry(adapters ...Adapter) *Registry {
	m := make(map[Kind]Adapter, len(adapters))
	for _, a := range adapters {
		m[a.Kind()] = a
	}
	return &Registry{adapters: m}
}

// Get returns an adapter by kind.
func (r *Registry) Get(kind Kind) (Adapter, bool) {
	a, ok := r.adapters[kind]
	return a, ok
}

func elapsedMs(start time.Time) int64 {
	return time.Since(start).Milliseconds()
}
