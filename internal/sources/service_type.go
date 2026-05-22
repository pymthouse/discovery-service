package sources

import "strings"

// ServiceType distinguishes legacy gateway discovery from on-chain registry manifests.
type ServiceType string

const (
	ServiceTypeLegacy   ServiceType = "legacy"
	ServiceTypeRegistry ServiceType = "registry"
)

// ParseServiceTypes normalizes request filters; empty means all types.
func ParseServiceTypes(raw []string) []ServiceType {
	if len(raw) == 0 {
		return []ServiceType{ServiceTypeLegacy, ServiceTypeRegistry}
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
		return []ServiceType{ServiceTypeLegacy, ServiceTypeRegistry}
	}
	return out
}

func (t ServiceType) Valid() bool {
	return t == ServiceTypeLegacy || t == ServiceTypeRegistry
}
