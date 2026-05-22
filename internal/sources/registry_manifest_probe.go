package sources

import (
	"net/url"
	"strings"
)

const registryWellKnownPath = "/.well-known/livepeer-registry.json"

// ManifestFetchCandidates returns bounded probe URLs for a subgraph serviceURI.
func ManifestFetchCandidates(serviceURI string) []string {
	serviceURI = strings.TrimSpace(serviceURI)
	if serviceURI == "" {
		return nil
	}
	if strings.HasSuffix(strings.ToLower(serviceURI), "livepeer-registry.json") {
		return []string{serviceURI}
	}

	candidates := []string{serviceURI}
	if strings.Contains(serviceURI, "/.well-known/") {
		return dedupeURLs(candidates)
	}

	u, err := url.Parse(serviceURI)
	if err != nil || u.Host == "" {
		return dedupeURLs(candidates)
	}

	base := *u
	base.Path = ""
	base.RawPath = ""
	base.RawQuery = ""
	base.Fragment = ""

	next := base
	next.Path = registryWellKnownPath
	if candidate := next.String(); candidate != serviceURI {
		candidates = append(candidates, candidate)
	}
	return dedupeURLs(candidates)
}

func dedupeURLs(urls []string) []string {
	seen := make(map[string]struct{}, len(urls))
	out := make([]string, 0, len(urls))
	for _, u := range urls {
		u = strings.TrimSpace(u)
		if u == "" {
			continue
		}
		if _, ok := seen[u]; ok {
			continue
		}
		seen[u] = struct{}{}
		out = append(out, u)
	}
	return out
}
