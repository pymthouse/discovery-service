package sources

import (
	"fmt"
	"regexp"
	"strings"
)

const maxQueryRows = 1000

var capabilityPattern = regexp.MustCompile(`^[a-zA-Z0-9._:-]+$`)

var fallbackCapabilities = []string{
	"noop",
	"streamdiffusion",
	"streamdiffusion-sdxl",
	"streamdiffusion-sdxl-v2v",
}

const capabilitiesSQL = `SELECT DISTINCT capability_name
FROM semantic.network_capabilities
WHERE timestamp_ts >= now() - INTERVAL 1 HOUR
  AND warm_bool = 1
ORDER BY capability_name
FORMAT JSON`

const leaderboardSQLTemplate = `SELECT
    cap.orch_uri AS orch_uri,
    cap.gpu_name AS gpu_name,
    round(cap.gpu_mem_gb, 1) AS gpu_gb,
    cap.avail AS avail,
    cap.total_cap AS total_cap,
    cap.price_per_unit AS price_per_unit,
    round(lat.best_latency, 1) AS best_lat_ms,
    round(lat.avg_latency, 1) AS avg_lat_ms,
    round(stab.swing_ratio, 2) AS swap_ratio,
    round(stab.avg_avail, 1) AS avg_avail
FROM (
    SELECT
        orch_uri,
        gpu_name,
        round(gpu_memory_total_gbs, 1) AS gpu_mem_gb,
        argMax(capacity_available, timestamp_ts) AS avail,
        argMax(total_capacity, timestamp_ts) AS total_cap,
        argMax(price_per_unit, timestamp_ts) AS price_per_unit
    FROM semantic.network_capabilities
    WHERE timestamp_ts >= now() - INTERVAL 1 HOUR
      AND capability_name = '%s'
      AND warm_bool = 1
    GROUP BY orch_uri, gpu_name, gpu_memory_total_gbs
    HAVING avail > 0
) AS cap
LEFT JOIN (
    SELECT
        orchestrator_url,
        avg(avg_latency) AS avg_latency,
        min(best_latency) AS best_latency
    FROM semantic.gateway_latency_summary
    WHERE timestamp_hour_ts >= now() - INTERVAL 24 HOUR
    GROUP BY orchestrator_url
) AS lat ON cap.orch_uri = lat.orchestrator_url
LEFT JOIN (
    SELECT
        orch_uri,
        (max(capacity_available) - min(capacity_available))
            / greatest(argMax(total_capacity, timestamp_ts), 1) AS swing_ratio,
        avg(capacity_available) AS avg_avail
    FROM semantic.network_capabilities
    WHERE timestamp_ts >= now() - INTERVAL 1 HOUR
      AND capability_name = '%s'
      AND warm_bool = 1
    GROUP BY orch_uri
) AS stab ON cap.orch_uri = stab.orch_uri
ORDER BY
    lat.best_latency ASC NULLS LAST,
    stab.swing_ratio ASC NULLS LAST,
    cap.price_per_unit ASC
LIMIT %d
FORMAT JSON`

func validateCapability(capability string) error {
	if capability == "" {
		return fmt.Errorf("capability is required")
	}
	if !capabilityPattern.MatchString(capability) {
		return fmt.Errorf("capability contains invalid characters")
	}
	if len(capability) > 128 {
		return fmt.Errorf("capability must be 128 characters or fewer")
	}
	return nil
}

func buildLeaderboardSQL(capability string, topN int) (string, error) {
	if err := validateCapability(capability); err != nil {
		return "", err
	}
	if topN < 1 || topN > 1000 {
		return "", fmt.Errorf("topN must be between 1 and 1000")
	}
	escaped := strings.ReplaceAll(capability, "'", "\\'")
	return fmt.Sprintf(leaderboardSQLTemplate, escaped, escaped, topN), nil
}

// CHRow is a ClickHouse leaderboard result row.
type CHRow struct {
	OrchURI      string   `json:"orch_uri"`
	GPUName      string   `json:"gpu_name"`
	GPUGb        float64  `json:"gpu_gb"`
	Avail        float64  `json:"avail"`
	TotalCap     float64  `json:"total_cap"`
	PricePerUnit float64  `json:"price_per_unit"`
	BestLatMs    *float64 `json:"best_lat_ms"`
	AvgLatMs     *float64 `json:"avg_lat_ms"`
	SwapRatio    *float64 `json:"swap_ratio"`
	AvgAvail     *float64 `json:"avg_avail"`
}

func chRowToNormalized(r CHRow, capability string) NormalizedOrch {
	return NormalizedOrch{
		OrchURI:      r.OrchURI,
		GPUName:      r.GPUName,
		GPUGb:        r.GPUGb,
		Avail:        r.Avail,
		TotalCap:     r.TotalCap,
		PricePerUnit: r.PricePerUnit,
		BestLatMs:    r.BestLatMs,
		AvgLatMs:     r.AvgLatMs,
		SwapRatio:    r.SwapRatio,
		AvgAvail:     r.AvgAvail,
		Capabilities: []string{capability},
	}
}
