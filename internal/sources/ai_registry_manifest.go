package sources

import (
	"context"
	"strings"
	"sync"
	"time"

	"github.com/livepeer/discovery-service/internal/config"
)

// AIRegistryManifestAdapter reads serviceURI pointers from the AI Service Registry
// contract, then probes the advertised HTTPS host for registry manifests.
type AIRegistryManifestAdapter struct {
	cfg      config.Config
	subgraph *SubgraphAdapter
}

func NewAIRegistryManifest(cfg config.Config) *AIRegistryManifestAdapter {
	return &AIRegistryManifestAdapter{
		cfg:      cfg,
		subgraph: NewSubgraph(cfg),
	}
}

func (a *AIRegistryManifestAdapter) Kind() Kind { return KindAIRegistryManifest }

func (a *AIRegistryManifestAdapter) FetchAll(ctx context.Context) (FetchResult, error) {
	start := time.Now()
	if !a.cfg.RegistryManifestRefreshEnabled {
		return FetchResult{
			Stats: Stats{OK: true, Fetched: 0, DurationMs: elapsedMs(start)},
		}, nil
	}
	if strings.TrimSpace(a.cfg.AIServiceRegistryAddress) == "" {
		return FetchResult{
			Stats: Stats{OK: true, Fetched: 0, DurationMs: elapsedMs(start)},
		}, nil
	}

	sub, err := a.subgraph.FetchAll(ctx)
	if err != nil {
		return FetchResult{Stats: Stats{OK: false, DurationMs: elapsedMs(start), ErrorMessage: err.Error()}}, err
	}

	refs := collectAIRegistryRefs(ctx, a.cfg, sub.Rows)

	all := fetchRegistryManifestRefs(ctx, refs, a.cfg)
	return FetchResult{
		Rows:  all,
		Stats: Stats{OK: true, Fetched: len(all), DurationMs: elapsedMs(start)},
	}, nil
}

func collectAIRegistryRefs(
	ctx context.Context,
	cfg config.Config,
	rows []NormalizedOrch,
) []registryManifestRef {
	maxOrchestrators := cfg.RegistryManifestMaxOrchestrators
	if maxOrchestrators <= 0 {
		maxOrchestrators = 1000
	}
	concurrency := cfg.RegistryManifestMaxConcurrency
	if concurrency <= 0 {
		concurrency = 25
	}

	type lookupResult struct {
		eth        string
		serviceURI string
	}
	results := make(chan lookupResult, len(rows))
	sem := make(chan struct{}, concurrency)
	var wg sync.WaitGroup
	launched := 0

	for _, row := range rows {
		if row.EthAddress == "" {
			continue
		}
		launched++
		if launched > maxOrchestrators {
			break
		}
		wg.Add(1)
		go func(eth string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			serviceURI, err := lookupServiceURI(
				ctx,
				cfg.AIServiceRegistryRPCURL,
				cfg.AIServiceRegistryAddress,
				eth,
				"discovery-service/ai-registry",
				registryManifestTimeout(cfg),
			)
			if err != nil || serviceURI == "" {
				return
			}
			results <- lookupResult{eth: eth, serviceURI: serviceURI}
		}(row.EthAddress)
	}

	go func() {
		wg.Wait()
		close(results)
	}()

	refs := make([]registryManifestRef, 0)
	seenURI := make(map[string]struct{})
	for result := range results {
		if _, ok := seenURI[result.serviceURI]; ok {
			continue
		}
		seenURI[result.serviceURI] = struct{}{}
		refs = append(refs, registryManifestRef(result))
	}
	return refs
}
