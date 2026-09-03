package sources

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/livepeer/discovery-service/internal/config"
)

const (
	firstTranscoderABI = "getFirstTranscoderInPool()"
	nextTranscoderABI  = "getNextTranscoderInPool(address)"
)

// ServiceRegistryAdapter walks the BondingManager transcoder pool and reads
// registered serviceURIs the same way go-livepeer discovery does
// (TranscoderPool + GetServiceURI). Protocol ServiceRegistry and AI
// ServiceRegistry are both queried so classic and AI URIs are listed.
type ServiceRegistryAdapter struct {
	cfg config.Config
}

func NewServiceRegistry(cfg config.Config) *ServiceRegistryAdapter {
	return &ServiceRegistryAdapter{
		cfg: cfg,
	}
}

func (a *ServiceRegistryAdapter) Kind() Kind { return KindServiceRegistry }

func (a *ServiceRegistryAdapter) FetchAll(ctx context.Context) (FetchResult, error) {
	start := time.Now()
	if !a.cfg.ServiceRegistryRefreshEnabled {
		return FetchResult{
			Stats: Stats{OK: true, Fetched: 0, DurationMs: elapsedMs(start)},
		}, nil
	}

	rpcURL := strings.TrimSpace(a.cfg.AIServiceRegistryRPCURL)
	bonding := strings.TrimSpace(a.cfg.BondingManagerAddress)
	if rpcURL == "" || bonding == "" {
		return FetchResult{
			Stats: Stats{OK: true, Fetched: 0, DurationMs: elapsedMs(start)},
		}, nil
	}

	timeout := registryManifestTimeout(a.cfg)
	addrs, err := walkTranscoderPool(ctx, rpcURL, bonding, a.cfg.RegistryManifestMaxOrchestrators, timeout)
	if err != nil {
		return FetchResult{
			Stats: Stats{OK: false, DurationMs: elapsedMs(start), ErrorMessage: err.Error()},
		}, err
	}

	rows := collectRegisteredServiceURIs(ctx, a.cfg, addrs, timeout)
	return FetchResult{
		Rows:  rows,
		Stats: Stats{OK: true, Fetched: len(rows), DurationMs: elapsedMs(start)},
	}, nil
}

func walkTranscoderPool(
	ctx context.Context,
	rpcURL string,
	bondingManager string,
	maxOrchestrators int,
	timeout time.Duration,
) ([]string, error) {
	if maxOrchestrators <= 0 {
		maxOrchestrators = 1000
	}

	firstData := "0x" + methodSelector(firstTranscoderABI)
	raw, err := ethCall(ctx, rpcURL, bondingManager, firstData, "discovery-service/service-registry", timeout)
	if err != nil {
		return nil, fmt.Errorf("BondingManager getFirstTranscoderInPool: %w", err)
	}
	addr, err := decodeABIAddress(raw)
	if err != nil {
		return nil, fmt.Errorf("BondingManager getFirstTranscoderInPool: %w", err)
	}

	out := make([]string, 0)
	seen := make(map[string]struct{})
	for !isZeroAddress(addr) {
		key := strings.ToLower(addr)
		if _, ok := seen[key]; ok {
			break
		}
		seen[key] = struct{}{}
		out = append(out, key)
		if len(out) >= maxOrchestrators {
			break
		}

		padded, err := paddedAddress(addr)
		if err != nil {
			return nil, err
		}
		nextData := "0x" + methodSelector(nextTranscoderABI) + padded
		raw, err = ethCall(ctx, rpcURL, bondingManager, nextData, "discovery-service/service-registry", timeout)
		if err != nil {
			return nil, fmt.Errorf("BondingManager getNextTranscoderInPool: %w", err)
		}
		addr, err = decodeABIAddress(raw)
		if err != nil {
			return nil, fmt.Errorf("BondingManager getNextTranscoderInPool: %w", err)
		}
	}
	return out, nil
}

func collectRegisteredServiceURIs(
	ctx context.Context,
	cfg config.Config,
	addrs []string,
	timeout time.Duration,
) []NormalizedOrch {
	contracts := registeredServiceRegistryContracts(cfg)
	if len(contracts) == 0 || len(addrs) == 0 {
		return nil
	}

	concurrency := registryManifestConcurrency(cfg)
	type lookupResult struct {
		eth        string
		serviceURI string
	}
	results := make(chan lookupResult, len(addrs)*len(contracts))
	sem := make(chan struct{}, concurrency)
	var wg sync.WaitGroup

	for _, eth := range addrs {
		for _, contract := range contracts {
			wg.Add(1)
			go func(eth string, contract string) {
				defer wg.Done()
				sem <- struct{}{}
				defer func() { <-sem }()

				serviceURI, err := lookupServiceURI(
					ctx,
					cfg.AIServiceRegistryRPCURL,
					contract,
					eth,
					"discovery-service/service-registry",
					timeout,
				)
				if err != nil {
					return
				}
				serviceURI = normalizeRegisteredServiceURI(serviceURI)
				if serviceURI == "" {
					return
				}
				results <- lookupResult{
					eth:        eth,
					serviceURI: serviceURI,
				}
			}(eth, contract)
		}
	}

	go func() {
		wg.Wait()
		close(results)
	}()

	out := make([]NormalizedOrch, 0)
	seen := make(map[string]struct{})
	for result := range results {
		key := result.eth + "\x00" + result.serviceURI
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, NormalizedOrch{
			EthAddress: result.eth,
			OrchURI:    result.serviceURI,
		})
	}
	return out
}

func registeredServiceRegistryContracts(cfg config.Config) []string {
	out := make([]string, 0, 2)
	seen := make(map[string]struct{})
	for _, contract := range []string{cfg.ServiceRegistryAddress, cfg.AIServiceRegistryAddress} {
		contract = strings.TrimSpace(contract)
		if contract == "" {
			continue
		}
		key := strings.ToLower(contract)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, contract)
	}
	return out
}

// normalizeRegisteredServiceURI matches go-livepeer discovery.parseURI:
// a missing scheme is treated as https://.
func normalizeRegisteredServiceURI(serviceURI string) string {
	serviceURI = strings.TrimSpace(serviceURI)
	if serviceURI == "" {
		return ""
	}
	if !strings.HasPrefix(strings.ToLower(serviceURI), "http") {
		serviceURI = "https://" + serviceURI
	}
	u, err := url.ParseRequestURI(serviceURI)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return ""
	}
	return strings.TrimRight(u.String(), "/")
}
