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
	membershipKeys := make(map[orchKey]struct{})
	membershipSource := ""

	if strategy == "union" {
		kinds := make([]string, len(enabled))
		for i, s := range enabled {
			kinds[i] = string(s.Kind)
			for k := range sourceIndexes[s.Kind] {
				membershipKeys[k] = struct{}{}
			}
		}
		membershipSource = "union(" + strings.Join(kinds, ",") + ")"
		if len(membershipKeys) == 0 {
			warnings = append(warnings, "All sources returned 0 rows — empty dataset")
		}
		return membershipKeys, membershipSource, warnings
	}

	membershipSource = string(enabled[0].Kind)
	idx := sourceIndexes[enabled[0].Kind]
	if len(idx) == 0 && len(enabled) > 1 {
		for _, s := range enabled[1:] {
			if len(sourceIndexes[s.Kind]) > 0 {
				membershipSource = string(s.Kind)
				idx = sourceIndexes[s.Kind]
				warnings = append(warnings, "Primary membership source returned 0 rows — fallback to "+string(s.Kind))
				break
			}
		}
	}
	for k := range idx {
		membershipKeys[k] = struct{}{}
	}
	return membershipKeys, membershipSource, warnings
}

func buildEthUriMaps(
	sourceIndexes map[sources.Kind]map[orchKey][]sources.NormalizedOrch,
) (map[string]string, map[string]string) {
	uriToEth := make(map[string]string)
	ethToUri := make(map[string]string)
	for _, idx := range sourceIndexes {
		for _, rows := range idx {
			for _, r := range rows {
				if r.EthAddress != "" && r.OrchURI != "" {
					uriToEth[r.OrchURI] = strings.ToLower(r.EthAddress)
					ethToUri[strings.ToLower(r.EthAddress)] = r.OrchURI
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
		s := string(k)
		if strings.HasPrefix(s, "uri:") {
			if eth, ok := uriToEth[s[4:]]; ok {
				ek := orchKey("eth:" + eth)
				if _, ok2 := membershipKeys[ek]; ok2 {
					return ek, true
				}
			}
		}
		if strings.HasPrefix(s, "eth:") {
			if uri, ok := ethToUri[s[4:]]; ok {
				uk := orchKey("uri:" + uri)
				if _, ok2 := membershipKeys[uk]; ok2 {
					return uk, true
				}
			}
		}
		return "", false
	}
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

func mergeMemberOrchestrator(
	memberKey orchKey,
	enabled []SourceConfig,
	sourceRows map[sources.Kind][]sources.NormalizedOrch,
	fieldPriority map[string][]sources.Kind,
	conflicts *[]ConflictEntry,
) mergedOrch {
	m := mergedOrch{}
	resolveField := func(field string, apply func(*mergedOrch, sources.NormalizedOrch)) {
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
			} else {
				losers = append(losers, LoserEntry{Source: src, Value: fieldVal(r, field)})
			}
		}
		if winner == "" {
			return
		}
		apply(&m, winnerRow)
		if len(losers) > 0 {
			*conflicts = append(*conflicts, ConflictEntry{
				OrchKey: string(memberKey),
				Field:   field,
				Winner:  winner,
				Losers:  losers,
			})
		}
	}

	resolveField("orchUri", func(m *mergedOrch, r sources.NormalizedOrch) { m.orchURI = r.OrchURI })
	resolveField("ethAddress", func(m *mergedOrch, r sources.NormalizedOrch) { m.ethAddress = r.EthAddress })
	resolveField("gpuName", func(m *mergedOrch, r sources.NormalizedOrch) { m.gpuName = r.GPUName })
	resolveField("gpuGb", func(m *mergedOrch, r sources.NormalizedOrch) { m.gpuGb = r.GPUGb })
	resolveField("avail", func(m *mergedOrch, r sources.NormalizedOrch) { m.avail = r.Avail })
	resolveField("totalCap", func(m *mergedOrch, r sources.NormalizedOrch) { m.totalCap = r.TotalCap })
	resolveField("pricePerUnit", func(m *mergedOrch, r sources.NormalizedOrch) { m.pricePerUnit = r.PricePerUnit })
	resolveField("bestLatMs", func(m *mergedOrch, r sources.NormalizedOrch) { m.bestLatMs = r.BestLatMs })
	resolveField("avgLatMs", func(m *mergedOrch, r sources.NormalizedOrch) { m.avgLatMs = r.AvgLatMs })
	resolveField("swapRatio", func(m *mergedOrch, r sources.NormalizedOrch) { m.swapRatio = r.SwapRatio })
	resolveField("avgAvail", func(m *mergedOrch, r sources.NormalizedOrch) { m.avgAvail = r.AvgAvail })
	resolveField("score", func(m *mergedOrch, r sources.NormalizedOrch) { m.score = r.Score })

	capSet := make(map[string]struct{})
	for _, rows := range sourceRows {
		for _, r := range rows {
			for _, c := range r.Capabilities {
				capSet[c] = struct{}{}
			}
		}
	}
	for c := range capSet {
		m.capabilities = append(m.capabilities, c)
	}
	sort.Strings(m.capabilities)
	return m
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
			if cap == "__uncategorized" {
				continue
			}
			row := DatasetRow{
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
			capabilities[cap] = append(capabilities[cap], row)
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
