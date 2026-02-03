//go:build !integration

package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/github/gh-aw/pkg/testutil"
)

func TestParseAwInfo(t *testing.T) {
	// Create a temporary directory for test files
	tmpDir := testutil.TempDir(t, "test-*")

	t.Run("staged true as boolean", func(t *testing.T) {
		// Create aw_info.json with staged: true as boolean
		infoData := map[string]any{
			"engine_id":     "claude",
			"staged":        true,
			"workflow_name": "test-workflow",
		}
		infoBytes, _ := json.Marshal(infoData)
		infoPath := filepath.Join(tmpDir, "aw_info_staged_true.json")
		err := os.WriteFile(infoPath, infoBytes, 0644)
		if err != nil {
			t.Fatalf("Failed to write test file: %v", err)
		}

		info, err := parseAwInfo(infoPath, false)
		if err != nil {
			t.Fatalf("parseAwInfo failed: %v", err)
		}
		if !info.Staged {
			t.Error("Expected parseAwInfo to return staged: true for staged: true")
		}
		if info.EngineID != "claude" {
			t.Errorf("Expected engine_id to be 'claude', got '%s'", info.EngineID)
		}
		if info.WorkflowName != "test-workflow" {
			t.Errorf("Expected workflow_name to be 'test-workflow', got '%s'", info.WorkflowName)
		}
	})

	t.Run("staged false as boolean", func(t *testing.T) {
		// Create aw_info.json with staged: false as boolean
		infoData := map[string]any{
			"engine_id":     "claude",
			"staged":        false,
			"workflow_name": "test-workflow",
		}
		infoBytes, _ := json.Marshal(infoData)
		infoPath := filepath.Join(tmpDir, "aw_info_staged_false.json")
		err := os.WriteFile(infoPath, infoBytes, 0644)
		if err != nil {
			t.Fatalf("Failed to write test file: %v", err)
		}

		info, err := parseAwInfo(infoPath, false)
		if err != nil {
			t.Fatalf("parseAwInfo failed: %v", err)
		}
		if info.Staged {
			t.Error("Expected parseAwInfo to return staged: false for staged: false")
		}
	})

	t.Run("no staged field", func(t *testing.T) {
		// Create aw_info.json without staged field
		infoData := map[string]any{
			"engine_id":     "claude",
			"workflow_name": "test-workflow",
		}
		infoBytes, _ := json.Marshal(infoData)
		infoPath := filepath.Join(tmpDir, "aw_info_no_staged.json")
		err := os.WriteFile(infoPath, infoBytes, 0644)
		if err != nil {
			t.Fatalf("Failed to write test file: %v", err)
		}

		info, err := parseAwInfo(infoPath, false)
		if err != nil {
			t.Fatalf("parseAwInfo failed: %v", err)
		}
		if info.Staged {
			t.Error("Expected parseAwInfo to return staged: false when staged field is missing")
		}
	})

	t.Run("missing file", func(t *testing.T) {
		// Test with non-existent file
		nonExistentPath := filepath.Join(tmpDir, "nonexistent.json")
		_, err := parseAwInfo(nonExistentPath, false)
		if err == nil {
			t.Error("Expected parseAwInfo to return error for missing file")
		}
	})

	t.Run("invalid json", func(t *testing.T) {
		// Create invalid JSON file
		invalidPath := filepath.Join(tmpDir, "invalid.json")
		err := os.WriteFile(invalidPath, []byte("invalid json content"), 0644)
		if err != nil {
			t.Fatalf("Failed to write test file: %v", err)
		}

		_, err = parseAwInfo(invalidPath, false)
		if err == nil {
			t.Error("Expected parseAwInfo to return error for invalid JSON")
		}
	})

	t.Run("staged as directory with nested file", func(t *testing.T) {
		// Create a directory with the same name and nested aw_info.json
		dirPath := filepath.Join(tmpDir, "aw_info_dir")
		err := os.MkdirAll(dirPath, 0755)
		if err != nil {
			t.Fatalf("Failed to create directory: %v", err)
		}

		// Create nested aw_info.json with staged: true
		infoData := map[string]any{
			"engine_id":     "claude",
			"staged":        true,
			"workflow_name": "test-workflow",
		}
		infoBytes, _ := json.Marshal(infoData)
		nestedPath := filepath.Join(dirPath, "aw_info.json")
		err = os.WriteFile(nestedPath, infoBytes, 0644)
		if err != nil {
			t.Fatalf("Failed to write nested test file: %v", err)
		}

		info, err := parseAwInfo(dirPath, false)
		if err != nil {
			t.Fatalf("parseAwInfo failed: %v", err)
		}
		if !info.Staged {
			t.Error("Expected parseAwInfo to return staged: true for nested staged: true")
		}
	})

	t.Run("complete aw_info structure", func(t *testing.T) {
		// Test parsing all fields in aw_info.json
		infoData := map[string]any{
			"engine_id":     "claude",
			"engine_name":   "Claude AI",
			"model":         "claude-3-5-sonnet-20241022",
			"version":       "1.0",
			"workflow_name": "test-workflow",
			"staged":        true,
			"created_at":    "2024-01-15T10:30:00Z",
			"run_id":        12345,
			"run_number":    67,
			"repository":    "owner/repo",
		}
		infoBytes, _ := json.Marshal(infoData)
		infoPath := filepath.Join(tmpDir, "aw_info_complete.json")
		err := os.WriteFile(infoPath, infoBytes, 0644)
		if err != nil {
			t.Fatalf("Failed to write test file: %v", err)
		}

		info, err := parseAwInfo(infoPath, false)
		if err != nil {
			t.Fatalf("parseAwInfo failed: %v", err)
		}

		if info.EngineID != "claude" {
			t.Errorf("Expected engine_id to be 'claude', got '%s'", info.EngineID)
		}
		if info.EngineName != "Claude AI" {
			t.Errorf("Expected engine_name to be 'Claude AI', got '%s'", info.EngineName)
		}
		if info.Model != "claude-3-5-sonnet-20241022" {
			t.Errorf("Expected model to be 'claude-3-5-sonnet-20241022', got '%s'", info.Model)
		}
		if info.Version != "1.0" {
			t.Errorf("Expected version to be '1.0', got '%s'", info.Version)
		}
		if info.WorkflowName != "test-workflow" {
			t.Errorf("Expected workflow_name to be 'test-workflow', got '%s'", info.WorkflowName)
		}
		if !info.Staged {
			t.Error("Expected staged to be true")
		}
		if info.CreatedAt != "2024-01-15T10:30:00Z" {
			t.Errorf("Expected created_at to be '2024-01-15T10:30:00Z', got '%s'", info.CreatedAt)
		}
		if info.Repository != "owner/repo" {
			t.Errorf("Expected repository to be 'owner/repo', got '%s'", info.Repository)
		}
	})
}
