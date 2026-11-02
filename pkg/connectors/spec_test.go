package connectors

import (
	"path/filepath"
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

// Versioned YAML fixtures exercise legacy compatibility, not Markdown authoring.
func TestProviderFixtures(t *testing.T) {
	files, err := filepath.Glob("testdata/providers/*/spec.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if len(files) == 0 {
		t.Fatal("legacy provider spec fixtures are missing")
	}
	for _, file := range files {
		t.Run(filepath.Base(filepath.Dir(file)), func(t *testing.T) {
			if _, err := LoadSpec(file); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestDecodeRejectsUnknownFields(t *testing.T) {
	var spec ConnectorSpec
	if err := Decode([]byte(`{"apiVersion":"x","surprise":true}`), &spec); err == nil {
		t.Fatal("unknown fields must fail")
	}
}
