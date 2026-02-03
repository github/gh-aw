//go:build !integration

package cli

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/github/gh-aw/pkg/testutil"
)

// TestArtifactNamingBackwardCompatibility tests that both old and new artifact
// directory names are correctly flattened to the expected file names
func TestArtifactNamingBackwardCompatibility(t *testing.T) {
	tests := []struct {
		name               string
		artifactDirName    string
		fileNameInArtifact string
		expectedFileName   string
	}{
		// Old artifact names (backward compatibility - for workflows generated before v5 changes)
		{
			name:               "old aw_info artifact",
			artifactDirName:    "aw_info",
			fileNameInArtifact: "aw_info.json",
			expectedFileName:   "aw_info.json",
		},
		{
			name:               "old safe_output artifact",
			artifactDirName:    "safe_output",
			fileNameInArtifact: "safe_output.jsonl",
			expectedFileName:   "safe_output.jsonl",
		},
		{
			name:               "old agent_output artifact",
			artifactDirName:    "agent_output",
			fileNameInArtifact: "agent_output.json",
			expectedFileName:   "agent_output.json",
		},
		{
			name:               "old prompt artifact",
			artifactDirName:    "prompt",
			fileNameInArtifact: "prompt.txt",
			expectedFileName:   "prompt.txt",
		},
		// New artifact names (forward compatibility - for workflows generated with upload-artifact@v5 naming)
		{
			name:               "new aw-info artifact",
			artifactDirName:    "aw-info",
			fileNameInArtifact: "aw_info.json",
			expectedFileName:   "aw_info.json",
		},
		{
			name:               "new safe-output artifact",
			artifactDirName:    "safe-output",
			fileNameInArtifact: "safe_output.jsonl",
			expectedFileName:   "safe_output.jsonl",
		},
		{
			name:               "new agent-output artifact",
			artifactDirName:    "agent-output",
			fileNameInArtifact: "agent_output.json",
			expectedFileName:   "agent_output.json",
		},
		{
			name:               "new prompt artifact (unchanged)",
			artifactDirName:    "prompt",
			fileNameInArtifact: "prompt.txt",
			expectedFileName:   "prompt.txt",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := testutil.TempDir(t, "test-*")

			// Create artifact directory structure as it would be downloaded by gh run download
			artifactDir := filepath.Join(tmpDir, tt.artifactDirName)
			if err := os.MkdirAll(artifactDir, 0755); err != nil {
				t.Fatalf("Failed to create artifact directory: %v", err)
			}

			// Create file inside artifact directory
			artifactFile := filepath.Join(artifactDir, tt.fileNameInArtifact)
			if err := os.WriteFile(artifactFile, []byte("test content"), 0644); err != nil {
				t.Fatalf("Failed to create artifact file: %v", err)
			}

			// Run flatten (this is what happens during artifact download)
			if err := flattenSingleFileArtifacts(tmpDir, false); err != nil {
				t.Fatalf("flattenSingleFileArtifacts failed: %v", err)
			}

			// Verify expected file exists at root level (where audit/logs commands expect it)
			expectedPath := filepath.Join(tmpDir, tt.expectedFileName)
			if _, err := os.Stat(expectedPath); os.IsNotExist(err) {
				t.Errorf("Expected file %s not found after flattening", tt.expectedFileName)
			}

			// Verify artifact directory was removed
			if _, err := os.Stat(artifactDir); err == nil {
				t.Errorf("Artifact directory %s should be removed after flattening", tt.artifactDirName)
			}

			// Verify content is intact
			content, err := os.ReadFile(expectedPath)
			if err != nil {
				t.Errorf("Failed to read flattened file: %v", err)
			} else if string(content) != "test content" {
				t.Errorf("File content corrupted after flattening")
			}
		})
	}
}

// TestAuditCommandFindsNewArtifacts verifies that the audit command can find artifacts
// with both old and new naming schemes after flattening
func TestAuditCommandFindsNewArtifacts(t *testing.T) {
	// Simulate downloading new artifacts with upload-artifact@v5 naming
	tmpDir := testutil.TempDir(t, "test-*")

	// Create artifact structure as it would be downloaded by gh run download (new naming)
	newArtifacts := map[string]string{
		"aw-info/aw_info.json":            `{"engine_id":"claude","workflow_name":"test","run_id":123456}`,
		"safe-output/safe_output.jsonl":   `{"action":"create_issue","title":"test"}`,
		"agent-output/agent_output.json":  `{"safe_outputs":[]}`,
		"prompt/prompt.txt":               "Test prompt",
		"agent-stdio-log/agent-stdio.log": "Agent logs",
	}

	for path, content := range newArtifacts {
		fullPath := filepath.Join(tmpDir, path)
		if err := os.MkdirAll(filepath.Dir(fullPath), 0755); err != nil {
			t.Fatalf("Failed to create directory for %s: %v", path, err)
		}
		if err := os.WriteFile(fullPath, []byte(content), 0644); err != nil {
			t.Fatalf("Failed to write file %s: %v", path, err)
		}
	}

	// Run flattening (this happens during download)
	if err := flattenSingleFileArtifacts(tmpDir, false); err != nil {
		t.Fatalf("flattenSingleFileArtifacts failed: %v", err)
	}

	// Verify audit command can find all expected files by their standard names
	expectedFiles := []string{
		"aw_info.json",
		"safe_output.jsonl",
		"agent_output.json",
		"prompt.txt",
		"agent-stdio.log",
	}

	for _, filename := range expectedFiles {
		path := filepath.Join(tmpDir, filename)
		if _, err := os.Stat(path); os.IsNotExist(err) {
			t.Errorf("Audit command would fail to find %s after flattening", filename)
		}
	}

	// Verify artifact directories were removed
	removedDirs := []string{"aw-info", "safe-output", "agent-output", "prompt", "agent-stdio-log"}
	for _, dir := range removedDirs {
		path := filepath.Join(tmpDir, dir)
		if _, err := os.Stat(path); err == nil {
			t.Errorf("Artifact directory %s should be removed after flattening", dir)
		}
	}
}
