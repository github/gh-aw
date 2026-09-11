//go:build !integration

package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/jsonschema-go/jsonschema"
)

func TestJSONSchemaCommand(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		schemaName  string
		oneOfCount  int
		validOutput []any
	}{
		{
			name:       "audit",
			schemaName: "audit",
			oneOfCount: 3,
			validOutput: []any{
				AuditData{
					Overview:        OverviewData{},
					Metrics:         MetricsData{},
					DownloadedFiles: []FileInfo{},
					MCPServerHealth: &MCPServerHealth{
						Servers: []MCPServerHealthDetail{{
							MCPServerStatsBase: MCPServerStatsBase{
								ServerName:    "github",
								ToolCallCount: 3,
								ErrorCount:    1,
							},
							RequestCount: 3,
						}},
					},
				},
				AuditDiff{},
				[]AuditDiff{{}},
			},
		},
		{
			name:       "logs",
			schemaName: "logs",
			oneOfCount: 2,
			validOutput: []any{
				LogsData{
					ToolUsage: []ToolUsageSummary{{
						ToolUsageStatsBase: ToolUsageStatsBase{
							ToolName:  "github_get_issue",
							CallCount: 3,
						},
						Runs: 1,
					}},
					MCPFailures: []MCPFailureSummary{{
						ServerName: "github",
						AggregatedSummaryBase: AggregatedSummaryBase{
							Count:     1,
							Workflows: []string{"example"},
							RunIDs:    []int64{1},
						},
					}},
					AccessLog: &AccessLogSummary{
						ByWorkflow: map[string]*DomainAnalysis{
							"example": {
								AnalysisBase: AnalysisBase{
									TotalRequests:   3,
									AllowedRequests: 2,
									BlockedRequests: 1,
								},
							},
						},
					},
				},
				CrossRunAuditReport{
					MCPHealth: []MCPServerCrossRunHealth{{
						MCPServerStatsBase: MCPServerStatsBase{
							ServerName:    "github",
							ToolCallCount: 3,
							ErrorCount:    1,
						},
						RunsConnected: 1,
						TotalRuns:     1,
					}},
				},
			},
		},
		{
			name:       "logs-jsonl",
			schemaName: "logs-jsonl",
			oneOfCount: 3,
			validOutput: []any{
				cachedLogsJSONLRunItemSchema{
					SchemaVersion: cachedLogsJSONLSchemaVersion,
					Kind:          cachedLogsJSONLKindRun,
					Run: cachedLogsJSONLRunData{
						RunData: RunData{RunID: 42},
						Audit: &AuditData{
							MCPServerHealth: &MCPServerHealth{
								Servers: []MCPServerHealthDetail{{
									MCPServerStatsBase: MCPServerStatsBase{
										ServerName:    "github",
										ToolCallCount: 3,
										ErrorCount:    1,
									},
									RequestCount: 3,
								}},
							},
						},
					},
				},
				map[string]any{
					"schema_version": cachedLogsJSONLSchemaVersion,
					"kind":           cachedLogsJSONLKindWorkflowRuns,
					"request": cachedWorkflowRunsRequest{
						Host:       "github.com",
						Repository: "github/gh-aw",
						Args:       []string{"run", "list"},
					},
					"payload": []any{map[string]any{
						"databaseId": 42, "number": 7,
						"url":    "https://github.com/github/gh-aw/actions/runs/42",
						"status": "completed", "conclusion": "success", "workflowName": "Daily report",
						"createdAt": "2026-09-01T10:00:00Z", "startedAt": "2026-09-01T10:00:01Z",
						"updatedAt": "2026-09-01T10:02:00Z", "event": "schedule", "headBranch": "main",
						"headSha": "abc123", "displayTitle": "Daily report", "attempt": 1,
						"futureField": map[string]any{"nested": true},
					}},
				},
				cachedLogsJSONLRateLimitItemSchema{
					SchemaVersion: cachedLogsJSONLSchemaVersion,
					Kind:          cachedLogsJSONLKindRateLimit,
					RateLimit: GitHubAPIRateLimitReport{
						Host:  "github.com",
						Start: &GitHubAPIRateLimitState{Limit: 5000, Remaining: 4999},
					},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			first, stderr, err := executeJSONSchemaCommand(tt.schemaName)
			if err != nil {
				t.Fatalf("json-schema %s failed: %v", tt.schemaName, err)
			}
			if stderr != "" {
				t.Fatalf("json-schema %s wrote to stderr: %q", tt.schemaName, stderr)
			}
			if !json.Valid([]byte(first)) {
				t.Fatalf("json-schema %s returned invalid JSON", tt.schemaName)
			}
			if !strings.HasSuffix(first, "\n") {
				t.Fatalf("json-schema %s output does not end with a newline", tt.schemaName)
			}
			if strings.Contains(first, "\x1b[") {
				t.Fatalf("json-schema %s output contains ANSI escape sequences", tt.schemaName)
			}

			var schema jsonschema.Schema
			if err := json.Unmarshal([]byte(first), &schema); err != nil {
				t.Fatalf("unmarshal %s schema: %v", tt.schemaName, err)
			}
			resolved, err := schema.Resolve(&jsonschema.ResolveOptions{})
			if err != nil {
				t.Fatalf("resolve %s schema: %v", tt.schemaName, err)
			}
			if len(schema.OneOf) != tt.oneOfCount {
				t.Errorf("%s schema has %d output shapes, want %d", tt.schemaName, len(schema.OneOf), tt.oneOfCount)
			}
			for _, output := range tt.validOutput {
				data, err := json.Marshal(output)
				if err != nil {
					t.Fatalf("marshal %T: %v", output, err)
				}
				var value any
				if err := json.Unmarshal(data, &value); err != nil {
					t.Fatalf("unmarshal %T: %v", output, err)
				}
				if err := resolved.Validate(value); err != nil {
					t.Errorf("%s schema does not validate %T output: %v", tt.schemaName, output, err)
				}
			}

			expected, err := GenerateNamedOutputSchema(tt.schemaName)
			if err != nil {
				t.Fatalf("generate expected %s schema: %v", tt.schemaName, err)
			}
			if first != string(expected) {
				t.Errorf("json-schema %s output differs from shared generator", tt.schemaName)
			}

			second, secondStderr, err := executeJSONSchemaCommand(tt.schemaName)
			if err != nil {
				t.Fatalf("second json-schema %s invocation failed: %v", tt.schemaName, err)
			}
			if secondStderr != "" {
				t.Fatalf("second json-schema %s invocation wrote to stderr: %q", tt.schemaName, secondStderr)
			}
			if first != second {
				t.Errorf("json-schema %s output is not deterministic", tt.schemaName)
			}
		})
	}
}

func TestJSONSchemaCommandRejectsInvalidArguments(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		args []string
		want string
	}{
		{name: "missing", want: "accepts 1 arg(s), received 0"},
		{name: "unknown", args: []string{"unknown"}, want: "unsupported schema: unknown"},
		{name: "extra", args: []string{"audit", "extra-argument"}, want: "accepts 1 arg(s), received 2"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			stdout, _, err := executeJSONSchemaCommand(tt.args...)
			if err == nil {
				t.Fatal("expected json-schema command to fail")
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error %q does not contain %q", err, tt.want)
			}
			if stdout != "" {
				t.Fatalf("failed command wrote partial output to stdout: %q", stdout)
			}
		})
	}
}

func TestGeneratedOutputSchemasAreCurrent(t *testing.T) {
	t.Parallel()

	for _, schemaName := range []string{"audit", "logs", "logs-jsonl"} {
		t.Run(schemaName, func(t *testing.T) {
			t.Parallel()

			expected, err := GenerateNamedOutputSchema(schemaName)
			if err != nil {
				t.Fatalf("generate %s schema: %v", schemaName, err)
			}
			path := filepath.Join("..", "..", "schemas", schemaName+".schema.json")
			actual, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read generated schema %s: %v", path, err)
			}
			if !bytes.Equal(actual, expected) {
				t.Errorf("%s is stale; run make recompile", path)
			}
		})
	}
}

func executeJSONSchemaCommand(args ...string) (stdout string, stderr string, err error) {
	var stdoutBuffer bytes.Buffer
	var stderrBuffer bytes.Buffer
	cmd := newJSONSchemaCommand(&stdoutBuffer)
	cmd.SetErr(&stderrBuffer)
	cmd.SetArgs(args)
	err = cmd.Execute()
	return stdoutBuffer.String(), stderrBuffer.String(), err
}
