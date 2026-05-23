package resolver

import (
	"github.com/livepeer/discovery-service/internal/sources"
)

// BuildRegistryDataset maps registry manifest rows to per-capability dataset rows
// without merging them with legacy gateway metrics.
func BuildRegistryDataset(rows []sources.NormalizedOrch) map[string][]DatasetRow {
	capabilities := make(map[string][]DatasetRow)
	for _, r := range rows {
		if r.EffectiveServiceType() != sources.ServiceTypeRegistry {
			continue
		}
		score := r.Score
		if score == 0 {
			score = 1
		}
		for _, cap := range r.Capabilities {
			if cap == "" {
				continue
			}
			capabilities[cap] = append(capabilities[cap], DatasetRow{
				ServiceType:     string(sources.ServiceTypeRegistry),
				EthAddress:      r.EthAddress,
				OrchURI:         r.OrchURI,
				Score:           score,
				OfferingID:      r.OfferingID,
				InteractionMode: r.InteractionMode,
				WorkUnit:        r.WorkUnit,
				PricePerUnitWei: r.PricePerUnitWei,
			})
		}
	}
	return capabilities
}
