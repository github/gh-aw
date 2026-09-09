package main

import (
	"bytes"
	"encoding/json"
	"testing"
)

func TestGenerateLogsSchema(t *testing.T) {
	var first bytes.Buffer
	if err := generateLogsSchema(&first); err != nil {
		t.Fatalf("generateLogsSchema() error = %v", err)
	}

	var schema struct {
		Type       string                     `json:"type"`
		Properties map[string]json.RawMessage `json:"properties"`
	}
	if err := json.Unmarshal(first.Bytes(), &schema); err != nil {
		t.Fatalf("generated schema is not valid JSON: %v", err)
	}
	if schema.Type != "object" {
		t.Errorf("schema type = %q, want object", schema.Type)
	}
	for _, property := range []string{"summary", "runs", "logs_location"} {
		if _, ok := schema.Properties[property]; !ok {
			t.Errorf("schema is missing property %q", property)
		}
	}

	var second bytes.Buffer
	if err := generateLogsSchema(&second); err != nil {
		t.Fatalf("second generateLogsSchema() error = %v", err)
	}
	if !bytes.Equal(first.Bytes(), second.Bytes()) {
		t.Error("schema generation is not deterministic")
	}
}
