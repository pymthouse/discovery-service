package sources

import (
	"bytes"
	"encoding/json"
	"strings"
)

// CapabilityList normalizes the capability shapes currently observed across
// discovery surfaces:
//   - legacy webhook strings: ["streamdiffusion-sdxl"]
//   - livepeer-network-modules v3 entries: {"name": "...", "offerings": [...]}
//   - coordinator tuples: {"capability_id": "...", "offering_id": "..."}
//   - payee daemon catalog entries: {"capability": "...", "offerings": [...]}
type CapabilityList []string

func (l *CapabilityList) UnmarshalJSON(data []byte) error {
	var raw []json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}

	out := make([]string, 0, len(raw))
	for _, item := range raw {
		for _, cap := range capabilityNamesFromRaw(item) {
			out = appendUniqueString(out, cap)
		}
	}
	*l = out
	return nil
}

func capabilityNamesFromRaw(data []byte) []string {
	data = bytes.TrimSpace(data)
	if len(data) == 0 || bytes.Equal(data, []byte("null")) {
		return nil
	}

	var s string
	if err := json.Unmarshal(data, &s); err == nil {
		return normalizeLegacyCapabilityNames(s)
	}

	var obj map[string]json.RawMessage
	if err := json.Unmarshal(data, &obj); err != nil {
		return nil
	}

	for _, key := range []string{"capability_id", "name", "capability"} {
		if raw, ok := obj[key]; ok {
			if err := json.Unmarshal(raw, &s); err == nil {
				return normalizeOpaqueCapabilityNames(s)
			}
		}
	}

	return nil
}

func extractCapabilityName(raw string) string {
	if idx := strings.LastIndex(raw, "/"); idx >= 0 {
		return raw[idx+1:]
	}
	return raw
}

// ExtractCapabilityName returns the bare capability/model name, stripping any
// "pipeline/" prefix (e.g. "live-video-to-video/streamdiffusion-sdxl" ->
// "streamdiffusion-sdxl"). Legacy webhook capabilities are materialized under
// the bare model name, so discovery queries that arrive in pipeline/model form
// must be normalized to match.
func ExtractCapabilityName(raw string) string {
	return extractCapabilityName(strings.TrimSpace(raw))
}

func normalizeLegacyCapabilityNames(raw string) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	return []string{extractCapabilityName(raw)}
}

func normalizeOpaqueCapabilityNames(raw string) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	return []string{raw}
}

func appendUniqueString(slice []string, v string) []string {
	if v == "" {
		return slice
	}
	for _, s := range slice {
		if s == v {
			return slice
		}
	}
	return append(slice, v)
}
