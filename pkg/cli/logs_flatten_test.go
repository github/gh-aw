//go:build !integration

package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/testutil"
)

// Tests for artifact unfold rule implementation
// Unfold rule: If an artifact download folder contains a single file, move the file to root and delete the folder

func TestFlattenSingleFileArtifacts(t *testing.T) {
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
	// Test with non-existent directory
	err := flattenSingleFileArtifacts("/nonexistent/directory", false)
	if err == nil {
		t.Error("Expected error for non-existent directory, got nil")
	}
}

func TestFlattenSingleFileArtifactsWithAuditFiles(t *testing.T) {
	// Test that flattening works correctly for typical audit artifact files
	// This test uses unified agent-artifacts structure
	tmpDir := testutil.TempDir(t, "test-*")

	// Create unified agent-artifacts structure as it would be downloaded by gh run download
	// All single-file artifacts are now in agent-artifacts/tmp/gh-aw/
	nestedPath := filepath.Join(tmpDir, "agent-artifacts", "tmp", "gh-aw")
	if err := os.MkdirAll(nestedPath, 0755); err != nil {
		t.Fatalf("Failed to create agent-artifacts directory: %v", err)
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

	// Verify agent-artifacts directory is removed
	agentArtifactsDir := filepath.Join(tmpDir, "agent-artifacts")
	if _, err := os.Stat(agentArtifactsDir); err == nil {
		t.Errorf("agent-artifacts directory should be removed after flattening")
	}
}

func TestAuditCanFindFlattenedArtifacts(t *testing.T) {
	// Simulate what the audit command does - check that it can find artifacts after flattening
	// This test uses unified agent-artifacts structure
	tmpDir := testutil.TempDir(t, "test-*")

	// Create realistic unified artifact structure before flattening
	nestedPath := filepath.Join(tmpDir, "agent-artifacts", "tmp", "gh-aw")
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
				// Create the structure: agent-artifacts/tmp/gh-aw/...
				nestedPath := filepath.Join(dir, "agent-artifacts", "tmp", "gh-aw")
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
			unexpectedDirs: []string{"agent-artifacts", "tmp", "gh-aw"},
			unexpectedFiles: []string{
				"agent-artifacts/tmp/gh-aw/aw_info.json",
				"tmp/gh-aw/aw_info.json",
			},
		},
		{
			name: "no agent-artifacts directory - no-op",
			setup: func(dir string) error {
				// Create a regular file structure without agent-artifacts
				return os.WriteFile(filepath.Join(dir, "regular.txt"), []byte("test"), 0644)
			},
			expectedFiles: []string{"regular.txt"},
		},
		{
			name: "agent-artifacts without tmp/gh-aw structure - flatten directly",
			setup: func(dir string) error {
				// Create agent-artifacts with new structure (files directly in agent-artifacts/)
				artifactDir := filepath.Join(dir, "agent-artifacts")
				if err := os.MkdirAll(artifactDir, 0755); err != nil {
					return err
				}
				// Create file directly in agent-artifacts (new structure)
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
			unexpectedFiles: []string{"agent-artifacts/file.txt"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
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
