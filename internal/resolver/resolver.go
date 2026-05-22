package resolver

import (
	"sort"
	"strings"

	"github.com/livepeer/discovery-service/internal/sources"
)

var defaultFieldPriority = map[string][]sources.Kind{
	"orchUri":      {sources.KindSubgraph, sources.KindClickHouse, sources.KindNaapDiscover, sources.KindRemoteSigner, sources.KindNaapPricing},
	"ethAddress":   {sources.KindSubgraph, sources.KindNaapPricing, sources.KindClickHouse, sources.KindNaapDiscover, sources.KindRemoteSigner},
	"gpuName":      {sources.KindClickHouse, sources.KindNaapDiscover},
	"gpuGb":        {sources.KindClickHouse, sources.KindNaapDiscover},
	"avail":        {sources.KindClickHouse},
	"totalCap":     {sources.KindClickHouse},
	"pricePerUnit": {sources.KindClickHouse, sources.KindNaapPricing},
	"bestLatMs":    {sources.KindClickHouse},
	"avgLatMs":     {sources.KindClickHouse},
	"swapRatio":    {sources.KindClickHouse},
	"avgAvail":     {sources.KindClickHouse},
	"capabilities": {sources.KindClickHouse, sources.KindNaapDiscover, sources.KindRemoteSigner},
	"score":        {sources.KindNaapDiscover, sources.KindRemoteSigner},
}

type orchKey string

func keyFor(row sources.NormalizedOrch) orchKey {
	if row.EthAddress != "" {
		return orchKey("eth:" + strings.ToLower(row.EthAddress))
	}
	if row.OrchURI != "" {
		return orchKey("uri:" + row.OrchURI)
	}
	return orchKey("unknown")
}

func indexByOrch(rows []sources.NormalizedOrch) map[orchKey][]sources.NormalizedOrch {
	m := make(map[orchKey][]sources.NormalizedOrch)
	for _, r := range rows {
		k := keyFor(r)
		m[k] = append(m[k], r)
	}
	return m
}

type mergedOrch struct {
	orchURI      string
	ethAddress   string
	gpuName      string
	gpuGb        float64
	avail        float64
	totalCap     float64
	pricePerUnit float64
	bestLatMs    *float64
	avgLatMs     *float64
	swapRatio    *float64
	avgAvail     *float64
	capabilities []string
	score        float64
}

// Resolve merges per-source rows into per-capability dataset rows.
func Resolve(perSource map[sources.Kind][]sources.NormalizedOrch, cfg Config) Result {
	conflicts := []ConflictEntry{}
	dropped := []DroppedEntry{}
	warnings := []string{}
	perSourceCounts := map[string]int{}

	enabled := make([]SourceConfig, 0)
	for _, s := range cfg.Sources {
		if s.Enabled {
			enabled = append(enabled, s)
		}
	}
	sort.Slice(enabled, func(i, j int) bool { return enabled[i].Priority < enabled[j].Priority })

	if len(enabled) == 0 {
		warnings = append(warnings, "No sources enabled — returning empty dataset")
		return Result{
			Capabilities: map[string][]DatasetRow{},
			Audit: AuditEntry{
				MembershipSource: "none",
				Warnings:         warnings,
				PerSourceCounts:  perSourceCounts,
			},
		}
	}

	fieldPriority := make(map[string][]sources.Kind)
	for k, v := range defaultFieldPriority {
		fieldPriority[k] = append([]sources.Kind(nil), v...)
	}
	for k, v := range cfg.FieldPriority {
		fieldPriority[k] = v
	}

	sourceIndexes := make(map[sources.Kind]map[orchKey][]sources.NormalizedOrch)
	for _, s := range enabled {
		rows := perSource[s.Kind]
		perSourceCounts[string(s.Kind)] = len(rows)
		sourceIndexes[s.Kind] = indexByOrch(rows)
	}

	strategy := cfg.MembershipStrategy
	if strategy == "" {
		strategy = "union"
	}

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
	} else {
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
	}

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

	for k := range membershipKeys {
		s := string(k)
		if strings.HasPrefix(s, "uri:") {
			uri := s[4:]
			if eth, ok := uriToEth[uri]; ok {
				ethKey := orchKey("eth:" + eth)
				if _, has := membershipKeys[ethKey]; has {
					delete(membershipKeys, k)
				}
			}
		}
	}

	resolveKey := func(k orchKey) (orchKey, bool) {
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

	if strategy != "union" {
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
	}

	merged := make(map[orchKey]mergedOrch)

	for memberKey := range membershipKeys {
		m := mergedOrch{}
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
			if winner != "" {
				apply(&m, winnerRow)
				if len(losers) > 0 {
					conflicts = append(conflicts, ConflictEntry{
						OrchKey: string(memberKey),
						Field:   field,
						Winner:  winner,
						Losers:  losers,
					})
				}
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
		merged[memberKey] = m
	}

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

	return Result{
		Capabilities: capabilities,
		Audit: AuditEntry{
			MembershipSource:   membershipSource,
			TotalOrchestrators: totalOrch,
			TotalCapabilities:  len(capabilities),
			Conflicts:          conflicts,
			Dropped:            dropped,
			Warnings:           warnings,
			PerSourceCounts:    perSourceCounts,
		},
	}
}

func fieldSet(r sources.NormalizedOrch, field string) bool {
	switch field {
	case "orchUri":
		return r.OrchURI != ""
	case "ethAddress":
		return r.EthAddress != ""
	case "gpuName":
		return r.GPUName != ""
	case "gpuGb":
		return r.GPUGb != 0
	case "avail":
		return r.Avail != 0
	case "totalCap":
		return r.TotalCap != 0
	case "pricePerUnit":
		return r.PricePerUnit != 0
	case "bestLatMs":
		return r.BestLatMs != nil
	case "avgLatMs":
		return r.AvgLatMs != nil
	case "swapRatio":
		return r.SwapRatio != nil
	case "avgAvail":
		return r.AvgAvail != nil
	case "score":
		return r.Score != 0
	default:
		return false
	}
}

func fieldVal(r sources.NormalizedOrch, field string) any {
	switch field {
	case "orchUri":
		return r.OrchURI
	case "ethAddress":
		return r.EthAddress
	case "gpuName":
		return r.GPUName
	case "gpuGb":
		return r.GPUGb
	case "avail":
		return r.Avail
	case "totalCap":
		return r.TotalCap
	case "pricePerUnit":
		return r.PricePerUnit
	case "bestLatMs":
		return r.BestLatMs
	case "avgLatMs":
		return r.AvgLatMs
	case "swapRatio":
		return r.SwapRatio
	case "avgAvail":
		return r.AvgAvail
	case "score":
		return r.Score
	default:
		return nil
	}
}
