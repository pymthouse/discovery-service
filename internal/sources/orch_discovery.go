package sources

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/livepeer/discovery-service/internal/config"
)

// KindOrchDiscovery is the audit key for live-runner /discovery probing.
const KindOrchDiscovery Kind = "orch-discovery"

// LiveRunnerAppClaim is one orchestrator advertising a live-runner app.
type LiveRunnerAppClaim struct {
	OrchURI string
	App     string
	Score   float64
}

type orchDiscoveryEntry struct {
	Address string                `json:"address"`
	Runners []orchDiscoveryRunner `json:"runners"`
}

type orchDiscoveryRunner struct {
	URL string `json:"url"`
	App string `json:"app"`
}

// OrchDiscoveryURL builds GET {orchURI}/discovery.
func OrchDiscoveryURL(orchURI string) string {
	orchURI = strings.TrimSpace(orchURI)
	if orchURI == "" {
		return ""
	}
	u, err := url.Parse(orchURI)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return ""
	}
	u.RawQuery = ""
	u.Fragment = ""
	base := strings.TrimRight(u.Path, "/")
	if base == "" {
		u.Path = "/discovery"
	} else {
		u.Path = base + "/discovery"
	}
	u.RawPath = ""
	return u.String()
}

// CollectOrchURIs returns unique non-empty orchestrator URIs from source rows.
func CollectOrchURIs(perSource map[Kind][]NormalizedOrch, max int) []string {
	seen := make(map[string]struct{})
	out := make([]string, 0)
	for _, rows := range perSource {
		for _, r := range rows {
			uri := strings.TrimRight(strings.TrimSpace(r.OrchURI), "/")
			if uri == "" {
				continue
			}
			if _, ok := seen[uri]; ok {
				continue
			}
			seen[uri] = struct{}{}
			out = append(out, uri)
			if max > 0 && len(out) >= max {
				return out
			}
		}
	}
	return out
}

// AppendOrchURIs appends unique orchestrator URIs up to max total entries.
func AppendOrchURIs(existing, extra []string, max int) []string {
	if len(extra) == 0 {
		return existing
	}
	seen := make(map[string]struct{}, len(existing)+len(extra))
	out := make([]string, 0, len(existing)+len(extra))
	add := func(uri string) {
		uri = strings.TrimRight(strings.TrimSpace(uri), "/")
		if uri == "" {
			return
		}
		if _, ok := seen[uri]; ok {
			return
		}
		seen[uri] = struct{}{}
		out = append(out, uri)
	}
	for _, uri := range existing {
		add(uri)
	}
	for _, uri := range extra {
		add(uri)
		if max > 0 && len(out) >= max {
			return out
		}
	}
	return out
}

// ParseOrchDiscoveryBody extracts live-runner app claims from a /discovery JSON body.
func ParseOrchDiscoveryBody(body []byte, fallbackURI string) []LiveRunnerAppClaim {
	var entries []orchDiscoveryEntry
	if err := json.Unmarshal(body, &entries); err != nil {
		return nil
	}
	fallback := strings.TrimRight(strings.TrimSpace(fallbackURI), "/")
	out := make([]LiveRunnerAppClaim, 0)
	seen := make(map[string]struct{})
	for _, entry := range entries {
		orchURI := strings.TrimRight(strings.TrimSpace(entry.Address), "/")
		if orchURI == "" {
			orchURI = fallback
		}
		if orchURI == "" {
			continue
		}
		for _, runner := range entry.Runners {
			app := strings.TrimSpace(runner.App)
			url := strings.TrimSpace(runner.URL)
			if app == "" || url == "" {
				continue
			}
			key := orchURI + "\x00" + app
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			out = append(out, LiveRunnerAppClaim{
				OrchURI: orchURI,
				App:     app,
				Score:   1,
			})
		}
	}
	return out
}

// MergeLiveRunnerAppClaims prefers preferred claims over fallback for the same orch+app.
func MergeLiveRunnerAppClaims(preferred, fallback []LiveRunnerAppClaim) []LiveRunnerAppClaim {
	seen := make(map[string]struct{}, len(preferred)+len(fallback))
	out := make([]LiveRunnerAppClaim, 0, len(preferred)+len(fallback))
	add := func(claims []LiveRunnerAppClaim) {
		for _, c := range claims {
			orch := strings.TrimRight(strings.TrimSpace(c.OrchURI), "/")
			app := strings.TrimSpace(c.App)
			if orch == "" || app == "" {
				continue
			}
			key := orch + "\x00" + app
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			score := c.Score
			if score == 0 {
				score = 1
			}
			out = append(out, LiveRunnerAppClaim{OrchURI: orch, App: app, Score: score})
		}
	}
	add(preferred)
	add(fallback)
	return out
}

// ProbeOrchDiscoveryOptions controls concurrent /discovery probing.
type ProbeOrchDiscoveryOptions struct {
	TimeoutMs      int64
	MaxConcurrency int
}

// orchDiscoveryHTTPClient builds a client for probing orchestrator /discovery.
// Orchestrators commonly use self-signed or hostname-mismatched TLS certs, so
// verification is skipped for this probe path only.
func orchDiscoveryHTTPClient(timeout time.Duration) *http.Client {
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	return &http.Client{
		Timeout: timeout,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{
				InsecureSkipVerify: true, //nolint:gosec // orch nodes often use self-signed certs
			},
		},
	}
}

func httpGetOrchDiscovery(ctx context.Context, client *http.Client, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	res, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = res.Body.Close() }()
	body, err := io.ReadAll(res.Body)
	if err != nil {
		return nil, err
	}
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return nil, fmt.Errorf("HTTP %d: %s", res.StatusCode, truncate(string(body), 200))
	}
	return body, nil
}

type orchProbeResults struct {
	mu       sync.Mutex
	claims   []LiveRunnerAppClaim
	errCount int
	probed   int
}

func (r *orchProbeResults) record(body []byte, uri string, err error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.probed++
	if err != nil {
		r.errCount++
		return
	}
	r.claims = append(r.claims, ParseOrchDiscoveryBody(body, uri)...)
}

func probeOrchDiscoveryURI(
	ctx context.Context,
	client *http.Client,
	sem chan struct{},
	uri string,
	results *orchProbeResults,
) {
	sem <- struct{}{}
	defer func() { <-sem }()

	body, err := httpGetOrchDiscovery(ctx, client, OrchDiscoveryURL(uri))
	results.record(body, uri, err)
}

func orchProbeStats(start time.Time, claims []LiveRunnerAppClaim, errCount, probed int) Stats {
	stats := Stats{
		OK:         true,
		Fetched:    len(claims),
		DurationMs: elapsedMs(start),
	}
	if errCount == 0 || len(claims) > 0 {
		return stats
	}
	if errCount == probed {
		stats.ErrorMessage = fmt.Sprintf("all %d orch /discovery probes failed", errCount)
		return stats
	}
	stats.ErrorMessage = fmt.Sprintf(
		"%d/%d orch /discovery probes failed and no claims collected",
		errCount,
		probed,
	)
	return stats
}

// ProbeOrchDiscovery GETs each orch's /discovery and returns app claims.
// Soft-fails per URI; never fails the overall call.
// TLS certificate verification is skipped so self-signed orch endpoints still work.
func ProbeOrchDiscovery(ctx context.Context, orchURIs []string, opts ProbeOrchDiscoveryOptions) ([]LiveRunnerAppClaim, Stats) {
	start := time.Now()
	if len(orchURIs) == 0 {
		return nil, Stats{OK: true, Fetched: 0, DurationMs: elapsedMs(start)}
	}

	timeout := time.Duration(opts.TimeoutMs) * time.Millisecond
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	concurrency := opts.MaxConcurrency
	if concurrency <= 0 {
		concurrency = 25
	}
	client := orchDiscoveryHTTPClient(timeout)

	sem := make(chan struct{}, concurrency)
	var wg sync.WaitGroup
	results := orchProbeResults{
		claims: make([]LiveRunnerAppClaim, 0),
	}

	for _, uri := range orchURIs {
		if OrchDiscoveryURL(uri) == "" {
			continue
		}
		wg.Add(1)
		go func(uri string) {
			defer wg.Done()
			probeOrchDiscoveryURI(ctx, client, sem, uri, &results)
		}(uri)
	}
	wg.Wait()

	merged := MergeLiveRunnerAppClaims(results.claims, nil)
	return merged, orchProbeStats(start, merged, results.errCount, results.probed)
}

// ProbeOptionsFromConfig maps config fields onto probe options.
func ProbeOptionsFromConfig(cfg config.Config) ProbeOrchDiscoveryOptions {
	return ProbeOrchDiscoveryOptions{
		TimeoutMs:      cfg.OrchDiscoveryTimeoutMs,
		MaxConcurrency: cfg.OrchDiscoveryMaxConcurrency,
	}
}
