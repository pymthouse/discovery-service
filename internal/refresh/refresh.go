package refresh

import (
	"context"
	"encoding/json"
	"sync"
	"time"

	"github.com/livepeer/discovery-service/internal/config"
	"github.com/livepeer/discovery-service/internal/db"
	"github.com/livepeer/discovery-service/internal/resolver"
	"github.com/livepeer/discovery-service/internal/sources"
	"github.com/livepeer/discovery-service/pkg/discotypes"
)

// Service runs global dataset refresh.
type Service struct {
	cfg      config.Config
	store    *db.Store
	registry *sources.Registry
}

// New creates a refresh service.
func New(cfg config.Config, store *db.Store, registry *sources.Registry) *Service {
	return &Service{cfg: cfg, store: store, registry: registry}
}

// Run fetches all enabled sources, resolves, and writes the dataset.
func (s *Service) Run(ctx context.Context, refreshedBy string) (discotypes.RefreshResult, error) {
	t0 := time.Now()
	result := discotypes.RefreshResult{Refreshed: true}

	sourceRows, err := s.store.LoadSources(ctx)
	if err != nil {
		return result, err
	}

	resolverCfg := resolver.Config{MembershipStrategy: s.cfg.MembershipStrategy}
	for _, row := range sourceRows {
		resolverCfg.Sources = append(resolverCfg.Sources, resolver.SourceConfig{
			Kind:     sources.Kind(row.Kind),
			Priority: row.Priority,
			Enabled:  row.Enabled,
		})
	}

	perSource := make(map[sources.Kind][]sources.NormalizedOrch)
	sourceStats := make(map[string]sources.Stats)
	var mu sync.Mutex
	var wg sync.WaitGroup

	fetchCtx := sources.FetchCtx{}
	_ = fetchCtx

	for _, sc := range resolverCfg.Sources {
		if !sc.Enabled {
			continue
		}
		adapter, ok := s.registry.Get(sc.Kind)
		if !ok {
			continue
		}
		wg.Add(1)
		go func(kind sources.Kind, a sources.Adapter) {
			defer wg.Done()
			fr, err := a.FetchAll(ctx)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				sourceStats[string(kind)] = sources.Stats{
					OK:           false,
					Fetched:      0,
					DurationMs:   time.Since(t0).Milliseconds(),
					ErrorMessage: err.Error(),
				}
				perSource[kind] = nil
				return
			}
			perSource[kind] = fr.Rows
			sourceStats[string(kind)] = fr.Stats
		}(sc.Kind, adapter)
	}
	wg.Wait()

	res := resolver.Resolve(perSource, resolverCfg)

	flat := make(map[string][]db.FlatRow)
	for cap, rows := range res.Capabilities {
		for _, r := range rows {
			flat[cap] = append(flat[cap], db.FlatRow{
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
			})
		}
	}

	_, capCount, err := s.store.WriteDataset(ctx, flat, refreshedBy)
	if err != nil {
		return result, err
	}

	auditJSON, _ := json.Marshal(res.Audit)
	perSourceJSON, _ := json.Marshal(sourceStats)
	_ = s.store.WriteAudit(ctx, refreshedBy, time.Since(t0).Milliseconds(), auditJSON, perSourceJSON)

	result.Capabilities = capCount
	result.Orchestrators = res.Audit.TotalOrchestrators
	result.DurationMs = time.Since(t0).Milliseconds()
	return result, nil
}
