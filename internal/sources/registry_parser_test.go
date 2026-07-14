package sources

import (
	"testing"
)

func TestParseRegistryManifestBodyV3(t *testing.T) {
	body := []byte(`{
		"schema_version":"3.0.1",
		"eth_address":"0xabcdef0000000000000000000000000000000000",
		"issued_at":"2026-01-01T00:00:00Z",
		"nodes":[{
			"id":"node-1",
			"url":"https://worker.example.com",
			"capabilities":[{
				"name":"openai:/v1/chat/completions",
				"work_unit":"token",
				"offerings":[{"id":"gpt-oss-20b","price_per_work_unit_wei":"1000"}]
			}]
		}],
		"signature":{"alg":"eth-personal-sign","value":"0x00","signed_canonical_bytes_sha256":"0x00"}
	}`)

	rows, err := ParseRegistryManifestBody(body)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 {
		t.Fatalf("got %d rows, want 1", len(rows))
	}
	if rows[0].WorkerURL != "https://worker.example.com" {
		t.Fatalf("worker = %q", rows[0].WorkerURL)
	}
	if rows[0].CapabilityID != "openai:/v1/chat/completions" {
		t.Fatalf("capability = %q", rows[0].CapabilityID)
	}
	if rows[0].OfferingID != "gpt-oss-20b" {
		t.Fatalf("offering = %q", rows[0].OfferingID)
	}
	if rows[0].WorkUnit != "token" {
		t.Fatalf("work unit = %q", rows[0].WorkUnit)
	}
	if rows[0].PricePerUnitWei != "1000" {
		t.Fatalf("price = %q", rows[0].PricePerUnitWei)
	}

	normalized := registryRowsToNormalized(rows)
	if normalized[0].ServiceType != ServiceTypeModules {
		t.Fatalf("service type = %q", normalized[0].ServiceType)
	}
}

func TestParseRegistryManifestBodyV3CapabilityWithoutOfferings(t *testing.T) {
	body := []byte(`{
		"eth_address":"0xabcdef0000000000000000000000000000000000",
		"nodes":[{
			"url":"https://worker.example.com/",
			"capabilities":[{
				"name":"openai:/v1/chat/completions",
				"work_unit":"token"
			}]
		}]
	}`)

	rows, err := ParseRegistryManifestBody(body)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 {
		t.Fatalf("got %d rows, want 1", len(rows))
	}
	if rows[0].WorkerURL != "https://worker.example.com" {
		t.Fatalf("worker = %q", rows[0].WorkerURL)
	}
	if rows[0].CapabilityID != "openai:/v1/chat/completions" {
		t.Fatalf("capability = %q", rows[0].CapabilityID)
	}
	if rows[0].WorkUnit != "token" || rows[0].OfferingID != "" {
		t.Fatalf("unexpected offering row: %#v", rows[0])
	}
}

func TestParseRegistryManifestBodyCoordinatorEnvelope(t *testing.T) {
	body := []byte(`{
		"manifest":{
			"spec_version":"0.1.0",
			"publication_seq":1,
			"issued_at":"2026-01-01T00:00:00Z",
			"expires_at":"2027-01-01T00:00:00Z",
			"orch":{"eth_address":"0xd00354656922168815fcd1e51cbddb9e359e3c7f"},
			"capabilities":[
				{
					"capability_id":"daydream:scope:v1",
					"offering_id":"default",
					"interaction_mode":"session-control-external-media@v0",
					"work_unit":{"name":"session"},
					"price_per_unit_wei":"1000",
					"worker_url":"https://ai-rig-worker.example.com/path"
				}
			]
		},
		"signature":{"algorithm":"ed25519","value":"deadbeef"}
	}`)

	rows, err := ParseRegistryManifestBody(body)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 {
		t.Fatalf("got %d rows, want 1", len(rows))
	}
	if rows[0].CapabilityID != "daydream:scope:v1" {
		t.Fatalf("capability = %q", rows[0].CapabilityID)
	}
	if rows[0].OfferingID != "default" {
		t.Fatalf("offering = %q", rows[0].OfferingID)
	}
	if rows[0].InteractionMode != "session-control-external-media@v0" {
		t.Fatalf("interaction mode = %q", rows[0].InteractionMode)
	}
}

func TestManifestFetchCandidates(t *testing.T) {
	got := ManifestFetchCandidates("https://orch.example.com:8935/")
	want := []string{
		"https://orch.example.com:8935/",
		"https://orch.example.com:8935/.well-known/livepeer-registry.json",
	}
	if len(got) != len(want) {
		t.Fatalf("got %#v, want %#v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("candidate[%d] = %q, want %q", i, got[i], want[i])
		}
	}

	exact := ManifestFetchCandidates("https://orch.example.com/.well-known/livepeer-registry.json")
	if len(exact) != 1 || exact[0] != "https://orch.example.com/.well-known/livepeer-registry.json" {
		t.Fatalf("exact manifest candidates = %#v", exact)
	}
}
