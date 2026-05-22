package sources

import "testing"

func TestParseServiceTypes(t *testing.T) {
	all := ParseServiceTypes(nil)
	if len(all) != 2 {
		t.Fatalf("default types = %#v", all)
	}
	registryOnly := ParseServiceTypes([]string{"registry"})
	if len(registryOnly) != 1 || registryOnly[0] != ServiceTypeRegistry {
		t.Fatalf("registry filter = %#v", registryOnly)
	}
}
