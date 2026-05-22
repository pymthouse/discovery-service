// Package discotypes defines public discovery API types.
package discotypes

// DatasetRow is the canonical materialized orchestrator row per capability.
type DatasetRow struct {
	OrchURI      string   `json:"orchUri"`
	GPUName      string   `json:"gpuName"`
	GPUGb        float64  `json:"gpuGb"`
	Avail        float64  `json:"avail"`
	TotalCap     float64  `json:"totalCap"`
	PricePerUnit float64  `json:"pricePerUnit"`
	BestLatMs    *float64 `json:"bestLatMs"`
	AvgLatMs     *float64 `json:"avgLatMs"`
	SwapRatio    *float64 `json:"swapRatio"`
	AvgAvail     *float64 `json:"avgAvail"`
	Score        float64  `json:"score,omitempty"`
	SLAScore     *float64 `json:"slaScore,omitempty"`
}

// Filters are client-supplied query constraints (no persisted plans).
type Filters struct {
	GPURamGbMin     *float64 `json:"gpuRamGbMin,omitempty"`
	GPURamGbMax     *float64 `json:"gpuRamGbMax,omitempty"`
	PriceMax        *float64 `json:"priceMax,omitempty"`
	MaxAvgLatencyMs *float64 `json:"maxAvgLatencyMs,omitempty"`
	MaxSwapRatio    *float64 `json:"maxSwapRatio,omitempty"`
}

// SLAWeights for optional weighted reranking.
type SLAWeights struct {
	Latency  *float64 `json:"latency,omitempty"`
	SwapRate *float64 `json:"swapRate,omitempty"`
	Price    *float64 `json:"price,omitempty"`
}

// QueryRequest is POST /v1/discovery/query body.
type QueryRequest struct {
	Capabilities []string    `json:"capabilities"`
	TopN         *int        `json:"topN,omitempty"`
	Filters      *Filters    `json:"filters,omitempty"`
	SLAWeights   *SLAWeights `json:"slaWeights,omitempty"`
	SLAMinScore  *float64    `json:"slaMinScore,omitempty"`
	SortBy       string      `json:"sortBy,omitempty"`
}

// QueryResponse is POST /v1/discovery/query response.
type QueryResponse struct {
	Results         map[string][]DatasetRow `json:"results"`
	DatasetVersion  int64                   `json:"datasetVersion"`
	QueryTimeMs     int64                   `json:"queryTimeMs"`
	SourceFreshness *FreshnessMeta          `json:"sourceFreshness,omitempty"`
}

// FreshnessMeta describes materialized dataset age.
type FreshnessMeta struct {
	RefreshedAt     *int64 `json:"refreshedAt,omitempty"`
	RefreshedBy     string `json:"refreshedBy,omitempty"`
	AgeMs           int64  `json:"ageMs"`
	CapabilityCount int    `json:"capabilityCount"`
}

// WebhookOrchestrator matches go-livepeer remote signer / webhook discovery JSON.
type WebhookOrchestrator struct {
	Address      string   `json:"address"`
	Score        float32  `json:"score"`
	Capabilities []string `json:"capabilities"`
}

// RefreshResult is returned from dataset refresh.
type RefreshResult struct {
	Refreshed     bool  `json:"refreshed"`
	Capabilities  int   `json:"capabilities"`
	Orchestrators int   `json:"orchestrators"`
	DurationMs    int64 `json:"durationMs"`
}
