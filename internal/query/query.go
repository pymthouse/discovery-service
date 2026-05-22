package query

import (
	"context"
	"math"
	"sort"

	"github.com/livepeer/discovery-service/internal/db"
	"github.com/livepeer/discovery-service/internal/sources"
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
	topN := clampTopN(req.TopN, s.maxTopN)
	sqlFilters := filtersFromRequest(req)
	serviceTypes := serviceTypeFilter(req.ServiceTypes)

	results := make(map[string][]discotypes.DatasetRow)
	for _, cap := range req.Capabilities {
		rows, err := s.store.QueryRows(ctx, cap, serviceTypes, sqlFilters, maxCandidateRows)
		if err != nil {
			return discotypes.QueryResponse{}, err
		}
		results[cap] = finalizeCapabilityRows(evaluateRows(rows, req), req, topN)
	}

	return attachFreshness(s.store, ctx, discotypes.QueryResponse{Results: results})
}

func clampTopN(requested *int, maxTopN int) int {
	topN := defaultTopN
	if requested == nil {
		return topN
	}
	topN = *requested
	if topN < 1 {
		return 1
	}
	if topN > maxTopN {
		return maxTopN
	}
	return topN
}

func filtersFromRequest(req discotypes.QueryRequest) db.QueryFilters {
	if req.Filters == nil {
		return db.QueryFilters{}
	}
	return db.QueryFilters{
		GPURamGbMin:     req.Filters.GPURamGbMin,
		GPURamGbMax:     req.Filters.GPURamGbMax,
		PriceMax:        req.Filters.PriceMax,
		MaxAvgLatencyMs: req.Filters.MaxAvgLatencyMs,
		MaxSwapRatio:    req.Filters.MaxSwapRatio,
	}
}

func finalizeCapabilityRows(
	scored []discotypes.DatasetRow,
	req discotypes.QueryRequest,
	topN int,
) []discotypes.DatasetRow {
	if req.SLAMinScore != nil {
		scored = filterByMinSLA(scored, *req.SLAMinScore)
	}
	sortRows(scored, req.SortBy)
	if len(scored) > topN {
		return scored[:topN]
	}
	return scored
}

func filterByMinSLA(rows []discotypes.DatasetRow, min float64) []discotypes.DatasetRow {
	filtered := rows[:0]
	for _, r := range rows {
		if r.SLAScore != nil && *r.SLAScore >= min {
			filtered = append(filtered, r)
		}
	}
	return filtered
}

func attachFreshness(store *db.Store, ctx context.Context, resp discotypes.QueryResponse) (discotypes.QueryResponse, error) {
	meta, err := store.GetConfig(ctx)
	if err != nil {
		return resp, err
	}
	if meta.LastRefreshedAt == nil {
		return resp, nil
	}
	age := meta.LastRefreshedAt.UnixMilli()
	resp.DatasetVersion = meta.DatasetVersion
	resp.SourceFreshness = &discotypes.FreshnessMeta{
		RefreshedAt:     &age,
		RefreshedBy:     meta.LastRefreshedBy,
		CapabilityCount: len(meta.KnownCapabilities),
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

func serviceTypeFilter(raw []string) []string {
	types := sources.ParseServiceTypes(raw)
	out := make([]string, 0, len(types))
	for _, t := range types {
		out = append(out, string(t))
	}
	return out
}

func flatToAPI(r db.FlatRow) discotypes.DatasetRow {
	return discotypes.DatasetRow{
		ServiceType:       r.ServiceType,
		EthAddress:        r.EthAddress,
		OfferingID:        r.OfferingID,
		InteractionMode:   r.InteractionMode,
		WorkUnit:          r.WorkUnit,
		PricePerUnitWei:   r.PricePerUnitWei,
		OrchURI:           r.OrchURI,
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

	raw := weights{
		latency:  valueOrDefault(w.Latency, def.latency),
		swapRate: valueOrDefault(w.SwapRate, def.swapRate),
		price:    valueOrDefault(w.Price, def.price),
	}
	sum := raw.latency + raw.swapRate + raw.price
	if sum == 0 {
		return def
	}
	return weights{
		latency:  raw.latency / sum,
		swapRate: raw.swapRate / sum,
		price:    raw.price / sum,
	}
}

func valueOrDefault(value *float64, fallback float64) float64 {
	if value == nil {
		return fallback
	}
	return *value
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
		applyRowMinMax(&mm, r)
	}
	return mm
}

func applyRowMinMax(mm *minMax, r db.FlatRow) {
	updateOptionalMinMax(r.BestLatMs, &mm.minLat, &mm.maxLat)
	updateOptionalMinMax(r.SwapRatio, &mm.minSwap, &mm.maxSwap)
	updateMinMax(r.PricePerUnit, &mm.minPrice, &mm.maxPrice)
}

func updateOptionalMinMax(value *float64, min *float64, max *float64) {
	if value == nil {
		return
	}
	updateMinMax(*value, min, max)
}

func updateMinMax(value float64, min *float64, max *float64) {
	if value < *min {
		*min = value
	}
	if value > *max {
		*max = value
	}
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
		sort.Slice(rows, func(i, j int) bool { return lessByLatency(i, j, rows) })
	case "price":
		sort.Slice(rows, func(i, j int) bool { return lessByPrice(i, j, rows) })
	case "swapRate":
		sort.Slice(rows, func(i, j int) bool { return lessBySwapRate(i, j, rows) })
	case "avail":
		sort.Slice(rows, func(i, j int) bool { return lessByAvail(i, j, rows) })
	case "slaScore", "":
		sort.Slice(rows, func(i, j int) bool { return lessBySLAScore(i, j, rows) })
	}
}

func lessByLatency(i, j int, rows []discotypes.DatasetRow) bool {
	a, b := rows[i].BestLatMs, rows[j].BestLatMs
	if a == nil {
		return false
	}
	if b == nil {
		return true
	}
	return *a < *b
}

func lessByPrice(i, j int, rows []discotypes.DatasetRow) bool {
	return rows[i].PricePerUnit < rows[j].PricePerUnit
}

func lessBySwapRate(i, j int, rows []discotypes.DatasetRow) bool {
	a, b := rows[i].SwapRatio, rows[j].SwapRatio
	if a == nil {
		return false
	}
	if b == nil {
		return true
	}
	return *a < *b
}

func lessByAvail(i, j int, rows []discotypes.DatasetRow) bool {
	return rows[i].Avail > rows[j].Avail
}

func lessBySLAScore(i, j int, rows []discotypes.DatasetRow) bool {
	a, b := 0.0, 0.0
	if rows[i].SLAScore != nil {
		a = *rows[i].SLAScore
	}
	if rows[j].SLAScore != nil {
		b = *rows[j].SLAScore
	}
	return a > b
}
