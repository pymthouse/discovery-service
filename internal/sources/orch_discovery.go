package sources

import (
	"context"
	"encoding/json"
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

// ProbeOrchDiscovery GETs each orch's /discovery and returns app claims.
// Soft-fails per URI; never fails the overall call.
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

	sem := make(chan struct{}, concurrency)
	var mu sync.Mutex
	var wg sync.WaitGroup
	claims := make([]LiveRunnerAppClaim, 0)
	var errCount int

	for _, uri := range orchURIs {
		uri := uri
		discoveryURL := OrchDiscoveryURL(uri)
		if discoveryURL == "" {
			continue
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			body, err := httpGetTimeout(ctx, discoveryURL, nil, timeout)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				errCount++
				return
			}
			parsed := ParseOrchDiscoveryBody(body, uri)
			if len(parsed) == 0 {
				return
			}
			claims = append(claims, parsed...)
		}()
	}
	wg.Wait()

	merged := MergeLiveRunnerAppClaims(claims, nil)
	stats := Stats{
		OK:         true,
		Fetched:    len(merged),
		DurationMs: elapsedMs(start),
	}
	if errCount > 0 && len(merged) == 0 {
		stats.ErrorMessage = "all orch /discovery probes failed"
	}
	return merged, stats
}

// ProbeOptionsFromConfig maps config fields onto probe options.
func ProbeOptionsFromConfig(cfg config.Config) ProbeOrchDiscoveryOptions {
	return ProbeOrchDiscoveryOptions{
		TimeoutMs:      cfg.OrchDiscoveryTimeoutMs,
		MaxConcurrency: cfg.OrchDiscoveryMaxConcurrency,
	}
}
