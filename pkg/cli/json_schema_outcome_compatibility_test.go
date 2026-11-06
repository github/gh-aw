//go:build !integration

package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/stretchr/testify/require"
)

func TestOutputSchemasValidateHistoricalOutcomeEvidence(t *testing.T) {
	t.Parallel()

	report := OutcomeReport{
		OutcomeEvaluation: outcomeEvidence(OutcomeStatusAccepted, EvidenceStrong, "closed"),
		Type:              "create_issue",
		CreatedAt:         "2026-09-01T10:00:00Z",
		CheckedAt:         "2026-09-01T11:00:00Z",
	}
	outcomes := []OutcomesData{{RunID: 42, Items: []OutcomeReport{report}}}
	episodes := &EpisodeOutcomesData{Episodes: []EpisodeOutcomeData{{
		EpisodeID: "episode-42",
		Items:     []EpisodeOutcomeObservation{{RunID: 42, OutcomeReport: report}},
	}}}
	audit := AuditData{Outcomes: []OutcomeReport{report}}
	tests := []struct {
		name        string
		schemaName  string
		output      any
		recordCount int
	}{
		{"audit", "audit", audit, 1},
		{"logs", "logs", LogsData{Outcomes: outcomes, EpisodeOutcomes: episodes}, 2},
		{"cross-run", "logs", CrossRunAuditReport{Outcomes: outcomes, EpisodeOutcomes: episodes}, 2},
		{"cached-audit", "logs-jsonl", cachedLogsJSONLRecord{
			SchemaVersion: cachedLogsJSONLSchemaVersion,
			Kind:          cachedLogsJSONLKindRun,
			Run:           &cachedLogsJSONLRunData{RunData: RunData{RunID: 42}, Audit: &audit},
		}, 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			generated, err := GenerateNamedOutputSchema(tt.schemaName)
			require.NoError(t, err)
			persisted, err := os.ReadFile(filepath.Join("..", "..", "schemas", tt.schemaName+".schema.json"))
			require.NoError(t, err)
			for _, source := range []struct {
				name string
				data []byte
			}{{"generated", generated}, {"persisted", persisted}} {
				t.Run(source.name, func(t *testing.T) {
					var schema jsonschema.Schema
					require.NoError(t, json.Unmarshal(source.data, &schema))
					resolved, err := schema.Resolve(nil)
					require.NoError(t, err)
					data, err := json.Marshal(tt.output)
					require.NoError(t, err)
					var value any
					require.NoError(t, json.Unmarshal(data, &value))
					records := outcomeEvidenceRecords(value)
					require.Len(t, records, tt.recordCount)

					for _, record := range records {
						require.Equal(t, false, record["zero_touch_evidence_complete"],
							"new output must emit completeness even when false")
					}
					require.NoError(t, resolved.Validate(value))
					for _, record := range records {
						record["zero_touch_evidence_complete"] = true
					}
					require.NoError(t, resolved.Validate(value))
					for _, record := range records {
						delete(record, "zero_touch_evidence_complete")
					}
					require.NoError(t, resolved.Validate(value), "historical outcomes must remain valid")

					records[0]["zero_touch_evidence_complete"] = "true"
					require.Error(t, resolved.Validate(value), "completeness must still be a boolean")
					delete(records[0], "zero_touch_evidence_complete")
					delete(records[0], "type")
					require.Error(t, resolved.Validate(value), "other required outcome fields must stay required")
				})
			}
		})
	}
}

func outcomeEvidenceRecords(value any) []map[string]any {
	var records []map[string]any
	switch value := value.(type) {
	case map[string]any:
		if _, ok := value["zero_touch_evidence_complete"]; ok {
			records = append(records, value)
		}
		for _, child := range value {
			records = append(records, outcomeEvidenceRecords(child)...)
		}
	case []any:
		for _, child := range value {
			records = append(records, outcomeEvidenceRecords(child)...)
		}
	}
	return records
}
