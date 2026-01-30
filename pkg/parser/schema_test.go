//go:build !integration

package parser

import (
	"os"
	"strings"
	"testing"
)

func TestValidateMainWorkflowFrontmatterWithSchema(t *testing.T) {
	tests := []struct {
		name        string
		frontmatter map[string]any
		wantErr     bool
		errContains string
	}{
		{
			name: "valid frontmatter with all allowed keys",
			frontmatter: map[string]any{
				"on": map[string]any{
					"push": map[string]any{
						"branches": []string{"main"},
					},
					"stop-after": "2024-12-31",
				},
				"permissions":     "read-all",
				"run-name":        "Test Run",
				"runs-on":         "ubuntu-latest",
				"timeout-minutes": 30,
				"concurrency":     "test",
				"env":             map[string]string{"TEST": "value"},
				"if":              "true",
				"steps":           []string{"step1"},
				"engine":          "claude",
				"tools":           map[string]any{"github": "test"},
				"command":         "test-workflow",
			},
			wantErr: false,
		},
		{
			name: "valid frontmatter with subset of keys",
			frontmatter: map[string]any{
				"on":     "push",
				"engine": "claude",
			},
			wantErr: false,
		},
		{
			name:        "empty frontmatter - missing required 'on' field",
			frontmatter: map[string]any{},
			wantErr:     true,
			errContains: "missing property 'on'",
		},
		{
			name: "valid engine string format - claude",
			frontmatter: map[string]any{
				"on":     "push",
				"engine": "claude",
			},
			wantErr: false,
		},
		{
			name: "valid engine string format - codex",
			frontmatter: map[string]any{
				"on":     "push",
				"engine": "codex",
			},
			wantErr: false,
		},
		{
			name: "valid engine object format - minimal",
			frontmatter: map[string]any{
				"on": "push",
				"engine": map[string]any{
					"id": "claude",
				},
			},
			wantErr: false,
		},
		{
			name: "valid engine object format - with version",
			frontmatter: map[string]any{
				"on": "push",
				"engine": map[string]any{
					"id":      "claude",
					"version": "beta",
				},
			},
			wantErr: false,
		},
		{
			name: "valid engine object format - with model",
			frontmatter: map[string]any{
				"on": "push",
				"engine": map[string]any{
					"id":    "codex",
					"model": "gpt-4o",
				},
			},
			wantErr: false,
		},
		{
			name: "valid engine object format - complete",
			frontmatter: map[string]any{
				"on": "push",
				"engine": map[string]any{
					"id":      "claude",
					"version": "beta",
					"model":   "claude-3-5-sonnet-20241022",
				},
			},
			wantErr: false,
		},
		{
			name: "invalid engine string format",
			frontmatter: map[string]any{
				"on":     "push",
				"engine": "invalid-engine",
			},
			wantErr:     true,
			errContains: "value must be one of 'claude', 'codex'",
		},
		{
			name: "invalid engine object format - invalid id",
			frontmatter: map[string]any{
				"on": "push",
				"engine": map[string]any{
					"id": "invalid-engine",
				},
			},
			wantErr:     true,
			errContains: "value must be one of 'claude', 'codex'",
		},
		{
			name: "invalid engine object format - missing id",
			frontmatter: map[string]any{
				"on": "push",
				"engine": map[string]any{
					"version": "beta",
					"model":   "gpt-4o",
				},
			},
			wantErr:     true,
			errContains: "missing property 'id'",
		},
		{
			name: "invalid engine object format - additional properties",
			frontmatter: map[string]any{
				"on": "push",
				"engine": map[string]any{
					"id":      "claude",
					"invalid": "property",
				},
			},
			wantErr:     true,
			errContains: "additional properties",
		},
		{
			name: "invalid frontmatter with unexpected key",
			frontmatter: map[string]any{
				"on":          "push",
				"invalid_key": "value",
			},
			wantErr:     true,
			errContains: "additional properties 'invalid_key' not allowed",
		},
		{
			name: "invalid frontmatter with multiple unexpected keys",
			frontmatter: map[string]any{
				"on":              "push",
				"invalid_key":     "value",
				"another_invalid": "value2",
			},
			wantErr:     true,
			errContains: "additional properties",
		},
		{
			name: "valid frontmatter with complex on object",
			frontmatter: map[string]any{
				"on": map[string]any{
					"schedule": []map[string]any{
						{"cron": "0 9 * * *"},
					},
					"workflow_dispatch": map[string]any{},
				},
				"engine": "claude",
			},
			wantErr: false,
		},
		{
			name: "valid frontmatter with command trigger",
			frontmatter: map[string]any{
				"on": map[string]any{
					"command": map[string]any{
						"name": "test-command",
					},
				},
				"permissions": map[string]any{
					"issues":   "write",
					"contents": "read",
				},
			},
			wantErr: false,
		},
		{
			name: "valid frontmatter with discussion trigger",
			frontmatter: map[string]any{
				"on": map[string]any{
					"discussion": map[string]any{
						"types": []string{"created", "edited", "answered"},
					},
				},
				"permissions": "read-all",
			},
			wantErr: false,
		},
		{
			name: "valid frontmatter with discussion_comment trigger",
			frontmatter: map[string]any{
				"on": map[string]any{
					"discussion_comment": map[string]any{
						"types": []string{"created", "edited"},
					},
				},
				"permissions": "read-all",
			},
			wantErr: false,
		},
		{
			name: "valid frontmatter with simple discussion trigger",
			frontmatter: map[string]any{
				"on":     "discussion",
				"engine": "claude",
			},
			wantErr: false,
		},
		{
			name: "valid frontmatter with branch_protection_rule trigger",
			frontmatter: map[string]any{
				"on": map[string]any{
					"branch_protection_rule": map[string]any{
						"types": []string{"created", "deleted"},
					},
				},
				"permissions": "read-all",
			},
			wantErr: false,
		},
		{
			name: "valid frontmatter with check_run trigger",
			frontmatter: map[string]any{
				"on": map[string]any{
					"check_run": map[string]any{
						"types": []string{"completed", "rerequested"},
					},
				},
				"permissions": "read-all",
			},
			wantErr: false,
		},
		{
			name: "valid frontmatter with check_suite trigger",
			frontmatter: map[string]any{
				"on": map[string]any{
					"check_suite": map[string]any{
						"types": []string{"completed"},
					},
				},
				"permissions": "read-all",
			},
			wantErr: false,
		},
		{
			name: "valid frontmatter with simple create trigger",
			frontmatter: map[string]any{
				"on":     "create",
				"engine": "claude",
			},
			wantErr: false,
		},
		{
			name: "valid frontmatter with simple delete trigger",
			frontmatter: map[string]any{
				"on":     "delete",
				"engine": "claude",
			},
			wantErr: false,
		},
		{
			name: "valid frontmatter with simple fork trigger",
			frontmatter: map[string]any{
				"on":     "fork",
				"engine": "claude",
			},
			wantErr: false,
		},
		{
			name: "valid frontmatter with simple gollum trigger",
			frontmatter: map[string]any{
				"on":     "gollum",
				"engine": "claude",
			},
			wantErr: false,
		},
		{
			name: "valid frontmatter with label trigger",
			frontmatter: map[string]any{
				"on": map[string]any{
					"label": map[string]any{
						"types": []string{"created", "deleted"},
					},
				},
				"permissions": "read-all",
			},
			wantErr: false,
		},
		{
			name: "valid frontmatter with merge_group trigger",
			frontmatter: map[string]any{
				"on": map[string]any{
					"merge_group": map[string]any{
						"types": []string{"checks_requested"},
					},
				},
				"permissions": "read-all",
			},
			wantErr: false,
		},
		{
			name: "valid frontmatter with milestone trigger",
			frontmatter: map[string]any{
				"on": map[string]any{
					"milestone": map[string]any{
						"types": []string{"opened", "closed"},
					},
				},
				"permissions": "read-all",
			},
			wantErr: false,
		},
		{
			name: "valid frontmatter with simple page_build trigger",
			frontmatter: map[string]any{
				"on":     "page_build",
				"engine": "claude",
			},
			wantErr: false,
		},
		{
			name: "valid frontmatter with simple public trigger",
			frontmatter: map[string]any{
				"on":     "public",
				"engine": "claude",
			},
			wantErr: false,
		},
		{
			name: "valid frontmatter with pull_request_target trigger",
			frontmatter: map[string]any{
				"on": map[string]any{
					"pull_request_target": map[string]any{
						"types":    []string{"opened", "synchronize"},
						"branches": []string{"main"},
					},
				},
				"permissions": "read-all",
			},
			wantErr: false,
		},
		{
			name: "valid frontmatter with pull_request_review trigger",
			frontmatter: map[string]any{
				"on": map[string]any{
					"pull_request_review": map[string]any{
						"types": []string{"submitted", "edited"},
					},
				},
				"permissions": "read-all",
			},
			wantErr: false,
		},
		{
			name: "valid frontmatter with registry_package trigger",
			frontmatter: map[string]any{
				"on": map[string]any{
					"registry_package": map[string]any{
						"types": []string{"published", "updated"},
					},
				},
				"permissions": "read-all",
			},
			wantErr: false,
		},
		{
			name: "valid frontmatter with repository_dispatch trigger",
			frontmatter: map[string]any{
				"on": map[string]any{
					"repository_dispatch": map[string]any{
						"types": []string{"custom-event", "deploy"},
					},
				},
				"permissions": "read-all",
			},
			wantErr: false,
		},
		{
			name: "valid frontmatter with simple status trigger",
			frontmatter: map[string]any{
				"on":     "status",
				"engine": "claude",
			},
			wantErr: false,
		},
		{
			name: "valid frontmatter with watch trigger",
			frontmatter: map[string]any{
				"on": map[string]any{
					"watch": map[string]any{
						"types": []string{"started"},
					},
				},
				"permissions": "read-all",
			},
			wantErr: false,
		},
		{
			name: "valid frontmatter with simple workflow_call trigger",
			frontmatter: map[string]any{
				"on":     "workflow_call",
				"engine": "claude",
			},
			wantErr: false,
		},
		{
			name: "valid frontmatter with updated issues trigger types",
			frontmatter: map[string]any{
				"on": map[string]any{
					"issues": map[string]any{
						"types": []string{"opened", "typed", "untyped"},
					},
				},
				"permissions": "read-all",
			},
			wantErr: false,
		},
		{
			name: "valid frontmatter with issues trigger lock-for-agent field",
			frontmatter: map[string]any{
				"on": map[string]any{
					"issues": map[string]any{
						"types":          []string{"opened"},
						"lock-for-agent": true,
					},
				},
				"permissions": "read-all",
			},
			wantErr: false,
		},
		{
			name: "valid frontmatter with issues trigger lock-for-agent false",
			frontmatter: map[string]any{
				"on": map[string]any{
					"issues": map[string]any{
						"types":          []string{"opened"},
						"lock-for-agent": false,
					},
				},
				"permissions": "read-all",
			},
			wantErr: false,
		},
		{
			name: "valid frontmatter with issue_comment trigger lock-for-agent field",
			frontmatter: map[string]any{
				"on": map[string]any{
					"issue_comment": map[string]any{
						"types":          []string{"created"},
						"lock-for-agent": true,
					},
				},
				"permissions": "read-all",
			},
			wantErr: false,
		},
		{
			name: "valid frontmatter with issue_comment trigger lock-for-agent false",
			frontmatter: map[string]any{
				"on": map[string]any{
					"issue_comment": map[string]any{
						"types":          []string{"created"},
						"lock-for-agent": false,
					},
				},
				"permissions": "read-all",
			},
			wantErr: false,
		},
		{
			name: "valid frontmatter with updated pull_request trigger types",
			frontmatter: map[string]any{
				"on": map[string]any{
					"pull_request": map[string]any{
						"types": []string{"opened", "milestoned", "demilestoned", "ready_for_review", "auto_merge_enabled"},
					},
				},
				"permissions": "read-all",
			},
			wantErr: false,
		},
		{
			name: "valid frontmatter with detailed permissions",
			frontmatter: map[string]any{
				"on": "push",
				"permissions": map[string]any{
					"contents":      "read",
					"issues":        "write",
					"pull-requests": "read",
					"models":        "read",
				},
			},
			wantErr: false,
		},
		{
			name: "valid frontmatter with single cache configuration",
			frontmatter: map[string]any{
				"on": "push",
				"cache": map[string]any{
					"key":          "node-modules-${{ hashFiles('package-lock.json') }}",
					"path":         "node_modules",
					"restore-keys": []string{"node-modules-"},
				},
			},
			wantErr: false,
		},
		{
			name: "valid frontmatter with multiple cache configurations",
			frontmatter: map[string]any{
				"on": "push",
				"cache": []any{
					map[string]any{
						"key":  "cache1",
						"path": "path1",
					},
					map[string]any{
						"key":                "cache2",
						"path":               []string{"path2", "path3"},
						"restore-keys":       "restore-key",
						"fail-on-cache-miss": true,
					},
				},
			},
			wantErr: false,
		},
		{
			name: "invalid cache configuration missing required key",
			frontmatter: map[string]any{
				"cache": map[string]any{
					"path": "node_modules",
				},
			},
			wantErr:     true,
			errContains: "missing property 'key'",
		},
		// Test cases for additional properties validation
		{
			name: "invalid permissions with additional property",
			frontmatter: map[string]any{
				"on": "push",
				"permissions": map[string]any{
					"contents":     "read",
					"invalid_perm": "write",
				},
			},
			wantErr:     true,
			errContains: "additional properties 'invalid_perm' not allowed",
		},
		{
			name: "invalid on trigger with additional properties",
			frontmatter: map[string]any{
				"on": map[string]any{
					"push": map[string]any{
						"branches":     []string{"main"},
						"invalid_prop": "value",
					},
				},
			},
			wantErr:     true,
			errContains: "additional properties 'invalid_prop' not allowed",
		},
		{
			name: "invalid schedule with additional properties",
			frontmatter: map[string]any{
				"on": map[string]any{
					"schedule": []map[string]any{
						{
							"cron":         "0 9 * * *",
							"invalid_prop": "value",
						},
					},
				},
			},
			wantErr:     true,
			errContains: "additional properties 'invalid_prop' not allowed",
		},
		{
			name: "invalid empty schedule array",
			frontmatter: map[string]any{
				"on": map[string]any{
					"schedule": []map[string]any{},
				},
			},
			wantErr:     true,
			errContains: "minItems: got 0, want 1",
		},
		{
			name: "invalid empty toolsets array",
			frontmatter: map[string]any{
				"on": "push",
				"tools": map[string]any{
					"github": map[string]any{
						"toolsets": []string{},
					},
				},
			},
			wantErr:     true,
			errContains: "minItems",
		},
		{
			name: "invalid empty issue names array",
			frontmatter: map[string]any{
				"on": map[string]any{
					"issues": map[string]any{
						"types": []string{"labeled"},
						"names": []string{},
					},
				},
			},
			wantErr:     true,
			errContains: "minItems",
		},
		{
			name: "invalid empty pull_request names array",
			frontmatter: map[string]any{
				"on": map[string]any{
					"pull_request": map[string]any{
						"types": []string{"labeled"},
						"names": []string{},
					},
				},
			},
			wantErr:     true,
			errContains: "minItems",
		},
		{
			name: "valid schedule with multiple cron entries",
			frontmatter: map[string]any{
				"on": map[string]any{
					"schedule": []map[string]any{
						{"cron": "0 9 * * *"},
						{"cron": "0 17 * * *"},
					},
				},
				"engine": "claude",
			},
			wantErr: false,
		},
		{
			name: "invalid workflow_dispatch with additional properties",
			frontmatter: map[string]any{
				"on": map[string]any{
					"workflow_dispatch": map[string]any{
						"inputs": map[string]any{
							"test_input": map[string]any{
								"description": "Test input",
								"type":        "string",
							},
						},
						"invalid_prop": "value",
					},
				},
			},
			wantErr:     true,
			errContains: "additional properties 'invalid_prop' not allowed",
		},
		{
			name: "invalid concurrency with additional properties",
			frontmatter: map[string]any{
				"concurrency": map[string]any{
					"group":              "test-group",
					"cancel-in-progress": true,
					"invalid_prop":       "value",
				},
			},
			wantErr:     true,
			errContains: "additional properties 'invalid_prop' not allowed",
		},
		{
			name: "invalid runs-on object with additional properties",
			frontmatter: map[string]any{
				"runs-on": map[string]any{
					"group":        "test-group",
					"labels":       []string{"ubuntu-latest"},
					"invalid_prop": "value",
				},
			},
			wantErr:     true,
			errContains: "additional properties 'invalid_prop' not allowed",
		},
		{
			name: "invalid github tools with additional properties",
			frontmatter: map[string]any{
				"tools": map[string]any{
					"github": map[string]any{
						"allowed":      []string{"create_issue"},
						"invalid_prop": "value",
					},
				},
			},
			wantErr:     true,
			errContains: "additional properties 'invalid_prop' not allowed",
		},
		{
			name: "invalid claude top-level field (deprecated)",
			frontmatter: map[string]any{
				"claude": map[string]any{
					"model": "claude-3",
				},
			},
			wantErr:     true,
			errContains: "additional properties 'claude' not allowed",
		},
		{
			name: "invalid safe-outputs configuration with additional properties",
			frontmatter: map[string]any{
				"safe-outputs": map[string]any{
					"create-issue": map[string]any{
						"title-prefix": "[ai] ",
						"invalid_prop": "value",
					},
				},
			},
			wantErr:     true,
			errContains: "additional properties 'invalid_prop' not allowed",
		},
		{
			name: "invalid permissions with unsupported repository-projects property",
			frontmatter: map[string]any{
				"on": "push",
				"permissions": map[string]any{
					"contents":            "read",
					"attestations":        "write",
					"id-token":            "write",
					"packages":            "read",
					"pages":               "write",
					"repository-projects": "none",
				},
			},
			wantErr: true,
		},
		{
			name: "valid claude engine with network permissions",
			frontmatter: map[string]any{
				"on": "push",
				"engine": map[string]any{
					"id": "claude",
				},
			},
			wantErr: false,
		},
		{
			name: "valid codex engine without permissions",
			frontmatter: map[string]any{
				"on": "push",
				"engine": map[string]any{
					"id":    "codex",
					"model": "gpt-4o",
				},
			},
			wantErr: false,
		},
		{
			name: "valid codex string engine (no permissions possible)",
			frontmatter: map[string]any{
				"on":     "push",
				"engine": "codex",
			},
			wantErr: false,
		},
		{
			name: "valid network defaults",
			frontmatter: map[string]any{
				"on":      "push",
				"network": "defaults",
			},
			wantErr: false,
		},
		{
			name: "valid network empty object",
			frontmatter: map[string]any{
				"on":      "push",
				"network": map[string]any{},
			},
			wantErr: false,
		},
		{
			name: "valid network with allowed domains",
			frontmatter: map[string]any{
				"on": "push",
				"network": map[string]any{
					"allowed": []string{"example.com", "*.trusted.com"},
				},
			},
			wantErr: false,
		},
		{
			name: "invalid network string (not defaults)",
			frontmatter: map[string]any{
				"on":      "push",
				"network": "invalid",
			},
			wantErr:     true,
			errContains: "oneOf",
		},
		{
			name: "invalid network object with unknown property",
			frontmatter: map[string]any{
				"on": "push",
				"network": map[string]any{
					"invalid": []string{"example.com"},
				},
			},
			wantErr:     true,
			errContains: "additional properties 'invalid' not allowed",
		},
		{
			name: "missing required on field",
			frontmatter: map[string]any{
				"engine": "claude",
				"permissions": map[string]any{
					"contents": "read",
				},
			},
			wantErr:     true,
			errContains: "missing property 'on'",
		},
		{
			name: "missing required on field with other valid fields",
			frontmatter: map[string]any{
				"engine":          "copilot",
				"timeout-minutes": 30,
				"permissions": map[string]any{
					"issues": "write",
				},
			},
			wantErr:     true,
			errContains: "missing property 'on'",
		},
		{
			name: "invalid: command trigger with issues event",
			frontmatter: map[string]any{
				"on": map[string]any{
					"command": map[string]any{
						"name": "test-bot",
					},
					"issues": map[string]any{
						"types": []string{"opened"},
					},
				},
			},
			wantErr:     true,
			errContains: "command trigger cannot be used with 'issues' event",
		},
		{
			name: "invalid: command trigger with issue_comment event",
			frontmatter: map[string]any{
				"on": map[string]any{
					"command": "test-bot",
					"issue_comment": map[string]any{
						"types": []string{"created"},
					},
				},
			},
			wantErr:     true,
			errContains: "command trigger cannot be used with 'issue_comment' event",
		},
		{
			name: "invalid: command trigger with pull_request event",
			frontmatter: map[string]any{
				"on": map[string]any{
					"command": map[string]any{
						"name": "test-bot",
					},
					"pull_request": map[string]any{
						"types": []string{"opened"},
					},
				},
			},
			wantErr:     true,
			errContains: "command trigger cannot be used with 'pull_request' event",
		},
		{
			name: "invalid: command trigger with pull_request_review_comment event",
			frontmatter: map[string]any{
				"on": map[string]any{
					"command": "test-bot",
					"pull_request_review_comment": map[string]any{
						"types": []string{"created"},
					},
				},
			},
			wantErr:     true,
			errContains: "command trigger cannot be used with 'pull_request_review_comment' event",
		},
		{
			name: "invalid: command trigger with multiple conflicting events",
			frontmatter: map[string]any{
				"on": map[string]any{
					"command": map[string]any{
						"name": "test-bot",
					},
					"issues": map[string]any{
						"types": []string{"opened"},
					},
					"pull_request": map[string]any{
						"types": []string{"opened"},
					},
				},
			},
			wantErr:     true,
			errContains: "command trigger cannot be used with these events",
		},
		{
			name: "valid: command trigger with non-conflicting events",
			frontmatter: map[string]any{
				"on": map[string]any{
					"command": map[string]any{
						"name": "test-bot",
					},
					"workflow_dispatch": nil,
					"schedule": []map[string]any{
						{"cron": "0 0 * * *"},
					},
				},
			},
			wantErr: false,
		},
		{
			name: "valid: command trigger alone",
			frontmatter: map[string]any{
				"on": map[string]any{
					"command": "test-bot",
				},
			},
			wantErr: false,
		},
		{
			name: "valid: command trigger as null (default workflow name)",
			frontmatter: map[string]any{
				"on": map[string]any{
					"command": nil,
				},
			},
			wantErr: false,
		},
		{
			name: "valid: issues event without command",
			frontmatter: map[string]any{
				"on": map[string]any{
					"issues": map[string]any{
						"types": []string{"opened"},
					},
				},
			},
			wantErr: false,
		},
		{
			name: "invalid: empty string for name field",
			frontmatter: map[string]any{
				"on":   "push",
				"name": "",
			},
			wantErr:     true,
			errContains: "minLength",
		},
		{
			name: "invalid: empty string for on field (string format)",
			frontmatter: map[string]any{
				"on": "",
			},
			wantErr:     true,
			errContains: "minLength",
		},
		{
			name: "invalid: empty string for command trigger (string format)",
			frontmatter: map[string]any{
				"on": map[string]any{
					"command": "",
				},
			},
			wantErr:     true,
			errContains: "minLength",
		},
		{
			name: "invalid: empty string for command.name field",
			frontmatter: map[string]any{
				"on": map[string]any{
					"command": map[string]any{
						"name": "",
					},
				},
			},
			wantErr:     true,
			errContains: "minLength",
		},
		{
			name: "invalid: command name starting with slash (string format)",
			frontmatter: map[string]any{
				"on": map[string]any{
					"command": "/mybot",
				},
			},
			wantErr:     true,
			errContains: "pattern",
		},
		{
			name: "invalid: command.name starting with slash (object format)",
			frontmatter: map[string]any{
				"on": map[string]any{
					"command": map[string]any{
						"name": "/mybot",
					},
				},
			},
			wantErr:     true,
			errContains: "pattern",
		},
		{
			name: "valid: command name without slash (string format)",
			frontmatter: map[string]any{
				"on": map[string]any{
					"command": "mybot",
				},
			},
			wantErr: false,
		},
		{
			name: "valid: command.name without slash (object format)",
			frontmatter: map[string]any{
				"on": map[string]any{
					"command": map[string]any{
						"name": "mybot",
					},
				},
			},
			wantErr: false,
		},
		{
			name: "invalid: empty events array for command trigger",
			frontmatter: map[string]any{
				"on": map[string]any{
					"command": map[string]any{
						"name":   "test-bot",
						"events": []any{},
					},
				},
			},
			wantErr:     true,
			errContains: "minItems",
		},
		{
			name: "valid: workflow_dispatch with 25 inputs (max allowed)",
			frontmatter: map[string]any{
				"on": map[string]any{
					"workflow_dispatch": map[string]any{
						"inputs": map[string]any{
							"input1":  map[string]any{"description": "Input 1", "type": "string"},
							"input2":  map[string]any{"description": "Input 2", "type": "string"},
							"input3":  map[string]any{"description": "Input 3", "type": "string"},
							"input4":  map[string]any{"description": "Input 4", "type": "string"},
							"input5":  map[string]any{"description": "Input 5", "type": "string"},
							"input6":  map[string]any{"description": "Input 6", "type": "string"},
							"input7":  map[string]any{"description": "Input 7", "type": "string"},
							"input8":  map[string]any{"description": "Input 8", "type": "string"},
							"input9":  map[string]any{"description": "Input 9", "type": "string"},
							"input10": map[string]any{"description": "Input 10", "type": "string"},
							"input11": map[string]any{"description": "Input 11", "type": "string"},
							"input12": map[string]any{"description": "Input 12", "type": "string"},
							"input13": map[string]any{"description": "Input 13", "type": "string"},
							"input14": map[string]any{"description": "Input 14", "type": "string"},
							"input15": map[string]any{"description": "Input 15", "type": "string"},
							"input16": map[string]any{"description": "Input 16", "type": "string"},
							"input17": map[string]any{"description": "Input 17", "type": "string"},
							"input18": map[string]any{"description": "Input 18", "type": "string"},
							"input19": map[string]any{"description": "Input 19", "type": "string"},
							"input20": map[string]any{"description": "Input 20", "type": "string"},
							"input21": map[string]any{"description": "Input 21", "type": "string"},
							"input22": map[string]any{"description": "Input 22", "type": "string"},
							"input23": map[string]any{"description": "Input 23", "type": "string"},
							"input24": map[string]any{"description": "Input 24", "type": "string"},
							"input25": map[string]any{"description": "Input 25", "type": "string"},
						},
					},
				},
			},
			wantErr: false,
		},
		{
			name: "invalid: workflow_dispatch with 26 inputs (exceeds max)",
			frontmatter: map[string]any{
				"on": map[string]any{
					"workflow_dispatch": map[string]any{
						"inputs": map[string]any{
							"input1":  map[string]any{"description": "Input 1", "type": "string"},
							"input2":  map[string]any{"description": "Input 2", "type": "string"},
							"input3":  map[string]any{"description": "Input 3", "type": "string"},
							"input4":  map[string]any{"description": "Input 4", "type": "string"},
							"input5":  map[string]any{"description": "Input 5", "type": "string"},
							"input6":  map[string]any{"description": "Input 6", "type": "string"},
							"input7":  map[string]any{"description": "Input 7", "type": "string"},
							"input8":  map[string]any{"description": "Input 8", "type": "string"},
							"input9":  map[string]any{"description": "Input 9", "type": "string"},
							"input10": map[string]any{"description": "Input 10", "type": "string"},
							"input11": map[string]any{"description": "Input 11", "type": "string"},
							"input12": map[string]any{"description": "Input 12", "type": "string"},
							"input13": map[string]any{"description": "Input 13", "type": "string"},
							"input14": map[string]any{"description": "Input 14", "type": "string"},
							"input15": map[string]any{"description": "Input 15", "type": "string"},
							"input16": map[string]any{"description": "Input 16", "type": "string"},
							"input17": map[string]any{"description": "Input 17", "type": "string"},
							"input18": map[string]any{"description": "Input 18", "type": "string"},
							"input19": map[string]any{"description": "Input 19", "type": "string"},
							"input20": map[string]any{"description": "Input 20", "type": "string"},
							"input21": map[string]any{"description": "Input 21", "type": "string"},
							"input22": map[string]any{"description": "Input 22", "type": "string"},
							"input23": map[string]any{"description": "Input 23", "type": "string"},
							"input24": map[string]any{"description": "Input 24", "type": "string"},
							"input25": map[string]any{"description": "Input 25", "type": "string"},
							"input26": map[string]any{"description": "Input 26", "type": "string"},
						},
					},
				},
			},
			wantErr:     true,
			errContains: "maxProperties",
		},
		{
			name: "valid metadata with various key-value pairs",
			frontmatter: map[string]any{
				"on": "workflow_dispatch",
				"metadata": map[string]any{
					"author":      "John Doe",
					"version":     "1.0.0",
					"category":    "automation",
					"description": "A workflow that automates something",
				},
			},
			wantErr: false,
		},
		{
			name: "valid metadata with max length key (64 chars)",
			frontmatter: map[string]any{
				"on": "workflow_dispatch",
				"metadata": map[string]any{
					"a123456789b123456789c123456789d123456789e123456789f123456789abcd": "value",
				},
			},
			wantErr: false,
		},
		{
			name: "valid metadata with max length value (1024 chars)",
			frontmatter: map[string]any{
				"on": "workflow_dispatch",
				"metadata": map[string]any{
					"long-value": strings.Repeat("a", 1024),
				},
			},
			wantErr: false,
		},
		{
			name: "invalid metadata with key too long (65 chars)",
			frontmatter: map[string]any{
				"on": "workflow_dispatch",
				"metadata": map[string]any{
					"a123456789b123456789c123456789d123456789e123456789f123456789abcde": "value",
				},
			},
			wantErr:     true,
			errContains: "additional properties",
		},
		{
			name: "invalid metadata with value too long (1025 chars)",
			frontmatter: map[string]any{
				"on": "workflow_dispatch",
				"metadata": map[string]any{
					"test": strings.Repeat("a", 1025),
				},
			},
			wantErr:     true,
			errContains: "maxLength",
		},
		{
			name: "invalid metadata with non-string value",
			frontmatter: map[string]any{
				"on": "workflow_dispatch",
				"metadata": map[string]any{
					"count": 123,
				},
			},
			wantErr:     true,
			errContains: "want string",
		},
		{
			name: "invalid metadata with empty key",
			frontmatter: map[string]any{
				"on": "workflow_dispatch",
				"metadata": map[string]any{
					"": "value",
				},
			},
			wantErr:     true,
			errContains: "additional properties",
		},
		{
			name: "invalid name too long (257 chars)",
			frontmatter: map[string]any{
				"on":   "workflow_dispatch",
				"name": strings.Repeat("a", 257),
			},
			wantErr:     true,
			errContains: "maxLength",
		},
		{
			name: "valid name at max length (256 chars)",
			frontmatter: map[string]any{
				"on":   "workflow_dispatch",
				"name": strings.Repeat("a", 256),
			},
			wantErr: false,
		},
		{
			name: "invalid description too long (10001 chars)",
			frontmatter: map[string]any{
				"on":          "workflow_dispatch",
				"description": strings.Repeat("a", 10001),
			},
			wantErr:     true,
			errContains: "maxLength",
		},
		{
			name: "valid description at max length (10000 chars)",
			frontmatter: map[string]any{
				"on":          "workflow_dispatch",
				"description": strings.Repeat("a", 10000),
			},
			wantErr: false,
		},
		{
			name: "invalid tracker-id too long (129 chars)",
			frontmatter: map[string]any{
				"on":         "workflow_dispatch",
				"tracker-id": strings.Repeat("a", 129),
			},
			wantErr:     true,
			errContains: "maxLength",
		},
		{
			name: "valid tracker-id at max length (128 chars)",
			frontmatter: map[string]any{
				"on":         "workflow_dispatch",
				"tracker-id": strings.Repeat("a", 128),
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateMainWorkflowFrontmatterWithSchema(tt.frontmatter)

			if tt.wantErr && err == nil {
				t.Errorf("ValidateMainWorkflowFrontmatterWithSchema() expected error, got nil")
				return
			}

			if !tt.wantErr && err != nil {
				t.Errorf("ValidateMainWorkflowFrontmatterWithSchema() error = %v", err)
				return
			}

			if tt.wantErr && err != nil && tt.errContains != "" {
				if !strings.Contains(err.Error(), tt.errContains) {
					t.Errorf("ValidateMainWorkflowFrontmatterWithSchema() error = %v, expected to contain %v", err, tt.errContains)
				}
			}
		})
	}
}

func TestValidateIncludedFileFrontmatterWithSchema(t *testing.T) {
	tests := []struct {
		name        string
		frontmatter map[string]any
		wantErr     bool
		errContains string
	}{
		{
			name: "valid frontmatter with tools only",
			frontmatter: map[string]any{
				"tools": map[string]any{"github": "test"},
			},
			wantErr: false,
		},
		{
			name:        "empty frontmatter",
			frontmatter: map[string]any{},
			wantErr:     false,
		},
		{
			name: "invalid frontmatter with on trigger",
			frontmatter: map[string]any{
				"on":    "push",
				"tools": map[string]any{"github": "test"},
			},
			wantErr:     true,
			errContains: "cannot be used in shared workflows",
		},
		{
			name: "invalid frontmatter with multiple unexpected keys",
			frontmatter: map[string]any{
				"on":          "push",
				"permissions": "read-all",
				"tools":       map[string]any{"github": "test"},
			},
			wantErr:     true,
			errContains: "cannot be used in shared workflows",
		},
		{
			name: "invalid frontmatter with only unexpected keys",
			frontmatter: map[string]any{
				"on":          "push",
				"permissions": "read-all",
			},
			wantErr:     true,
			errContains: "cannot be used in shared workflows",
		},
		{
			name: "valid frontmatter with complex tools object",
			frontmatter: map[string]any{
				"tools": map[string]any{
					"github": map[string]any{
						"allowed": []string{"list_issues", "issue_read"},
					},
					"bash": []string{"echo", "ls"},
				},
			},
			wantErr: false,
		},
		{
			name: "valid frontmatter with bash as boolean true",
			frontmatter: map[string]any{
				"tools": map[string]any{
					"bash": true,
				},
			},
			wantErr: false,
		},
		{
			name: "valid frontmatter with bash as boolean false",
			frontmatter: map[string]any{
				"tools": map[string]any{
					"bash": false,
				},
			},
			wantErr: false,
		},
		{
			name: "valid frontmatter with bash as null",
			frontmatter: map[string]any{
				"tools": map[string]any{
					"bash": nil,
				},
			},
			wantErr: false,
		},
		{
			name: "valid frontmatter with custom MCP tool",
			frontmatter: map[string]any{
				"tools": map[string]any{
					"myTool": map[string]any{
						"mcp": map[string]any{
							"type":    "http",
							"url":     "https://api.contoso.com",
							"headers": map[string]any{"Authorization": "Bearer token"},
						},
						"allowed": []string{"api_call1", "api_call2"},
					},
				},
			},
			wantErr: false,
		},
		{
			name: "valid frontmatter with HTTP MCP tool with underscored headers",
			frontmatter: map[string]any{
				"tools": map[string]any{
					"datadog": map[string]any{
						"type": "http",
						"url":  "https://mcp.datadoghq.com/api/unstable/mcp-server/mcp",
						"headers": map[string]any{
							"DD_API_KEY":         "test-key",
							"DD_APPLICATION_KEY": "test-app",
							"DD_SITE":            "datadoghq.com",
						},
						"allowed": []string{"get-monitors", "get-monitor"},
					},
				},
			},
			wantErr: false,
		},
		{
			name: "valid frontmatter with cache-memory as boolean true",
			frontmatter: map[string]any{
				"tools": map[string]any{
					"cache-memory": true,
				},
			},
			wantErr: false,
		},
		{
			name: "valid frontmatter with cache-memory as boolean false",
			frontmatter: map[string]any{
				"tools": map[string]any{
					"cache-memory": false,
				},
			},
			wantErr: false,
		},
		{
			name: "valid frontmatter with cache-memory as nil",
			frontmatter: map[string]any{
				"tools": map[string]any{
					"cache-memory": nil,
				},
			},
			wantErr: false,
		},
		{
			name: "valid frontmatter with cache-memory as object with key",
			frontmatter: map[string]any{
				"tools": map[string]any{
					"cache-memory": map[string]any{
						"key": "custom-memory-${{ github.workflow }}",
					},
				},
			},
			wantErr: false,
		},
		{
			name: "valid frontmatter with cache-memory with all valid options",
			frontmatter: map[string]any{
				"tools": map[string]any{
					"cache-memory": map[string]any{
						"key":            "custom-key",
						"retention-days": 30,
						"description":    "Test cache description",
					},
				},
			},
			wantErr: false,
		},
		{
			name: "invalid cache-memory with invalid retention-days (too low)",
			frontmatter: map[string]any{
				"tools": map[string]any{
					"cache-memory": map[string]any{
						"retention-days": 0,
					},
				},
			},
			wantErr:     true,
			errContains: "got 0, want 1",
		},
		{
			name: "invalid cache-memory with invalid retention-days (too high)",
			frontmatter: map[string]any{
				"tools": map[string]any{
					"cache-memory": map[string]any{
						"retention-days": 91,
					},
				},
			},
			wantErr:     true,
			errContains: "got 91, want 90",
		},
		{
			name: "invalid cache-memory with unsupported docker-image field",
			frontmatter: map[string]any{
				"tools": map[string]any{
					"cache-memory": map[string]any{
						"docker-image": "custom/memory:latest",
					},
				},
			},
			wantErr:     true,
			errContains: "additional properties 'docker-image' not allowed",
		},
		{
			name: "invalid cache-memory with additional property",
			frontmatter: map[string]any{
				"tools": map[string]any{
					"cache-memory": map[string]any{
						"key":            "custom-key",
						"invalid_option": "value",
					},
				},
			},
			wantErr:     true,
			errContains: "additional properties 'invalid_option' not allowed",
		},
		{
			name: "invalid: included file cannot have inputs at root level",
			frontmatter: map[string]any{
				"inputs": map[string]any{
					"input1": map[string]any{"description": "Input 1", "type": "string"},
				},
			},
			wantErr:     true,
			errContains: "additional properties 'inputs' not allowed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateIncludedFileFrontmatterWithSchema(tt.frontmatter)

			if tt.wantErr && err == nil {
				t.Errorf("ValidateIncludedFileFrontmatterWithSchema() expected error, got nil")
				return
			}

			if !tt.wantErr && err != nil {
				t.Errorf("ValidateIncludedFileFrontmatterWithSchema() error = %v", err)
				return
			}

			if tt.wantErr && err != nil && tt.errContains != "" {
				if !strings.Contains(err.Error(), tt.errContains) {
					t.Errorf("ValidateIncludedFileFrontmatterWithSchema() error = %v, expected to contain %v", err, tt.errContains)
				}
			}
		})
	}
}

func TestValidateWithSchema(t *testing.T) {
	tests := []struct {
		name        string
		frontmatter map[string]any
		schema      string
		context     string
		wantErr     bool
		errContains string
	}{
		{
			name: "valid data with simple schema",
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
			context: "test context",
			wantErr: false,
		},
		{
			name: "invalid data with additional property",
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
			context:     "test context",
			wantErr:     true,
			errContains: "additional properties 'invalid' not allowed",
		},
		{
			name: "invalid schema JSON",
			frontmatter: map[string]any{
				"name": "test",
			},
			schema:      `invalid json`,
			context:     "test context",
			wantErr:     true,
			errContains: "schema validation error for test context",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateWithSchema(tt.frontmatter, tt.schema, tt.context)

			if tt.wantErr && err == nil {
				t.Errorf("validateWithSchema() expected error, got nil")
				return
			}

			if !tt.wantErr && err != nil {
				t.Errorf("validateWithSchema() error = %v", err)
				return
			}

			if tt.wantErr && err != nil && tt.errContains != "" {
				if !strings.Contains(err.Error(), tt.errContains) {
					t.Errorf("validateWithSchema() error = %v, expected to contain %v", err, tt.errContains)
				}
			}
		})
	}
}

func TestValidateWithSchemaAndLocation_CleanedErrorMessage(t *testing.T) {
	// Test that error messages are properly cleaned of unhelpful jsonschema prefixes
	frontmatter := map[string]any{
		"on":               "push",
		"timeout_minu tes": 10, // Invalid property name with space
	}

	// Create a temporary test file
	tempFile := "/tmp/gh-aw/test_schema_validation.md"
	// Ensure the directory exists
	if err := os.MkdirAll("/tmp/gh-aw", 0755); err != nil {
		t.Fatalf("Failed to create temp directory: %v", err)
	}
	err := os.WriteFile(tempFile, []byte(`---
on: push
timeout_minu tes: 10
---

# Test workflow`), 0644)
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	defer os.Remove(tempFile)

	err = ValidateMainWorkflowFrontmatterWithSchemaAndLocation(frontmatter, tempFile)

	if err == nil {
		t.Fatal("Expected validation error, got nil")
	}

	errorMsg := err.Error()

	// The error message should NOT contain the unhelpful jsonschema prefixes
	if strings.Contains(errorMsg, "jsonschema validation failed") {
		t.Errorf("Error message should not contain 'jsonschema validation failed' prefix, got: %s", errorMsg)
	}

	if strings.Contains(errorMsg, "- at '': ") {
		t.Errorf("Error message should not contain '- at '':' prefix, got: %s", errorMsg)
	}

	// The error message should contain the friendly rewritten error description
	if !strings.Contains(errorMsg, "Unknown property: timeout_minu tes") {
		t.Errorf("Error message should contain the validation error, got: %s", errorMsg)
	}

	// The error message should be formatted with location information
	if !strings.Contains(errorMsg, tempFile) {
		t.Errorf("Error message should contain file path, got: %s", errorMsg)
	}
}

func TestValidateMCPConfigWithSchema(t *testing.T) {
	tests := []struct {
		name        string
		mcpConfig   map[string]any
		toolName    string
		wantErr     bool
		errContains string
	}{
		{
			name: "valid stdio MCP config with command",
			mcpConfig: map[string]any{
				"type":    "stdio",
				"command": "npx",
				"args":    []string{"-y", "@modelcontextprotocol/server-memory"},
			},
			toolName: "memory",
			wantErr:  false,
		},
		{
			name: "valid http MCP config with url",
			mcpConfig: map[string]any{
				"type": "http",
				"url":  "https://api.example.com/mcp",
			},
			toolName: "api-server",
			wantErr:  false,
		},
		{
			name: "invalid: empty string for command field",
			mcpConfig: map[string]any{
				"type":    "stdio",
				"command": "",
			},
			toolName:    "test-tool",
			wantErr:     true,
			errContains: "minLength",
		},
		{
			name: "invalid: empty string for url field",
			mcpConfig: map[string]any{
				"type": "http",
				"url":  "",
			},
			toolName:    "test-tool",
			wantErr:     true,
			errContains: "minLength",
		},
		{
			name: "valid stdio MCP config with container",
			mcpConfig: map[string]any{
				"type":      "stdio",
				"container": "ghcr.io/modelcontextprotocol/server-memory",
			},
			toolName: "memory",
			wantErr:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateMCPConfigWithSchema(tt.mcpConfig, tt.toolName)

			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateMCPConfigWithSchema() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if err != nil && tt.errContains != "" {
				if !strings.Contains(err.Error(), tt.errContains) {
					t.Errorf("Error message should contain %q, got: %v", tt.errContains, err)
				}
			}
		})
	}
}

// TestGetSafeOutputTypeKeys tests extracting safe output type keys from the embedded schema
func TestGetSafeOutputTypeKeys(t *testing.T) {
	keys, err := GetSafeOutputTypeKeys()
	if err != nil {
		t.Fatalf("GetSafeOutputTypeKeys() returned error: %v", err)
	}

	// Should return multiple keys
	if len(keys) == 0 {
		t.Error("GetSafeOutputTypeKeys() returned empty list")
	}

	// Should include known safe output types
	expectedKeys := []string{
		"create-issue",
		"add-comment",
		"create-discussion",
		"create-pull-request",
		"update-issue",
	}

	keySet := make(map[string]bool)
	for _, key := range keys {
		keySet[key] = true
	}

	for _, expected := range expectedKeys {
		if !keySet[expected] {
			t.Errorf("GetSafeOutputTypeKeys() missing expected key: %s", expected)
		}
	}

	// Should NOT include meta-configuration fields
	metaFields := []string{
		"allowed-domains",
		"staged",
		"env",
		"github-token",
		"app",
		"max-patch-size",
		"jobs",
		"runs-on",
		"messages",
	}

	for _, meta := range metaFields {
		if keySet[meta] {
			t.Errorf("GetSafeOutputTypeKeys() should not include meta field: %s", meta)
		}
	}

	// Keys should be sorted
	for i := 1; i < len(keys); i++ {
		if keys[i-1] > keys[i] {
			t.Errorf("GetSafeOutputTypeKeys() keys are not sorted: %s > %s", keys[i-1], keys[i])
		}
	}
}
