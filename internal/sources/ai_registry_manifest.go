package sources

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/livepeer/discovery-service/internal/config"
	"golang.org/x/crypto/sha3"
)

const serviceURIABI = "getServiceURI(address)"

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

			serviceURI, err := lookupAIRegistryServiceURI(ctx, cfg, eth)
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

func lookupAIRegistryServiceURI(ctx context.Context, cfg config.Config, ethAddress string) (string, error) {
	rpcURL := strings.TrimSpace(cfg.AIServiceRegistryRPCURL)
	contract := strings.TrimSpace(cfg.AIServiceRegistryAddress)
	if rpcURL == "" || contract == "" {
		return "", nil
	}
	callData, err := serviceURICallData(ethAddress)
	if err != nil {
		return "", err
	}

	payload, _ := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "eth_call",
		"params": []any{
			map[string]string{
				"to":   contract,
				"data": callData,
			},
			"latest",
		},
	})

	timeout := time.Duration(cfg.RegistryManifestTimeoutMs) * time.Millisecond
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, rpcURL, bytes.NewReader(payload))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "discovery-service/ai-registry")

	res, err := (&http.Client{Timeout: timeout}).Do(req)
	if err != nil {
		return "", err
	}
	defer func() { _ = res.Body.Close() }()
	body, err := io.ReadAll(res.Body)
	if err != nil {
		return "", err
	}
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return "", fmt.Errorf("AI registry RPC HTTP %d: %s", res.StatusCode, truncate(string(body), 200))
	}

	var out struct {
		Result string `json:"result"`
		Error  *struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return "", err
	}
	if out.Error != nil {
		return "", fmt.Errorf("AI registry RPC %d: %s", out.Error.Code, out.Error.Message)
	}
	return decodeABIString(out.Result)
}

func serviceURICallData(ethAddress string) (string, error) {
	addr := strings.TrimPrefix(strings.ToLower(strings.TrimSpace(ethAddress)), "0x")
	if len(addr) != 40 {
		return "", fmt.Errorf("invalid eth address %q", ethAddress)
	}
	if _, err := hex.DecodeString(addr); err != nil {
		return "", fmt.Errorf("invalid eth address %q: %w", ethAddress, err)
	}

	hash := sha3.NewLegacyKeccak256()
	_, _ = hash.Write([]byte(serviceURIABI))
	selector := hash.Sum(nil)[:4]
	return "0x" + hex.EncodeToString(selector) + strings.Repeat("0", 24) + addr, nil
}

func decodeABIString(raw string) (string, error) {
	raw = strings.TrimPrefix(strings.TrimSpace(raw), "0x")
	if raw == "" {
		return "", nil
	}
	data, err := hex.DecodeString(raw)
	if err != nil {
		return "", err
	}
	if len(data) < 64 {
		return "", nil
	}
	offset := intFromWord(data[:32])
	if offset < 0 || offset+32 > len(data) {
		return "", fmt.Errorf("invalid ABI string offset %d", offset)
	}
	length := intFromWord(data[offset : offset+32])
	if length == 0 {
		return "", nil
	}
	start := offset + 32
	if length < 0 || start+length > len(data) {
		return "", fmt.Errorf("invalid ABI string length %d", length)
	}
	return strings.TrimSpace(string(data[start : start+length])), nil
}

func intFromWord(word []byte) int {
	n := 0
	for _, b := range word {
		if n > (int(^uint(0)>>1)-int(b))/256 {
			return -1
		}
		n = n*256 + int(b)
	}
	return n
}
