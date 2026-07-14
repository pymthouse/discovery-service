package refresh

import (
	"context"
	"encoding/json"
	"strings"
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

	legacyPerSource := make(map[sources.Kind][]sources.NormalizedOrch)
	var registryRows []sources.NormalizedOrch
	for kind, rows := range perSource {
		if kind == sources.KindRegistryManifest || kind == sources.KindAIRegistryManifest {
			registryRows = append(registryRows, rows...)
			continue
		}
		legacyPerSource[kind] = rows
	}

	legacyRes := resolver.Resolve(legacyPerSource, resolverCfg)
	registryCaps := resolver.BuildRegistryDataset(registryRows)

	flat := make([]db.FlatRow, 0)
	flat = append(flat, resolverRowsToFlat(legacyRes.Capabilities)...)
	flat = append(flat, resolverRowsToFlat(registryCaps)...)

	liveRunnerClaims, liveRunnerStats := collectLiveRunnerAppClaims(ctx, s.cfg, perSource)
	sourceStats[string(sources.KindOrchDiscovery)] = liveRunnerStats
	flat = append(flat, liveRunnerClaimsToFlat(liveRunnerClaims)...)

	_, capCount, err := s.store.WriteDataset(ctx, flat, refreshedBy)
	if err != nil {
		return result, err
	}

	auditJSON, _ := json.Marshal(legacyRes.Audit)
	perSourceJSON, _ := json.Marshal(sourceStats)
	_ = s.store.WriteAudit(ctx, refreshedBy, time.Since(t0).Milliseconds(), auditJSON, perSourceJSON)

	result.Capabilities = capCount
	result.Orchestrators = legacyRes.Audit.TotalOrchestrators + countRegistryOrchestrators(registryRows)
	result.DurationMs = time.Since(t0).Milliseconds()
	return result, nil
}

func collectLiveRunnerAppClaims(
	ctx context.Context,
	cfg config.Config,
	perSource map[sources.Kind][]sources.NormalizedOrch,
) ([]sources.LiveRunnerAppClaim, sources.Stats) {
	signerClaims := liveRunnerClaimsFromRemoteSigner(perSource[sources.KindRemoteSigner])
	if !cfg.OrchDiscoveryRefreshEnabled {
		merged := sources.MergeLiveRunnerAppClaims(nil, signerClaims)
		return merged, sources.Stats{
			OK:      true,
			Fetched: len(merged),
		}
	}

	orchURIs := sources.CollectOrchURIs(perSource, cfg.OrchDiscoveryMaxOrchestrators)
	orchURIs = sources.AppendOrchURIs(orchURIs, cfg.OrchDiscoveryExtraURIs, cfg.OrchDiscoveryMaxOrchestrators)
	probeClaims, stats := sources.ProbeOrchDiscovery(ctx, orchURIs, sources.ProbeOptionsFromConfig(cfg))
	merged := sources.MergeLiveRunnerAppClaims(probeClaims, signerClaims)
	stats.Fetched = len(merged)
	return merged, stats
}

func liveRunnerClaimsFromRemoteSigner(rows []sources.NormalizedOrch) []sources.LiveRunnerAppClaim {
	out := make([]sources.LiveRunnerAppClaim, 0)
	seen := make(map[string]struct{})
	for _, r := range rows {
		orch := strings.TrimRight(strings.TrimSpace(r.OrchURI), "/")
		if orch == "" {
			continue
		}
		score := r.Score
		if score == 0 {
			score = 1
		}
		for _, app := range r.LiveRunnerApps {
			app = strings.TrimSpace(app)
			if app == "" {
				continue
			}
			key := orch + "\x00" + app
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			out = append(out, sources.LiveRunnerAppClaim{
				OrchURI: orch,
				App:     app,
				Score:   score,
			})
		}
	}
	return out
}

func liveRunnerClaimsToFlat(claims []sources.LiveRunnerAppClaim) []db.FlatRow {
	flat := make([]db.FlatRow, 0, len(claims))
	for _, c := range claims {
		if c.OrchURI == "" || c.App == "" {
			continue
		}
		score := c.Score
		if score == 0 {
			score = 1
		}
		flat = append(flat, db.FlatRow{
			ServiceType: string(sources.ServiceTypeLiveRunner),
			Capability:  c.App,
			OrchURI:     c.OrchURI,
			Score:       score,
		})
	}
	return flat
}

func resolverRowsToFlat(capabilities map[string][]resolver.DatasetRow) []db.FlatRow {
	flat := make([]db.FlatRow, 0)
	for cap, rows := range capabilities {
		for _, r := range rows {
			if r.OrchURI == "" {
				continue
			}
			serviceType := r.ServiceType
			if serviceType == "" {
				serviceType = string(sources.ServiceTypeLiveVideoToVideo)
			}
			flat = append(flat, db.FlatRow{
				ServiceType:     serviceType,
				Capability:      cap,
				EthAddress:      r.EthAddress,
				OrchURI:         r.OrchURI,
				GPUName:         r.GPUName,
				GPUGb:           r.GPUGb,
				Avail:           r.Avail,
				TotalCap:        r.TotalCap,
				PricePerUnit:    r.PricePerUnit,
				BestLatMs:       r.BestLatMs,
				AvgLatMs:        r.AvgLatMs,
				SwapRatio:       r.SwapRatio,
				AvgAvail:        r.AvgAvail,
				Score:           r.Score,
				OfferingID:      r.OfferingID,
				InteractionMode: r.InteractionMode,
				WorkUnit:        r.WorkUnit,
				PricePerUnitWei: r.PricePerUnitWei,
			})
		}
	}
	return flat
}

func countRegistryOrchestrators(rows []sources.NormalizedOrch) int {
	seen := make(map[string]struct{})
	for _, r := range rows {
		key := r.OrchURI
		if key == "" {
			key = r.EthAddress
		}
		if key == "" {
			continue
		}
		seen[key] = struct{}{}
	}
	return len(seen)
}
