//go:build !integration

package parser

import (
	"strings"
	"testing"
)

func TestValidateWithSchemaAndLocation(t *testing.T) {
	tests := []struct {
		name           string
		frontmatter    map[string]any
		schema         string
		context        string
		filePath       string
		wantErr        bool
		errContains    []string
		errNotContains []string
	}{
		{
			name: "valid data should not error",
			frontmatter: map[string]any{
				"name": "test",
			},
			schema: `{
				"type": "object",
				"properties": {
					"name": {"type": "string"}
				},
				"additionalProperties": false
			}`,
			context:  "test context",
			filePath: "/test/file.md",
			wantErr:  false,
		},
		{
			name: "invalid data should show file location and clean error",
			frontmatter: map[string]any{
				"name":    "test",
				"invalid": "value",
			},
			schema: `{
				"type": "object",
				"properties": {
					"name": {"type": "string"}
				},
				"additionalProperties": false
			}`,
			context:  "test context",
			filePath: "/test/file.md",
			wantErr:  true,
			errContains: []string{
				"/test/file.md:2:1:",
				"Unknown property: invalid",
			},
			errNotContains: []string{
				"contoso.com",
				"example.com",
				"http://",
			},
		},
		{
			name: "schema error without location should still work",
			frontmatter: map[string]any{
				"name":    "test",
				"invalid": "value",
			},
			schema: `{
				"type": "object",
				"properties": {
					"name": {"type": "string"}
				},
				"additionalProperties": false
			}`,
			context:  "test context",
			filePath: "", // No file path
			wantErr:  true,
			errContains: []string{
				"Unknown property: invalid",
			},
			errNotContains: []string{
				"contoso.com",
				"example.com",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateWithSchemaAndLocation(tt.frontmatter, tt.schema, tt.context, tt.filePath)

			if tt.wantErr && err == nil {
				t.Errorf("validateWithSchemaAndLocation() expected error, got nil")
				return
			}

			if !tt.wantErr && err != nil {
				t.Errorf("validateWithSchemaAndLocation() error = %v", err)
				return
			}

			if tt.wantErr && err != nil {
				errorMsg := err.Error()

				// Check that expected strings are present
				for _, expected := range tt.errContains {
					if !strings.Contains(errorMsg, expected) {
						t.Errorf("validateWithSchemaAndLocation() error = %v, expected to contain %v", errorMsg, expected)
					}
				}

				// Check that unwanted strings are not present
				for _, unwanted := range tt.errNotContains {
					if strings.Contains(errorMsg, unwanted) {
						t.Errorf("validateWithSchemaAndLocation() error = %v, should not contain %v", errorMsg, unwanted)
					}
				}
			}
		})
	}
}

func TestSchemaURLDomainChange(t *testing.T) {
	// Test that the schema URL no longer uses example.com
	frontmatter := map[string]any{
		"invalid": "value",
	}

	err := validateWithSchema(frontmatter, `{
		"type": "object",
		"additionalProperties": false
	}`, "test")

	if err == nil {
		t.Fatal("Expected validation error")
	}

	errorMsg := err.Error()

	// Should not contain example.com
	if strings.Contains(errorMsg, "example.com") {
		t.Errorf("Error message should not contain 'example.com', got: %s", errorMsg)
	}

	// Should contain contoso.com (in the basic validation, before cleanup)
	if !strings.Contains(errorMsg, "contoso.com") {
		t.Errorf("Error message should contain 'contoso.com', got: %s", errorMsg)
	}
}

func TestValidateMainWorkflowFrontmatterWithSchemaAndLocation(t *testing.T) {
	tests := []struct {
		name        string
		frontmatter map[string]any
		filePath    string
		wantErr     bool
		errContains string
	}{
		{
			name: "valid workflow frontmatter",
			frontmatter: map[string]any{
				"on":     "push",
				"engine": "claude",
			},
			filePath: "/test/workflow.md",
			wantErr:  false,
		},
		{
			name: "valid pull_request_target ready_for_review trigger",
			frontmatter: map[string]any{
				"on": map[string]any{
					"pull_request_target": map[string]any{
						"types": []any{"ready_for_review"},
					},
				},
				"engine": "claude",
			},
			filePath: "/test/workflow.md",
			wantErr:  false,
		},
		{
			name: "valid secret scanning alerts permission",
			frontmatter: map[string]any{
				"on": "push",
				"permissions": map[string]any{
					"secret-scanning-alerts": "read",
				},
			},
			filePath: "/test/workflow.md",
			wantErr:  false,
		},
		{
			name: "valid max-runs positive integer",
			frontmatter: map[string]any{
				"on":       "push",
				"max-runs": 1,
			},
			filePath: "/test/workflow.md",
			wantErr:  false,
		},
		{
			name: "valid max-runs expression",
			frontmatter: map[string]any{
				"on":       "push",
				"max-runs": "${{ inputs.max-runs }}",
			},
			filePath: "/test/workflow.md",
			wantErr:  false,
		},
		{
			name: "invalid max-runs zero",
			frontmatter: map[string]any{
				"on":       "push",
				"max-runs": 0,
			},
			filePath:    "/test/workflow.md",
			wantErr:     true,
			errContains: "minimum",
		},
		{
			name: "invalid workflow frontmatter with location",
			frontmatter: map[string]any{
				"on":      "push",
				"invalid": "field",
			},
			filePath:    "/test/workflow.md",
			wantErr:     true,
			errContains: "/test/workflow.md:2:1:",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateMainWorkflowFrontmatterWithSchemaAndLocation(tt.frontmatter, tt.filePath)

			if tt.wantErr && err == nil {
				t.Errorf("ValidateMainWorkflowFrontmatterWithSchemaAndLocation() expected error, got nil")
				return
			}

			if !tt.wantErr && err != nil {
				t.Errorf("ValidateMainWorkflowFrontmatterWithSchemaAndLocation() error = %v", err)
				return
			}

			if tt.wantErr && err != nil && tt.errContains != "" {
				if !strings.Contains(err.Error(), tt.errContains) {
					t.Errorf("ValidateMainWorkflowFrontmatterWithSchemaAndLocation() error = %v, expected to contain %v", err, tt.errContains)
				}
			}
		})
	}
}

func TestValidateMainWorkflowFrontmatterWithSchemaAndLocation_AdditionalProperties(t *testing.T) {
	tests := []struct {
		name        string
		frontmatter map[string]any
		filePath    string
		wantErr     bool
		errContains string
	}{
		{
			name: "invalid permissions with additional property shows location",
			frontmatter: map[string]any{
				"on": "push",
				"permissions": map[string]any{
					"contents":     "read",
					"invalid_perm": "write",
				},
			},
			filePath:    "/test/workflow.md",
			wantErr:     true,
			errContains: "/test/workflow.md:2:1:",
		},
		{
			name: "invalid trigger with additional property shows location",
			frontmatter: map[string]any{
				"on": map[string]any{
					"push": map[string]any{
						"branches":     []string{"main"},
						"invalid_prop": "value",
					},
				},
			},
			filePath:    "/test/workflow.md",
			wantErr:     true,
			errContains: "/test/workflow.md:2:1:",
		},
		{
			name: "invalid tools configuration with additional property shows location",
			frontmatter: map[string]any{
				"tools": map[string]any{
					"github": map[string]any{
						"allowed":      []string{"create_issue"},
						"invalid_prop": "value",
					},
				},
			},
			filePath:    "/test/workflow.md",
			wantErr:     true,
			errContains: "/test/workflow.md:2:1:",
		},
		{
			name: "workflow_call input typo now fails with additional property error",
			frontmatter: map[string]any{
				"on": map[string]any{
					"workflow_call": map[string]any{
						"inputs": map[string]any{
							"payload": map[string]any{
								"type":    "string",
								"requird": true,
							},
						},
					},
				},
			},
			filePath:    "/test/workflow.md",
			wantErr:     true,
			errContains: "requird",
		},
		{
			name: "workflow_call secret typo now fails with additional property error",
			frontmatter: map[string]any{
				"on": map[string]any{
					"workflow_call": map[string]any{
						"secrets": map[string]any{
							"token": map[string]any{
								"requird": true,
							},
						},
					},
				},
			},
			filePath:    "/test/workflow.md",
			wantErr:     true,
			errContains: "requird",
		},
		{
			name: "dispatch_repository input typo now fails with additional property error",
			frontmatter: map[string]any{
				"on": "workflow_dispatch",
				"safe-outputs": map[string]any{
					"dispatch_repository": map[string]any{
						"relay": map[string]any{
							"event_type": "dispatch",
							"repository": "github/gh-aw",
							"inputs": map[string]any{
								"payload": map[string]any{
									"type":    "string",
									"requird": true,
								},
							},
						},
					},
				},
			},
			filePath:    "/test/workflow.md",
			wantErr:     true,
			errContains: "requird",
		},
		{
			name: "top-level roles field is rejected pointing to on.roles",
			frontmatter: map[string]any{
				"on":    "push",
				"roles": []string{"admin", "maintainer", "write"},
			},
			filePath:    "/test/workflow.md",
			wantErr:     true,
			errContains: "'roles' belongs under 'on'",
		},
		{
			name: "dispatch-repository key is accepted by schema",
			frontmatter: map[string]any{
				"on": "workflow_dispatch",
				"safe-outputs": map[string]any{
					"dispatch-repository": map[string]any{
						"relay": map[string]any{
							"workflow":   "router.yml",
							"event_type": "dispatch",
							"repository": "github/gh-aw",
						},
					},
				},
			},
			filePath: "/test/workflow.md",
			wantErr:  false,
		},
		{
			name: "valid workflow_call input still compiles",
			frontmatter: map[string]any{
				"on": map[string]any{
					"workflow_call": map[string]any{
						"inputs": map[string]any{
							"payload": map[string]any{
								"type":     "string",
								"required": true,
							},
						},
					},
				},
			},
			filePath: "/test/workflow.md",
			wantErr:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateMainWorkflowFrontmatterWithSchemaAndLocation(tt.frontmatter, tt.filePath)

			if tt.wantErr && err == nil {
				t.Errorf("ValidateMainWorkflowFrontmatterWithSchemaAndLocation() expected error, got nil")
				return
			}

			if !tt.wantErr && err != nil {
				t.Errorf("ValidateMainWorkflowFrontmatterWithSchemaAndLocation() error = %v", err)
				return
			}

			if tt.wantErr && err != nil && tt.errContains != "" {
				if !strings.Contains(err.Error(), tt.errContains) {
					t.Errorf("ValidateMainWorkflowFrontmatterWithSchemaAndLocation() error = %v, expected to contain %v", err, tt.errContains)
				}
			}
		})
	}
}

func TestValidateMainWorkflowFrontmatterWithSchemaAndLocation_AcceptsJobRunsOnObjectForm(t *testing.T) {
	frontmatter := map[string]any{
		"on": "workflow_dispatch",
		"jobs": map[string]any{
			"my-prefetch": map[string]any{
				"runs-on": map[string]any{
					"group": "arc-custom",
				},
				"steps": []any{
					map[string]any{
						"run": "echo hello",
					},
				},
			},
		},
	}

	err := ValidateMainWorkflowFrontmatterWithSchemaAndLocation(frontmatter, "/test/workflow.md")
	if err != nil {
		t.Fatalf("ValidateMainWorkflowFrontmatterWithSchemaAndLocation() unexpected error = %v", err)
	}
}

func TestValidateMainWorkflowFrontmatterWithSchemaAndLocation_AcceptsJobRunsOnStringForm(t *testing.T) {
	frontmatter := map[string]any{
		"on": "workflow_dispatch",
		"jobs": map[string]any{
			"my-prefetch": map[string]any{
				"runs-on": "ubuntu-latest",
				"steps": []any{
					map[string]any{
						"run": "echo hello",
					},
				},
			},
		},
	}

	err := ValidateMainWorkflowFrontmatterWithSchemaAndLocation(frontmatter, "/test/workflow.md")
	if err != nil {
		t.Fatalf("ValidateMainWorkflowFrontmatterWithSchemaAndLocation() unexpected error = %v", err)
	}
}

func TestValidateMainWorkflowFrontmatterWithSchemaAndLocation_AcceptsJobRunsOnArrayForm(t *testing.T) {
	frontmatter := map[string]any{
		"on": "workflow_dispatch",
		"jobs": map[string]any{
			"my-prefetch": map[string]any{
				"runs-on": []any{"self-hosted", "linux"},
				"steps": []any{
					map[string]any{
						"run": "echo hello",
					},
				},
			},
		},
	}

	err := ValidateMainWorkflowFrontmatterWithSchemaAndLocation(frontmatter, "/test/workflow.md")
	if err != nil {
		t.Fatalf("ValidateMainWorkflowFrontmatterWithSchemaAndLocation() unexpected error = %v", err)
	}
}

func TestValidateMainWorkflowFrontmatterWithSchemaAndLocation_AcceptsRunsOnSlimArrayForm(t *testing.T) {
	frontmatter := map[string]any{
		"on":           "workflow_dispatch",
		"runs-on-slim": []any{"self-hosted", "linux"},
	}

	err := ValidateMainWorkflowFrontmatterWithSchemaAndLocation(frontmatter, "/test/workflow.md")
	if err != nil {
		t.Fatalf("ValidateMainWorkflowFrontmatterWithSchemaAndLocation() unexpected error = %v", err)
	}
}

func TestValidateMainWorkflowFrontmatterWithSchemaAndLocation_AcceptsRunsOnSlimObjectForm(t *testing.T) {
	frontmatter := map[string]any{
		"on": "workflow_dispatch",
		"runs-on-slim": map[string]any{
			"group":  "arc-custom",
			"labels": []any{"ubuntu2404", "x64"},
		},
	}

	err := ValidateMainWorkflowFrontmatterWithSchemaAndLocation(frontmatter, "/test/workflow.md")
	if err != nil {
		t.Fatalf("ValidateMainWorkflowFrontmatterWithSchemaAndLocation() unexpected error = %v", err)
	}
}

func TestValidateMainWorkflowFrontmatterWithSchemaAndLocation_AcceptsSafeOutputsRunsOnArrayForm(t *testing.T) {
	frontmatter := map[string]any{
		"on": "workflow_dispatch",
		"safe-outputs": map[string]any{
			"create-issue": map[string]any{},
			"runs-on":      []any{"self-hosted", "linux", "x64"},
		},
	}

	err := ValidateMainWorkflowFrontmatterWithSchemaAndLocation(frontmatter, "/test/workflow.md")
	if err != nil {
		t.Fatalf("ValidateMainWorkflowFrontmatterWithSchemaAndLocation() unexpected error = %v", err)
	}
}

func TestValidateMainWorkflowFrontmatterWithSchemaAndLocation_AcceptsThreatDetectionRunsOnObjectForm(t *testing.T) {
	frontmatter := map[string]any{
		"on": "workflow_dispatch",
		"safe-outputs": map[string]any{
			"create-issue": map[string]any{},
			"threat-detection": map[string]any{
				"runs-on": map[string]any{
					"group":  "arc-custom",
					"labels": []any{"linux", "x64"},
				},
			},
		},
	}

	err := ValidateMainWorkflowFrontmatterWithSchemaAndLocation(frontmatter, "/test/workflow.md")
	if err != nil {
		t.Fatalf("ValidateMainWorkflowFrontmatterWithSchemaAndLocation() unexpected error = %v", err)
	}
}

func TestValidateMainWorkflowFrontmatterWithSchemaAndLocation_AcceptsAllowedBaseBranchesInCreatePullRequest(t *testing.T) {
	frontmatter := map[string]any{
		"on": map[string]any{
			"workflow_dispatch": map[string]any{},
		},
		"permissions": map[string]any{
			"contents":      "read",
			"pull-requests": "read",
		},
		"engine": map[string]any{
			"id":    "copilot",
			"model": "gpt-5.4",
		},
		"network": map[string]any{
			"allowed": []any{"defaults"},
		},
		"tools": map[string]any{
			"edit": map[string]any{},
			"bash": true,
		},
		"safe-outputs": map[string]any{
			"create-pull-request": map[string]any{
				"allowed-base-branches": []any{"main", "release/*"},
			},
		},
	}

	err := ValidateMainWorkflowFrontmatterWithSchemaAndLocation(frontmatter, "/test/workflow.md")
	if err != nil {
		t.Fatalf("expected allowed-base-branches to be accepted under safe-outputs.create-pull-request, got error: %v", err)
	}
}

func TestValidateMainWorkflowFrontmatterWithSchemaAndLocation_AcceptsPatchLimitsInCreatePullRequest(t *testing.T) {
	frontmatter := map[string]any{
		"on": map[string]any{
			"workflow_dispatch": map[string]any{},
		},
		"permissions": map[string]any{
			"contents":      "read",
			"pull-requests": "read",
		},
		"engine": map[string]any{
			"id":    "copilot",
			"model": "gpt-5.4",
		},
		"network": map[string]any{
			"allowed": []any{"defaults"},
		},
		"tools": map[string]any{
			"edit": map[string]any{},
			"bash": true,
		},
		"safe-outputs": map[string]any{
			"create-pull-request": map[string]any{
				"max-patch-size":  2048,
				"max-patch-files": 300,
			},
		},
	}

	err := ValidateMainWorkflowFrontmatterWithSchemaAndLocation(frontmatter, "/test/workflow.md")
	if err != nil {
		t.Fatalf("expected patch limits to be accepted under safe-outputs.create-pull-request, got error: %v", err)
	}
}

func TestValidateMainWorkflowFrontmatterWithSchemaAndLocation_AcceptsPatchSizeInPushToPullRequestBranch(t *testing.T) {
	frontmatter := map[string]any{
		"on": map[string]any{
			"workflow_dispatch": map[string]any{},
		},
		"permissions": map[string]any{
			"contents":      "read",
			"pull-requests": "read",
		},
		"engine": map[string]any{
			"id":    "copilot",
			"model": "gpt-5.4",
		},
		"network": map[string]any{
			"allowed": []any{"defaults"},
		},
		"tools": map[string]any{
			"edit": map[string]any{},
			"bash": true,
		},
		"safe-outputs": map[string]any{
			"push-to-pull-request-branch": map[string]any{
				"max-patch-size": 2048,
			},
		},
	}

	err := ValidateMainWorkflowFrontmatterWithSchemaAndLocation(frontmatter, "/test/workflow.md")
	if err != nil {
		t.Fatalf("expected max-patch-size to be accepted under safe-outputs.push-to-pull-request-branch, got error: %v", err)
	}
}

func TestValidateMainWorkflowFrontmatterWithSchemaAndLocation_RejectsTopLevelCommand(t *testing.T) {
	frontmatter := map[string]any{
		"on":      "push",
		"command": "my-cmd",
	}

	err := ValidateMainWorkflowFrontmatterWithSchemaAndLocation(frontmatter, "/test/workflow.md")
	if err == nil {
		t.Fatal("expected top-level command to be rejected")
	}

	if !strings.Contains(err.Error(), "Unknown property: command") {
		t.Fatalf("expected unknown property error for command, got: %v", err)
	}
}

func TestValidateIncludedFileFrontmatterWithSchemaAndLocation_SkipsCustomAgentFiles(t *testing.T) {
	// Custom agent files may contain Copilot-specific fields that are not in the
	// gh-aw main workflow schema (e.g. user-invokable, disable-model-invocation,
	// tools as an array).  Schema validation must be skipped for these files.
	agentFrontmatter := map[string]any{
		"description":              "My custom agent",
		"user-invokable":           true,
		"disable-model-invocation": false,
	}

	agentPaths := []string{
		"/repo/.github/agents/my-agent.md",
		".github/agents/my-agent.md",
		"/some/path/.github/agents/sub/helper.md",
	}

	for _, path := range agentPaths {
		err := ValidateIncludedFileFrontmatterWithSchemaAndLocation(agentFrontmatter, path)
		if err != nil {
			t.Errorf("expected custom agent file %q to pass validation without errors, got: %v", path, err)
		}
	}
}

func TestValidateIncludedFileFrontmatterWithSchemaAndLocation_DeclarativeEngineWithVersionAndPreAgentSteps(t *testing.T) {
	frontmatter := map[string]any{
		"runtimes": map[string]any{
			"python": map[string]any{"version": "3.12"},
		},
		"pre-agent-steps": []any{
			map[string]any{
				"name": "Install engine",
				"run":  "python3 -m pip install aider-chat",
			},
		},
		"engine": map[string]any{
			"id":           "aider",
			"version":      "0.86.2",
			"display-name": "Aider",
			"description":  "Aider CLI",
			"experimental": true,
			"provider": map[string]any{
				"name": "github",
			},
			"behaviors": map[string]any{
				"execution": map[string]any{
					"command-name": "aider",
					"step-name":    "Execute Aider CLI",
				},
			},
		},
	}

	err := ValidateIncludedFileFrontmatterWithSchemaAndLocation(frontmatter, "/repo/.github/workflows/shared/aider.md")
	if err != nil {
		t.Fatalf("expected declarative engine with version and pre-agent-steps to pass validation, got: %v", err)
	}
}

func TestValidateIncludedFileFrontmatterWithSchemaAndLocation_ConcurrencyJobDiscriminator(t *testing.T) {
	t.Run("allows job discriminator", func(t *testing.T) {
		err := ValidateIncludedFileFrontmatterWithSchemaAndLocation(map[string]any{
			"concurrency": map[string]any{
				"job-discriminator": "${{ github.run_id }}",
			},
		}, "/repo/.github/workflows/shared/concurrency.md")
		if err != nil {
			t.Fatalf("expected concurrency.job-discriminator to be allowed, got: %v", err)
		}
	})

	t.Run("rejects unsupported workflow concurrency fields", func(t *testing.T) {
		err := ValidateIncludedFileFrontmatterWithSchemaAndLocation(map[string]any{
			"concurrency": map[string]any{
				"group":              "shared-group",
				"cancel-in-progress": true,
			},
		}, "/repo/.github/workflows/shared/concurrency.md")
		if err == nil || !strings.Contains(err.Error(), "unsupported key: cancel-in-progress") {
			t.Fatalf("expected unsupported concurrency field error, got: %v", err)
		}
	})
}

func TestValidateMainWorkflowFrontmatterWithSchemaAndLocation_MaxStack(t *testing.T) {
	tests := []struct {
		name        string
		maxStack    any
		wantErr     bool
		errContains string
	}{
		{
			name:     "max-stack: 1 is valid (default)",
			maxStack: 1,
			wantErr:  false,
		},
		{
			name:     "max-stack: 2 is valid",
			maxStack: 2,
			wantErr:  false,
		},
		{
			name:     "max-stack: 5 is valid (intermediate value)",
			maxStack: 5,
			wantErr:  false,
		},
		{
			name:     "max-stack: -1 is valid (disable stack protection)",
			maxStack: -1,
			wantErr:  false,
		},
		{
			name:        "max-stack: 0 is rejected",
			maxStack:    0,
			wantErr:     true,
			errContains: "max-stack",
		},
		{
			name:        "max-stack: -2 is rejected",
			maxStack:    -2,
			wantErr:     true,
			errContains: "max-stack",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			frontmatter := map[string]any{
				"on": map[string]any{
					"pull_request": map[string]any{
						"types":     []any{"opened"},
						"max-stack": tt.maxStack,
					},
				},
			}
			err := ValidateMainWorkflowFrontmatterWithSchemaAndLocation(frontmatter, "/test/workflow.md")
			if tt.wantErr && err == nil {
				t.Errorf("expected validation error for max-stack: %v, got nil", tt.maxStack)
				return
			}
			if !tt.wantErr && err != nil {
				t.Errorf("unexpected validation error for max-stack: %v: %v", tt.maxStack, err)
				return
			}
			if tt.wantErr && err != nil && tt.errContains != "" {
				if !strings.Contains(err.Error(), tt.errContains) {
					t.Errorf("expected error containing %q, got: %v", tt.errContains, err)
				}
			}
		})
	}
}

func TestValidateMainWorkflowFrontmatterWithSchemaAndLocation_PullRequestReviewMaxStack(t *testing.T) {
	tests := []struct {
		name        string
		maxStack    any
		wantErr     bool
		errContains string
	}{
		{
			name:     "max-stack: 1 is valid (default)",
			maxStack: 1,
			wantErr:  false,
		},
		{
			name:     "max-stack: 2 is valid",
			maxStack: 2,
			wantErr:  false,
		},
		{
			name:     "max-stack: 5 is valid (intermediate value)",
			maxStack: 5,
			wantErr:  false,
		},
		{
			name:     "max-stack: -1 is valid (disable stack protection)",
			maxStack: -1,
			wantErr:  false,
		},
		{
			name:        "max-stack: 0 is rejected",
			maxStack:    0,
			wantErr:     true,
			errContains: "max-stack",
		},
		{
			name:        "max-stack: -2 is rejected",
			maxStack:    -2,
			wantErr:     true,
			errContains: "max-stack",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			frontmatter := map[string]any{
				"on": map[string]any{
					"pull_request_review": map[string]any{
						"types":     []any{"submitted"},
						"max-stack": tt.maxStack,
					},
				},
			}
			err := ValidateMainWorkflowFrontmatterWithSchemaAndLocation(frontmatter, "/test/workflow.md")
			if tt.wantErr && err == nil {
				t.Errorf("expected validation error for max-stack: %v, got nil", tt.maxStack)
				return
			}
			if !tt.wantErr && err != nil {
				t.Errorf("unexpected validation error for max-stack: %v: %v", tt.maxStack, err)
				return
			}
			if tt.wantErr && err != nil && tt.errContains != "" {
				if !strings.Contains(err.Error(), tt.errContains) {
					t.Errorf("expected error containing %q, got: %v", tt.errContains, err)
				}
			}
		})
	}
}
