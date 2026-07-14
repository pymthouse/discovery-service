package resolver

import (
	"github.com/livepeer/discovery-service/internal/sources"
)

// BuildRegistryDataset maps modules/registry manifest rows to per-capability
// dataset rows without merging them with live-video / batch gateway metrics.
func BuildRegistryDataset(rows []sources.NormalizedOrch) map[string][]DatasetRow {
	capabilities := make(map[string][]DatasetRow)
	for _, r := range rows {
		if r.EffectiveServiceType() != sources.ServiceTypeModules {
			continue
		}
		for _, cap := range r.Capabilities {
			if cap == "" {
				continue
			}
			capabilities[cap] = append(capabilities[cap], DatasetRow{
				ServiceType:     string(sources.ServiceTypeModules),
				EthAddress:      r.EthAddress,
				OrchURI:         r.OrchURI,
				Score:           r.Score,
				OfferingID:      r.OfferingID,
				InteractionMode: r.InteractionMode,
				WorkUnit:        r.WorkUnit,
				PricePerUnitWei: r.PricePerUnitWei,
			})
		}
	}
	return capabilities
}
