//go:build !integration

package workflow

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/testutil"
	"github.com/goccy/go-yaml"
)

func TestUpdateIssueArtifactLimits(t *testing.T) {
	for _, tc := range []struct {
		name   string
		config string
		count  int
		size   int
	}{
		{"default", "", 10, 1048576},
		{"positive", "max-issue-artifacts: 2", 2, 1048576},
		{"zero", "max-issue-artifacts: 0", 0, 1048576},
		{"negative", "max-issue-artifacts: -2", -2, 1048576},
		{"smaller-size", "max-issue-artifact-size: 1024", 10, 1024},
		{"larger-size", "max-issue-artifact-size: 2097152", 10, 2097152},
		{"human-default-size", "max-issue-artifact-size: 1mb", 10, 1048576},
		{"human-small-size", "max-issue-artifact-size: 512kb", 10, 524288},
		{"human-fractional-size", "max-issue-artifact-size: 1.5mb", 10, 1572864},
		{"human-spaced-size", "max-issue-artifact-size: ' 2 MiB '", 10, 2097152},
		{"human-short-unit", "max-issue-artifact-size: 1m", 10, 1048576},
		{"quoted-byte-count", "max-issue-artifact-size: '1024'", 10, 1024},
		{"both-limits", "max-issue-artifacts: 3\n    max-issue-artifact-size: 512kb", 3, 524288},
		{"body-disabled", "body: false\n    max-issue-artifacts: 2", 2, 1048576},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var outputMap map[string]any
			if err := yaml.Unmarshal([]byte("update-issue:\n    "+tc.config+"\n"), &outputMap); err != nil {
				t.Fatal(err)
			}
			cfg := NewCompiler().parseUpdateIssuesConfig(outputMap)
			if cfg == nil || cfg.artifactSizeParseError != nil {
				t.Fatalf("Expected valid update-issue configuration, got %+v", cfg)
			}
			if cfg.maxIssueArtifacts() != tc.count || cfg.maxIssueArtifactSize() != tc.size {
				t.Fatalf("Parsed limits = %d, %d; want %d, %d", cfg.maxIssueArtifacts(), cfg.maxIssueArtifactSize(), tc.count, tc.size)
			}

			dir := testutil.TempDir(t, "update-artifact-limits")
			path := filepath.Join(dir, "limits.md")
			source := fmt.Sprintf("---\non: workflow_dispatch\nengine: copilot\npermissions:\n  contents: read\nsafe-outputs:\n  update-issue:\n    %s\n---\nUpdate an issue with an artifact.\n", tc.config)
			if err := os.WriteFile(path, []byte(source), 0600); err != nil {
				t.Fatal(err)
			}
			if err := NewCompiler().CompileWorkflow(path); err != nil {
				t.Fatal(err)
			}
			content, err := os.ReadFile(filepath.Join(dir, "limits.lock.yml"))
			if err != nil {
				t.Fatal(err)
			}
			var handlerConfig map[string]map[string]any
			for line := range strings.SplitSeq(string(content), "\n") {
				if _, value, found := strings.Cut(line, "GH_AW_SAFE_OUTPUTS_HANDLER_CONFIG:"); found {
					value = strings.TrimSpace(value)
					if strings.HasPrefix(value, `"`) {
						value, err = strconv.Unquote(value)
						if err != nil {
							t.Fatal(err)
						}
					}
					if err := json.Unmarshal([]byte(value), &handlerConfig); err != nil {
						t.Fatal(err)
					}
					break
				}
			}
			handler := handlerConfig["update_issue"]
			if handler["max_issue_artifacts"] != float64(tc.count) || handler["max_issue_artifact_size"] != float64(tc.size) {
				t.Fatalf("Unexpected compiled update_issue limits: %v", handler)
			}
			if tc.name == "body-disabled" && handler["allow_body"] != false {
				t.Fatalf("Artifact configuration changed body permission: %v", handler)
			}
			constraint := "Issue artifacts are disabled; omit artifacts."
			if tc.count > 0 {
				constraint = fmt.Sprintf("Maximum %d artifact(s) can be attached per issue.", tc.count)
				if !strings.Contains(string(content), fmt.Sprintf("Maximum artifact body size: %d UTF-8 bytes.", tc.size)) {
					t.Error("Compiled tool description is missing artifact size limit")
				}
			}
			if !strings.Contains(string(content), constraint) {
				t.Errorf("Compiled tool description is missing %q", constraint)
			}
		})
	}
}

func TestUpdateIssueArtifactLimitsRejectInvalid(t *testing.T) {
	for _, tc := range []struct {
		config string
		field  string
	}{
		{"max-isssue-artifacts: 3", "max-isssue-artifacts"},
		{"max-issue-artifacts: 3\n    max-isssue-artifacts: 4", "max-isssue-artifacts"},
		{"max-issue-artifacts: 1.5", "max-issue-artifacts"},
		{"max-issue-artifacts: '2'", "max-issue-artifacts"},
		{"max-issue-artifact-size: 0", "max-issue-artifact-size"},
		{"max-issue-artifact-size: -1", "max-issue-artifact-size"},
		{"max-issue-artifact-size: 1.5", "max-issue-artifact-size"},
		{"max-issue-artifact-size: 9007199254740992", "max-issue-artifact-size"},
		{"max-issue-artifact-size: 0mb", "max-issue-artifact-size"},
		{"max-issue-artifact-size: -1mb", "max-issue-artifact-size"},
		{"max-issue-artifact-size: 0.1kb", "max-issue-artifact-size"},
		{"max-issue-artifact-size: 0.9b", "max-issue-artifact-size"},
		{"max-issue-artifact-size: mb", "max-issue-artifact-size"},
		{"max-issue-artifact-size: 1xb", "max-issue-artifact-size"},
		{"max-issue-artifact-size: 1e3mb", "max-issue-artifact-size"},
		{"max-issue-artifact-size: NaN", "max-issue-artifact-size"},
		{"max-issue-artifact-size: Inf", "max-issue-artifact-size"},
		{"max-issue-artifact-size: 9007199254740992b", "max-issue-artifact-size"},
		{"max-issue-artifact-size: 99999999999999999999999999999999999999999999999999999999999999999999mb", "max-issue-artifact-size"},
	} {
		t.Run(tc.config, func(t *testing.T) {
			dir := testutil.TempDir(t, "invalid-update-artifact-limits")
			path := filepath.Join(dir, "invalid.md")
			source := fmt.Sprintf("---\non: workflow_dispatch\nengine: copilot\nsafe-outputs:\n  update-issue:\n    %s\n---\nUpdate an issue.\n", tc.config)
			if err := os.WriteFile(path, []byte(source), 0600); err != nil {
				t.Fatal(err)
			}
			if err := NewCompiler().CompileWorkflow(path); err == nil || !strings.Contains(err.Error(), tc.field) {
				t.Fatalf("Expected explicit %s error, got %v", tc.field, err)
			}
			if _, err := os.Stat(filepath.Join(dir, "invalid.lock.yml")); !os.IsNotExist(err) {
				t.Fatal("Invalid configuration must not publish a lock file")
			}
		})
	}
}

func TestUpdateIssueArtifactSizeParseError(t *testing.T) {
	cfg := NewCompiler().parseUpdateIssuesConfig(map[string]any{
		"update-issue": map[string]any{"max-issue-artifact-size": "0.1kb"},
	})
	if cfg == nil || cfg.artifactSizeParseError == nil {
		t.Fatal("Invalid human size must retain a parse error")
	}
	err := validateSafeOutputsMax(&SafeOutputsConfig{UpdateIssues: cfg})
	if err == nil || !strings.Contains(err.Error(), "safe-outputs.update-issue.max-issue-artifact-size") {
		t.Fatalf("Expected qualified artifact-size validation error, got %v", err)
	}
}

func TestUpdateIssueArtifactOnlyToolEnabled(t *testing.T) {
	cfg := NewCompiler().parseUpdateIssuesConfig(map[string]any{
		"update-issue": map[string]any{
			"body":                false,
			"max-issue-artifacts": 2,
		},
	})
	if cfg == nil || cfg.Title != nil || cfg.Status != nil || cfg.Body == nil || *cfg.Body {
		t.Fatalf("Expected title/status/body updates disabled, got %+v", cfg)
	}
	data := &WorkflowData{SafeOutputs: &SafeOutputsConfig{UpdateIssues: cfg}}
	if _, enabled := computeEnabledToolNames(data)["update_issue"]; !enabled {
		t.Fatal("Artifact-only update_issue tool must remain enabled")
	}
	metaJSON, err := generateToolsMetaJSON(data, "")
	if err != nil {
		t.Fatal(err)
	}
	var meta ToolsMeta
	if err := json.Unmarshal([]byte(metaJSON), &meta); err != nil {
		t.Fatal(err)
	}
	if len(meta.RequiredFieldRemovals["update_issue"]) != 0 || len(meta.RequiredFieldAdditions["update_issue"]) != 0 {
		t.Fatalf("Artifact-only tool must not require title/body/status: %+v", meta)
	}
	var tools []struct {
		Name        string `json:"name"`
		InputSchema struct {
			Properties map[string]any `json:"properties"`
			Required   []string       `json:"required"`
		} `json:"inputSchema"`
	}
	if err := json.Unmarshal([]byte(safeOutputsToolsJSONContent), &tools); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, tool := range tools {
		if tool.Name != "update_issue" {
			continue
		}
		found = true
		if _, ok := tool.InputSchema.Properties["artifacts"]; !ok {
			t.Fatal("update_issue tool schema must expose artifacts")
		}
		for _, field := range tool.InputSchema.Required {
			if field == "title" || field == "body" || field == "status" {
				t.Fatalf("Artifact-only update must not require %s", field)
			}
		}
	}
	if !found {
		t.Fatal("Missing update_issue tool schema")
	}
	configJSON, err := GetValidationConfigJSONWithDataSchema([]string{"update_issue"}, nil, false, nil)
	if err != nil {
		t.Fatal(err)
	}
	var configs map[string]TypeValidationConfig
	if err := json.Unmarshal([]byte(configJSON), &configs); err != nil {
		t.Fatal(err)
	}
	validation := configs["update_issue"]
	if !strings.HasSuffix(validation.CustomValidation, ",artifacts") || validation.Fields["artifacts"].Type != "array" {
		t.Fatalf("Generated validation must accept an artifact-only update: %+v", validation)
	}
}
