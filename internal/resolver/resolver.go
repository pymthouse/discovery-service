package resolver

import (
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
	warnings := []string{}

	enabled := sortedEnabledSources(cfg)
	if len(enabled) == 0 {
		warnings = append(warnings, "No sources enabled — returning empty dataset")
		return Result{
			Capabilities: map[string][]DatasetRow{},
			Audit: AuditEntry{
				MembershipSource: "none",
				Warnings:         warnings,
				PerSourceCounts:  map[string]int{},
			},
		}
	}

	fieldPriority := mergeFieldPriority(cfg)
	sourceIndexes, perSourceCounts := buildSourceIndexes(perSource, enabled)

	strategy := cfg.MembershipStrategy
	if strategy == "" {
		strategy = "union"
	}

	membershipKeys, membershipSource, warnings := computeMembership(strategy, enabled, sourceIndexes, warnings)
	uriToEth, ethToUri := buildEthUriMaps(enabled, sourceIndexes)
	dedupeUriMembershipKeys(membershipKeys, uriToEth)

	resolveKey := membershipKeyResolver(membershipKeys, uriToEth, ethToUri)
	dropped := collectDroppedOutsideMembership(strategy, enabled, sourceIndexes, membershipSource, resolveKey)

	merged := make(map[orchKey]mergedOrch)
	for memberKey := range membershipKeys {
		sourceRows := sourceRowsForMember(memberKey, enabled, sourceIndexes, uriToEth, ethToUri)
		merged[memberKey] = mergeMemberOrchestrator(memberKey, enabled, sourceRows, fieldPriority, &conflicts)
	}

	capabilities, totalOrch := buildCapabilityDataset(merged)

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
