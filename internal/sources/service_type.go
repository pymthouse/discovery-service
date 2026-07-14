package sources

import "strings"

// ServiceType distinguishes capability classes in the discovery dataset.
type ServiceType string

const (
	// ServiceTypeLiveVideoToVideo is classic live ComfyStream / trickle pipelines
	// advertised as live-video-to-video/<model> (formerly labeled "legacy").
	ServiceTypeLiveVideoToVideo ServiceType = "live-video-to-video"
	// ServiceTypeLiveRunner is live-runner apps discovered via orch serviceURL /discovery.
	ServiceTypeLiveRunner ServiceType = "live-runner"
	// ServiceTypeModules is Livepeer Modules / on-chain registry manifests.
	ServiceTypeModules ServiceType = "modules"
	// ServiceTypeBatch is request-response AI pipelines (text-to-image, etc.).
	ServiceTypeBatch ServiceType = "batch"
)

// DefaultServiceTypes is used when a request omits serviceType filters.
// Live gateways care about live-video-to-video and live-runner; batch and
// modules must be requested explicitly.
var DefaultServiceTypes = []ServiceType{
	ServiceTypeLiveVideoToVideo,
	ServiceTypeLiveRunner,
}

// AllServiceTypes lists every valid service class.
var AllServiceTypes = []ServiceType{
	ServiceTypeLiveVideoToVideo,
	ServiceTypeLiveRunner,
	ServiceTypeModules,
	ServiceTypeBatch,
}

// batchPipelines are request-response AI pipeline prefixes (before "/").
var batchPipelines = map[string]struct{}{
	"text-to-image":       {},
	"image-to-image":      {},
	"image-to-video":      {},
	"upscale":             {},
	"audio-to-text":       {},
	"segment-anything-2":  {},
	"image-to-text":       {},
	"text-to-speech":      {},
	"llm":                 {},
}

// ParseServiceTypes normalizes request filters; empty means the live default set.
func ParseServiceTypes(raw []string) []ServiceType {
	if len(raw) == 0 {
		return append([]ServiceType(nil), DefaultServiceTypes...)
	}
	seen := make(map[ServiceType]struct{})
	out := make([]ServiceType, 0, len(raw))
	for _, item := range raw {
		st := ServiceType(strings.TrimSpace(strings.ToLower(item)))
		if !st.Valid() {
			continue
		}
		if _, ok := seen[st]; ok {
			continue
		}
		seen[st] = struct{}{}
		out = append(out, st)
	}
	if len(out) == 0 {
		return append([]ServiceType(nil), DefaultServiceTypes...)
	}
	return out
}

func (t ServiceType) Valid() bool {
	switch t {
	case ServiceTypeLiveVideoToVideo, ServiceTypeLiveRunner, ServiceTypeModules, ServiceTypeBatch:
		return true
	default:
		return false
	}
}

// IsBatchPipeline reports whether pipeline is a known batch AI pipeline name.
func IsBatchPipeline(pipeline string) bool {
	_, ok := batchPipelines[strings.ToLower(strings.TrimSpace(pipeline))]
	return ok
}

// ClassifyPipelineCapability maps a legacy webhook/discover string capability
// onto a service type. Object-shaped module entries are handled separately.
func ClassifyPipelineCapability(raw string) ServiceType {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ServiceTypeLiveVideoToVideo
	}
	pipeline, _, ok := strings.Cut(raw, "/")
	if !ok {
		// Bare model names from ClickHouse / classic webhooks are live pipelines.
		return ServiceTypeLiveVideoToVideo
	}
	pipeline = strings.ToLower(strings.TrimSpace(pipeline))
	switch {
	case pipeline == string(ServiceTypeLiveVideoToVideo):
		return ServiceTypeLiveVideoToVideo
	case IsBatchPipeline(pipeline):
		return ServiceTypeBatch
	default:
		// Unknown pipeline/model strings stay with the live class so existing
		// gateway capabilities keep working under the default filter.
		return ServiceTypeLiveVideoToVideo
	}
}
