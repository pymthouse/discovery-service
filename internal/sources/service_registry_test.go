package sources

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/livepeer/discovery-service/internal/config"
)

func TestNormalizeRegisteredServiceURI(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"https://ai1.eliteencoder.net:8936", "https://ai1.eliteencoder.net:8936"},
		{"https://ai1.eliteencoder.net:8936/", "https://ai1.eliteencoder.net:8936"},
		{"ai1.eliteencoder.net:8936", "https://ai1.eliteencoder.net:8936"},
		{"", ""},
		{"not a uri", ""},
	}
	for _, tt := range tests {
		got := normalizeRegisteredServiceURI(tt.in)
		if got != tt.want {
			t.Fatalf("normalizeRegisteredServiceURI(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestRegisteredServiceRegistryContractsDedupes(t *testing.T) {
	got := registeredServiceRegistryContracts(config.Config{
		ServiceRegistryAddress:   "0xC92d3A360b8f9e083bA64DE15d95Cf8180897431",
		AIServiceRegistryAddress: "0xc92d3a360b8f9e083ba64de15d95cf8180897431",
	})
	if len(got) != 1 {
		t.Fatalf("got %#v", got)
	}
}

func TestServiceRegistryFetchAllDisabled(t *testing.T) {
	a := NewServiceRegistry(config.Config{ServiceRegistryRefreshEnabled: false})
	fr, err := a.FetchAll(context.Background())
	if err != nil || !fr.Stats.OK || fr.Stats.Fetched != 0 || len(fr.Rows) != 0 {
		t.Fatalf("disabled source should no-op: %#v err=%v", fr, err)
	}
	if a.Kind() != KindServiceRegistry {
		t.Fatalf("kind = %q", a.Kind())
	}
}

func TestServiceRegistryFetchAllWalksPoolAndReadsURIs(t *testing.T) {
	const (
		bonding  = "0x35bcf3c30594191d53231e4ff333e8a770453e40"
		protocol = "0xc92d3a360b8f9e083ba64de15d95cf8180897431"
		ai       = "0x04c0b249740175999e5bf5c9ac1dA92431ef34c5"
		orch1    = "0x1111111111111111111111111111111111111111"
		orch2    = "0x2222222222222222222222222222222222222222"
	)
	firstSel := methodSelector(firstTranscoderABI)
	nextSel := methodSelector(nextTranscoderABI)
	uriSel := methodSelector(serviceURIABI)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req struct {
			Params []json.RawMessage `json:"params"`
		}
		if err := json.Unmarshal(body, &req); err != nil || len(req.Params) == 0 {
			http.Error(w, "bad rpc", http.StatusBadRequest)
			return
		}
		var call struct {
			To   string `json:"to"`
			Data string `json:"data"`
		}
		if err := json.Unmarshal(req.Params[0], &call); err != nil {
			http.Error(w, "bad call", http.StatusBadRequest)
			return
		}
		to := strings.ToLower(call.To)
		data := strings.ToLower(strings.TrimPrefix(call.Data, "0x"))
		result := "0x" + strings.Repeat("0", 64)

		switch {
		case to == bonding && strings.HasPrefix(data, firstSel):
			result = addressWord(orch1)
		case to == bonding && strings.HasPrefix(data, nextSel) && strings.HasSuffix(data, strings.TrimPrefix(orch1, "0x")):
			result = addressWord(orch2)
		case to == bonding && strings.HasPrefix(data, nextSel) && strings.HasSuffix(data, strings.TrimPrefix(orch2, "0x")):
			result = addressWord("0x0000000000000000000000000000000000000000")
		case to == protocol && strings.HasPrefix(data, uriSel) && strings.HasSuffix(data, strings.TrimPrefix(orch1, "0x")):
			result = encodeABIString("https://classic.example:8935")
		case to == protocol && strings.HasPrefix(data, uriSel) && strings.HasSuffix(data, strings.TrimPrefix(orch2, "0x")):
			result = encodeABIString("")
		case to == strings.ToLower(ai) && strings.HasPrefix(data, uriSel) && strings.HasSuffix(data, strings.TrimPrefix(orch1, "0x")):
			result = encodeABIString("https://ai1.eliteencoder.net:8936")
		case to == strings.ToLower(ai) && strings.HasPrefix(data, uriSel) && strings.HasSuffix(data, strings.TrimPrefix(orch2, "0x")):
			result = encodeABIString("ai2.example:8936")
		}

		_ = json.NewEncoder(w).Encode(map[string]any{
			"jsonrpc": "2.0",
			"id":      1,
			"result":  result,
		})
	}))
	defer srv.Close()

	a := NewServiceRegistry(config.Config{
		ServiceRegistryRefreshEnabled:    true,
		AIServiceRegistryRPCURL:          srv.URL,
		BondingManagerAddress:            bonding,
		ServiceRegistryAddress:           protocol,
		AIServiceRegistryAddress:         ai,
		RegistryManifestTimeoutMs:        2000,
		RegistryManifestMaxConcurrency:   4,
		RegistryManifestMaxOrchestrators: 10,
	})
	fr, err := a.FetchAll(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !fr.Stats.OK || fr.Stats.Fetched != 3 {
		t.Fatalf("stats=%#v rows=%#v", fr.Stats, fr.Rows)
	}

	got := map[string]string{}
	for _, row := range fr.Rows {
		got[row.EthAddress+"|"+row.OrchURI] = row.OrchURI
	}
	want := []string{
		orch1 + "|https://classic.example:8935",
		orch1 + "|https://ai1.eliteencoder.net:8936",
		orch2 + "|https://ai2.example:8936",
	}
	for _, key := range want {
		if _, ok := got[key]; !ok {
			t.Fatalf("missing %q in %#v", key, fr.Rows)
		}
	}
}

func TestWalkTranscoderPoolStopsOnCycle(t *testing.T) {
	const bonding = "0x35bcf3c30594191d53231e4ff333e8a770453e40"
	const orch1 = "0x1111111111111111111111111111111111111111"
	firstSel := methodSelector(firstTranscoderABI)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req struct {
			Params []json.RawMessage `json:"params"`
		}
		_ = json.Unmarshal(body, &req)
		if len(req.Params) == 0 {
			http.Error(w, "bad rpc", http.StatusBadRequest)
			return
		}
		var call struct {
			Data string `json:"data"`
		}
		_ = json.Unmarshal(req.Params[0], &call)
		data := strings.ToLower(strings.TrimPrefix(call.Data, "0x"))
		result := addressWord(orch1)
		if !strings.HasPrefix(data, firstSel) {
			result = addressWord(orch1)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"result": result})
	}))
	defer srv.Close()

	addrs, err := walkTranscoderPool(context.Background(), srv.URL, bonding, 100, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(addrs) != 1 || addrs[0] != orch1 {
		t.Fatalf("got %#v", addrs)
	}
}

func TestDecodeABIAddress(t *testing.T) {
	got, err := decodeABIAddress("0x0000000000000000000000001111111111111111111111111111111111111111")
	if err != nil {
		t.Fatal(err)
	}
	if got != "0x1111111111111111111111111111111111111111" {
		t.Fatalf("got %q", got)
	}
	if !isZeroAddress("0x0000000000000000000000000000000000000000") {
		t.Fatal("expected zero address")
	}
}

func addressWord(addr string) string {
	addr = strings.TrimPrefix(strings.ToLower(addr), "0x")
	return "0x" + strings.Repeat("0", 24) + addr
}

func encodeABIString(s string) string {
	data := hex.EncodeToString([]byte(s))
	for len(data)%64 != 0 {
		data += "00"
	}
	return "0x" + fmt.Sprintf("%064x", 32) + fmt.Sprintf("%064x", len(s)) + data
}
