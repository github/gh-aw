//go:build !integration

package workflow

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/testutil"
)

func TestMCPLogsUpload(t *testing.T) {
	// Create a temporary directory for the test
	tmpDir := testutil.TempDir(t, "test-*")

	// Create a test markdown file with Playwright tool configuration
	testMarkdown := `---
on:
  workflow_dispatch:
tools:
  playwright:
    allowed_domains: ["example.com"]
engine: claude
---

# Test MCP Logs Upload

This is a test workflow to validate MCP logs upload generation.

Please navigate to example.com and take a screenshot.
`

	// Write the test file
	mdFile := filepath.Join(tmpDir, "test-mcp-logs.md")
	if err := os.WriteFile(mdFile, []byte(testMarkdown), 0644); err != nil {
		t.Fatalf("Failed to write test markdown file: %v", err)
	}

	// Initialize compiler
	compiler := NewCompiler()

	// Compile the workflow
	err := compiler.CompileWorkflow(mdFile)
	if err != nil {
		t.Fatalf("Failed to compile workflow: %v", err)
	}

	// Read the generated lock file
	lockFile := filepath.Join(tmpDir, "test-mcp-logs.lock.yml")
	lockContent, err := os.ReadFile(lockFile)
	if err != nil {
		t.Fatalf("Failed to read lock file: %v", err)
	}

	lockContentStr := string(lockContent)

	// Verify Playwright MCP configuration uses official Docker image
	if !strings.Contains(lockContentStr, "mcr.microsoft.com/playwright/mcp") {
		t.Error("Expected Playwright MCP configuration to include official Docker image 'mcr.microsoft.com/playwright/mcp'")
	}

	// Verify MCP logs are uploaded via the unified artifact upload
	if !strings.Contains(lockContentStr, "- name: Upload agent artifacts") {
		t.Error("Expected 'Upload agent artifacts' step to be in generated workflow")
	}

	// Verify the upload step uses actions/upload-artifact@b7c566a772e6b6bfb58ed0dc250532a479d7789f
	if !strings.Contains(lockContentStr, "uses: actions/upload-artifact@b7c566a772e6b6bfb58ed0dc250532a479d7789f") {
		t.Error("Expected upload-artifact action to be used for artifact upload step")
	}

	// Verify the MCP logs path is included in the unified upload
	if !strings.Contains(lockContentStr, "/tmp/gh-aw/mcp-logs/") {
		t.Error("Expected artifact path '/tmp/gh-aw/mcp-logs/' in unified upload")
	}

	// Verify the upload step has 'if-no-files-found: ignore' condition
	if !strings.Contains(lockContentStr, "if-no-files-found: ignore") {
		t.Error("Expected 'if-no-files-found: ignore' in upload step")
	}

	// Verify the upload step has 'if: always()' condition
	uploadArtifactsIndex := strings.Index(lockContentStr, "- name: Upload agent artifacts")
	if uploadArtifactsIndex == -1 {
		t.Fatal("Upload agent artifacts step not found")
	}

	// Find the next step after upload agent artifacts step
	nextUploadStart := uploadArtifactsIndex + len("- name: Upload agent artifacts")
	uploadStepEnd := strings.Index(lockContentStr[nextUploadStart:], "- name:")
	if uploadStepEnd == -1 {
		uploadStepEnd = len(lockContentStr) - nextUploadStart
	}
	uploadArtifactsStep := lockContentStr[uploadArtifactsIndex : nextUploadStart+uploadStepEnd]

	if !strings.Contains(uploadArtifactsStep, "if: always()") {
		t.Error("Expected upload agent artifacts step to have 'if: always()' condition")
	}

	// Verify step ordering: unified artifact upload should be after agentic execution
	agenticIndex := strings.Index(lockContentStr, "Execute Claude Code")
	if agenticIndex == -1 {
		// Try alternative agentic step names
		agenticIndex = strings.Index(lockContentStr, "npx @anthropic-ai/claude-code")
		if agenticIndex == -1 {
			agenticIndex = strings.Index(lockContentStr, "uses: githubnext/claude-action")
		}
	}

	if agenticIndex != -1 && uploadArtifactsIndex != -1 {
		if uploadArtifactsIndex <= agenticIndex {
			t.Error("Unified artifact upload step should appear after agentic execution step")
		}
	}
}

func TestMCPLogsUploadWithoutPlaywright(t *testing.T) {
	// Create a temporary directory for the test
	tmpDir := testutil.TempDir(t, "test-*")

	// Create a test markdown file without Playwright tool configuration
	testMarkdown := `---
on:
  workflow_dispatch:
tools:
  github:
    allowed: [get_repository]
engine: claude
---

# Test Without Playwright

This workflow does not use Playwright but should still have MCP logs upload.
`

	// Write the test file
	mdFile := filepath.Join(tmpDir, "test-no-playwright.md")
	if err := os.WriteFile(mdFile, []byte(testMarkdown), 0644); err != nil {
		t.Fatalf("Failed to write test markdown file: %v", err)
	}

	// Initialize compiler
	compiler := NewCompiler()

	// Compile the workflow
	err := compiler.CompileWorkflow(mdFile)
	if err != nil {
		t.Fatalf("Failed to compile workflow: %v", err)
	}

	// Read the generated lock file
	lockFile := filepath.Join(tmpDir, "test-no-playwright.lock.yml")
	lockContent, err := os.ReadFile(lockFile)
	if err != nil {
		t.Fatalf("Failed to read lock file: %v", err)
	}

	lockContentStr := string(lockContent)

	// Verify MCP logs path EXISTS in unified artifact upload even when no Playwright is used
	if !strings.Contains(lockContentStr, "- name: Upload agent artifacts") {
		t.Error("Expected 'Upload agent artifacts' step to be present")
	}

	if !strings.Contains(lockContentStr, "/tmp/gh-aw/mcp-logs/") {
		t.Error("Expected MCP logs path in unified artifact upload even when Playwright is not used")
	}

	// Verify the upload step uses actions/upload-artifact@b7c566a772e6b6bfb58ed0dc250532a479d7789f
	if !strings.Contains(lockContentStr, "uses: actions/upload-artifact@b7c566a772e6b6bfb58ed0dc250532a479d7789f") {
		t.Error("Expected upload-artifact action to be used for artifact upload step")
	}

	// Verify the upload step has 'if-no-files-found: ignore' condition
	if !strings.Contains(lockContentStr, "if-no-files-found: ignore") {
		t.Error("Expected 'if-no-files-found: ignore' in upload step")
	}
}
