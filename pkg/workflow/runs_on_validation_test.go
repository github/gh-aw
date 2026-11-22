//go:build !integration

package workflow

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/github/gh-aw/pkg/stringutil"
	"github.com/goccy/go-yaml"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestImportedRunsOn(t *testing.T) {
	for _, tt := range []struct {
		name, shared, local, wantError string
		wantRunner                     any
	}{
		{name: "first import wins", shared: "ubuntu-24.04", wantRunner: "ubuntu-24.04"},
		{name: "main overrides import", shared: "macos-latest", local: "runs-on: ubuntu-22.04\n", wantRunner: "ubuntu-22.04"},
		{name: "label array", shared: "[self-hosted, linux, x64]", wantRunner: []any{"self-hosted", "linux", "x64"}},
		{name: "runner group", shared: "{group: linux-pool, labels: [linux]}", wantRunner: map[string]any{"group": "linux-pool", "labels": []any{"linux"}}},
		{name: "reject unsupported runner", shared: "macos-latest", wantError: "unsupported runner 'macos-latest'"},
		{name: "reject empty runner group", shared: "{}", wantError: "runs-on object is empty"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			dir := filepath.Join(t.TempDir(), ".github", "workflows")
			require.NoError(t, os.MkdirAll(dir, 0755))
			require.NoError(t, os.WriteFile(filepath.Join(dir, "shared.md"), []byte("---\nruns-on: "+tt.shared+"\nengine: claude\n---\n"), 0644))
			require.NoError(t, os.WriteFile(filepath.Join(dir, "second.md"), []byte("---\nruns-on: ubuntu-latest\n---\n"), 0644))
			main := filepath.Join(dir, "main.md")
			require.NoError(t, os.WriteFile(main, []byte(`---
on: workflow_dispatch
strict: false
permissions:
  contents: read
safe-outputs:
  add-comment:
imports:
  - shared.md
  - second.md
`+tt.local+`---
Say hello.
`), 0644))

			err := NewCompiler().CompileWorkflow(main)
			if tt.wantError != "" {
				require.ErrorContains(t, err, tt.wantError)
				return
			}
			require.NoError(t, err)
			lock, err := os.ReadFile(stringutil.MarkdownToLockFile(main))
			require.NoError(t, err)
			var workflow struct {
				Jobs map[string]struct {
					RunsOn any `yaml:"runs-on"`
				} `yaml:"jobs"`
			}
			require.NoError(t, yaml.Unmarshal(lock, &workflow))
			assert.Equal(t, tt.wantRunner, workflow.Jobs["agent"].RunsOn)
			assert.Equal(t, "ubuntu-latest", workflow.Jobs["detection"].RunsOn)
		})
	}
}

func TestValidateRunsOn(t *testing.T) {
	tests := []struct {
		name        string
		frontmatter map[string]any
		wantErr     bool
		errorInMsg  string
		description string
	}{
		{
			name:        "no runs-on field",
			frontmatter: map[string]any{},
			wantErr:     false,
			description: "Missing runs-on should pass validation",
		},
		{
			name:        "ubuntu-latest string",
			frontmatter: map[string]any{"runs-on": "ubuntu-latest"},
			wantErr:     false,
			description: "ubuntu-latest should be allowed",
		},
		{
			name:        "windows-latest string",
			frontmatter: map[string]any{"runs-on": "windows-latest"},
			wantErr:     false,
			description: "windows-latest should be allowed",
		},
		{
			name:        "self-hosted string",
			frontmatter: map[string]any{"runs-on": "self-hosted"},
			wantErr:     false,
			description: "self-hosted should be allowed",
		},
		{
			name:        "macos-latest string",
			frontmatter: map[string]any{"runs-on": "macos-latest"},
			wantErr:     true,
			errorInMsg:  "macos-latest",
			description: "macos-latest should be rejected",
		},
		{
			name:        "macos-14 string",
			frontmatter: map[string]any{"runs-on": "macos-14"},
			wantErr:     true,
			errorInMsg:  "macos-14",
			description: "macos-14 should be rejected",
		},
		{
			name:        "macos-13 string",
			frontmatter: map[string]any{"runs-on": "macos-13"},
			wantErr:     true,
			errorInMsg:  "macos-13",
			description: "macos-13 should be rejected",
		},
		{
			name:        "bare macos string",
			frontmatter: map[string]any{"runs-on": "macos"},
			wantErr:     true,
			errorInMsg:  "macos",
			description: "bare 'macos' runner label should be rejected",
		},
		{
			name:        "ubuntu array",
			frontmatter: map[string]any{"runs-on": []any{"self-hosted", "linux"}},
			wantErr:     false,
			description: "Array with linux runners should be allowed",
		},
		{
			name:        "macos in array",
			frontmatter: map[string]any{"runs-on": []any{"self-hosted", "macos-latest"}},
			wantErr:     true,
			errorInMsg:  "macos-latest",
			description: "Array containing macos runner should be rejected",
		},
		{
			name: "object with linux labels",
			frontmatter: map[string]any{
				"runs-on": map[string]any{
					"group":  "ubuntu-runners",
					"labels": []any{"ubuntu-latest"},
				},
			},
			wantErr:     false,
			description: "Object form with linux labels should be allowed",
		},
		{
			name: "object with macos labels",
			frontmatter: map[string]any{
				"runs-on": map[string]any{
					"group":  "macos-runners",
					"labels": []any{"macos-14"},
				},
			},
			wantErr:     true,
			errorInMsg:  "macos-14",
			description: "Object form with macos labels should be rejected",
		},
		{
			name:        "error message contains FAQ link",
			frontmatter: map[string]any{"runs-on": "macos-latest"},
			wantErr:     true,
			errorInMsg:  macOSRunnerFAQURL,
			description: "Error should include FAQ link",
		},
		{
			name:        "error message explains reason",
			frontmatter: map[string]any{"runs-on": "macos-latest"},
			wantErr:     true,
			errorInMsg:  "containers",
			description: "Error should explain containers requirement",
		},
		{
			name:        "macos in runs-on-slim array",
			frontmatter: map[string]any{"runs-on-slim": []any{"self-hosted", "macos-14"}},
			wantErr:     true,
			errorInMsg:  "runs-on-slim",
			description: "runs-on-slim array containing macos runner should be rejected",
		},
		{
			name: "macos in safe-outputs.runs-on array",
			frontmatter: map[string]any{
				"safe-outputs": map[string]any{
					"runs-on": []any{"self-hosted", "macos-latest"},
				},
			},
			wantErr:     true,
			errorInMsg:  "safe-outputs.runs-on",
			description: "safe-outputs.runs-on array containing macos runner should be rejected",
		},
		{
			name: "macos in safe-outputs.threat-detection.runs-on labels",
			frontmatter: map[string]any{
				"safe-outputs": map[string]any{
					"threat-detection": map[string]any{
						"runs-on": map[string]any{
							"group":  "runner-group",
							"labels": []any{"linux", "macos-latest"},
						},
					},
				},
			},
			wantErr:     true,
			errorInMsg:  "safe-outputs.threat-detection.runs-on",
			description: "threat-detection runs-on labels containing macos runner should be rejected",
		},
		{
			name: "macos string in safe-outputs.threat-detection.runs-on",
			frontmatter: map[string]any{
				"safe-outputs": map[string]any{
					"threat-detection": map[string]any{
						"runs-on": "macos-latest",
					},
				},
			},
			wantErr:     true,
			errorInMsg:  "safe-outputs.threat-detection.runs-on",
			description: "threat-detection runs-on string macos runner should be rejected",
		},
		{
			name: "macos in safe-outputs.threat-detection.runs-on array",
			frontmatter: map[string]any{
				"safe-outputs": map[string]any{
					"threat-detection": map[string]any{
						"runs-on": []any{"self-hosted", "macOS", "arm64"},
					},
				},
			},
			wantErr:     true,
			errorInMsg:  "safe-outputs.threat-detection.runs-on",
			description: "threat-detection runs-on array containing macos runner should be rejected",
		},
		{
			name: "linux runner in safe-outputs.threat-detection.runs-on",
			frontmatter: map[string]any{
				"safe-outputs": map[string]any{
					"threat-detection": map[string]any{
						"runs-on": "ubuntu-latest",
					},
				},
			},
			wantErr:     false,
			description: "threat-detection runs-on with a Linux runner should be accepted",
		},
		{
			name: "macos in custom safe-job runs-on labels",
			frontmatter: map[string]any{
				"safe-outputs": map[string]any{
					"jobs": map[string]any{
						"notify": map[string]any{
							"runs-on": map[string]any{
								"group":  "runner-group",
								"labels": []any{"linux", "macos-latest"},
							},
						},
					},
				},
			},
			wantErr:     true,
			errorInMsg:  "safe-outputs.jobs.notify.runs-on",
			description: "custom safe-job runs-on labels containing macos should be rejected",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateRunsOn(tt.frontmatter, "test-workflow.md")

			if tt.wantErr {
				require.Error(t, err, "Test: %s - Expected error but got nil", tt.description)
				if tt.errorInMsg != "" {
					require.ErrorContains(t, err, tt.errorInMsg,
						"Error should contain '%s' for: %s", tt.errorInMsg, tt.description)
				}
			} else {
				assert.NoError(t, err, "Test: %s - Expected no error but got: %v", tt.description, err)
			}
		})
	}
}

func TestExtractRunnerLabels(t *testing.T) {
	tests := []struct {
		name     string
		runsOn   any
		expected []string
	}{
		{
			name:     "string label",
			runsOn:   "ubuntu-latest",
			expected: []string{"ubuntu-latest"},
		},
		{
			name:     "array of labels",
			runsOn:   []any{"self-hosted", "linux"},
			expected: []string{"self-hosted", "linux"},
		},
		{
			name: "object with labels",
			runsOn: map[string]any{
				"labels": []any{"linux", "x64"},
			},
			expected: []string{"linux", "x64"},
		},
		{
			name: "object without labels",
			runsOn: map[string]any{
				"group": "my-group",
			},
			expected: nil,
		},
		{
			name:     "nil",
			runsOn:   nil,
			expected: nil,
		},
		{
			name:     "integer (unsupported type)",
			runsOn:   42,
			expected: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := extractRunnerLabels(tt.runsOn)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestValidateSelfHostedRunners(t *testing.T) {
	tests := []struct {
		name       string
		jobs       map[string]any
		wantErr    bool
		errContain string
	}{
		{
			name: "self-hosted label",
			jobs: map[string]any{"agent": map[string]any{"runs-on": []any{"self-hosted", "linux"}}},
		},
		{
			name: "runner group",
			jobs: map[string]any{"safe_outputs": map[string]any{"runs-on": map[string]any{"group": "arc-pool", "labels": []any{"linux"}}}},
		},
		{
			name:       "GitHub-hosted runner",
			jobs:       map[string]any{"agent": map[string]any{"runs-on": "ubuntu-latest"}},
			wantErr:    true,
			errContain: `job "agent"`,
		},
		{
			name:       "custom label without self-hosted proof",
			jobs:       map[string]any{"agent": map[string]any{"runs-on": "my-runner"}},
			wantErr:    true,
			errContain: "self-hosted runner",
		},
		{
			name:       "omitted runner",
			jobs:       map[string]any{"safe_outputs": map[string]any{"steps": []any{}}},
			wantErr:    true,
			errContain: `job "safe_outputs" has no runs-on value`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateSelfHostedRunners(map[string]any{"jobs": tt.jobs})
			if tt.wantErr {
				require.Error(t, err)
				require.ErrorContains(t, err, tt.errContain)
				return
			}
			assert.NoError(t, err)
		})
	}
}

func TestValidateRunsOnValue(t *testing.T) {
	tests := []struct {
		name       string
		value      any
		wantErr    bool
		errContain string
	}{
		{
			name:    "string is valid",
			value:   "ubuntu-latest",
			wantErr: false,
		},
		{
			name:    "array of strings is valid",
			value:   []any{"self-hosted", "linux"},
			wantErr: false,
		},
		{
			name: "object with group and labels is valid",
			value: map[string]any{
				"group":  "my-group",
				"labels": []any{"linux", "x64"},
			},
			wantErr: false,
		},
		{
			name:       "array with non-string entry is invalid",
			value:      []any{"linux", 42},
			wantErr:    true,
			errContain: "array entry has type int",
		},
		{
			name: "object with invalid key is invalid",
			value: map[string]any{
				"runner": "ubuntu-latest",
			},
			wantErr:    true,
			errContain: "runs-on object key 'runner' is not supported",
		},
		{
			name:       "empty object is invalid",
			value:      map[string]any{},
			wantErr:    true,
			errContain: "runs-on object is empty",
		},
		{
			name:       "unsupported type is invalid",
			value:      123,
			wantErr:    true,
			errContain: "runs-on has type int",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateRunsOnValue(tt.value)
			if tt.wantErr {
				require.Error(t, err)
				require.ErrorContains(t, err, tt.errContain)
				return
			}
			assert.NoError(t, err)
		})
	}
}
