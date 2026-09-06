//go:build !integration

package workflow

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExtractDriveMemoryConfig(t *testing.T) {
	tests := []struct {
		name         string
		raw          any
		wantDrives   int
		wantErr      string
		wantDiskSize string
	}{
		{name: "null enables default", raw: nil, wantDrives: 1},
		{name: "true enables default", raw: true, wantDrives: 1},
		{name: "false disables", raw: false, wantDrives: 0},
		{
			name: "object configuration",
			raw: map[string]any{
				"drive-name":   "agent-state",
				"disk-size":    "20G",
				"prefetch":     true,
				"restore-only": true,
			},
			wantDrives: 1,
		},
		{
			name: "multiple drives",
			raw: []any{
				map[string]any{"id": "default"},
				map[string]any{"id": "reference"},
			},
			wantDrives: 2,
		},
		{
			name: "duplicate IDs",
			raw: []any{
				map[string]any{"id": "notes"},
				map[string]any{"id": "notes"},
			},
			wantErr: "duplicate drive-memory id",
		},
		{
			name:    "unsafe ID",
			raw:     []any{map[string]any{"id": "../../outside"}},
			wantErr: "invalid drive-memory id",
		},
		{
			name:    "unsafe drive name",
			raw:     map[string]any{"drive-name": "../outside"},
			wantErr: "invalid drive-memory drive-name",
		},
		{
			name:    "non-object array entry",
			raw:     []any{"notes"},
			wantErr: "array entries must be objects",
		},
		{
			name:       "disk size without suffix",
			raw:        map[string]any{"disk-size": "500"},
			wantDrives: 1,
		},
		{
			name:    "disk size with invalid suffix",
			raw:     map[string]any{"disk-size": "1GB"},
			wantErr: "invalid drive-memory disk-size",
		},
		{
			name:         "disk size with lowercase suffix is normalized",
			raw:          map[string]any{"disk-size": "100m"},
			wantDrives:   1,
			wantDiskSize: "100M",
		},
		{
			name:         "disk size with surrounding whitespace is trimmed",
			raw:          map[string]any{"disk-size": "  100M  "},
			wantDrives:   1,
			wantDiskSize: "100M",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			compiler := NewCompiler()
			config, err := compiler.extractDriveMemoryConfig(&ToolsConfig{
				DriveMemory: &DriveMemoryToolConfig{Raw: tt.raw},
			})
			if tt.wantErr != "" {
				require.ErrorContains(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
			require.NotNil(t, config)
			require.Len(t, config.Drives, tt.wantDrives)
			if tt.wantDrives > 0 {
				assert.NotEmpty(t, config.Drives[0].DriveName)
			}
			if tt.wantDiskSize != "" {
				assert.Equal(t, tt.wantDiskSize, config.Drives[0].DiskSize)
			}
		})
	}
}

func TestDriveMemoryPathsRejectUnsafeIDs(t *testing.T) {
	assert.Equal(t, "/tmp/gh-aw/drive-memory", driveMemoryDirFor("default"))
	assert.Equal(t, "/tmp/gh-aw/drive-memory-notes", driveMemoryDirFor("notes"))
	assert.Equal(t, ".gh-aw-drive-memory-notes", driveMemoryMountPathFor("notes"))
	assert.NotContains(t, driveMemoryDirFor("../outside"), "../")
	assert.NotContains(t, driveMemoryMountPathFor("nested/path"), "/")
}

func TestDriveMemoryEmitsExperimentalWarning(t *testing.T) {
	compiler := NewCompiler(WithVersion("dev"))
	compiler.SetBatchMode(true)
	compiler.emitExperimentalFeatureWarnings(&WorkflowData{
		DriveMemoryConfig: &DriveMemoryConfig{Drives: defaultDriveMemoryEntries()},
	})

	assert.Equal(t, 1, compiler.GetExperimentalFeatureUsage()["Using experimental feature: drive-memory"])
	assert.Positive(t, compiler.GetWarningCount())
}

func TestValidateDriveMemoryRuntime(t *testing.T) {
	driveConfig := &DriveMemoryConfig{Drives: defaultDriveMemoryEntries()}
	tests := []struct {
		name    string
		data    *WorkflowData
		wantErr string
	}{
		{
			name: "default runner",
			data: &WorkflowData{DriveMemoryConfig: driveConfig},
		},
		{
			name: "ubuntu-latest runner",
			data: &WorkflowData{DriveMemoryConfig: driveConfig, RunsOn: "runs-on: ubuntu-latest"},
		},
		{
			name:    "unsupported runner",
			data:    &WorkflowData{DriveMemoryConfig: driveConfig, RunsOn: "runs-on: windows-latest"},
			wantErr: "requires runs-on: ubuntu-latest",
		},
		{
			name:    "main job container",
			data:    &WorkflowData{DriveMemoryConfig: driveConfig, Container: "container: node:24"},
			wantErr: "cannot be used with a job container",
		},
		{
			name: "custom restore job runner",
			data: &WorkflowData{
				DriveMemoryConfig: driveConfig,
				Jobs: map[string]any{
					"reader": map[string]any{"restore-memory": true, "runs-on": "self-hosted"},
				},
			},
			wantErr: "jobs.reader.restore-memory requires runs-on: ubuntu-latest",
		},
		{
			name: "custom restore job container",
			data: &WorkflowData{
				DriveMemoryConfig: driveConfig,
				Jobs: map[string]any{
					"reader": map[string]any{"restore-memory": true, "container": "node:24"},
				},
			},
			wantErr: "jobs.reader.restore-memory cannot use drive-memory with a job container",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateDriveMemoryRuntime(tt.data)
			if tt.wantErr == "" {
				require.NoError(t, err)
				return
			}
			require.ErrorContains(t, err, tt.wantErr)
		})
	}
}

func TestGenerateDriveMemorySteps(t *testing.T) {
	data := &WorkflowData{
		DriveMemoryConfig: &DriveMemoryConfig{Drives: []DriveMemoryEntry{
			{
				ID:                "default",
				DriveName:         "agent-state",
				DiskSize:          "20G",
				Prefetch:          true,
				AllowedExtensions: []string{".json"},
			},
			{
				ID:                "reference",
				DriveName:         "shared-reference",
				RestoreOnly:       true,
				AllowedExtensions: []string{".md"},
			},
		}},
	}

	var setup strings.Builder
	generateDriveMemorySteps(&setup, data, func(action string) string { return action + "@test-pin" })
	setupYAML := setup.String()
	assert.Contains(t, setupYAML, "actions/gh-drives-preview/checkout@")
	assert.Contains(t, setupYAML, "drive-name: \"agent-state\"")
	assert.Contains(t, setupYAML, "disk-size: \"20G\"")
	assert.Contains(t, setupYAML, "prefetch: true")
	assert.Contains(t, setupYAML, "write: true")
	assert.Contains(t, setupYAML, "drive-name: \"shared-reference\"")
	assert.Contains(t, setupYAML, "write: false")
	assert.Contains(t, setupYAML, "ln -s \"$GITHUB_WORKSPACE/.gh-aw-drive-memory\" \"/tmp/gh-aw/drive-memory\"")
	assert.Contains(t, setupYAML, "GH_AW_MIN_INTEGRITY: none")

	var persist strings.Builder
	generateDriveMemoryPersistence(&persist, data, func(action string) string { return action + "@test-pin" })
	persistYAML := persist.String()
	assert.Contains(t, persistYAML, "actions/gh-drives-preview/commit@")
	assert.Contains(t, persistYAML, "if: success()")
	assert.NotContains(t, persistYAML, "if: always()")
	assert.NotContains(t, persistYAML, "shared-reference")

	prompt := buildDriveMemoryPromptSection(data.DriveMemoryConfig)
	require.NotNil(t, prompt)
	assert.Contains(t, prompt.Content, "/tmp/gh-aw/drive-memory/")
	assert.Contains(t, prompt.Content, "/tmp/gh-aw/drive-memory-reference/")
	assert.Contains(t, prompt.Content, "read-only")
}

func TestCopilotDriveMemoryAddDirWithoutCacheMemory(t *testing.T) {
	args := (&CopilotEngine{}).buildCopilotFeatureArgs(&WorkflowData{
		DriveMemoryConfig: &DriveMemoryConfig{Drives: []DriveMemoryEntry{{ID: "notes"}}},
	}, nil, nil)

	assert.Contains(t, args, "--add-dir")
	assert.Contains(t, args, "/tmp/gh-aw/drive-memory-notes/")
}

func TestDriveMemoryPersistenceWithoutValidationUsesDefaultSuccessCondition(t *testing.T) {
	data := &WorkflowData{
		DriveMemoryConfig: &DriveMemoryConfig{Drives: []DriveMemoryEntry{{
			ID:        "default",
			DriveName: "agent-state",
		}}},
	}

	var persist strings.Builder
	generateDriveMemoryPersistence(&persist, data, func(action string) string { return action + "@test-pin" })

	assert.Contains(t, persist.String(), "Commit drive-memory file share (default)")
	assert.Contains(t, persist.String(), "actions/gh-drives-preview/commit@test-pin")
	assert.NotContains(t, persist.String(), "if:")
}

// TestDriveMemoryValidationConfigAndGeneratedSteps mirrors
// TestCacheMemoryValidationConfigAndGeneratedSteps and
// TestRepoMemoryValidationConfigAndGeneratedSteps to confirm the script-based
// custom validation hook is wired uniformly for drive-memory too: parsed from
// config, passed through as VALIDATION_SCRIPT_B64 to validate_memory_step.cjs,
// and gating persistence on the validation step's outcome.
func TestDriveMemoryValidationConfigAndGeneratedSteps(t *testing.T) {
	compiler := NewCompiler()
	config, err := compiler.extractDriveMemoryConfig(&ToolsConfig{
		DriveMemory: &DriveMemoryToolConfig{Raw: []any{
			map[string]any{
				"id":         "default",
				"drive-name": "agent-state",
				"validation": map[string]any{
					"script":          "if (!fs.existsSync(path.join(memoryRoot, 'index.json'))) throw new Error('missing index');",
					"timeout-minutes": 1,
				},
			},
		}},
	})
	require.NoError(t, err)
	require.NotNil(t, config)
	require.Len(t, config.Drives, 1)
	require.NotNil(t, config.Drives[0].Validation)
	assert.Equal(t, 1, config.Drives[0].Validation.TimeoutMinutes)

	data := &WorkflowData{DriveMemoryConfig: config}

	var validation strings.Builder
	generateDriveMemoryValidation(&validation, data)
	validationYAML := validation.String()
	assert.Contains(t, validationYAML, "Validate drive-memory file types (default)")
	assert.Contains(t, validationYAML, "VALIDATION_SCRIPT_B64:")
	assert.Contains(t, validationYAML, "validate_memory_step.cjs")
	assert.Contains(t, validationYAML, "id: "+driveMemoryValidationStepID("default"))

	var persist strings.Builder
	generateDriveMemoryPersistence(&persist, data, func(action string) string { return action + "@test-pin" })
	assert.Contains(t, persist.String(), "steps."+driveMemoryValidationStepID("default")+".outcome == 'success'")
}

func TestDriveMemoryRestorePreservesIntegrityLevel(t *testing.T) {
	compiler := NewCompiler()
	data := &WorkflowData{
		DriveMemoryConfig: &DriveMemoryConfig{Drives: defaultDriveMemoryEntries()},
		ParsedTools: &ToolsConfig{GitHub: &GitHubToolConfig{
			MinIntegrity: GitHubIntegrityApproved,
		}},
	}

	steps := strings.Join(compiler.generateDriveMemoryRestoreLines(data), "")
	assert.Contains(t, steps, "GH_AW_MIN_INTEGRITY: approved")

	var preActivation strings.Builder
	compiler.generatePreActivationDriveMemoryRestoreSteps(&preActivation, data)
	assert.Contains(t, preActivation.String(), "GH_AW_MIN_INTEGRITY: approved")
}

func TestCompileDriveMemoryWorkflow(t *testing.T) {
	tmpDir := testutil.TempDir(t, "drive-memory")
	workflowPath := filepath.Join(tmpDir, "drive.md")
	require.NoError(t, os.WriteFile(workflowPath, []byte(`---
name: Drive memory
on: workflow_dispatch
engine: copilot
strict: false
tools:
  drive-memory:
    drive-name: agent-state
    disk-size: 20G
    prefetch: true
---

Use drive memory.
`), 0o644))

	compiler := NewCompiler()
	require.NoError(t, compiler.CompileWorkflow(workflowPath))
	lockContent, err := os.ReadFile(filepath.Join(tmpDir, "drive.lock.yml"))
	require.NoError(t, err)
	lockYAML := string(lockContent)

	assert.Contains(t, lockYAML, "actions/gh-drives-preview/checkout@c9163b96f9720dc55e0de19b37028ae22dcfa42a")
	assert.Contains(t, lockYAML, "actions/gh-drives-preview/commit@c9163b96f9720dc55e0de19b37028ae22dcfa42a")
	assert.Contains(t, lockYAML, "drives: write")
	assert.Contains(t, lockYAML, "id-token: write")
	assert.Contains(t, lockYAML, "contents: read")
	assert.Contains(t, lockYAML, "/tmp/gh-aw/drive-memory/")
}

func TestCompileDriveMemoryThreatDetectionWorkflow(t *testing.T) {
	tmpDir := testutil.TempDir(t, "drive-memory-threat-detection")
	workflowPath := filepath.Join(tmpDir, "drive.md")
	require.NoError(t, os.WriteFile(workflowPath, []byte(`---
name: Drive memory with threat detection
on: workflow_dispatch
engine: copilot
strict: false
tools:
  drive-memory: true
safe-outputs:
  create-issue:
  threat-detection: true
---

Use drive memory.
`), 0o644))

	compiler := NewCompiler()
	require.NoError(t, compiler.CompileWorkflow(workflowPath))
	lockContent, err := os.ReadFile(filepath.Join(tmpDir, "drive.lock.yml"))
	require.NoError(t, err)
	lockYAML := string(lockContent)
	agentSection := extractJobSection(lockYAML, "agent")
	updateSection := extractJobSection(lockYAML, updateDriveMemoryJobName)

	assert.Contains(t, agentSection, "write: false")
	assert.Contains(t, agentSection, "Capture drive-memory baseline")
	assert.Contains(t, agentSection, "Upload drive-memory data as artifact")
	assert.Contains(t, agentSection, "Upload drive-memory baseline")
	assert.Contains(t, agentSection, "drives: read")
	assert.NotContains(t, agentSection, "actions/gh-drives-preview/commit@")
	assert.Contains(t, updateSection, "runs-on: ubuntu-latest")
	assert.Contains(t, updateSection, "drives: write")
	assert.Contains(t, updateSection, "Check drive-memory for concurrent updates")
	assert.Contains(t, updateSection, "refusing to overwrite a newer version")
	assert.Contains(t, updateSection, "Download drive-memory artifact")
	assert.Contains(t, updateSection, "actions/gh-drives-preview/commit@")
}

func TestCompileRestoreOnlyDriveMemoryWorkflow(t *testing.T) {
	tmpDir := testutil.TempDir(t, "drive-memory-restore-only")
	workflowPath := filepath.Join(tmpDir, "drive.md")
	require.NoError(t, os.WriteFile(workflowPath, []byte(`---
name: Restore-only drive memory
on: workflow_dispatch
engine: copilot
strict: false
tools:
  drive-memory:
    restore-only: true
---

Read drive memory.
`), 0o644))

	compiler := NewCompiler()
	require.NoError(t, compiler.CompileWorkflow(workflowPath))
	lockContent, err := os.ReadFile(filepath.Join(tmpDir, "drive.lock.yml"))
	require.NoError(t, err)
	agentSection := extractJobSection(string(lockContent), "agent")

	assert.Contains(t, agentSection, "write: false")
	assert.Contains(t, agentSection, "drives: read")
	assert.NotContains(t, agentSection, "actions/gh-drives-preview/commit@")
	assert.NotContains(t, string(lockContent), updateDriveMemoryJobName+":")
}

func TestDriveMemoryThreatDetectionJob(t *testing.T) {
	compiler := NewCompiler()
	data := &WorkflowData{
		DriveMemoryConfig: &DriveMemoryConfig{Drives: []DriveMemoryEntry{{
			ID:                "default",
			DriveName:         "agent-state",
			AllowedExtensions: []string{".json"},
		}}},
		SafeOutputs: &SafeOutputsConfig{ThreatDetection: &ThreatDetectionConfig{}},
	}

	var setup strings.Builder
	generateDriveMemorySteps(&setup, data, getActionPin)
	assert.Contains(t, setup.String(), "write: false")

	var upload strings.Builder
	generateDriveMemoryPersistence(&upload, data, getActionPin)
	assert.Contains(t, upload.String(), "name: drive-memory")
	assert.Contains(t, upload.String(), "include-hidden-files: true")

	job, err := compiler.buildUpdateDriveMemoryJob(data, true)
	require.NoError(t, err)
	require.NotNil(t, job)
	assert.Equal(t, "runs-on: ubuntu-latest", job.RunsOn)
	assert.Contains(t, job.Permissions, "drives: write")
	assert.Contains(t, strings.Join(job.Steps, "\n"), "actions/gh-drives-preview/commit@")
}

func TestDriveMemoryUpdateCheckoutStepIDsAreUnique(t *testing.T) {
	compiler := NewCompiler()
	steps := strings.Join([]string{
		compiler.buildDriveMemoryUpdateCheckoutStep(DriveMemoryEntry{ID: "a-b"}, ".gh-aw-drive-memory-a-b"),
		compiler.buildDriveMemoryUpdateCheckoutStep(DriveMemoryEntry{ID: "a_b"}, ".gh-aw-drive-memory-a_b"),
	}, "\n")

	assert.Contains(t, steps, "id: checkout_drive_612d62")
	assert.Contains(t, steps, "id: checkout_drive_615f62")
}
