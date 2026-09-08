//go:build !integration

package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestHelperProcess is the subprocess entry point for fake-gh tests.
// It is invoked by the test binary itself (via os.Executable) when
// GO_TEST_HELPER_PROCESS=1 is set, so we can provide a blocking "gh"
// that hangs indefinitely until killed by context cancellation.
func TestHelperProcess(t *testing.T) {
	if os.Getenv("GO_TEST_HELPER_PROCESS") != "1" {
		return
	}
	// Block forever — the parent will kill us via context cancellation.
	select {}
}

// makeFakeGH writes a tiny fake "gh" executable into dir that just
// re-invokes the current test binary in TestHelperProcess mode.
func makeFakeGH(t *testing.T, dir string) {
	t.Helper()
	self, err := os.Executable()
	require.NoError(t, err)

	if runtime.GOOS == "windows" {
		// .bat wrapper that re-runs the test binary in helper mode
		script := fmt.Sprintf("@echo off\r\n\"%s\" -test.run=TestHelperProcess -test.v 2>nul\r\n", self)
		err = os.WriteFile(filepath.Join(dir, "gh.bat"), []byte(script), 0o755)
	} else {
		script := fmt.Sprintf("#!/bin/sh\nGO_TEST_HELPER_PROCESS=1 exec \"%s\" -test.run=TestHelperProcess -test.v \"$@\"\n", self)
		err = os.WriteFile(filepath.Join(dir, "gh"), []byte(script), 0o755)
	}
	require.NoError(t, err)
}

// withFakeBlockingGH prepends dir to PATH so that workflow.ExecGHContext
// picks up the blocking fake gh instead of the real one.
func withFakeBlockingGH(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	makeFakeGH(t, dir)

	orig := os.Getenv("PATH")
	t.Setenv("PATH", dir+string(os.PathListSeparator)+orig)

	// Also clear GH_TOKEN / GITHUB_TOKEN so ExecGHContext doesn't try to
	// set up authentication (which requires the real gh binary).
	t.Setenv("GH_TOKEN", "")
	t.Setenv("GITHUB_TOKEN", "")
}

// TestFetchGitHubWorkflows_ContextCancellation verifies that cancelling the
// context while the gh subprocess is blocked causes fetchGitHubWorkflows to
// return promptly with a context error.
func TestFetchGitHubWorkflows_ContextCancellation(t *testing.T) {
	if _, err := exec.LookPath("sh"); runtime.GOOS != "windows" && err != nil {
		t.Skip("sh not available")
	}

	withFakeBlockingGH(t)

	ctx, cancel := context.WithCancel(context.Background())
	// Cancel immediately so the subprocess is killed as soon as it starts.
	cancel()

	start := time.Now()
	_, err := fetchGitHubWorkflows(ctx, "", false)
	elapsed := time.Since(start)

	require.Error(t, err)
	assert.True(t, errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded),
		"expected context error, got: %v", err)
	assert.Less(t, elapsed, 5*time.Second, "fetchGitHubWorkflows should return promptly on cancellation")
}

func TestFetchGitHubWorkflows_RequestsAllWorkflows(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell script fake gh is not supported on Windows")
	}

	fakeBinDir := t.TempDir()
	argsLogPath := filepath.Join(fakeBinDir, "gh-args.log")
	fakeGH := filepath.Join(fakeBinDir, "gh")
	script := "#!/bin/sh\n" +
		"printf '%s\\n' \"$*\" > \"" + argsLogPath + "\"\n" +
		"printf '%s\\n' '[{\"workflows\":[{\"id\":1,\"name\":\"first\",\"path\":\".github/workflows/first.lock.yml\",\"state\":\"active\"}]},{\"workflows\":[{\"id\":2,\"name\":\"second\",\"path\":\".github/workflows/second.lock.yml\",\"state\":\"active\"}]}]'\n"
	require.NoError(t, os.WriteFile(fakeGH, []byte(script), 0o755))
	t.Setenv("PATH", fakeBinDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	workflows, err := fetchGitHubWorkflows(context.Background(), "owner/repo", true)
	require.NoError(t, err)
	require.Len(t, workflows, 2, "workflow discovery must include workflows from every API page")

	args, err := os.ReadFile(argsLogPath)
	require.NoError(t, err)
	assert.Contains(t, string(args), "api --paginate --slurp repos/owner/repo/actions/workflows?per_page=100",
		"workflow discovery must paginate all workflow API pages")
}

// TestFetchLatestRunsByRef_ContextCancellation verifies that cancelling the
// context while the gh subprocess (run list) is blocked causes
// fetchLatestRunsByRef to return promptly with a context error.
func TestFetchLatestRunsByRef_ContextCancellation(t *testing.T) {
	if _, err := exec.LookPath("sh"); runtime.GOOS != "windows" && err != nil {
		t.Skip("sh not available")
	}

	withFakeBlockingGH(t)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	start := time.Now()
	_, err := fetchLatestRunsByRef(ctx, "main", "", false)
	elapsed := time.Since(start)

	require.Error(t, err)
	assert.True(t, errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded),
		"expected context error, got: %v", err)
	assert.Less(t, elapsed, 5*time.Second, "fetchLatestRunsByRef should return promptly on cancellation")
}

func TestIsWorkflowFile(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		filename string
		expected bool
	}{
		{
			name:     "regular workflow file",
			filename: "my-workflow.md",
			expected: true,
		},
		{
			name:     "README.md should be excluded",
			filename: "README.md",
			expected: false,
		},
		{
			name:     "readme.md lowercase should be excluded",
			filename: "readme.md",
			expected: false,
		},
		{
			name:     "ReadMe.md mixed case should be excluded",
			filename: "ReadMe.md",
			expected: false,
		},
		{
			name:     "READM.md with different name is included",
			filename: "READM.md",
			expected: true,
		},
		{
			name:     "README-workflow.md with prefix is included",
			filename: "README-workflow.md",
			expected: true,
		},
		{
			name:     "workflow-README.md with suffix is included",
			filename: "workflow-README.md",
			expected: true,
		},
		{
			name:     "path with README.md at end should be excluded",
			filename: ".github/workflows/README.md",
			expected: false,
		},
		{
			name:     "path with regular workflow should be included",
			filename: ".github/workflows/my-workflow.md",
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isWorkflowFile(tt.filename)
			assert.Equal(t, tt.expected, result, "isWorkflowFile(%q) should return %v", tt.filename, tt.expected)
		})
	}
}

func TestFilterWorkflowFiles(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		input    []string
		expected []string
	}{
		{
			name:     "empty list",
			input:    []string{},
			expected: []string{},
		},
		{
			name: "no README.md files",
			input: []string{
				"workflow1.md",
				"workflow2.md",
				"my-test.md",
			},
			expected: []string{
				"workflow1.md",
				"workflow2.md",
				"my-test.md",
			},
		},
		{
			name: "filter out README.md",
			input: []string{
				"workflow1.md",
				"README.md",
				"workflow2.md",
			},
			expected: []string{
				"workflow1.md",
				"workflow2.md",
			},
		},
		{
			name: "filter out readme.md (lowercase)",
			input: []string{
				"workflow1.md",
				"readme.md",
				"workflow2.md",
			},
			expected: []string{
				"workflow1.md",
				"workflow2.md",
			},
		},
		{
			name: "filter out ReadMe.md (mixed case)",
			input: []string{
				"workflow1.md",
				"ReadMe.md",
				"workflow2.md",
			},
			expected: []string{
				"workflow1.md",
				"workflow2.md",
			},
		},
		{
			name: "keep README-prefixed files",
			input: []string{
				"README-workflow.md",
				"README.md",
				"workflow-README.md",
			},
			expected: []string{
				"README-workflow.md",
				"workflow-README.md",
			},
		},
		{
			name: "filter with full paths",
			input: []string{
				".github/workflows/workflow1.md",
				".github/workflows/README.md",
				".github/workflows/workflow2.md",
			},
			expected: []string{
				".github/workflows/workflow1.md",
				".github/workflows/workflow2.md",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := filterWorkflowFiles(tt.input)
			// Use ElementsMatch to handle nil vs empty slice differences
			if len(tt.expected) == 0 && len(result) == 0 {
				return // Both are empty, test passes
			}
			assert.Equal(t, tt.expected, result, "filterWorkflowFiles should filter correctly")
		})
	}
}

// TestGetMarkdownWorkflowFilesExcludesREADME tests that getMarkdownWorkflowFiles filters out README.md
func TestGetMarkdownWorkflowFilesExcludesREADME(t *testing.T) {
	// Create a temporary directory structure
	tempDir := t.TempDir()
	workflowsDir := filepath.Join(tempDir, ".github", "workflows")
	if err := os.MkdirAll(workflowsDir, 0755); err != nil {
		t.Fatalf("Failed to create workflows directory: %v", err)
	}

	// Create several workflow files
	testFiles := map[string]string{
		"workflow1.md":   "---\non: push\n---\n# Workflow 1",
		"workflow2.md":   "---\non: pull_request\n---\n# Workflow 2",
		"README.md":      "# This is a README",
		"readme.md":      "# This is a readme",
		"ReadMe.md":      "# This is a ReadMe",
		"my-workflow.md": "---\non: workflow_dispatch\n---\n# My Workflow",
		"README-test.md": "---\non: push\n---\n# README Test",
		"test-README.md": "---\non: push\n---\n# Test README",
	}

	for filename, content := range testFiles {
		path := filepath.Join(workflowsDir, filename)
		if err := os.WriteFile(path, []byte(content), 0644); err != nil {
			t.Fatalf("Failed to write test file %s: %v", filename, err)
		}
	}

	// Change to the temp directory
	originalDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("Failed to get current directory: %v", err)
	}
	defer os.Chdir(originalDir)

	if err := os.Chdir(tempDir); err != nil {
		t.Fatalf("Failed to change directory: %v", err)
	}

	// Get markdown workflow files
	files, err := getMarkdownWorkflowFiles("")
	if err != nil {
		t.Fatalf("getMarkdownWorkflowFiles failed: %v", err)
	}

	// Extract basenames for easier checking
	var basenames []string
	for _, file := range files {
		basenames = append(basenames, filepath.Base(file))
	}

	// Should include regular workflow files and files with README in the middle
	expectedFiles := []string{"workflow1.md", "workflow2.md", "my-workflow.md", "README-test.md", "test-README.md"}
	for _, expected := range expectedFiles {
		assert.Contains(t, basenames, expected, "Should include %s", expected)
	}

	// Should NOT include any README.md variants (exact name, case-insensitive)
	excludedFiles := []string{"README.md", "readme.md", "ReadMe.md"}
	for _, excluded := range excludedFiles {
		assert.NotContains(t, basenames, excluded, "Should NOT include %s", excluded)
	}

	// Verify total count
	assert.Len(t, files, 5, "Should have exactly 5 workflow files (excluding README variants)")
}

func TestFilterMarkdownFilesWithFrontmatter(t *testing.T) {
	t.Parallel()
	tempDir := t.TempDir()
	workflowsDir := filepath.Join(tempDir, ".github", "workflows")
	err := os.MkdirAll(workflowsDir, 0o755)
	require.NoError(t, err)

	testFiles := map[string]string{
		"workflow1.md":           "---\non: push\n---\n# Workflow 1",
		"workflow-crlf.md":       "---\r\non: push\r\n---\r\n# Workflow CRLF",
		"docs.md":                "# This is documentation",
		"empty.md":               "",
		"leading-whitespace.md":  "  ---\non: push\n---\n# Valid Frontmatter Start",
		"delimiter-not-first.md": "# Header\n---\non: push\n---\n# Not Valid Frontmatter Start",
	}

	for filename, content := range testFiles {
		path := filepath.Join(workflowsDir, filename)
		err := os.WriteFile(path, []byte(content), 0o644)
		require.NoError(t, err)
	}

	inputFiles := []string{
		filepath.Join(workflowsDir, "workflow1.md"),
		filepath.Join(workflowsDir, "workflow-crlf.md"),
		filepath.Join(workflowsDir, "docs.md"),
		filepath.Join(workflowsDir, "empty.md"),
		filepath.Join(workflowsDir, "leading-whitespace.md"),
		filepath.Join(workflowsDir, "delimiter-not-first.md"),
	}

	filtered, err := filterMarkdownFilesWithFrontmatter(inputFiles)
	require.NoError(t, err)
	assert.Len(t, filtered, 3)
	assert.Contains(t, filtered, filepath.Join(workflowsDir, "workflow1.md"))
	assert.Contains(t, filtered, filepath.Join(workflowsDir, "workflow-crlf.md"))
	assert.Contains(t, filtered, filepath.Join(workflowsDir, "leading-whitespace.md"))
}
