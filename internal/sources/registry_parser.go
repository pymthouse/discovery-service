package sources

import (
	"encoding/json"
	"strings"
)

// RegistryOfferingRow is one capability/offering tuple from an on-chain manifest.
type RegistryOfferingRow struct {
	EthAddress      string
	WorkerURL       string
	CapabilityID    string
	OfferingID      string
	InteractionMode string
	WorkUnit        string
	PricePerUnitWei string
}

type v3Manifest struct {
	SchemaVersion string   `json:"schema_version"`
	EthAddress    string   `json:"eth_address"`
	Nodes         []v3Node `json:"nodes"`
}

type v3Node struct {
	URL          string         `json:"url"`
	WorkerEth    string         `json:"worker_eth_address"`
	Capabilities []v3Capability `json:"capabilities"`
}

type v3Capability struct {
	Name      string       `json:"name"`
	WorkUnit  string       `json:"work_unit"`
	Offerings []v3Offering `json:"offerings"`
}

type v3Offering struct {
	ID                  string `json:"id"`
	PricePerWorkUnitWei string `json:"price_per_work_unit_wei"`
}

type coordinatorEnvelope struct {
	Manifest coordinatorManifest `json:"manifest"`
}

type coordinatorManifest struct {
	Orch         coordinatorOrchInfo   `json:"orch"`
	Capabilities []coordinatorCapEntry `json:"capabilities"`
}

type coordinatorOrchInfo struct {
	EthAddress string `json:"eth_address"`
	ServiceURI string `json:"service_uri"`
}

type coordinatorCapEntry struct {
	CapabilityID    string              `json:"capability_id"`
	OfferingID      string              `json:"offering_id"`
	InteractionMode string              `json:"interaction_mode"`
	PricePerUnitWei string              `json:"price_per_unit_wei"`
	WorkerURL       string              `json:"worker_url"`
	WorkUnit        coordinatorWorkUnit `json:"work_unit"`
}

type coordinatorWorkUnit struct {
	Name string `json:"name"`
}

// ParseRegistryManifestBody extracts registry offering rows from manifest JSON.
func ParseRegistryManifestBody(body []byte) ([]RegistryOfferingRow, error) {
	if rows := parseV3ManifestRows(body); len(rows) > 0 {
		return rows, nil
	}
	if rows := parseCoordinatorManifestRows(body); len(rows) > 0 {
		return rows, nil
	}
	return nil, nil
}

func parseV3ManifestRows(body []byte) []RegistryOfferingRow {
	var m v3Manifest
	if err := json.Unmarshal(body, &m); err != nil || len(m.Nodes) == 0 {
		return nil
	}
	eth := strings.ToLower(strings.TrimSpace(m.EthAddress))
	out := make([]RegistryOfferingRow, 0)
	for _, node := range m.Nodes {
		worker := strings.TrimRight(strings.TrimSpace(node.URL), "/")
		if worker == "" {
			continue
		}
		nodeEth := strings.ToLower(strings.TrimSpace(node.WorkerEth))
		if nodeEth == "" {
			nodeEth = eth
		}
		for _, cap := range node.Capabilities {
			capID := strings.TrimSpace(cap.Name)
			if capID == "" {
				continue
			}
			workUnit := strings.TrimSpace(cap.WorkUnit)
			if len(cap.Offerings) == 0 {
				out = append(out, RegistryOfferingRow{
					EthAddress:      nodeEth,
					WorkerURL:       worker,
					CapabilityID:    capID,
					WorkUnit:        workUnit,
					PricePerUnitWei: "",
				})
				continue
			}
			for _, off := range cap.Offerings {
				offID := strings.TrimSpace(off.ID)
				if offID == "" {
					continue
				}
				out = append(out, RegistryOfferingRow{
					EthAddress:      nodeEth,
					WorkerURL:       worker,
					CapabilityID:    capID,
					OfferingID:      offID,
					WorkUnit:        workUnit,
					PricePerUnitWei: strings.TrimSpace(off.PricePerWorkUnitWei),
				})
			}
		}
	}
	return out
}

func parseCoordinatorManifestRows(body []byte) []RegistryOfferingRow {
	var env coordinatorEnvelope
	if err := json.Unmarshal(body, &env); err != nil || len(env.Manifest.Capabilities) == 0 {
		return nil
	}
	eth := strings.ToLower(strings.TrimSpace(env.Manifest.Orch.EthAddress))
	fallback := strings.TrimSpace(env.Manifest.Orch.ServiceURI)
	out := make([]RegistryOfferingRow, 0, len(env.Manifest.Capabilities))
	for _, tuple := range env.Manifest.Capabilities {
		capID := strings.TrimSpace(tuple.CapabilityID)
		offID := strings.TrimSpace(tuple.OfferingID)
		if capID == "" || offID == "" {
			continue
		}
		worker := coordinatorWorkerURL(tuple, fallback)
		if worker == "" {
			continue
		}
		workUnit := strings.TrimSpace(tuple.WorkUnit.Name)
		out = append(out, RegistryOfferingRow{
			EthAddress:      eth,
			WorkerURL:       worker,
			CapabilityID:    capID,
			OfferingID:      offID,
			InteractionMode: strings.TrimSpace(tuple.InteractionMode),
			WorkUnit:        workUnit,
			PricePerUnitWei: strings.TrimSpace(tuple.PricePerUnitWei),
		})
	}
	return out
}

func coordinatorWorkerURL(tuple coordinatorCapEntry, fallback string) string {
	url := strings.TrimRight(strings.TrimSpace(tuple.WorkerURL), "/")
	if url != "" {
		return url
	}
	return strings.TrimRight(strings.TrimSpace(fallback), "/")
}

func registryRowsToNormalized(rows []RegistryOfferingRow) []NormalizedOrch {
	out := make([]NormalizedOrch, 0, len(rows))
	for _, r := range rows {
		if r.CapabilityID == "" || r.WorkerURL == "" {
			continue
		}
		out = append(out, NormalizedOrch{
			ServiceType:     ServiceTypeModules,
			EthAddress:      r.EthAddress,
			OrchURI:         r.WorkerURL,
			Capabilities:    []string{r.CapabilityID},
			Score:           1,
			OfferingID:      r.OfferingID,
			InteractionMode: r.InteractionMode,
			WorkUnit:        r.WorkUnit,
			PricePerUnitWei: r.PricePerUnitWei,
		})
	}
	return out
}
