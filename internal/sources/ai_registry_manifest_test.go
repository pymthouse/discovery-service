package sources

import "testing"

func TestServiceURICallData(t *testing.T) {
	got, err := serviceURICallData("0xd00354656922168815fcd1e51cbddb9e359e3c7f")
	if err != nil {
		t.Fatal(err)
	}
	const want = "0x214c2a4b000000000000000000000000d00354656922168815fcd1e51cbddb9e359e3c7f"
	if got != want {
		t.Fatalf("call data = %q, want %q", got, want)
	}
}

func TestDecodeABIString(t *testing.T) {
	raw := "0x" +
		"0000000000000000000000000000000000000000000000000000000000000020" +
		"000000000000000000000000000000000000000000000000000000000000001f" +
		"68747470733a2f2f636f6f7264696e61746f722e786f64656170702e78797a" +
		"00000000000000000000000000000000000000000000000000000000000000"
	got, err := decodeABIString(raw)
	if err != nil {
		t.Fatal(err)
	}
	if got != "https://coordinator.xodeapp.xyz" {
		t.Fatalf("decoded string = %q", got)
	}
}
