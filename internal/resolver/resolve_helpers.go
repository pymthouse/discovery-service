package resolver

import (
	"sort"
	"strings"

	"github.com/livepeer/discovery-service/internal/sources"
)

func sortedEnabledSources(cfg Config) []SourceConfig {
	enabled := make([]SourceConfig, 0)
	for _, s := range cfg.Sources {
		if s.Enabled {
			enabled = append(enabled, s)
		}
	}
	sort.Slice(enabled, func(i, j int) bool { return enabled[i].Priority < enabled[j].Priority })
	return enabled
}

func mergeFieldPriority(cfg Config) map[string][]sources.Kind {
	fieldPriority := make(map[string][]sources.Kind)
	for k, v := range defaultFieldPriority {
		fieldPriority[k] = append([]sources.Kind(nil), v...)
	}
	for k, v := range cfg.FieldPriority {
		fieldPriority[k] = v
	}
	return fieldPriority
}

func buildSourceIndexes(
	perSource map[sources.Kind][]sources.NormalizedOrch,
	enabled []SourceConfig,
) (map[sources.Kind]map[orchKey][]sources.NormalizedOrch, map[string]int) {
	sourceIndexes := make(map[sources.Kind]map[orchKey][]sources.NormalizedOrch)
	perSourceCounts := make(map[string]int)
	for _, s := range enabled {
		rows := perSource[s.Kind]
		perSourceCounts[string(s.Kind)] = len(rows)
		sourceIndexes[s.Kind] = indexByOrch(rows)
	}
	return sourceIndexes, perSourceCounts
}

func computeMembership(
	strategy string,
	enabled []SourceConfig,
	sourceIndexes map[sources.Kind]map[orchKey][]sources.NormalizedOrch,
	warnings []string,
) (map[orchKey]struct{}, string, []string) {
	if strategy == "union" {
		return computeUnionMembership(enabled, sourceIndexes, warnings)
	}
	return computePrimaryMembership(enabled, sourceIndexes, warnings)
}

func computeUnionMembership(
	enabled []SourceConfig,
	sourceIndexes map[sources.Kind]map[orchKey][]sources.NormalizedOrch,
	warnings []string,
) (map[orchKey]struct{}, string, []string) {
	membershipKeys := make(map[orchKey]struct{})
	kinds := make([]string, len(enabled))
	for i, s := range enabled {
		kinds[i] = string(s.Kind)
		for k := range sourceIndexes[s.Kind] {
			membershipKeys[k] = struct{}{}
		}
	}
	if len(membershipKeys) == 0 {
		warnings = append(warnings, "All sources returned 0 rows — empty dataset")
	}
	return membershipKeys, "union(" + strings.Join(kinds, ",") + ")", warnings
}

func computePrimaryMembership(
	enabled []SourceConfig,
	sourceIndexes map[sources.Kind]map[orchKey][]sources.NormalizedOrch,
	warnings []string,
) (map[orchKey]struct{}, string, []string) {
	membershipSource := string(enabled[0].Kind)
	idx := sourceIndexes[enabled[0].Kind]
	if len(idx) == 0 && len(enabled) > 1 {
		for _, s := range enabled[1:] {
			if len(sourceIndexes[s.Kind]) == 0 {
				continue
			}
			membershipSource = string(s.Kind)
			idx = sourceIndexes[s.Kind]
			warnings = append(warnings, "Primary membership source returned 0 rows — fallback to "+string(s.Kind))
			break
		}
	}
	membershipKeys := make(map[orchKey]struct{}, len(idx))
	for k := range idx {
		membershipKeys[k] = struct{}{}
	}
	return membershipKeys, membershipSource, warnings
}

func buildEthUriMaps(
	enabled []SourceConfig,
	sourceIndexes map[sources.Kind]map[orchKey][]sources.NormalizedOrch,
) (map[string]string, map[string]string) {
	uriToEth := make(map[string]string)
	ethToUri := make(map[string]string)
	for _, s := range enabled {
		idx := sourceIndexes[s.Kind]
		for _, rows := range idx {
			for _, r := range rows {
				if r.EthAddress == "" || r.OrchURI == "" {
					continue
				}
				eth := strings.ToLower(r.EthAddress)
				if _, ok := uriToEth[r.OrchURI]; !ok {
					uriToEth[r.OrchURI] = eth
				}
				if _, ok := ethToUri[eth]; !ok {
					ethToUri[eth] = r.OrchURI
				}
			}
		}
	}
	return uriToEth, ethToUri
}

func dedupeUriMembershipKeys(membershipKeys map[orchKey]struct{}, uriToEth map[string]string) {
	for k := range membershipKeys {
		s := string(k)
		if !strings.HasPrefix(s, "uri:") {
			continue
		}
		uri := s[4:]
		if eth, ok := uriToEth[uri]; ok {
			ethKey := orchKey("eth:" + eth)
			if _, has := membershipKeys[ethKey]; has {
				delete(membershipKeys, k)
			}
		}
	}
}

func membershipKeyResolver(
	membershipKeys map[orchKey]struct{},
	uriToEth, ethToUri map[string]string,
) func(orchKey) (orchKey, bool) {
	return func(k orchKey) (orchKey, bool) {
		if _, ok := membershipKeys[k]; ok {
			return k, true
		}
		if alt, ok := alternateMembershipKey(k, membershipKeys, uriToEth, ethToUri); ok {
			return alt, true
		}
		return "", false
	}
}

func alternateMembershipKey(
	k orchKey,
	membershipKeys map[orchKey]struct{},
	uriToEth, ethToUri map[string]string,
) (orchKey, bool) {
	s := string(k)
	if strings.HasPrefix(s, "uri:") {
		if eth, ok := uriToEth[s[4:]]; ok {
			return membershipKeyIfPresent(orchKey("eth:"+eth), membershipKeys)
		}
	}
	if strings.HasPrefix(s, "eth:") {
		if uri, ok := ethToUri[s[4:]]; ok {
			return membershipKeyIfPresent(orchKey("uri:"+uri), membershipKeys)
		}
	}
	return "", false
}

func membershipKeyIfPresent(key orchKey, membershipKeys map[orchKey]struct{}) (orchKey, bool) {
	if _, ok := membershipKeys[key]; ok {
		return key, true
	}
	return "", false
}

func collectDroppedOutsideMembership(
	strategy string,
	enabled []SourceConfig,
	sourceIndexes map[sources.Kind]map[orchKey][]sources.NormalizedOrch,
	membershipSource string,
	resolveKey func(orchKey) (orchKey, bool),
) []DroppedEntry {
	if strategy == "union" {
		return nil
	}
	dropped := make([]DroppedEntry, 0)
	for _, s := range enabled {
		if string(s.Kind) == membershipSource {
			continue
		}
		for k := range sourceIndexes[s.Kind] {
			if _, ok := resolveKey(k); !ok {
				dropped = append(dropped, DroppedEntry{
					OrchKey: string(k),
					Source:  s.Kind,
					Reason:  "not present in membership source (" + membershipSource + ")",
				})
			}
		}
	}
	return dropped
}

func sourceRowsForMember(
	memberKey orchKey,
	enabled []SourceConfig,
	sourceIndexes map[sources.Kind]map[orchKey][]sources.NormalizedOrch,
	uriToEth, ethToUri map[string]string,
) map[sources.Kind][]sources.NormalizedOrch {
	sourceRows := make(map[sources.Kind][]sources.NormalizedOrch)
	for _, s := range enabled {
		idx := sourceIndexes[s.Kind]
		if rows, ok := idx[memberKey]; ok {
			sourceRows[s.Kind] = rows
			continue
		}
		mk := string(memberKey)
		if strings.HasPrefix(mk, "eth:") {
			if uri, ok := ethToUri[mk[4:]]; ok {
				if rows, ok := idx[orchKey("uri:"+uri)]; ok {
					sourceRows[s.Kind] = rows
				}
			}
		} else if strings.HasPrefix(mk, "uri:") {
			if eth, ok := uriToEth[mk[4:]]; ok {
				if rows, ok := idx[orchKey("eth:"+eth)]; ok {
					sourceRows[s.Kind] = rows
				}
			}
		}
	}
	return sourceRows
}

type fieldMerge struct {
	name  string
	apply func(*mergedOrch, sources.NormalizedOrch)
}

var mergedFieldMerges = []fieldMerge{
	{name: "orchUri", apply: func(m *mergedOrch, r sources.NormalizedOrch) { m.orchURI = r.OrchURI }},
	{name: "ethAddress", apply: func(m *mergedOrch, r sources.NormalizedOrch) { m.ethAddress = r.EthAddress }},
	{name: "gpuName", apply: func(m *mergedOrch, r sources.NormalizedOrch) { m.gpuName = r.GPUName }},
	{name: "gpuGb", apply: func(m *mergedOrch, r sources.NormalizedOrch) { m.gpuGb = r.GPUGb }},
	{name: "avail", apply: func(m *mergedOrch, r sources.NormalizedOrch) { m.avail = r.Avail }},
	{name: "totalCap", apply: func(m *mergedOrch, r sources.NormalizedOrch) { m.totalCap = r.TotalCap }},
	{name: "pricePerUnit", apply: func(m *mergedOrch, r sources.NormalizedOrch) { m.pricePerUnit = r.PricePerUnit }},
	{name: "bestLatMs", apply: func(m *mergedOrch, r sources.NormalizedOrch) { m.bestLatMs = r.BestLatMs }},
	{name: "avgLatMs", apply: func(m *mergedOrch, r sources.NormalizedOrch) { m.avgLatMs = r.AvgLatMs }},
	{name: "swapRatio", apply: func(m *mergedOrch, r sources.NormalizedOrch) { m.swapRatio = r.SwapRatio }},
	{name: "avgAvail", apply: func(m *mergedOrch, r sources.NormalizedOrch) { m.avgAvail = r.AvgAvail }},
	{name: "score", apply: func(m *mergedOrch, r sources.NormalizedOrch) { m.score = r.Score }},
}

func mergeMemberOrchestrator(
	memberKey orchKey,
	enabled []SourceConfig,
	sourceRows map[sources.Kind][]sources.NormalizedOrch,
	fieldPriority map[string][]sources.Kind,
	conflicts *[]ConflictEntry,
) mergedOrch {
	m := mergedOrch{}
	for _, field := range mergedFieldMerges {
		applyMergedField(memberKey, field.name, enabled, sourceRows, fieldPriority, conflicts, &m, field.apply)
	}
	m.capabilities = mergeCapabilitiesByPriority(enabled, sourceRows, fieldPriority)
	return m
}

func applyMergedField(
	memberKey orchKey,
	field string,
	enabled []SourceConfig,
	sourceRows map[sources.Kind][]sources.NormalizedOrch,
	fieldPriority map[string][]sources.Kind,
	conflicts *[]ConflictEntry,
	m *mergedOrch,
	apply func(*mergedOrch, sources.NormalizedOrch),
) {
	winner, winnerRow, losers := pickFieldWinner(field, enabled, sourceRows, fieldPriority)
	if winner == "" {
		return
	}
	apply(m, winnerRow)
	if len(losers) == 0 {
		return
	}
	*conflicts = append(*conflicts, ConflictEntry{
		OrchKey: string(memberKey),
		Field:   field,
		Winner:  winner,
		Losers:  losers,
	})
}

func pickFieldWinner(
	field string,
	enabled []SourceConfig,
	sourceRows map[sources.Kind][]sources.NormalizedOrch,
	fieldPriority map[string][]sources.Kind,
) (sources.Kind, sources.NormalizedOrch, []LoserEntry) {
	priority := fieldPriority[field]
	if len(priority) == 0 {
		for _, s := range enabled {
			priority = append(priority, s.Kind)
		}
	}
	var winner sources.Kind
	var winnerRow sources.NormalizedOrch
	var losers []LoserEntry
	for _, src := range priority {
		rows, ok := sourceRows[src]
		if !ok || len(rows) == 0 {
			continue
		}
		r := rows[0]
		if !fieldSet(r, field) {
			continue
		}
		if winner == "" {
			winner = src
			winnerRow = r
			continue
		}
		losers = append(losers, LoserEntry{Source: src, Value: fieldVal(r, field)})
	}
	return winner, winnerRow, losers
}

func mergeCapabilitiesByPriority(
	enabled []SourceConfig,
	sourceRows map[sources.Kind][]sources.NormalizedOrch,
	fieldPriority map[string][]sources.Kind,
) []typedCapability {
	priority := fieldPriority["capabilities"]
	if len(priority) == 0 {
		for _, s := range enabled {
			priority = append(priority, s.Kind)
		}
	}
	type key struct {
		name string
		st   sources.ServiceType
	}
	seen := make(map[key]struct{})
	out := make([]typedCapability, 0)
	for _, src := range priority {
		rows, ok := sourceRows[src]
		if !ok || len(rows) == 0 {
			continue
		}
		for _, c := range collectTypedCapabilities(rows) {
			k := key{name: c.name, st: c.serviceType}
			if _, ok := seen[k]; ok {
				continue
			}
			seen[k] = struct{}{}
			out = append(out, c)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].serviceType != out[j].serviceType {
			return out[i].serviceType < out[j].serviceType
		}
		return out[i].name < out[j].name
	})
	return out
}

func collectTypedCapabilities(rows []sources.NormalizedOrch) []typedCapability {
	type key struct {
		name string
		st   sources.ServiceType
	}
	seen := make(map[key]struct{})
	out := make([]typedCapability, 0)
	for _, r := range rows {
		st := r.EffectiveServiceType()
		for _, c := range r.Capabilities {
			if c == "" || c == "__uncategorized" {
				continue
			}
			k := key{name: c, st: st}
			if _, ok := seen[k]; ok {
				continue
			}
			seen[k] = struct{}{}
			out = append(out, typedCapability{name: c, serviceType: st})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].serviceType != out[j].serviceType {
			return out[i].serviceType < out[j].serviceType
		}
		return out[i].name < out[j].name
	})
	return out
}

func collectCapabilities(rows []sources.NormalizedOrch) []string {
	typed := collectTypedCapabilities(rows)
	caps := make([]string, 0, len(typed))
	seen := make(map[string]struct{}, len(typed))
	for _, c := range typed {
		if _, ok := seen[c.name]; ok {
			continue
		}
		seen[c.name] = struct{}{}
		caps = append(caps, c.name)
	}
	return caps
}

func buildCapabilityDataset(merged map[orchKey]mergedOrch) (map[string][]DatasetRow, int) {
	capabilities := make(map[string][]DatasetRow)
	seenOrch := make(map[orchKey]struct{})
	totalOrch := 0

	for key, m := range merged {
		if len(m.capabilities) == 0 {
			continue
		}
		for _, cap := range m.capabilities {
			st := cap.serviceType
			if st == "" {
				st = sources.ServiceTypeLiveVideoToVideo
			}
			row := DatasetRow{
				ServiceType:  string(st),
				EthAddress:   m.ethAddress,
				OrchURI:      m.orchURI,
				GPUName:      m.gpuName,
				GPUGb:        m.gpuGb,
				Avail:        m.avail,
				TotalCap:     m.totalCap,
				PricePerUnit: m.pricePerUnit,
				BestLatMs:    m.bestLatMs,
				AvgLatMs:     m.avgLatMs,
				SwapRatio:    m.swapRatio,
				AvgAvail:     m.avgAvail,
				Score:        m.score,
			}
			capabilities[cap.name] = append(capabilities[cap.name], row)
			if _, ok := seenOrch[key]; !ok {
				seenOrch[key] = struct{}{}
				totalOrch++
			}
		}
	}

	for cap, rows := range capabilities {
		if len(rows) == 0 {
			delete(capabilities, cap)
		}
	}
	return capabilities, totalOrch
}
