package query

import (
	"context"
	"math"
	"sort"

	"github.com/livepeer/discovery-service/internal/db"
	"github.com/livepeer/discovery-service/pkg/discotypes"
)

const defaultTopN = 10
const maxCandidateRows = 1000

// Service evaluates client-driven queries over the materialized dataset.
type Service struct {
	store   *db.Store
	maxTopN int
}

// New creates a query service.
func New(store *db.Store, maxTopN int) *Service {
	if maxTopN <= 0 {
		maxTopN = 1000
	}
	return &Service{store: store, maxTopN: maxTopN}
}

// Execute runs POST /v1/discovery/query logic.
func (s *Service) Execute(ctx context.Context, req discotypes.QueryRequest) (discotypes.QueryResponse, error) {
	topN := defaultTopN
	if req.TopN != nil {
		topN = *req.TopN
		if topN < 1 {
			topN = 1
		}
		if topN > s.maxTopN {
			topN = s.maxTopN
		}
	}

	sqlFilters := db.QueryFilters{}
	if req.Filters != nil {
		sqlFilters.GPURamGbMin = req.Filters.GPURamGbMin
		sqlFilters.GPURamGbMax = req.Filters.GPURamGbMax
		sqlFilters.PriceMax = req.Filters.PriceMax
		sqlFilters.MaxAvgLatencyMs = req.Filters.MaxAvgLatencyMs
		sqlFilters.MaxSwapRatio = req.Filters.MaxSwapRatio
	}

	results := make(map[string][]discotypes.DatasetRow)
	for _, cap := range req.Capabilities {
		rows, err := s.store.QueryRows(ctx, cap, sqlFilters, maxCandidateRows)
		if err != nil {
			return discotypes.QueryResponse{}, err
		}
		scored := evaluateRows(rows, req)
		if req.SLAMinScore != nil {
			min := *req.SLAMinScore
			filtered := scored[:0]
			for _, r := range scored {
				if r.SLAScore != nil && *r.SLAScore >= min {
					filtered = append(filtered, r)
				}
			}
			scored = filtered
		}
		sortRows(scored, req.SortBy)
		if len(scored) > topN {
			scored = scored[:topN]
		}
		results[cap] = scored
	}

	meta, _ := s.store.GetConfig(ctx)
	resp := discotypes.QueryResponse{Results: results}
	if meta.LastRefreshedAt != nil {
		age := meta.LastRefreshedAt.UnixMilli()
		resp.DatasetVersion = meta.DatasetVersion
		resp.SourceFreshness = &discotypes.FreshnessMeta{
			RefreshedAt:     &age,
			RefreshedBy:     meta.LastRefreshedBy,
			CapabilityCount: len(meta.KnownCapabilities),
		}
	}
	return resp, nil
}

func evaluateRows(rows []db.FlatRow, req discotypes.QueryRequest) []discotypes.DatasetRow {
	chRows := make([]db.FlatRow, len(rows))
	copy(chRows, rows)

	useSLA := req.SLAWeights != nil || req.SLAMinScore != nil || req.SortBy == "slaScore"
	if !useSLA {
		out := make([]discotypes.DatasetRow, len(rows))
		for i, r := range rows {
			out[i] = flatToAPI(r)
		}
		return out
	}

	w := normalizeWeights(req.SLAWeights)
	mm := computeMinMax(chRows)
	out := make([]discotypes.DatasetRow, len(rows))
	for i, r := range rows {
		row := flatToAPI(r)
		score := computeSLAScore(r, w, mm)
		row.SLAScore = &score
		out[i] = row
	}
	return out
}

func flatToAPI(r db.FlatRow) discotypes.DatasetRow {
	return discotypes.DatasetRow{
		OrchURI:      r.OrchURI,
		GPUName:      r.GPUName,
		GPUGb:        r.GPUGb,
		Avail:        r.Avail,
		TotalCap:     r.TotalCap,
		PricePerUnit: r.PricePerUnit,
		BestLatMs:    r.BestLatMs,
		AvgLatMs:     r.AvgLatMs,
		SwapRatio:    r.SwapRatio,
		AvgAvail:     r.AvgAvail,
		Score:        r.Score,
	}
}

type weights struct {
	latency  float64
	swapRate float64
	price    float64
}

func normalizeWeights(w *discotypes.SLAWeights) weights {
	def := weights{latency: 0.4, swapRate: 0.3, price: 0.3}
	if w == nil {
		return def
	}
	lat := 0.4
	swap := 0.3
	price := 0.3
	if w.Latency != nil {
		lat = *w.Latency
	}
	if w.SwapRate != nil {
		swap = *w.SwapRate
	}
	if w.Price != nil {
		price = *w.Price
	}
	sum := lat + swap + price
	if sum == 0 {
		return def
	}
	return weights{latency: lat / sum, swapRate: swap / sum, price: price / sum}
}

type minMax struct {
	minLat, maxLat     float64
	minSwap, maxSwap   float64
	minPrice, maxPrice float64
}

func computeMinMax(rows []db.FlatRow) minMax {
	mm := minMax{
		minLat: math.Inf(1), maxLat: math.Inf(-1),
		minSwap: math.Inf(1), maxSwap: math.Inf(-1),
		minPrice: math.Inf(1), maxPrice: math.Inf(-1),
	}
	for _, r := range rows {
		if r.BestLatMs != nil {
			if *r.BestLatMs < mm.minLat {
				mm.minLat = *r.BestLatMs
			}
			if *r.BestLatMs > mm.maxLat {
				mm.maxLat = *r.BestLatMs
			}
		}
		if r.SwapRatio != nil {
			if *r.SwapRatio < mm.minSwap {
				mm.minSwap = *r.SwapRatio
			}
			if *r.SwapRatio > mm.maxSwap {
				mm.maxSwap = *r.SwapRatio
			}
		}
		if r.PricePerUnit < mm.minPrice {
			mm.minPrice = r.PricePerUnit
		}
		if r.PricePerUnit > mm.maxPrice {
			mm.maxPrice = r.PricePerUnit
		}
	}
	return mm
}

func norm(value *float64, min, max float64) float64 {
	if value == nil {
		return 0.5
	}
	if max == min {
		return 1
	}
	return 1 - (*value-min)/(max-min)
}

func computeSLAScore(r db.FlatRow, w weights, mm minMax) float64 {
	lat := norm(r.BestLatMs, mm.minLat, mm.maxLat)
	swap := norm(r.SwapRatio, mm.minSwap, mm.maxSwap)
	price := norm(&r.PricePerUnit, mm.minPrice, mm.maxPrice)
	score := w.latency*lat + w.swapRate*swap + w.price*price
	return math.Round(score*1000) / 1000
}

func sortRows(rows []discotypes.DatasetRow, sortBy string) {
	switch sortBy {
	case "latency":
		sort.Slice(rows, func(i, j int) bool {
			a, b := rows[i].BestLatMs, rows[j].BestLatMs
			if a == nil {
				return false
			}
			if b == nil {
				return true
			}
			return *a < *b
		})
	case "price":
		sort.Slice(rows, func(i, j int) bool {
			return rows[i].PricePerUnit < rows[j].PricePerUnit
		})
	case "swapRate":
		sort.Slice(rows, func(i, j int) bool {
			a, b := rows[i].SwapRatio, rows[j].SwapRatio
			if a == nil {
				return false
			}
			if b == nil {
				return true
			}
			return *a < *b
		})
	case "avail":
		sort.Slice(rows, func(i, j int) bool {
			return rows[i].Avail > rows[j].Avail
		})
	case "slaScore", "":
		sort.Slice(rows, func(i, j int) bool {
			a, b := 0.0, 0.0
			if rows[i].SLAScore != nil {
				a = *rows[i].SLAScore
			}
			if rows[j].SLAScore != nil {
				b = *rows[j].SLAScore
			}
			return a > b
		})
	}
}
