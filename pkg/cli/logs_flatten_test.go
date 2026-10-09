//go:build !integration

package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFlattenDownloadedAgentRoutingArtifacts(t *testing.T) {
	for _, artifactName := range []string{"agent", "agent-artifacts", "abc123-agent"} {
		t.Run(artifactName, func(t *testing.T) {
			t.Parallel()
			runDir := t.TempDir()
			write := func(relative, content string) {
				path := filepath.Join(runDir, relative)
				require.NoError(t, os.MkdirAll(filepath.Dir(path), 0755))
				require.NoError(t, os.WriteFile(path, []byte(content), 0600))
			}
			write("info/aw_info.json", `{"model":"agent","engine_id":"claude"}`)
			write("activation/aw_info.json", `{"model":"agent","engine_id":"claude"}`)
			write("activation/aw-prompts/prompt.txt", "prompt")
			write("usage/aw_info.json", `{"model":"agent","engine_id":"claude"}`)
			write("usage/aw_session.jsonl", "{\"type\":\"session.format\"}\n")
			write(filepath.Join(artifactName, "agent", "aw_info.json"), `{"model":"claude-sonnet-5","requested_model":"agent","engine_id":"claude","model_routing":{"status":"selected"}}`)
			write(filepath.Join(artifactName, "agent", "awf-routing-outcome.json"), `{"status":"selected","endpoint":"/v1/messages"}`)

			require.NoError(t, flattenDownloadedArtifacts(context.Background(), downloadArtifactsOptions{outputDir: runDir}))
			assert.Equal(t, "claude-sonnet-5", readAwInfoModel(t, filepath.Join(runDir, "aw_info.json")))
			assert.JSONEq(t, `{"status":"selected","endpoint":"/v1/messages"}`, readFlattenTestFile(t, filepath.Join(runDir, "agent", "awf-routing-outcome.json")))
			assert.Equal(t, filepath.Join(runDir, "agent", "aw_info.json"), findAwInfoPath(runDir))
			assert.FileExists(t, filepath.Join(runDir, "aw-prompts", "prompt.txt"))
			assert.FileExists(t, filepath.Join(runDir, "usage", "aw_session.jsonl"))
			assert.DirExists(t, filepath.Join(runDir, "agent"))
		})
	}
}

func TestFlattenDownloadedUsageOnlyAwInfo(t *testing.T) {
	t.Parallel()
	runDir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(runDir, "usage"), 0755))
	require.NoError(t, os.WriteFile(filepath.Join(runDir, "usage", "aw_info.json"), []byte(`{"model":"final-from-usage"}`), 0600))
	require.NoError(t, os.WriteFile(filepath.Join(runDir, "usage", "aw_session.jsonl"), []byte("{}\n"), 0600))
	require.NoError(t, flattenDownloadedArtifacts(context.Background(), downloadArtifactsOptions{
		outputDir: runDir, artifactFilter: []string{constants.UsageArtifactName.String()},
	}))
	assert.Equal(t, "final-from-usage", readAwInfoModel(t, filepath.Join(runDir, "aw_info.json")))
}

func readAwInfoModel(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	var info struct {
		Model string `json:"model"`
	}
	require.NoError(t, json.Unmarshal(data, &info))
	return info.Model
}

func readFlattenTestFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	return string(data)
}

func TestFlattenSingleFileArtifacts(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name            string
		setup           func(string) error
		expectedFiles   []string
		expectedDirs    []string
		unexpectedFiles []string
		unexpectedDirs  []string
	}{
		{
			name: "single file artifact gets flattened",
			setup: func(dir string) error {
				artifactDir := filepath.Join(dir, "my-artifact")
				if err := os.MkdirAll(artifactDir, 0755); err != nil {
					return err
				}
				return os.WriteFile(filepath.Join(artifactDir, "output.json"), []byte("test"), 0644)
			},
			expectedFiles:   []string{"output.json"},
			unexpectedDirs:  []string{"my-artifact"},
			unexpectedFiles: []string{"my-artifact/output.json"},
		},
		{
			name: "multi-file artifact not flattened",
			setup: func(dir string) error {
				artifactDir := filepath.Join(dir, "multi-artifact")
				if err := os.MkdirAll(artifactDir, 0755); err != nil {
					return err
				}
				if err := os.WriteFile(filepath.Join(artifactDir, "file1.txt"), []byte("test1"), 0644); err != nil {
					return err
				}
				return os.WriteFile(filepath.Join(artifactDir, "file2.txt"), []byte("test2"), 0644)
			},
			expectedDirs:    []string{"multi-artifact"},
			expectedFiles:   []string{"multi-artifact/file1.txt", "multi-artifact/file2.txt"},
			unexpectedFiles: []string{"file1.txt", "file2.txt"},
		},
		{
			name: "artifact with subdirectory not flattened",
			setup: func(dir string) error {
				artifactDir := filepath.Join(dir, "nested-artifact")
				if err := os.MkdirAll(filepath.Join(artifactDir, "subdir"), 0755); err != nil {
					return err
				}
				if err := os.WriteFile(filepath.Join(artifactDir, "file.txt"), []byte("test"), 0644); err != nil {
					return err
				}
				return os.WriteFile(filepath.Join(artifactDir, "subdir", "nested.txt"), []byte("test"), 0644)
			},
			expectedDirs:    []string{"nested-artifact"},
			expectedFiles:   []string{"nested-artifact/file.txt", "nested-artifact/subdir/nested.txt"},
			unexpectedFiles: []string{"file.txt"},
		},
		{
			name: "multiple single-file artifacts all get flattened",
			setup: func(dir string) error {
				for i := 1; i <= 3; i++ {
					artifactDir := filepath.Join(dir, fmt.Sprintf("artifact-%d", i))
					if err := os.MkdirAll(artifactDir, 0755); err != nil {
						return err
					}
					if err := os.WriteFile(filepath.Join(artifactDir, fmt.Sprintf("file%d.txt", i)), []byte("test"), 0644); err != nil {
						return err
					}
				}
				return nil
			},
			expectedFiles:  []string{"file1.txt", "file2.txt", "file3.txt"},
			unexpectedDirs: []string{"artifact-1", "artifact-2", "artifact-3"},
		},
		{
			name: "empty artifact directory not touched",
			setup: func(dir string) error {
				return os.MkdirAll(filepath.Join(dir, "empty-artifact"), 0755)
			},
			expectedDirs: []string{"empty-artifact"},
		},
		{
			name: "downloaded artifact markers are not flattened",
			setup: func(dir string) error {
				return markArtifactDownloaded(dir, string(ArtifactSetAll))
			},
			expectedDirs:  []string{downloadedArtifactsMarkerDir},
			expectedFiles: []string{downloadedArtifactsMarkerDir + "/all"},
		},
		{
			name: "regular files in output dir not affected",
			setup: func(dir string) error {
				// Create a regular file in output dir
				if err := os.WriteFile(filepath.Join(dir, "standalone.txt"), []byte("test"), 0644); err != nil {
					return err
				}
				// Create a single-file artifact
				artifactDir := filepath.Join(dir, "single-artifact")
				if err := os.MkdirAll(artifactDir, 0755); err != nil {
					return err
				}
				return os.WriteFile(filepath.Join(artifactDir, "artifact.json"), []byte("test"), 0644)
			},
			expectedFiles:  []string{"standalone.txt", "artifact.json"},
			unexpectedDirs: []string{"single-artifact"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			tmpDir := testutil.TempDir(t, "test-*")

			// Setup test structure
			if err := tt.setup(tmpDir); err != nil {
				t.Fatalf("Setup failed: %v", err)
			}

			// Run flatten function
			if err := flattenSingleFileArtifacts(tmpDir, true); err != nil {
				t.Fatalf("flattenSingleFileArtifacts failed: %v", err)
			}

			// Verify expected files exist
			for _, file := range tt.expectedFiles {
				path := filepath.Join(tmpDir, file)
				if _, err := os.Stat(path); os.IsNotExist(err) {
					t.Errorf("Expected file %s does not exist", file)
				}
			}

			// Verify expected directories exist
			for _, dir := range tt.expectedDirs {
				path := filepath.Join(tmpDir, dir)
				info, err := os.Stat(path)
				if os.IsNotExist(err) {
					t.Errorf("Expected directory %s does not exist", dir)
				} else if err == nil && !info.IsDir() {
					t.Errorf("Expected %s to be a directory", dir)
				}
			}

			// Verify unexpected files don't exist
			for _, file := range tt.unexpectedFiles {
				path := filepath.Join(tmpDir, file)
				if _, err := os.Stat(path); err == nil {
					t.Errorf("Unexpected file %s exists", file)
				}
			}

			// Verify unexpected directories don't exist
			for _, dir := range tt.unexpectedDirs {
				path := filepath.Join(tmpDir, dir)
				if _, err := os.Stat(path); err == nil {
					t.Errorf("Unexpected directory %s exists", dir)
				}
			}
		})
	}
}

func TestFlattenSingleFileArtifactsInvalidDirectory(t *testing.T) {
	t.Parallel()
	// Test with non-existent directory
	err := flattenSingleFileArtifacts("/nonexistent/directory", false)
	if err == nil {
		t.Error("Expected error for non-existent directory, got nil")
	}
}

func TestFlattenSingleFileArtifactsWithAuditFiles(t *testing.T) {
	t.Parallel()
	// Test that flattening works correctly for typical audit artifact files
	// This test uses unified agent structure
	tmpDir := testutil.TempDir(t, "test-*")

	// Create unified agent structure as it would be downloaded by gh run download
	// All single-file artifacts are now in agent/tmp/gh-aw/
	nestedPath := filepath.Join(tmpDir, "agent", "tmp", "gh-aw")
	if err := os.MkdirAll(nestedPath, 0755); err != nil {
		t.Fatalf("Failed to create agent directory: %v", err)
	}

	unifiedArtifacts := map[string]string{
		"aw_info.json":      `{"engine_id":"claude","workflow_name":"test"}`,
		"safe_output.jsonl": `{"action":"create_issue","title":"test"}`,
		"aw.patch":          "diff --git a/test.txt b/test.txt\n",
	}

	for filename, content := range unifiedArtifacts {
		fullPath := filepath.Join(nestedPath, filename)
		if err := os.WriteFile(fullPath, []byte(content), 0644); err != nil {
			t.Fatalf("Failed to write file %s: %v", filename, err)
		}
	}

	// Also create multi-file artifact directories (these remain separate)
	multiFileArtifacts := map[string]string{
		"agent_outputs/output1.txt":        "log output 1",
		"agent_outputs/output2.txt":        "log output 2",
		"agent_outputs/nested/subfile.txt": "nested file",
	}

	for path, content := range multiFileArtifacts {
		fullPath := filepath.Join(tmpDir, path)
		if err := os.MkdirAll(filepath.Dir(fullPath), 0755); err != nil {
			t.Fatalf("Failed to create directory for %s: %v", path, err)
		}
		if err := os.WriteFile(fullPath, []byte(content), 0644); err != nil {
			t.Fatalf("Failed to write file %s: %v", path, err)
		}
	}

	// Run flattening for unified artifact
	if err := flattenUnifiedArtifact(tmpDir, true); err != nil {
		t.Fatalf("flattenUnifiedArtifact failed: %v", err)
	}

	// Also run single file artifact flattening for any remaining separate artifacts
	if err := flattenSingleFileArtifacts(tmpDir, true); err != nil {
		t.Fatalf("flattenSingleFileArtifacts failed: %v", err)
	}

	// Verify single-file artifacts are flattened and findable by audit command
	auditExpectedFiles := []string{
		"aw_info.json",
		"safe_output.jsonl",
		"aw.patch",
	}

	for _, file := range auditExpectedFiles {
		path := filepath.Join(tmpDir, file)
		if _, err := os.Stat(path); os.IsNotExist(err) {
			t.Errorf("Audit expected file %s not found at top level after flattening", file)
		} else {
			// Verify file content is intact
			content, err := os.ReadFile(path)
			if err != nil {
				t.Errorf("Failed to read flattened file %s: %v", file, err)
			} else if len(content) == 0 {
				t.Errorf("Flattened file %s is empty", file)
			}
		}
	}

	// Verify multi-file artifact directory is preserved
	agentOutputsDir := filepath.Join(tmpDir, "agent_outputs")
	if info, err := os.Stat(agentOutputsDir); os.IsNotExist(err) {
		t.Error("agent_outputs directory should be preserved")
	} else if !info.IsDir() {
		t.Error("agent_outputs should be a directory")
	}

	// Verify files within multi-file artifact are intact
	multiFileArtifactFiles := []string{
		"agent_outputs/output1.txt",
		"agent_outputs/output2.txt",
		"agent_outputs/nested/subfile.txt",
	}

	for _, file := range multiFileArtifactFiles {
		path := filepath.Join(tmpDir, file)
		if _, err := os.Stat(path); os.IsNotExist(err) {
			t.Errorf("Multi-file artifact file %s should be preserved", file)
		}
	}

	// Verify agent directory is removed
	agentArtifactsDir := filepath.Join(tmpDir, "agent")
	if _, err := os.Stat(agentArtifactsDir); err == nil {
		t.Errorf("agent directory should be removed after flattening")
	}
}

func TestAuditCanFindFlattenedArtifacts(t *testing.T) {
	t.Parallel()
	// Simulate what the audit command does - check that it can find artifacts after flattening
	// This test uses unified agent structure
	tmpDir := testutil.TempDir(t, "test-*")

	// Create realistic unified artifact structure before flattening
	nestedPath := filepath.Join(tmpDir, "agent", "tmp", "gh-aw")
	if err := os.MkdirAll(nestedPath, 0755); err != nil {
		t.Fatalf("Setup failed: %v", err)
	}

	testArtifacts := map[string]string{
		"aw_info.json":      `{"engine_id":"claude","workflow_name":"github-mcp-tools-report","run_id":123456}`,
		"safe_output.jsonl": `{"action":"create_discussion","title":"GitHub MCP Tools Report"}`,
		"aw.patch":          "diff --git a/report.md b/report.md\nnew file mode 100644\n",
	}

	for filename, content := range testArtifacts {
		fullPath := filepath.Join(nestedPath, filename)
		if err := os.WriteFile(fullPath, []byte(content), 0644); err != nil {
			t.Fatalf("Setup failed: %v", err)
		}
	}

	// Flatten artifacts (this happens during download)
	if err := flattenUnifiedArtifact(tmpDir, false); err != nil {
		t.Fatalf("Flattening failed: %v", err)
	}

	// Simulate what generateAuditReport does - check for artifacts using filepath.Join(run.LogsPath, filename)
	artifactsToCheck := []struct {
		filename    string
		description string
	}{
		{"aw_info.json", "engine configuration"},
		{"safe_output.jsonl", "safe outputs"},
		{"aw.patch", "code changes"},
	}

	foundArtifacts := []string{}
	for _, artifact := range artifactsToCheck {
		artifactPath := filepath.Join(tmpDir, artifact.filename)
		if _, err := os.Stat(artifactPath); err == nil {
			foundArtifacts = append(foundArtifacts, fmt.Sprintf("%s (%s)", artifact.filename, artifact.description))
		}
	}

	// Verify all expected artifacts were found
	if len(foundArtifacts) != len(artifactsToCheck) {
		t.Errorf("Expected to find %d artifacts, but found %d", len(artifactsToCheck), len(foundArtifacts))
		t.Logf("Found artifacts: %v", foundArtifacts)
	}

	// Verify we can read aw_info.json directly (simulates parseAwInfo)
	awInfoPath := filepath.Join(tmpDir, "aw_info.json")
	data, err := os.ReadFile(awInfoPath)
	if err != nil {
		t.Fatalf("Failed to read aw_info.json after flattening: %v", err)
	}

	// Verify content is valid
	if !strings.Contains(string(data), "engine_id") {
		t.Error("aw_info.json content is corrupted after flattening")
	}
}

func TestFlattenUnifiedArtifact(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name            string
		setup           func(string) error
		expectedFiles   []string
		expectedDirs    []string
		unexpectedFiles []string
		unexpectedDirs  []string
	}{
		{
			name: "unified artifact with nested structure gets flattened",
			setup: func(dir string) error {
				// Create the structure: agent/tmp/gh-aw/...
				nestedPath := filepath.Join(dir, "agent", "tmp", "gh-aw")
				if err := os.MkdirAll(nestedPath, 0755); err != nil {
					return err
				}

				// Create test files
				if err := os.WriteFile(filepath.Join(nestedPath, "aw_info.json"), []byte("test"), 0644); err != nil {
					return err
				}

				// Create subdirectories with files
				promptDir := filepath.Join(nestedPath, "aw-prompts")
				if err := os.MkdirAll(promptDir, 0755); err != nil {
					return err
				}
				if err := os.WriteFile(filepath.Join(promptDir, "prompt.txt"), []byte("test"), 0644); err != nil {
					return err
				}

				mcpLogsDir := filepath.Join(nestedPath, "mcp-logs")
				if err := os.MkdirAll(mcpLogsDir, 0755); err != nil {
					return err
				}
				return os.WriteFile(filepath.Join(mcpLogsDir, "log.txt"), []byte("test"), 0644)
			},
			expectedFiles: []string{
				"aw_info.json",
				"aw-prompts/prompt.txt",
				"mcp-logs/log.txt",
			},
			expectedDirs: []string{
				"aw-prompts",
				"mcp-logs",
			},
			unexpectedDirs: []string{"agent", "tmp", "gh-aw"},
			unexpectedFiles: []string{
				"agent/tmp/gh-aw/aw_info.json",
				"tmp/gh-aw/aw_info.json",
			},
		},
		{
			name: "no agent directory - no-op",
			setup: func(dir string) error {
				// Create a regular file structure without agent
				return os.WriteFile(filepath.Join(dir, "regular.txt"), []byte("test"), 0644)
			},
			expectedFiles: []string{"regular.txt"},
		},
		{
			name: "new 'agent' artifact directory gets flattened",
			setup: func(dir string) error {
				// Create the new structure: agent/ (files directly, no tmp/gh-aw prefix)
				artifactDir := filepath.Join(dir, "agent")
				if err := os.MkdirAll(artifactDir, 0755); err != nil {
					return err
				}
				// agent_output.json at root of artifact
				if err := os.WriteFile(filepath.Join(artifactDir, "agent_output.json"), []byte("{}"), 0644); err != nil {
					return err
				}
				// safeoutputs.jsonl at root
				if err := os.WriteFile(filepath.Join(artifactDir, "safeoutputs.jsonl"), []byte("{}"), 0644); err != nil {
					return err
				}
				// mcp-logs subdirectory
				mcpLogsDir := filepath.Join(artifactDir, "mcp-logs")
				if err := os.MkdirAll(mcpLogsDir, 0755); err != nil {
					return err
				}
				return os.WriteFile(filepath.Join(mcpLogsDir, "log.txt"), []byte("log"), 0644)
			},
			expectedFiles:   []string{"agent_output.json", "safeoutputs.jsonl", "mcp-logs/log.txt"},
			expectedDirs:    []string{"mcp-logs"},
			unexpectedDirs:  []string{"agent"},
			unexpectedFiles: []string{"agent/agent_output.json"},
		},
		{
			name: "agent without tmp/gh-aw structure - flatten directly",
			setup: func(dir string) error {
				// Create agent with new structure (files directly in agent/)
				artifactDir := filepath.Join(dir, "agent")
				if err := os.MkdirAll(artifactDir, 0755); err != nil {
					return err
				}
				// Create file directly in agent (new structure)
				if err := os.WriteFile(filepath.Join(artifactDir, "file.txt"), []byte("test"), 0644); err != nil {
					return err
				}
				// Create a subdirectory with a file
				subDir := filepath.Join(artifactDir, "subdir")
				if err := os.MkdirAll(subDir, 0755); err != nil {
					return err
				}
				return os.WriteFile(filepath.Join(subDir, "nested.txt"), []byte("nested"), 0644)
			},
			expectedDirs:    []string{"subdir"},
			expectedFiles:   []string{"file.txt", "subdir/nested.txt"},
			unexpectedFiles: []string{"agent/file.txt"},
		},
		{
			name: "new 'agent' artifact takes precedence over legacy 'agent-artifacts'",
			setup: func(dir string) error {
				// Create BOTH: new 'agent' and old 'agent-artifacts'
				// Only 'agent' should be flattened; 'agent-artifacts' should remain untouched
				agentDir := filepath.Join(dir, "agent")
				if err := os.MkdirAll(agentDir, 0755); err != nil {
					return err
				}
				if err := os.WriteFile(filepath.Join(agentDir, "new-file.txt"), []byte("new"), 0644); err != nil {
					return err
				}
				legacyDir := filepath.Join(dir, "agent-artifacts")
				if err := os.MkdirAll(legacyDir, 0755); err != nil {
					return err
				}
				return os.WriteFile(filepath.Join(legacyDir, "old-file.txt"), []byte("old"), 0644)
			},
			expectedFiles:   []string{"new-file.txt"},
			unexpectedFiles: []string{"agent/new-file.txt"},
			// agent-artifacts is NOT flattened when agent/ is present
			unexpectedDirs: []string{"agent"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			tmpDir := testutil.TempDir(t, "test-flatten-unified-*")

			// Setup test structure
			if err := tt.setup(tmpDir); err != nil {
				t.Fatalf("Setup failed: %v", err)
			}

			// Run flattening
			if err := flattenUnifiedArtifact(tmpDir, true); err != nil {
				t.Fatalf("flattenUnifiedArtifact failed: %v", err)
			}

			// Verify expected files exist
			for _, file := range tt.expectedFiles {
				path := filepath.Join(tmpDir, file)
				info, err := os.Stat(path)
				if err != nil {
					t.Errorf("Expected file %s does not exist: %v", file, err)
				} else if info.IsDir() {
					t.Errorf("Expected %s to be a file, but it's a directory", file)
				}
			}

			// Verify expected directories exist
			for _, dir := range tt.expectedDirs {
				path := filepath.Join(tmpDir, dir)
				info, err := os.Stat(path)
				if err != nil {
					t.Errorf("Expected directory %s does not exist: %v", dir, err)
				} else if !info.IsDir() {
					t.Errorf("Expected %s to be a directory", dir)
				}
			}

			// Verify unexpected files don't exist
			for _, file := range tt.unexpectedFiles {
				path := filepath.Join(tmpDir, file)
				if _, err := os.Stat(path); err == nil {
					t.Errorf("Unexpected file %s exists", file)
				}
			}

			// Verify unexpected directories don't exist
			for _, dir := range tt.unexpectedDirs {
				path := filepath.Join(tmpDir, dir)
				if _, err := os.Stat(path); err == nil {
					t.Errorf("Unexpected directory %s exists", dir)
				}
			}
		})
	}
}

func TestFlattenAgentOutputFallbackArtifact(t *testing.T) {
	t.Parallel()
	t.Run("fallback artifact gets flattened", func(t *testing.T) {
		t.Parallel()
		tmpDir := testutil.TempDir(t, "test-flatten-agent-output-fallback-*")
		fallbackDir := filepath.Join(tmpDir, constants.AgentOutputFallbackArtifactName.String())
		require.NoError(t, os.MkdirAll(fallbackDir, 0755))
		require.NoError(t, os.WriteFile(filepath.Join(fallbackDir, constants.AgentOutputFilename.String()), []byte(`{"items":[]}`), 0644))
		require.NoError(t, os.WriteFile(filepath.Join(fallbackDir, constants.SafeOutputsFilename.String()), []byte("{}\n"), 0644))

		require.NoError(t, flattenAgentOutputFallbackArtifact(tmpDir, true))

		assert.FileExists(t, filepath.Join(tmpDir, constants.AgentOutputFilename.String()))
		assert.FileExists(t, filepath.Join(tmpDir, constants.SafeOutputsFilename.String()))
		assert.NoDirExists(t, fallbackDir)
	})

	t.Run("primary files win when fallback duplicates existing files", func(t *testing.T) {
		t.Parallel()
		tmpDir := testutil.TempDir(t, "test-flatten-agent-output-fallback-*")
		require.NoError(t, os.WriteFile(filepath.Join(tmpDir, constants.AgentOutputFilename.String()), []byte("primary"), 0644))

		fallbackDir := filepath.Join(tmpDir, constants.AgentOutputFallbackArtifactName.String())
		require.NoError(t, os.MkdirAll(fallbackDir, 0755))
		require.NoError(t, os.WriteFile(filepath.Join(fallbackDir, constants.AgentOutputFilename.String()), []byte("fallback"), 0644))
		require.NoError(t, os.WriteFile(filepath.Join(fallbackDir, constants.SafeOutputsFilename.String()), []byte("{}\n"), 0644))

		require.NoError(t, flattenAgentOutputFallbackArtifact(tmpDir, true))

		content, err := os.ReadFile(filepath.Join(tmpDir, constants.AgentOutputFilename.String()))
		require.NoError(t, err)
		assert.Equal(t, "primary", string(content))
		assert.FileExists(t, filepath.Join(tmpDir, constants.SafeOutputsFilename.String()))
		assert.NoDirExists(t, fallbackDir)
	})
}

// TestFlattenArtifactTree tests the shared flatten helper directly.

func TestFlattenArtifactTreeBasic(t *testing.T) {
	t.Parallel()
	outputDir := t.TempDir()
	artifactDir := filepath.Join(outputDir, "myartifact")
	require.NoError(t, os.MkdirAll(artifactDir, 0750), "setup: create artifactDir")
	require.NoError(t, os.WriteFile(filepath.Join(artifactDir, "file.txt"), []byte("hello"), 0600), "setup: write file")

	err := flattenArtifactTree(artifactDir, artifactDir, outputDir, "test artifact", false)
	require.NoError(t, err, "flattenArtifactTree should succeed")

	content, err := os.ReadFile(filepath.Join(outputDir, "file.txt"))
	require.NoError(t, err, "file should be moved to outputDir")
	assert.Equal(t, "hello", string(content), "file content should be preserved")

	_, err = os.Stat(artifactDir)
	assert.True(t, os.IsNotExist(err), "artifactDir should be removed after flattening")
}

func TestFlattenArtifactTreeNestedDirs(t *testing.T) {
	t.Parallel()
	outputDir := t.TempDir()
	artifactDir := filepath.Join(outputDir, "artifact")
	nestedDir := filepath.Join(artifactDir, "subdir", "deeper")
	require.NoError(t, os.MkdirAll(nestedDir, 0750), "setup: create nested dirs")
	require.NoError(t, os.WriteFile(filepath.Join(nestedDir, "nested.txt"), []byte("nested"), 0600), "setup: write nested file")

	err := flattenArtifactTree(artifactDir, artifactDir, outputDir, "nested artifact", false)
	require.NoError(t, err, "flattenArtifactTree should succeed with nested dirs")

	content, err := os.ReadFile(filepath.Join(outputDir, "subdir", "deeper", "nested.txt"))
	require.NoError(t, err, "nested file should be moved preserving relative path")
	assert.Equal(t, "nested", string(content), "nested file content should be preserved")

	_, err = os.Stat(artifactDir)
	assert.True(t, os.IsNotExist(err), "artifactDir should be removed")
}

func TestFlattenArtifactTreeDifferentSourceAndArtifactDir(t *testing.T) {
	t.Parallel()
	// Covers the old-structure unified artifact case where sourceDir is a subdirectory of artifactDir.
	outputDir := t.TempDir()
	artifactDir := filepath.Join(outputDir, "agent")
	sourceDir := filepath.Join(artifactDir, "tmp", "gh-aw")
	require.NoError(t, os.MkdirAll(sourceDir, 0750), "setup: create old-structure sourceDir")
	require.NoError(t, os.WriteFile(filepath.Join(sourceDir, "session.json"), []byte(`{}`), 0600), "setup: write file")

	err := flattenArtifactTree(sourceDir, artifactDir, outputDir, "unified agent artifact", false)
	require.NoError(t, err, "flattenArtifactTree should succeed")

	content, err := os.ReadFile(filepath.Join(outputDir, "session.json"))
	require.NoError(t, err, "file should be moved to outputDir relative to sourceDir")
	assert.Equal(t, `{}`, string(content), "file content should be preserved")

	_, err = os.Stat(artifactDir)
	assert.True(t, os.IsNotExist(err), "entire artifactDir should be removed, not just sourceDir")
}

func TestFlattenArtifactTreeEmptySource(t *testing.T) {
	t.Parallel()
	outputDir := t.TempDir()
	artifactDir := filepath.Join(outputDir, "empty-artifact")
	require.NoError(t, os.MkdirAll(artifactDir, 0750), "setup: create empty artifactDir")

	err := flattenArtifactTree(artifactDir, artifactDir, outputDir, "empty artifact", false)
	require.NoError(t, err, "flattenArtifactTree should succeed on empty dir")

	_, err = os.Stat(artifactDir)
	assert.True(t, os.IsNotExist(err), "empty artifactDir should be removed")
}

func TestFlattenArtifactTreeMultipleFiles(t *testing.T) {
	t.Parallel()
	outputDir := t.TempDir()
	artifactDir := filepath.Join(outputDir, "multi")
	require.NoError(t, os.MkdirAll(artifactDir, 0750), "setup: create artifactDir")

	files := map[string]string{
		"a.txt": "content-a",
		"b.txt": "content-b",
		"c.txt": "content-c",
	}
	for name, body := range files {
		require.NoError(t, os.WriteFile(filepath.Join(artifactDir, name), []byte(body), 0600), "setup: write "+name)
	}

	err := flattenArtifactTree(artifactDir, artifactDir, outputDir, "multi artifact", false)
	require.NoError(t, err, "flattenArtifactTree should succeed")

	for name, expected := range files {
		content, err := os.ReadFile(filepath.Join(outputDir, name))
		require.NoError(t, err, "file %s should exist in outputDir", name)
		assert.Equal(t, expected, string(content), "content of %s should be preserved", name)
	}
}

func TestFlattenArtifactTreeVerboseMode(t *testing.T) {
	t.Parallel()
	outputDir := t.TempDir()
	artifactDir := filepath.Join(outputDir, "verbose-artifact")
	require.NoError(t, os.MkdirAll(artifactDir, 0750), "setup: create artifactDir")
	require.NoError(t, os.WriteFile(filepath.Join(artifactDir, "log.txt"), []byte("log"), 0600), "setup: write file")

	err := flattenArtifactTree(artifactDir, artifactDir, outputDir, "verbose artifact", true)
	require.NoError(t, err, "flattenArtifactTree should succeed in verbose mode")

	_, err = os.ReadFile(filepath.Join(outputDir, "log.txt"))
	require.NoError(t, err, "file should be moved in verbose mode too")
}
