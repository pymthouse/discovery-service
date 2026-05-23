package resolver

import "github.com/livepeer/discovery-service/internal/sources"

// DatasetRow is the canonical per-capability row stored in Postgres.
type DatasetRow struct {
	ServiceType     string
	EthAddress      string
	OrchURI         string
	GPUName         string
	GPUGb           float64
	Avail           float64
	TotalCap        float64
	PricePerUnit    float64
	BestLatMs       *float64
	AvgLatMs        *float64
	SwapRatio       *float64
	AvgAvail        *float64
	Score           float64
	OfferingID      string
	InteractionMode string
	WorkUnit        string
	PricePerUnitWei string
}

// SourceConfig describes an enabled source for resolution.
type SourceConfig struct {
	Kind     sources.Kind
	Priority int
	Enabled  bool
}

// Config drives resolver behavior.
type Config struct {
	Sources            []SourceConfig
	FieldPriority      map[string][]sources.Kind
	MembershipStrategy string
}

// ConflictEntry records a field-level merge conflict.
type ConflictEntry struct {
	OrchKey string       `json:"orchKey"`
	Field   string       `json:"field"`
	Winner  sources.Kind `json:"winner"`
	Losers  []LoserEntry `json:"losers"`
}

// LoserEntry is a conflicting source value.
type LoserEntry struct {
	Source sources.Kind `json:"source"`
	Value  any          `json:"value"`
}

// DroppedEntry records orchestrators excluded from membership.
type DroppedEntry struct {
	OrchKey string       `json:"orchKey"`
	Source  sources.Kind `json:"source"`
	Reason  string       `json:"reason"`
}

// AuditEntry summarizes a resolution run.
type AuditEntry struct {
	MembershipSource   string          `json:"membershipSource"`
	TotalOrchestrators int             `json:"totalOrchestrators"`
	TotalCapabilities  int             `json:"totalCapabilities"`
	Conflicts          []ConflictEntry `json:"conflicts"`
	Dropped            []DroppedEntry  `json:"dropped"`
	Warnings           []string        `json:"warnings"`
	PerSourceCounts    map[string]int  `json:"perSourceCounts"`
}

// Result is the output of Resolve.
type Result struct {
	Capabilities map[string][]DatasetRow
	Audit        AuditEntry
}
