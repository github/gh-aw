package connectors

import (
	"os"
	"testing"
)

func TestConnectorSpec(t *testing.T) {
	spec, err := LoadSpec("testdata/example.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if spec.ID != "example.com/issues" || len(spec.Triggers) != 1 || len(spec.MCP) != 1 || len(spec.SafeOutputs) != 1 || len(spec.Context) != 1 {
		t.Fatal("example must exercise all four capability types")
	}
}

// Provider sessions add definitions under testdata/providers/<provider>/spec.yaml.
// This same test validates every definition against the shared contract.
func TestProviderFixtures(t *testing.T) {
	entries, err := os.ReadDir("testdata/providers")
	if os.IsNotExist(err) {
		t.Skip("provider fixtures are authored separately")
	}
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if entry.IsDir() {
			t.Run(entry.Name(), func(t *testing.T) {
				if _, err := LoadSpec("testdata/providers/" + entry.Name() + "/spec.yaml"); err != nil {
					t.Fatal(err)
				}
			})
		}
	}
}

func TestDecodeRejectsUnknownFields(t *testing.T) {
	var spec ConnectorSpec
	if err := Decode([]byte(`{"apiVersion":"x","surprise":true}`), &spec); err == nil {
		t.Fatal("unknown fields must fail")
	}
}
