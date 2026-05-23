package sources

import (
	"context"
	"sync"
	"time"

	"github.com/livepeer/discovery-service/internal/config"
)

// RegistryManifestAdapter probes on-chain serviceURI manifests for registry capabilities.
type RegistryManifestAdapter struct {
	cfg      config.Config
	subgraph *SubgraphAdapter
}

type registryManifestRef struct {
	eth        string
	serviceURI string
}

func NewRegistryManifest(cfg config.Config) *RegistryManifestAdapter {
	return &RegistryManifestAdapter{
		cfg:      cfg,
		subgraph: NewSubgraph(cfg),
	}
}

func (a *RegistryManifestAdapter) Kind() Kind { return KindRegistryManifest }

func (a *RegistryManifestAdapter) FetchAll(ctx context.Context) (FetchResult, error) {
	start := time.Now()
	if !a.cfg.RegistryManifestRefreshEnabled {
		return FetchResult{
			Stats: Stats{OK: true, Fetched: 0, DurationMs: elapsedMs(start)},
		}, nil
	}

	sub, err := a.subgraph.FetchAll(ctx)
	if err != nil {
		return FetchResult{Stats: Stats{OK: false, DurationMs: elapsedMs(start), ErrorMessage: err.Error()}}, err
	}

	refs := registryManifestRefsFromSubgraph(sub.Rows, a.cfg.RegistryManifestMaxOrchestrators)
	all := fetchRegistryManifestRefs(ctx, refs, a.cfg)
	return FetchResult{
		Rows:  all,
		Stats: Stats{OK: true, Fetched: len(all), DurationMs: elapsedMs(start)},
	}, nil
}

func registryManifestRefsFromSubgraph(rows []NormalizedOrch, maxOrchestrators int) []registryManifestRef {
	if maxOrchestrators <= 0 {
		maxOrchestrators = 1000
	}
	refs := make([]registryManifestRef, 0, len(rows))
	seenURI := make(map[string]struct{})
	for _, row := range rows {
		if row.OrchURI == "" {
			continue
		}
		if _, ok := seenURI[row.OrchURI]; ok {
			continue
		}
		seenURI[row.OrchURI] = struct{}{}
		refs = append(refs, registryManifestRef{eth: row.EthAddress, serviceURI: row.OrchURI})
		if len(refs) >= maxOrchestrators {
			break
		}
	}
	return refs
}

func fetchRegistryManifestRefs(
	ctx context.Context,
	refs []registryManifestRef,
	cfg config.Config,
) []NormalizedOrch {
	timeout := registryManifestTimeout(cfg)
	concurrency := registryManifestConcurrency(cfg)

	type probeResult struct {
		rows []NormalizedOrch
	}
	results := make(chan probeResult, len(refs))
	sem := make(chan struct{}, concurrency)
	var wg sync.WaitGroup

	for _, ref := range refs {
		wg.Add(1)
		go func(ref registryManifestRef) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			rows := fetchRegistryManifestRef(ctx, ref, timeout)
			if len(rows) > 0 {
				results <- probeResult{rows: rows}
			}
		}(ref)
	}

	go func() {
		wg.Wait()
		close(results)
	}()

	var all []NormalizedOrch
	for res := range results {
		all = append(all, res.rows...)
	}
	return all
}

func fetchRegistryManifestRef(ctx context.Context, ref registryManifestRef, timeout time.Duration) []NormalizedOrch {
	probeCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	body, ok := fetchFirstManifest(probeCtx, ref.serviceURI, timeout)
	if !ok {
		return nil
	}
	parsed, err := ParseRegistryManifestBody(body)
	if err != nil || len(parsed) == 0 {
		return nil
	}
	for i := range parsed {
		if parsed[i].EthAddress == "" {
			parsed[i].EthAddress = ref.eth
		}
	}
	return registryRowsToNormalized(parsed)
}

func registryManifestTimeout(cfg config.Config) time.Duration {
	timeout := time.Duration(cfg.RegistryManifestTimeoutMs) * time.Millisecond
	if timeout <= 0 {
		return 5 * time.Second
	}
	return timeout
}

func registryManifestConcurrency(cfg config.Config) int {
	if cfg.RegistryManifestMaxConcurrency <= 0 {
		return 25
	}
	return cfg.RegistryManifestMaxConcurrency
}

func fetchFirstManifest(ctx context.Context, serviceURI string, timeout time.Duration) ([]byte, bool) {
	for _, candidate := range ManifestFetchCandidates(serviceURI) {
		body, err := httpGetTimeout(ctx, candidate, nil, timeout)
		if err != nil {
			continue
		}
		parsed, _ := ParseRegistryManifestBody(body)
		if len(parsed) > 0 {
			return body, true
		}
	}
	return nil, false
}
