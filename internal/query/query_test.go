package query

import (
	"testing"

	"github.com/livepeer/discovery-service/internal/db"
	"github.com/livepeer/discovery-service/pkg/discotypes"
)

func TestComputeSLAScoreOrdering(t *testing.T) {
	rows := []db.FlatRow{
		{OrchURI: "a", PricePerUnit: 10, BestLatMs: ptr(100.0), SwapRatio: ptr(0.1)},
		{OrchURI: "b", PricePerUnit: 1, BestLatMs: ptr(50.0), SwapRatio: ptr(0.05)},
	}
	out := evaluateRows(rows, discotypes.QueryRequest{SortBy: "slaScore", SLAWeights: &discotypes.SLAWeights{}})
	if len(out) != 2 {
		t.Fatalf("expected 2 rows, got %d", len(out))
	}
	if out[0].SLAScore == nil || out[1].SLAScore == nil {
		t.Fatal("expected sla scores")
	}
}

func ptr(f float64) *float64 { return &f }
