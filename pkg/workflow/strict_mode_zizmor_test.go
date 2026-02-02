//go:build !integration

package workflow

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/testutil"
)

func TestStrictModeWithZizmor(t *testing.T) {
	// Note: This test documents the expected behavior but doesn't actually run zizmor
	// since that would require Docker. The actual zizmor integration is tested in
	// pkg/cli/zizmor_test.go
	tests := []struct {
		name        string
		content     string
		expectError bool
		errorMsg    string
	}{
		{
			name: "strict mode should be compatible with zizmor flag",
			content: `---
on: push
strict: true
permissions:
  contents: read
  issues: read
  pull-requests: read
engine: copilot
network:
  allowed:
    - "api.example.com"
---

# Test Workflow`,
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := testutil.TempDir(t, "strict-zizmor-test")

			testFile := filepath.Join(tmpDir, "test-workflow.md")
			if err := os.WriteFile(testFile, []byte(tt.content), 0644); err != nil {
				t.Fatal(err)
			}

			compiler := NewCompiler()
			compiler.SetStrictMode(true)
			err := compiler.CompileWorkflow(testFile)

			if tt.expectError && err == nil {
				t.Error("Expected compilation to fail but it succeeded")
			} else if !tt.expectError && err != nil {
				t.Errorf("Expected compilation to succeed but it failed: %v", err)
			} else if tt.expectError && err != nil && tt.errorMsg != "" {
				if !strings.Contains(err.Error(), tt.errorMsg) {
					t.Errorf("Expected error containing '%s', got '%s'", tt.errorMsg, err.Error())
				}
			}
		})
	}
}
