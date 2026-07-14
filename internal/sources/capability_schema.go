package sources

import (
	"bytes"
	"encoding/json"
	"strings"
)

// ParsedCapability is one capability name with its service class.
type ParsedCapability struct {
	Name        string
	ServiceType ServiceType
}

// CapabilityList normalizes the capability shapes currently observed across
// discovery surfaces:
//   - legacy webhook strings: ["streamdiffusion-sdxl"] or
//     ["live-video-to-video/streamdiffusion-sdxl"]
//   - batch pipeline strings: ["text-to-image/SG161222/..."]
//   - livepeer-network-modules v3 entries: {"name": "...", "offerings": [...]}
//   - coordinator tuples: {"capability_id": "...", "offering_id": "..."}
//   - payee daemon catalog entries: {"capability": "...", "offerings": [...]}
type CapabilityList []ParsedCapability

func (l *CapabilityList) UnmarshalJSON(data []byte) error {
	var raw []json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}

	out := make([]ParsedCapability, 0, len(raw))
	for _, item := range raw {
		for _, cap := range capabilityEntriesFromRaw(item) {
			out = appendUniqueParsed(out, cap)
		}
	}
	*l = out
	return nil
}

// Names returns capability names in order.
func (l CapabilityList) Names() []string {
	out := make([]string, 0, len(l))
	for _, c := range l {
		out = append(out, c.Name)
	}
	return out
}

func capabilityEntriesFromRaw(data []byte) []ParsedCapability {
	data = bytes.TrimSpace(data)
	if len(data) == 0 || bytes.Equal(data, []byte("null")) {
		return nil
	}

	var s string
	if err := json.Unmarshal(data, &s); err == nil {
		return normalizeLegacyCapabilityEntries(s)
	}

	var obj map[string]json.RawMessage
	if err := json.Unmarshal(data, &obj); err != nil {
		return nil
	}

	for _, key := range []string{"capability_id", "name", "capability"} {
		if raw, ok := obj[key]; ok {
			if err := json.Unmarshal(raw, &s); err == nil {
				return normalizeModuleCapabilityEntries(s)
			}
		}
	}

	return nil
}

func extractCapabilityName(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	pipeline, rest, ok := strings.Cut(raw, "/")
	if !ok {
		return raw
	}
	pipeline = strings.ToLower(strings.TrimSpace(pipeline))
	if pipeline == string(ServiceTypeLiveVideoToVideo) || IsBatchPipeline(pipeline) {
		rest = strings.TrimSpace(rest)
		if rest == "" {
			return ""
		}
		return rest
	}
	// Unknown "prefix/name" shapes (e.g. livepeer/streamdiffusion-sdxl) keep
	// the historical last-segment bare name used by classic webhooks.
	if idx := strings.LastIndex(raw, "/"); idx >= 0 {
		return raw[idx+1:]
	}
	return raw
}

// ExtractCapabilityName returns the bare capability/model name, stripping a
// known pipeline prefix when present (e.g. "live-video-to-video/streamdiffusion-sdxl"
// -> "streamdiffusion-sdxl", "text-to-image/org/model" -> "org/model").
// Live-video and batch capabilities are materialized under that bare name, so
// discovery queries that arrive in pipeline/model form must be normalized to match.
func ExtractCapabilityName(raw string) string {
	return extractCapabilityName(raw)
}

func normalizeLegacyCapabilityEntries(raw string) []ParsedCapability {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	name := extractCapabilityName(raw)
	if name == "" {
		return nil
	}
	return []ParsedCapability{{
		Name:        name,
		ServiceType: ClassifyPipelineCapability(raw),
	}}
}

func normalizeModuleCapabilityEntries(raw string) []ParsedCapability {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	return []ParsedCapability{{
		Name:        raw,
		ServiceType: ServiceTypeModules,
	}}
}

func appendUniqueParsed(slice []ParsedCapability, v ParsedCapability) []ParsedCapability {
	if v.Name == "" {
		return slice
	}
	for _, s := range slice {
		if s.Name == v.Name && s.ServiceType == v.ServiceType {
			return slice
		}
	}
	return append(slice, v)
}

// GroupCapabilitiesByServiceType splits parsed capabilities by service class.
func GroupCapabilitiesByServiceType(caps CapabilityList) map[ServiceType][]string {
	out := make(map[ServiceType][]string)
	for _, c := range caps {
		st := c.ServiceType
		if st == "" {
			st = ServiceTypeLiveVideoToVideo
		}
		out[st] = appendUniqueString(out[st], c.Name)
	}
	return out
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
