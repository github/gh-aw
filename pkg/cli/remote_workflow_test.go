//go:build !integration

package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/github/gh-aw/pkg/workflow"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var testSHAResolutionRetryDelays = []time.Duration{
	time.Millisecond,
	2 * time.Millisecond,
	3 * time.Millisecond,
}

func TestFetchLocalWorkflow(t *testing.T) {
	tests := []struct {
		name        string
		content     string
		expectError bool
	}{
		{
			name: "valid workflow file",
			content: `---
name: Test Workflow
on: workflow_dispatch
---

# Test Workflow

This is a test.
`,
			expectError: false,
		},
		{
			name:        "empty file",
			content:     "",
			expectError: false,
		},
		{
			name:        "minimal content",
			content:     "# Hello",
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a temporary file
			tempDir := t.TempDir()
			tempFile := filepath.Join(tempDir, "test-workflow.md")
			err := os.WriteFile(tempFile, []byte(tt.content), 0644)
			require.NoError(t, err, "should create temp file")

			spec := &WorkflowSpec{
				WorkflowPath: tempFile,
				WorkflowName: "test-workflow",
			}

			result, err := fetchLocalWorkflow(spec, false)

			if tt.expectError {
				assert.Error(t, err, "expected error")
			} else {
				require.NoError(t, err, "should not error")
				assert.Equal(t, []byte(tt.content), result.Content, "content should match")
				assert.True(t, result.IsLocal, "should be marked as local")
				assert.Empty(t, result.CommitSHA, "local workflows should not have commit SHA")
				assert.Equal(t, tempFile, result.SourcePath, "source path should match")
			}
		})
	}
}

func TestFetchLocalWorkflow_NonExistentFile(t *testing.T) {
	spec := &WorkflowSpec{
		WorkflowPath: "/nonexistent/path/to/workflow.md",
		WorkflowName: "nonexistent-workflow",
	}

	result, err := fetchLocalWorkflow(spec, false)

	require.Error(t, err, "should error for non-existent file")
	assert.Nil(t, result, "result should be nil on error")
	require.ErrorContains(t, err, "not found", "error should mention file not found")
}

func TestFetchLocalWorkflow_DirectoryInsteadOfFile(t *testing.T) {
	tempDir := t.TempDir()

	spec := &WorkflowSpec{
		WorkflowPath: tempDir, // Pass directory instead of file
		WorkflowName: "directory-workflow",
	}

	result, err := fetchLocalWorkflow(spec, false)

	require.Error(t, err, "should error when path is a directory")
	assert.Nil(t, result, "result should be nil on error")
}

func TestResolveCommitSHAWithRetries_TransientFailureThenSuccess(t *testing.T) {
	originalResolve := resolveRefToSHAForHost
	originalWait := waitBeforeSHAResolutionRetry
	originalDelays := shaResolutionRetryDelays
	defer func() {
		resolveRefToSHAForHost = originalResolve
		waitBeforeSHAResolutionRetry = originalWait
		shaResolutionRetryDelays = originalDelays
	}()

	shaResolutionRetryDelays = testSHAResolutionRetryDelays
	resolveAttempts := 0
	resolveRefToSHAForHost = func(_ context.Context, owner, repo, ref, host string) (string, error) {
		resolveAttempts++
		if resolveAttempts == 1 {
			return "", errors.New("HTTP 429: rate limit exceeded")
		}
		return "0123456789abcdef0123456789abcdef01234567", nil
	}

	sleeps := make([]time.Duration, 0)
	waitBeforeSHAResolutionRetry = func(ctx context.Context, delay time.Duration) error {
		sleeps = append(sleeps, delay)
		return nil
	}

	sha, err := resolveCommitSHAWithRetries(context.Background(), "owner", "repo", "main", ".github/workflows/test.md", "", false)
	require.NoError(t, err, "Transient failure should be retried and eventually succeed")
	assert.Equal(t, "0123456789abcdef0123456789abcdef01234567", sha, "Resolved SHA should be returned")
	assert.Equal(t, 2, resolveAttempts, "Resolution should retry once after initial transient failure")
	assert.Equal(t, []time.Duration{time.Millisecond}, sleeps, "Backoff should use first retry delay")
}

func TestResolveCommitSHAWithRetries_PermanentFailureDoesNotRetry(t *testing.T) {
	originalResolve := resolveRefToSHAForHost
	originalWait := waitBeforeSHAResolutionRetry
	originalDelays := shaResolutionRetryDelays
	defer func() {
		resolveRefToSHAForHost = originalResolve
		waitBeforeSHAResolutionRetry = originalWait
		shaResolutionRetryDelays = originalDelays
	}()

	shaResolutionRetryDelays = testSHAResolutionRetryDelays
	resolveAttempts := 0
	resolveRefToSHAForHost = func(_ context.Context, owner, repo, ref, host string) (string, error) {
		resolveAttempts++
		return "", errors.New("HTTP 404: Not Found")
	}

	sleepCalls := 0
	waitBeforeSHAResolutionRetry = func(ctx context.Context, delay time.Duration) error {
		sleepCalls++
		return nil
	}

	sha, err := resolveCommitSHAWithRetries(context.Background(), "owner", "repo", "main", ".github/workflows/test.md", "", false)
	require.Error(t, err, "Permanent failures should stop immediately")
	assert.Empty(t, sha, "No SHA should be returned when resolution fails")
	assert.Equal(t, 1, resolveAttempts, "Permanent failures should not retry")
	assert.Equal(t, 0, sleepCalls, "No backoff sleep should happen for permanent failures")
	require.ErrorContains(t, err, "Expected the GitHub API to return a commit SHA for the ref",
		"Error should explain expected behavior")
	require.ErrorContains(t, err, "@<40-char-sha>", "Error should include retry command with full SHA placeholder")
}

func TestResolveCommitSHAWithRetries_TransientFailureExhaustsRetries(t *testing.T) {
	originalResolve := resolveRefToSHAForHost
	originalWait := waitBeforeSHAResolutionRetry
	originalDelays := shaResolutionRetryDelays
	defer func() {
		resolveRefToSHAForHost = originalResolve
		waitBeforeSHAResolutionRetry = originalWait
		shaResolutionRetryDelays = originalDelays
	}()

	shaResolutionRetryDelays = testSHAResolutionRetryDelays
	resolveAttempts := 0
	resolveRefToSHAForHost = func(_ context.Context, owner, repo, ref, host string) (string, error) {
		resolveAttempts++
		return "", errors.New("timeout waiting for GitHub API")
	}

	sleepCalls := 0
	waitBeforeSHAResolutionRetry = func(ctx context.Context, delay time.Duration) error {
		sleepCalls++
		return nil
	}

	sha, err := resolveCommitSHAWithRetries(context.Background(), "owner", "repo", "main", ".github/workflows/test.md", "", false)
	require.Error(t, err, "Retries should fail after repeated transient failures")
	assert.Empty(t, sha, "No SHA should be returned when retries are exhausted")
	assert.Equal(t, 4, resolveAttempts, "Should attempt initial call plus three retries")
	assert.Equal(t, 3, sleepCalls, "Should sleep between each retry")
	require.ErrorContains(t, err, "after 3 retries", "Error should report retry exhaustion")
}

func TestResolveCommitSHAWithRetries_ContextCanceledDuringBackoff(t *testing.T) {
	originalResolve := resolveRefToSHAForHost
	originalWait := waitBeforeSHAResolutionRetry
	originalDelays := shaResolutionRetryDelays
	defer func() {
		resolveRefToSHAForHost = originalResolve
		waitBeforeSHAResolutionRetry = originalWait
		shaResolutionRetryDelays = originalDelays
	}()

	shaResolutionRetryDelays = testSHAResolutionRetryDelays
	resolveRefToSHAForHost = func(_ context.Context, owner, repo, ref, host string) (string, error) {
		return "", errors.New("HTTP 429: rate limit exceeded")
	}

	waitBeforeSHAResolutionRetry = func(ctx context.Context, delay time.Duration) error {
		return ctx.Err()
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	sha, err := resolveCommitSHAWithRetries(ctx, "owner", "repo", "main", ".github/workflows/test.md", "", false)
	require.Error(t, err, "Cancellation during retry backoff should fail fast")
	assert.Empty(t, sha, "No SHA should be returned when retry wait is canceled")
	require.ErrorContains(t, err, "retry wait was cancelled", "Error should explain cancellation reason")
	require.ErrorContains(t, err, "@<40-char-sha>", "Error should include exact SHA retry guidance")
}

func TestFetchIncludeFromSource_WorkflowSpecParsing(t *testing.T) {
	tests := []struct {
		name          string
		includePath   string
		baseSpec      *WorkflowSpec
		expectSection string
		expectError   bool
		errorContains string
	}{
		{
			name:          "two parts falls through to cannot resolve",
			includePath:   "owner/repo",
			baseSpec:      nil,
			expectSection: "",
			expectError:   true,
			errorContains: "cannot resolve include path", // Not a workflowspec format (only 2 parts)
		},
		{
			name:          "malformed workflowspec with empty repo rejects path with section",
			includePath:   "owner//path/file.md#section-name",
			baseSpec:      nil,
			expectSection: "#section-name",
			expectError:   true,
			errorContains: "cannot resolve include path",
		},
		{
			name:          "malformed workflowspec with empty repo rejects path without section",
			includePath:   "owner//path/file.md",
			baseSpec:      nil,
			expectSection: "",
			expectError:   true,
			errorContains: "cannot resolve include path",
		},
		{
			name:          "relative path without base spec",
			includePath:   "shared/file.md",
			baseSpec:      nil,
			expectSection: "",
			expectError:   true,
			errorContains: "cannot resolve include path",
		},
		{
			name:          "relative path with section but no base spec",
			includePath:   "shared/file.md#my-section",
			baseSpec:      nil,
			expectSection: "#my-section",
			expectError:   true,
			errorContains: "cannot resolve include path",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, section, err := FetchIncludeFromSource(t.Context(), tt.includePath, tt.baseSpec, false)

			if tt.expectError {
				require.Error(t, err, "expected error")
				if tt.errorContains != "" {
					require.ErrorContains(t, err, tt.errorContains, "error should contain expected text")
				}
			} else {
				require.NoError(t, err, "should not error")
			}

			// Section should always be extracted consistently
			assert.Equal(t, tt.expectSection, section, "section should match expected")
		})
	}
}

func TestFetchIncludeFromSource_SectionExtraction(t *testing.T) {
	// Test that section is consistently extracted regardless of path type
	tests := []struct {
		name          string
		includePath   string
		expectSection string
	}{
		{
			name:          "hash section",
			includePath:   "shared/file.md#section",
			expectSection: "#section",
		},
		{
			name:          "complex section with hyphens",
			includePath:   "shared/file.md#my-complex-section-name",
			expectSection: "#my-complex-section-name",
		},
		{
			name:          "no section",
			includePath:   "shared/file.md",
			expectSection: "",
		},
		{
			name:          "section at end of path with ref",
			includePath:   "shared/file.md@v1.0.0#section",
			expectSection: "#section", // Section is extracted from the end regardless of @ref position
		},
		{
			name:          "section after everything",
			includePath:   "shared/file.md#section-name",
			expectSection: "#section-name",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// We expect resolution errors in these unit tests, but section should still be extracted
			_, section, _ := FetchIncludeFromSource(t.Context(), tt.includePath, nil, false)
			assert.Equal(t, tt.expectSection, section, "section should be correctly extracted")
		})
	}
}

func TestGetParentDir(t *testing.T) {
	tests := []struct {
		name     string
		path     string
		expected string
	}{
		{
			name:     "simple path",
			path:     "dir/file.md",
			expected: "dir",
		},
		{
			name:     "deep path",
			path:     "a/b/c/file.md",
			expected: "a/b/c",
		},
		{
			name:     "no directory",
			path:     "file.md",
			expected: "",
		},
		{
			name:     "trailing slash",
			path:     "dir/",
			expected: "dir",
		},
		{
			name:     "empty string",
			path:     "",
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := getParentDir(tt.path)
			assert.Equal(t, tt.expected, result, "getParentDir(%q) should return %q", tt.path, tt.expected)
		})
	}
}

// TestFetchAndSaveRemoteFrontmatterImports_NoImports verifies that the function
// is a no-op when the workflow has no imports field.
func TestFetchAndSaveRemoteFrontmatterImports_NoImports(t *testing.T) {
	content := `---
engine: copilot
---

# Workflow with no imports
`
	spec := &WorkflowSpec{
		RepoSpec: RepoSpec{
			RepoSlug: "github/gh-aw",
			Version:  "main",
		},
		WorkflowPath: ".github/workflows/ci-coach.md",
	}

	tmpDir := t.TempDir()
	err := fetchAndSaveRemoteFrontmatterImports(t.Context(), content, spec, tmpDir, false, false, nil)
	require.NoError(t, err, "should not error when no imports are present")

	// No files should have been created
	entries, readErr := os.ReadDir(tmpDir)
	require.NoError(t, readErr)
	assert.Empty(t, entries, "no files should be created when no imports are present")
}

// TestFetchAndSaveRemoteFrontmatterImports_EmptyRepoSlug verifies that the function
// is a no-op when the spec has no remote repo (local workflow).
func TestFetchAndSaveRemoteFrontmatterImports_EmptyRepoSlug(t *testing.T) {
	content := `---
engine: copilot
imports:
  - shared/ci-data-analysis.md
---

# Workflow
`
	spec := &WorkflowSpec{
		RepoSpec: RepoSpec{
			RepoSlug: "", // local workflow – no remote repo
		},
		WorkflowPath: ".github/workflows/ci-coach.md",
	}

	tmpDir := t.TempDir()
	err := fetchAndSaveRemoteFrontmatterImports(t.Context(), content, spec, tmpDir, false, false, nil)
	require.NoError(t, err, "should not error for local workflow with empty RepoSlug")

	entries, readErr := os.ReadDir(tmpDir)
	require.NoError(t, readErr)
	assert.Empty(t, entries, "no files should be created for local workflows")
}

// TestFetchAndSaveRemoteFrontmatterImports_WorkflowSpecSkipped verifies that imports
// that are already in workflowspec format (owner/repo/path@ref) are skipped.
func TestFetchAndSaveRemoteFrontmatterImports_WorkflowSpecSkipped(t *testing.T) {
	content := `---
engine: copilot
imports:
  - github/gh-aw/.github/workflows/shared/ci-data-analysis.md@abc123
---

# Workflow
`
	spec := &WorkflowSpec{
		RepoSpec: RepoSpec{
			RepoSlug: "github/gh-aw",
			Version:  "main",
		},
		WorkflowPath: ".github/workflows/ci-coach.md",
	}

	tmpDir := t.TempDir()
	// This should not attempt any network calls; already-pinned imports are skipped.
	err := fetchAndSaveRemoteFrontmatterImports(t.Context(), content, spec, tmpDir, false, false, nil)
	require.NoError(t, err, "should not error for workflowspec imports")

	entries, readErr := os.ReadDir(tmpDir)
	require.NoError(t, readErr)
	assert.Empty(t, entries, "already-pinned workflowspec imports should not be downloaded")
}

// TestFetchAndSaveRemoteFrontmatterImports_UsesFormSkippedWhenWorkflowSpec verifies that
// an import written in the uses:/with: object form (GitHub Actions reusable workflow syntax)
// that points to an already-pinned workflowspec path is also skipped — exactly as the
// equivalent string-form import would be.  This exercises the map-item branch added to
// handle uses:/path: object imports.
func TestFetchAndSaveRemoteFrontmatterImports_UsesFormSkippedWhenWorkflowSpec(t *testing.T) {
	content := `---
engine: copilot
imports:
  - uses: github/gh-aw/.github/workflows/shared/mcp/serena.md@abc123
    with:
      languages: ["go"]
---

# Workflow with uses: form import
`
	spec := &WorkflowSpec{
		RepoSpec: RepoSpec{
			RepoSlug: "github/gh-aw",
			Version:  "main",
		},
		WorkflowPath: ".github/workflows/ci-coach.md",
	}

	tmpDir := t.TempDir()
	// The uses: import path is in workflowspec format so it should be skipped,
	// just like a string-form workflowspec import would be.
	err := fetchAndSaveRemoteFrontmatterImports(t.Context(), content, spec, tmpDir, false, false, nil)
	require.NoError(t, err, "should not error for uses: form workflowspec imports")

	entries, readErr := os.ReadDir(tmpDir)
	require.NoError(t, readErr)
	assert.Empty(t, entries, "uses: form workflowspec imports should not be downloaded")
}

func TestFetchAndSaveRemoteFrontmatterImports_NoImportsNoOpTracker(t *testing.T) {
	// Build a minimal FileTracker without calling NewFileTracker (which requires a real
	// git repository). We only need the tracking lists populated.
	tracker := &FileTracker{
		OriginalContent: make(map[string][]byte),
		gitRoot:         t.TempDir(),
	}

	content := `---
engine: copilot
---

# No imports
`
	spec := &WorkflowSpec{
		RepoSpec: RepoSpec{
			RepoSlug: "github/gh-aw",
			Version:  "v1.0.0",
		},
		WorkflowPath: ".github/workflows/test.md",
	}

	err := fetchAndSaveRemoteFrontmatterImports(t.Context(), content, spec, tracker.gitRoot, false, false, tracker)
	require.NoError(t, err)
	assert.Empty(t, tracker.CreatedFiles, "no files should be created when there are no imports")
	assert.Empty(t, tracker.ModifiedFiles, "no files should be modified when there are no imports")
}

// TestFetchAndSaveRemoteFrontmatterImports_SectionStrippedDedup verifies that two imports
// pointing to the same file via different #section fragments are treated as one file
// (deduplication via the shared seen set).
func TestFetchAndSaveRemoteFrontmatterImports_SectionStrippedDedup(t *testing.T) {
	// Both imports resolve to the same base file after stripping the #section fragment.
	// The first triggers a (failing) download attempt; the second is deduplicated and never
	// even reaches the download step.  Both use relative paths so the workflowspec-format
	// skip path is not taken.
	content := `---
engine: copilot
imports:
  - shared/reporting.md#SectionA
  - shared/reporting.md#SectionB
---

# Workflow
`
	spec := &WorkflowSpec{
		RepoSpec: RepoSpec{
			RepoSlug: "github/gh-aw",
			Version:  "v1.0.0",
		},
		WorkflowPath: ".github/workflows/ci-coach.md",
	}

	tmpDir := t.TempDir()
	// No network in unit tests: the download attempt for the first import will fail silently
	// (verbose=false).  The second import must be deduplicated without a second download.
	err := fetchAndSaveRemoteFrontmatterImports(t.Context(), content, spec, tmpDir, false, false, nil)
	require.NoError(t, err, "section-fragment deduplication should not error")

	entries, readErr := os.ReadDir(tmpDir)
	require.NoError(t, readErr)
	assert.Empty(t, entries, "no files should be written (download fails in unit tests)")
}

// TestFetchAndSaveRemoteFrontmatterImports_SkipExistingWithoutForce verifies that a relative
// import whose target file already exists on disk is skipped (not re-downloaded) when force=false.
// Because the existence check happens before the download, this test requires no network access.
func TestFetchAndSaveRemoteFrontmatterImports_SkipExistingWithoutForce(t *testing.T) {
	tmpDir := t.TempDir()
	sharedDir := filepath.Join(tmpDir, "shared")
	require.NoError(t, os.MkdirAll(sharedDir, 0755))
	existingContent := []byte("existing content")
	existingFile := filepath.Join(sharedDir, "ci-data-analysis.md")
	require.NoError(t, os.WriteFile(existingFile, existingContent, 0600))

	tracker := &FileTracker{
		OriginalContent: make(map[string][]byte),
		gitRoot:         tmpDir,
	}

	// Relative import: resolves to tmpDir/shared/ci-data-analysis.md which already exists.
	// With force=false, the function detects the file via os.Stat *before* attempting a
	// download, so no network call is made and the file is preserved unchanged.
	content := `---
engine: copilot
imports:
  - shared/ci-data-analysis.md
---
# Workflow
`
	spec := &WorkflowSpec{
		RepoSpec: RepoSpec{
			RepoSlug: "github/gh-aw",
			Version:  "v1.0.0",
		},
		WorkflowPath: ".github/workflows/ci-coach.md",
	}

	err := fetchAndSaveRemoteFrontmatterImports(t.Context(), content, spec, tmpDir, false, false, tracker)
	require.NoError(t, err)

	// The existing file must be untouched and not added to the tracker.
	gotContent, readErr := os.ReadFile(existingFile)
	require.NoError(t, readErr)
	assert.Equal(t, existingContent, gotContent, "pre-existing file must not be modified when force=false")
	assert.Empty(t, tracker.CreatedFiles, "pre-existing file must not appear in CreatedFiles")
	assert.Empty(t, tracker.ModifiedFiles, "pre-existing file must not appear in ModifiedFiles")
}

// TestFetchAndSaveRemoteFrontmatterImports_PathTraversal verifies that import paths that
// attempt to escape the repository root via ".." sequences are rejected by the
// remoteFilePath safety check (not just because of a download failure).
// The workflow is placed at the repo root (WorkflowPath="ci-coach.md") so that
// workflowBaseDir="" and path.Join("", "../etc/passwd") = "../etc/passwd", which
// triggers the explicit ".." rejection before any network call.
func TestFetchAndSaveRemoteFrontmatterImports_PathTraversal(t *testing.T) {
	tests := []struct {
		name       string
		importPath string
	}{
		{name: "parent directory traversal", importPath: "../etc/passwd"},
		{name: "deep traversal", importPath: "../../tmp/evil.md"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			content := fmt.Sprintf(`---
engine: copilot
imports:
  - %s
---
# Workflow
`, tc.importPath)
			// WorkflowPath at repo root → workflowBaseDir="" → path.Join("","../etc/passwd")="../etc/passwd"
			// which triggers the explicit ".." rejection before any network call.
			spec := &WorkflowSpec{
				RepoSpec: RepoSpec{
					RepoSlug: "github/gh-aw",
					Version:  "v1.0.0",
				},
				WorkflowPath: "ci-coach.md",
			}

			tmpDir := t.TempDir()
			err := fetchAndSaveRemoteFrontmatterImports(t.Context(), content, spec, tmpDir, false, false, nil)
			require.NoError(t, err, "path traversal should be silently rejected, not return an error")

			// No file must have been written anywhere
			entries, readErr := os.ReadDir(tmpDir)
			require.NoError(t, readErr)
			assert.Empty(t, entries, "traversal import %q must not write any file", tc.importPath)
		})
	}
}

func TestFetchAndSaveRemoteIncludes_PathTraversalRejected(t *testing.T) {
	mockFetch := func(_ context.Context, _ string, _ *WorkflowSpec, _ bool) ([]byte, string, error) {
		return []byte("# include body\n"), "", nil
	}

	tmpDir := t.TempDir()
	targetDir := filepath.Join(tmpDir, ".github", "workflows")
	require.NoError(t, os.MkdirAll(targetDir, 0o755))

	spec := &WorkflowSpec{
		RepoSpec: RepoSpec{RepoSlug: "github/gh-aw", Version: "main"},
	}
	err := fetchAndSaveRemoteIncludesWithOptions(t.Context(), "@include ../secrets/evil.md\n", includesFetchOptions{
		spec:      spec,
		targetDir: targetDir,
		fetchFn:   mockFetch,
	})
	require.Error(t, err)
	require.NoFileExists(t, filepath.Join(tmpDir, ".github", "secrets", "evil.md"))
}

func TestFetchAndSaveRemoteIncludes_NestedTraversalRejected(t *testing.T) {
	mockFetch := func(_ context.Context, _ string, _ *WorkflowSpec, _ bool) ([]byte, string, error) {
		return []byte("# include body\n"), "", nil
	}

	tmpDir := t.TempDir()
	targetDir := filepath.Join(tmpDir, ".github", "workflows")
	require.NoError(t, os.MkdirAll(targetDir, 0o755))

	spec := &WorkflowSpec{
		RepoSpec: RepoSpec{RepoSlug: "github/gh-aw", Version: "main"},
	}
	// A path that doesn't start with "../" but still escapes via a sub-directory component.
	err := fetchAndSaveRemoteIncludesWithOptions(t.Context(), "@include subdir/../../secrets/evil.md\n", includesFetchOptions{
		spec:      spec,
		targetDir: targetDir,
		fetchFn:   mockFetch,
	})
	require.Error(t, err)
	require.NoFileExists(t, filepath.Join(tmpDir, ".github", "secrets", "evil.md"))
}

func TestFetchAndSaveRemoteIncludes_SharedIncludeStaysUnderSharedDir(t *testing.T) {
	mockFetch := func(_ context.Context, _ string, _ *WorkflowSpec, _ bool) ([]byte, string, error) {
		return []byte("# include body\n"), "", nil
	}

	tmpDir := t.TempDir()
	targetDir := filepath.Join(tmpDir, ".github", "workflows")
	require.NoError(t, os.MkdirAll(targetDir, 0o755))

	spec := &WorkflowSpec{
		RepoSpec: RepoSpec{RepoSlug: "github/gh-aw", Version: "main"},
	}
	err := fetchAndSaveRemoteIncludesWithOptions(t.Context(), "@include shared/helper.md\n", includesFetchOptions{
		spec:      spec,
		targetDir: targetDir,
		fetchFn:   mockFetch,
	})
	require.NoError(t, err)
	assert.FileExists(t, filepath.Join(tmpDir, ".github", "shared", "helper.md"))
}

// TestFetchAndSaveRemoteFrontmatterImports_InvalidRepoSlug verifies that an invalid
// RepoSlug (not in owner/repo format) causes the function to return early without error.
func TestFetchAndSaveRemoteFrontmatterImports_InvalidRepoSlug(t *testing.T) {
	content := `---
engine: copilot
imports:
  - shared/ci-data-analysis.md
---
# Workflow
`
	spec := &WorkflowSpec{
		RepoSpec: RepoSpec{
			RepoSlug: "not-a-valid-slug", // missing slash → only one part
		},
		WorkflowPath: ".github/workflows/ci-coach.md",
	}

	tmpDir := t.TempDir()
	err := fetchAndSaveRemoteFrontmatterImports(t.Context(), content, spec, tmpDir, false, false, nil)
	require.NoError(t, err, "invalid RepoSlug should return nil without error")

	entries, readErr := os.ReadDir(tmpDir)
	require.NoError(t, readErr)
	assert.Empty(t, entries, "no files should be created for an invalid RepoSlug")
}

// TestFetchFrontmatterImportsRecursive_SiblingPathResolution verifies that when a
// shared file imports a sibling using a bare filename (no "/" character), the path
// is resolved relative to the importing file's directory rather than the original
// workflow base directory. This mirrors the compiler's determineNestedBaseDir logic.
func TestFetchFrontmatterImportsRecursive_SiblingPathResolution(t *testing.T) {
	tmpDir := t.TempDir()

	// Track which remote paths the downloader is asked for, and return minimal content.
	var downloadedPaths []string
	mockDownload := func(_ context.Context, _, _, remoteFilePath, _ string) ([]byte, error) {
		downloadedPaths = append(downloadedPaths, remoteFilePath)
		return []byte("# stub\n"), nil
	}

	// shared/control.md imports a sibling via a bare filename (no "/").
	controlContent := `---
imports:
  - control-aux.md
---
# Control
`

	seen := make(map[string]struct{})
	opts := frontmatterImportsOpts{
		owner:           "github",
		repo:            "gh-aw",
		ref:             "main",
		originalBaseDir: ".github/workflows",
		targetDir:       tmpDir,
		verbose:         false,
		force:           true, // force=true so the download step is always reached
		tracker:         &FileTracker{OriginalContent: make(map[string][]byte)},
		seen:            seen,
		downloadFn:      mockDownload,
	}

	// Process shared/control.md; currentBaseDir mirrors its location in the source repo.
	fetchFrontmatterImportsRecursive(t.Context(), controlContent, ".github/workflows/shared", opts)

	// "control-aux.md" (bare filename, no "/") must resolve relative to the importing
	// file's directory → ".github/workflows/shared/control-aux.md".
	require.Len(t, downloadedPaths, 1, "downloader must be called exactly once")
	assert.Equal(t, ".github/workflows/shared/control-aux.md", downloadedPaths[0],
		"sibling import must resolve to the shared/ subdir, not the workflow root")

	// The file must be saved at the correct local path inside targetDir.
	assert.FileExists(t, filepath.Join(tmpDir, "shared", "control-aux.md"),
		"file must be written to shared/control-aux.md")
	assert.NoFileExists(t, filepath.Join(tmpDir, "control-aux.md"),
		"file must not be written at the root level (wrong path resolution)")
}

// TestFetchFrontmatterImportsRecursive_RecurseIntoExistingFile verifies that when a
// top-level import already exists on disk (force=false), the function still recurses
// into that file's imports so that transitive dependencies are fetched. This test
// also covers sibling-path resolution: the transitive dep uses a bare filename, so it
// should resolve to the shared file's directory, not the workflow root.
func TestFetchFrontmatterImportsRecursive_RecurseIntoExistingFile(t *testing.T) {
	tmpDir := t.TempDir()
	sharedDir := filepath.Join(tmpDir, "shared")
	require.NoError(t, os.MkdirAll(sharedDir, 0755))

	// shared/control.md already exists locally; it imports control-aux.md (a sibling).
	controlContent := `---
imports:
  - control-aux.md
---
# Control
`
	require.NoError(t, os.WriteFile(filepath.Join(sharedDir, "control.md"), []byte(controlContent), 0600))

	// Track which remote paths are downloaded.
	var downloadedPaths []string
	mockDownload := func(_ context.Context, _, _, remoteFilePath, _ string) ([]byte, error) {
		downloadedPaths = append(downloadedPaths, remoteFilePath)
		return []byte("# stub\n"), nil
	}

	// Top-level content imports shared/control.md (which already exists on disk).
	topContent := `---
engine: copilot
imports:
  - shared/control.md
---
# Orchestrator
`

	seen := make(map[string]struct{})
	opts := frontmatterImportsOpts{
		owner:           "github",
		repo:            "gh-aw",
		ref:             "v1.0.0",
		originalBaseDir: ".github/workflows",
		targetDir:       tmpDir,
		verbose:         false,
		force:           false, // force=false so pre-existing files trigger the "skip + recurse" path
		tracker:         &FileTracker{OriginalContent: make(map[string][]byte)},
		seen:            seen,
		downloadFn:      mockDownload,
	}

	fetchFrontmatterImportsRecursive(t.Context(), topContent, ".github/workflows", opts)

	// shared/control.md existed on disk → must NOT be downloaded.
	assert.NotContains(t, downloadedPaths, ".github/workflows/shared/control.md",
		"pre-existing shared/control.md must not be re-downloaded")

	// The function must recurse into the existing file and discover its sibling dep.
	// control-aux.md (bare filename, no "/") must resolve to shared/control-aux.md.
	require.Len(t, downloadedPaths, 1, "transitive dep control-aux.md must be fetched exactly once")
	assert.Equal(t, ".github/workflows/shared/control-aux.md", downloadedPaths[0],
		"transitive dep must be fetched from the shared/ subdir")

	// The transitive dep must be saved at the correct local path.
	assert.FileExists(t, filepath.Join(tmpDir, "shared", "control-aux.md"),
		"transitive dep must be written to shared/control-aux.md")
	assert.NoFileExists(t, filepath.Join(tmpDir, "control-aux.md"),
		"transitive dep must not be written at the root level")
}

func TestFetchAndSaveRemoteRuntimeImports_EmptyRepoSlug(t *testing.T) {
	tmpDir := t.TempDir()
	workflowsDir := filepath.Join(tmpDir, ".github", "workflows")
	require.NoError(t, os.MkdirAll(workflowsDir, 0o755))

	err := fetchAndSaveRemoteRuntimeImports(
		t.Context(),
		"{{#runtime-import .github/skills/example/SKILL.md}}\n",
		&WorkflowSpec{},
		workflowsDir,
		false,
		false,
		nil,
	)
	require.NoError(t, err)
	assert.NoFileExists(t, filepath.Join(tmpDir, ".github", "skills", "example", "SKILL.md"))
}

func TestFetchAndSaveRemoteRuntimeImports_FetchesPinnedRecursiveClosure(t *testing.T) {
	original := downloadRemoteRuntimeImportFile
	defer func() { downloadRemoteRuntimeImportFile = original }()

	tmpDir := t.TempDir()
	workflowsDir := filepath.Join(tmpDir, ".github", "workflows")
	require.NoError(t, os.MkdirAll(workflowsDir, 0o755))

	type request struct {
		path string
		ref  string
		host string
	}
	var requests []request
	downloadRemoteRuntimeImportFile = func(_ context.Context, owner, repo, remotePath, ref, host string) ([]byte, error) {
		assert.Equal(t, "github", owner)
		assert.Equal(t, "gh-aw", repo)
		requests = append(requests, request{path: remotePath, ref: ref, host: host})
		switch remotePath {
		case ".github/skills/example/SKILL.md":
			return []byte("---\nname: Example\n---\n{{#runtime-import ./nested.md}}\n"), nil
		case ".github/skills/example/nested.md":
			return []byte("nested\n"), nil
		default:
			return nil, fmt.Errorf("unexpected path %s", remotePath)
		}
	}

	spec := &WorkflowSpec{
		RepoSpec: RepoSpec{
			RepoSlug: "github/gh-aw",
			Version:  "0123456789abcdef0123456789abcdef01234567",
		},
		WorkflowPath: ".github/workflows/example.md",
		Host:         "github.com",
	}

	err := fetchAndSaveRemoteRuntimeImports(
		t.Context(),
		"{{#runtime-import .github/skills/example/SKILL.md}}\n",
		spec,
		workflowsDir,
		false,
		true,
		nil,
	)
	require.NoError(t, err)
	assert.Equal(t, []request{
		{path: ".github/skills/example/SKILL.md", ref: "0123456789abcdef0123456789abcdef01234567", host: "github.com"},
		{path: ".github/skills/example/nested.md", ref: "0123456789abcdef0123456789abcdef01234567", host: "github.com"},
	}, requests)
	assert.FileExists(t, filepath.Join(tmpDir, ".github", "skills", "example", "SKILL.md"))
	assert.FileExists(t, filepath.Join(tmpDir, ".github", "skills", "example", "nested.md"))
}

// TestFetchFrontmatterImportsRecursive_RepoRootSlashPath verifies that when
// originalBaseDir is empty (workflow lives at the repo root) and an import
// path contains a "/" (e.g. "shared/helper.md"), the path is used as-is
// rather than being incorrectly prefixed with currentBaseDir.
func TestFetchFrontmatterImportsRecursive_RepoRootSlashPath(t *testing.T) {
	tmpDir := t.TempDir()

	var downloadedPaths []string
	mockDownload := func(_ context.Context, _, _, remoteFilePath, _ string) ([]byte, error) {
		downloadedPaths = append(downloadedPaths, remoteFilePath)
		return []byte("# stub\n"), nil
	}

	// shared/control.md imports shared/helper.md (a slash path).
	controlContent := `---
imports:
  - shared/helper.md
---
# Control
`

	seen := make(map[string]struct{})
	opts := frontmatterImportsOpts{
		owner:           "github",
		repo:            "gh-aw",
		ref:             "main",
		originalBaseDir: "", // empty: workflow lives at the repo root
		targetDir:       tmpDir,
		verbose:         false,
		force:           true,
		tracker:         &FileTracker{OriginalContent: make(map[string][]byte)},
		seen:            seen,
		downloadFn:      mockDownload,
	}

	// currentBaseDir mirrors shared/control.md's location.
	fetchFrontmatterImportsRecursive(t.Context(), controlContent, "shared", opts)

	// "shared/helper.md" (slash path, empty originalBaseDir) must be used as-is,
	// NOT prefixed with currentBaseDir ("shared/shared/helper.md" would be wrong).
	require.Len(t, downloadedPaths, 1, "downloader must be called exactly once")
	assert.Equal(t, "shared/helper.md", downloadedPaths[0],
		"slash import with empty originalBaseDir must be used as-is")
}

// --- extractDispatchWorkflowNames tests ---

// TestExtractDispatchWorkflowNames_ArrayFormat verifies that workflow names are extracted
// from the dispatch-workflow array (shorthand) format.
func TestExtractDispatchWorkflowNames_ArrayFormat(t *testing.T) {
	content := `---
engine: copilot
safe-outputs:
  dispatch-workflow:
    - workflow-a
    - workflow-b
---

# Workflow
`
	names := extractDispatchWorkflowNames(content)
	assert.Equal(t, []string{"workflow-a", "workflow-b"}, names, "should extract workflow names from array format")
}

// TestExtractDispatchWorkflowNames_MapFormat verifies that workflow names are extracted
// from the dispatch-workflow map format (with explicit workflows key).
func TestExtractDispatchWorkflowNames_MapFormat(t *testing.T) {
	content := `---
engine: copilot
safe-outputs:
  dispatch-workflow:
    workflows:
      - workflow-x
      - workflow-y
---

# Workflow
`
	names := extractDispatchWorkflowNames(content)
	assert.Equal(t, []string{"workflow-x", "workflow-y"}, names, "should extract workflow names from map format")
}

// TestExtractDispatchWorkflowNames_SkipMacros verifies that workflow names containing
// GitHub Actions expression syntax are filtered out.
func TestExtractDispatchWorkflowNames_SkipMacros(t *testing.T) {
	content := `---
engine: copilot
safe-outputs:
  dispatch-workflow:
    - plain-workflow
    - ${{ vars.WORKFLOW_NAME }}
    - ${{ needs.step.outputs.workflow }}
    - another-plain-workflow
---

# Workflow
`
	names := extractDispatchWorkflowNames(content)
	assert.Equal(t, []string{"plain-workflow", "another-plain-workflow"}, names, "should skip workflow names with GitHub Actions macro syntax")
}

// TestExtractDispatchWorkflowNames_NoSafeOutputs verifies that an empty slice is returned
// when there is no safe-outputs section.
func TestExtractDispatchWorkflowNames_NoSafeOutputs(t *testing.T) {
	content := `---
engine: copilot
---

# Workflow
`
	names := extractDispatchWorkflowNames(content)
	assert.Empty(t, names, "should return empty slice when no safe-outputs section")
}

// TestExtractDispatchWorkflowNames_NoDispatchWorkflow verifies that an empty slice is returned
// when safe-outputs exists but has no dispatch-workflow key.
func TestExtractDispatchWorkflowNames_NoDispatchWorkflow(t *testing.T) {
	content := `---
engine: copilot
safe-outputs:
  add-comment:
---

# Workflow
`
	names := extractDispatchWorkflowNames(content)
	assert.Empty(t, names, "should return empty slice when no dispatch-workflow key")
}

// TestExtractDispatchWorkflowNames_AllMacros verifies that all-macro lists return an empty slice.
func TestExtractDispatchWorkflowNames_AllMacros(t *testing.T) {
	content := `---
engine: copilot
safe-outputs:
  dispatch-workflow:
    - ${{ github.event.inputs.workflow }}
    - ${{ vars.WORKFLOW }}
---

# Workflow
`
	names := extractDispatchWorkflowNames(content)
	assert.Empty(t, names, "should return empty slice when all workflow names are macros")
}

// --- fetchAndSaveRemoteDispatchWorkflows tests ---

// TestFetchAndSaveRemoteDispatchWorkflows_NoSafeOutputs verifies that the function is a
// no-op when the workflow has no safe-outputs section.
func TestFetchAndSaveRemoteDispatchWorkflows_NoSafeOutputs(t *testing.T) {
	content := `---
engine: copilot
---

# Workflow
`
	spec := &WorkflowSpec{
		RepoSpec: RepoSpec{
			RepoSlug: "github/gh-aw",
			Version:  "main",
		},
		WorkflowPath: ".github/workflows/my-workflow.md",
	}

	tmpDir := t.TempDir()
	err := fetchAndSaveRemoteDispatchWorkflows(context.Background(), content, spec, tmpDir, false, false, nil)
	require.NoError(t, err, "should not error when no safe-outputs present")

	entries, readErr := os.ReadDir(tmpDir)
	require.NoError(t, readErr)
	assert.Empty(t, entries, "no files should be created when no dispatch-workflow configured")
}

// TestFetchAndSaveRemoteDispatchWorkflows_EmptyRepoSlug verifies that the function is a
// no-op when the spec has no remote repo (local workflow).
func TestFetchAndSaveRemoteDispatchWorkflows_EmptyRepoSlug(t *testing.T) {
	content := `---
engine: copilot
safe-outputs:
  dispatch-workflow:
    - dependent-workflow
---

# Workflow
`
	spec := &WorkflowSpec{
		RepoSpec: RepoSpec{
			RepoSlug: "", // local workflow
		},
		WorkflowPath: ".github/workflows/my-workflow.md",
	}

	tmpDir := t.TempDir()
	err := fetchAndSaveRemoteDispatchWorkflows(context.Background(), content, spec, tmpDir, false, false, nil)
	require.NoError(t, err, "should not error for local workflow")

	entries, readErr := os.ReadDir(tmpDir)
	require.NoError(t, readErr)
	assert.Empty(t, entries, "no files should be created for local workflows")
}

// TestFetchAndSaveRemoteDispatchWorkflows_OnlyMacros verifies that when all workflow names
// are GitHub Actions macro syntax, no download is attempted and the function is a no-op.
func TestFetchAndSaveRemoteDispatchWorkflows_OnlyMacros(t *testing.T) {
	content := `---
engine: copilot
safe-outputs:
  dispatch-workflow:
    - ${{ vars.WORKFLOW_TO_RUN }}
    - ${{ github.event.inputs.workflow }}
---

# Workflow
`
	spec := &WorkflowSpec{
		RepoSpec: RepoSpec{
			RepoSlug: "github/gh-aw",
			Version:  "main",
		},
		WorkflowPath: ".github/workflows/my-workflow.md",
	}

	tmpDir := t.TempDir()
	err := fetchAndSaveRemoteDispatchWorkflows(context.Background(), content, spec, tmpDir, false, false, nil)
	require.NoError(t, err, "should not error when all workflow names are macros")

	entries, readErr := os.ReadDir(tmpDir)
	require.NoError(t, readErr)
	assert.Empty(t, entries, "no files should be created when all workflow names are macros")
}

// TestFetchAndSaveRemoteDispatchWorkflows_SkipExistingWithoutForce verifies that an existing
// dispatch workflow file is not re-downloaded when force=false.
func TestFetchAndSaveRemoteDispatchWorkflows_SkipExistingWithoutForce(t *testing.T) {
	tmpDir := t.TempDir()
	// Pre-existing file with a matching source field so it is treated as same-source (skip).
	existingContent := []byte(`---
source: github/gh-aw/.github/workflows/dependent-workflow.md@v1.0.0
engine: copilot
---
# Existing dependent workflow
`)
	existingFile := filepath.Join(tmpDir, "dependent-workflow.md")
	require.NoError(t, os.WriteFile(existingFile, existingContent, 0600))

	content := `---
engine: copilot
safe-outputs:
  dispatch-workflow:
    - dependent-workflow
---

# Workflow
`
	spec := &WorkflowSpec{
		RepoSpec: RepoSpec{
			RepoSlug: "github/gh-aw",
			Version:  "v1.0.0",
		},
		WorkflowPath: ".github/workflows/my-workflow.md",
	}

	err := fetchAndSaveRemoteDispatchWorkflows(context.Background(), content, spec, tmpDir, false, false, nil)
	require.NoError(t, err)

	// The existing file must be untouched (no network call attempted because file already exists)
	gotContent, readErr := os.ReadFile(existingFile)
	require.NoError(t, readErr)
	assert.Equal(t, existingContent, gotContent, "pre-existing dispatch workflow file must not be modified when force=false")
}

// TestFetchAndSaveRemoteDispatchWorkflows_TrackerUpdated verifies that a pre-existing file
// that is skipped due to force=false does NOT appear in any tracker list.
func TestFetchAndSaveRemoteDispatchWorkflows_TrackerNoOpOnExisting(t *testing.T) {
	tmpDir := t.TempDir()
	existingFile := filepath.Join(tmpDir, "dep.md")
	// Pre-existing file with a matching source field so it is treated as same-source (skip).
	existingContent := `---
source: github/gh-aw/.github/workflows/dep.md@v1.0.0
engine: copilot
---
# Existing dep
`
	require.NoError(t, os.WriteFile(existingFile, []byte(existingContent), 0600))

	tracker := &FileTracker{
		OriginalContent: make(map[string][]byte),
		gitRoot:         tmpDir,
	}

	content := `---
engine: copilot
safe-outputs:
  dispatch-workflow:
    - dep
---
# Workflow
`
	spec := &WorkflowSpec{
		RepoSpec: RepoSpec{
			RepoSlug: "github/gh-aw",
			Version:  "v1.0.0",
		},
		WorkflowPath: ".github/workflows/my-workflow.md",
	}

	err := fetchAndSaveRemoteDispatchWorkflows(context.Background(), content, spec, tmpDir, false, false, tracker)
	require.NoError(t, err)
	assert.Empty(t, tracker.CreatedFiles, "pre-existing file must not appear in CreatedFiles")
	assert.Empty(t, tracker.ModifiedFiles, "pre-existing file must not appear in ModifiedFiles")
}

// TestFetchAndSaveRemoteDispatchWorkflows_InvalidRepoSlug verifies that an invalid
// RepoSlug (not in owner/repo format) causes the function to return early without error.
func TestFetchAndSaveRemoteDispatchWorkflows_InvalidRepoSlug(t *testing.T) {
	content := `---
engine: copilot
safe-outputs:
  dispatch-workflow:
    - dep-workflow
---
# Workflow
`
	spec := &WorkflowSpec{
		RepoSpec: RepoSpec{
			RepoSlug: "not-a-valid-slug",
		},
		WorkflowPath: ".github/workflows/my-workflow.md",
	}

	tmpDir := t.TempDir()
	err := fetchAndSaveRemoteDispatchWorkflows(context.Background(), content, spec, tmpDir, false, false, nil)
	require.NoError(t, err, "invalid RepoSlug should return nil without error")

	entries, readErr := os.ReadDir(tmpDir)
	require.NoError(t, readErr)
	assert.Empty(t, entries, "no files should be created for an invalid RepoSlug")
}

// --- fetchAndSaveRemoteCallWorkflows tests ---

// TestFetchAndSaveRemoteCallWorkflows_NoSafeOutputs verifies the function is a no-op when
// the workflow has no safe-outputs section.
func TestFetchAndSaveRemoteCallWorkflows_NoSafeOutputs(t *testing.T) {
	content := `---
engine: copilot
---

# Workflow
`
	spec := &WorkflowSpec{
		RepoSpec: RepoSpec{
			RepoSlug: "github/gh-aw",
			Version:  "main",
		},
		WorkflowPath: ".github/workflows/orchestrator.md",
	}

	tmpDir := t.TempDir()
	err := fetchAndSaveRemoteCallWorkflows(context.Background(), content, spec, tmpDir, false, false, nil)
	require.NoError(t, err)

	entries, readErr := os.ReadDir(tmpDir)
	require.NoError(t, readErr)
	assert.Empty(t, entries)
}

// TestFetchAndSaveRemoteCallWorkflows_EmptyRepoSlug verifies the function is a no-op for
// local workflows.
func TestFetchAndSaveRemoteCallWorkflows_EmptyRepoSlug(t *testing.T) {
	content := `---
engine: copilot
safe-outputs:
  call-workflow:
    - worker
---

# Workflow
`
	spec := &WorkflowSpec{
		RepoSpec: RepoSpec{
			RepoSlug: "", // local workflow
		},
		WorkflowPath: ".github/workflows/orchestrator.md",
	}

	tmpDir := t.TempDir()
	err := fetchAndSaveRemoteCallWorkflows(context.Background(), content, spec, tmpDir, false, false, nil)
	require.NoError(t, err)

	entries, readErr := os.ReadDir(tmpDir)
	require.NoError(t, readErr)
	assert.Empty(t, entries)
}

// TestFetchAndSaveRemoteCallWorkflows_OnlyMacros verifies that macro-only worker lists
// are skipped without error.
func TestFetchAndSaveRemoteCallWorkflows_OnlyMacros(t *testing.T) {
	content := `---
engine: copilot
safe-outputs:
  call-workflow:
    - ${{ vars.WORKER }}
---

# Workflow
`
	spec := &WorkflowSpec{
		RepoSpec: RepoSpec{
			RepoSlug: "github/gh-aw",
			Version:  "main",
		},
		WorkflowPath: ".github/workflows/orchestrator.md",
	}

	tmpDir := t.TempDir()
	err := fetchAndSaveRemoteCallWorkflows(context.Background(), content, spec, tmpDir, false, false, nil)
	require.NoError(t, err)

	entries, readErr := os.ReadDir(tmpDir)
	require.NoError(t, readErr)
	assert.Empty(t, entries)
}

// TestFetchAndSaveRemoteCallWorkflows_SkipExistingWithoutForce verifies that a pre-existing
// worker from the same source is silently skipped.
func TestFetchAndSaveRemoteCallWorkflows_SkipExistingWithoutForce(t *testing.T) {
	tmpDir := t.TempDir()
	// Pre-existing worker whose source matches what we would install.
	existingContent := []byte(`---
source: github/gh-aw/.github/workflows/worker.md@v1.0.0
engine: copilot
---
# Existing worker
`)
	existingFile := filepath.Join(tmpDir, "worker.md")
	require.NoError(t, os.WriteFile(existingFile, existingContent, 0600))

	content := `---
engine: copilot
safe-outputs:
  call-workflow:
    - worker
---

# Orchestrator
`
	spec := &WorkflowSpec{
		RepoSpec: RepoSpec{
			RepoSlug: "github/gh-aw",
			Version:  "v1.0.0",
		},
		WorkflowPath: ".github/workflows/orchestrator.md",
	}

	err := fetchAndSaveRemoteCallWorkflows(context.Background(), content, spec, tmpDir, false, false, nil)
	require.NoError(t, err)

	// File must be unchanged (no download attempted because source matches).
	gotContent, readErr := os.ReadFile(existingFile)
	require.NoError(t, readErr)
	assert.Equal(t, existingContent, gotContent)
}

// TestFetchAndSaveRemoteCallWorkflows_ConflictDifferentSource verifies that an existing
// worker from a different source causes an error when force=false.
func TestFetchAndSaveRemoteCallWorkflows_ConflictDifferentSource(t *testing.T) {
	tmpDir := t.TempDir()
	conflictContent := `---
source: otherorg/other-repo/.github/workflows/worker.md@v1
---
# Worker from other repo
`
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "worker.md"), []byte(conflictContent), 0600))

	content := `---
engine: copilot
safe-outputs:
  call-workflow:
    - worker
---

# Orchestrator
`
	spec := &WorkflowSpec{
		RepoSpec: RepoSpec{
			RepoSlug: "github/gh-aw",
			Version:  "main",
		},
		WorkflowPath: ".github/workflows/orchestrator.md",
	}

	err := fetchAndSaveRemoteCallWorkflows(context.Background(), content, spec, tmpDir, false, false, nil)
	require.Error(t, err)
	require.ErrorContains(t, err, "call-workflow worker")
	require.ErrorContains(t, err, "already exists")
}

// TestFetchAndSaveRemoteCallWorkflows_DownloadsWorker verifies that a worker is fetched
// from the remote repository and written with a source field.
func TestFetchAndSaveRemoteCallWorkflows_DownloadsWorker(t *testing.T) {
	tmpDir := t.TempDir()

	workerContent := `---
engine: copilot
---
# Worker
`
	downloader := func(_ context.Context, _, _, path, _ string) ([]byte, error) {
		if filepath.Ext(path) == ".md" {
			return []byte(workerContent), nil
		}
		return nil, errors.New("not found")
	}

	content := `---
engine: copilot
safe-outputs:
  call-workflow:
    - worker
---

# Orchestrator
`
	spec := &WorkflowSpec{
		RepoSpec: RepoSpec{
			RepoSlug: "github/gh-aw",
			Version:  "main",
		},
		WorkflowPath: ".github/workflows/orchestrator.md",
	}

	tracker := &FileTracker{
		OriginalContent: make(map[string][]byte),
		gitRoot:         tmpDir,
	}

	err := fetchAndSaveRemoteCallWorkflows(context.Background(), content, spec, tmpDir, false, false, tracker, downloader)
	require.NoError(t, err)

	workerPath := filepath.Join(tmpDir, "worker.md")
	got, readErr := os.ReadFile(workerPath)
	require.NoError(t, readErr)
	assert.Contains(t, string(got), "source:", "worker should have a source field injected")
	assert.Contains(t, string(got), "github/gh-aw", "worker source should include the repo slug")

	// New file must be in CreatedFiles.
	assert.Contains(t, tracker.CreatedFiles, workerPath)
}

// TestFetchAndSaveRemoteCallWorkflows_YmlFallback verifies that when the .md download
// fails, the function retries with a .yml extension.
func TestFetchAndSaveRemoteCallWorkflows_YmlFallback(t *testing.T) {
	tmpDir := t.TempDir()

	ymlContent := `name: Reusable worker
on:
  workflow_call:
jobs:
  run: {runs-on: ubuntu-latest, steps: [{run: echo hello}]}
`
	downloader := func(_ context.Context, _, _, remotePath, _ string) ([]byte, error) {
		switch filepath.Ext(remotePath) {
		case ".md":
			return nil, errors.New("not found")
		case ".yml":
			return []byte(ymlContent), nil
		}
		return nil, fmt.Errorf("unexpected path: %s", remotePath)
	}

	content := `---
engine: copilot
safe-outputs:
  call-workflow:
    - worker
---

# Orchestrator
`
	spec := &WorkflowSpec{
		RepoSpec: RepoSpec{
			RepoSlug: "github/gh-aw",
			Version:  "main",
		},
		WorkflowPath: ".github/workflows/orchestrator.md",
	}

	tracker := &FileTracker{
		OriginalContent: make(map[string][]byte),
		gitRoot:         tmpDir,
	}

	err := fetchAndSaveRemoteCallWorkflows(context.Background(), content, spec, tmpDir, false, false, tracker, downloader)
	require.NoError(t, err)

	ymlPath := filepath.Join(tmpDir, "worker.yml")
	got, readErr := os.ReadFile(ymlPath)
	require.NoError(t, readErr)
	assert.YAMLEq(t, ymlContent, string(got), "yml fallback content should be written verbatim")

	// New .yml file must be in CreatedFiles.
	assert.Contains(t, tracker.CreatedFiles, ymlPath)
}

func TestFetchAndSaveRemoteDispatchWorkflows_YmlFallback(t *testing.T) {
	tmpDir := t.TempDir()
	ymlContent := `name: Dispatched workflow
on:
  workflow_dispatch:
jobs:
  run: {runs-on: ubuntu-latest, steps: [{run: echo hello}]}
`
	downloader := func(_ context.Context, _, _, remotePath, _ string) ([]byte, error) {
		if filepath.Ext(remotePath) == ".yml" {
			return []byte(ymlContent), nil
		}
		return nil, errors.New("not found")
	}
	content := `---
engine: copilot
safe-outputs:
  dispatch-workflow:
    - worker
---
`
	spec := &WorkflowSpec{
		RepoSpec:     RepoSpec{RepoSlug: "github/gh-aw", Version: "main"},
		WorkflowPath: ".github/workflows/orchestrator.md",
	}
	tracker := &FileTracker{OriginalContent: make(map[string][]byte), gitRoot: tmpDir}

	err := fetchAndSaveRemoteDispatchWorkflows(context.Background(), content, spec, tmpDir, false, false, tracker, downloader)
	require.NoError(t, err)

	ymlPath := filepath.Join(tmpDir, "worker.yml")
	got, err := os.ReadFile(ymlPath)
	require.NoError(t, err)
	assert.YAMLEq(t, ymlContent, string(got))
	assert.Contains(t, tracker.CreatedFiles, ymlPath)
}

// TestFetchAndSaveRemoteDispatchWorkflows_ConflictDifferentSource verifies that an existing
// dispatch workflow from a different source causes an error when force=false.
func TestFetchAndSaveRemoteDispatchWorkflows_ConflictDifferentSource(t *testing.T) {
	tmpDir := t.TempDir()
	conflictContent := `---
source: otherorg/other-repo/.github/workflows/worker.md@v1
---
# Worker from other repo
`
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "worker.md"), []byte(conflictContent), 0600))

	content := `---
engine: copilot
safe-outputs:
  dispatch-workflow:
    - worker
---

# Orchestrator
`
	spec := &WorkflowSpec{
		RepoSpec: RepoSpec{
			RepoSlug: "github/gh-aw",
			Version:  "main",
		},
		WorkflowPath: ".github/workflows/orchestrator.md",
	}

	err := fetchAndSaveRemoteDispatchWorkflows(context.Background(), content, spec, tmpDir, false, false, nil)
	require.Error(t, err)
	require.ErrorContains(t, err, "dispatch workflow")
	require.ErrorContains(t, err, "already exists")

	// The existing file must be untouched.
	gotContent, readErr := os.ReadFile(filepath.Join(tmpDir, "worker.md"))
	require.NoError(t, readErr)
	assert.Equal(t, conflictContent, string(gotContent))
}

// TestFetchAndSaveRemoteDispatchWorkflows_TrackingBeforeWrite verifies that TrackModified is
// called with the original file content (i.e. before os.WriteFile overwrites it), so that
// RollbackAllFiles can restore the correct previous state.
func TestFetchAndSaveRemoteDispatchWorkflows_TrackingBeforeWrite(t *testing.T) {
	tmpDir := t.TempDir()

	originalContent := `---
source: github/gh-aw/.github/workflows/worker.md@v0.9.0
engine: copilot
---
# Old worker
`
	workerPath := filepath.Join(tmpDir, "worker.md")
	require.NoError(t, os.WriteFile(workerPath, []byte(originalContent), 0600))

	newContent := `---
engine: copilot
---
# New worker
`
	downloader := func(_ context.Context, _, _, remotePath, _ string) ([]byte, error) {
		if filepath.Ext(remotePath) == ".md" {
			return []byte(newContent), nil
		}
		return nil, errors.New("not found")
	}

	content := `---
engine: copilot
safe-outputs:
  dispatch-workflow:
    - worker
---

# Orchestrator
`
	spec := &WorkflowSpec{
		RepoSpec: RepoSpec{
			RepoSlug: "github/gh-aw",
			Version:  "v1.0.0",
		},
		WorkflowPath: ".github/workflows/orchestrator.md",
	}

	tracker := &FileTracker{
		OriginalContent: make(map[string][]byte),
		gitRoot:         tmpDir,
	}

	err := fetchAndSaveRemoteDispatchWorkflows(context.Background(), content, spec, tmpDir, false, true, tracker, downloader)
	require.NoError(t, err)

	// The tracker must have captured the *original* content, not the newly written bytes.
	absWorkerPath, _ := filepath.Abs(workerPath)
	captured, ok := tracker.OriginalContent[absWorkerPath]
	require.True(t, ok, "worker.md should be in OriginalContent")
	assert.Equal(t, []byte(originalContent), captured, "tracker should capture original content before overwrite")
}

// TestFetchAndSaveRemoteCallWorkflows_TrackingBeforeWrite verifies that TrackModified is
// called with the original file content (i.e. before os.WriteFile overwrites it), so that
// RollbackAllFiles can restore the correct previous state.
func TestFetchAndSaveRemoteCallWorkflows_TrackingBeforeWrite(t *testing.T) {
	tmpDir := t.TempDir()

	originalContent := `---
source: github/gh-aw/.github/workflows/worker.md@v0.9.0
engine: copilot
---
# Old worker
`
	workerPath := filepath.Join(tmpDir, "worker.md")
	require.NoError(t, os.WriteFile(workerPath, []byte(originalContent), 0600))

	newContent := `---
engine: copilot
---
# New worker
`
	downloader := func(_ context.Context, _, _, remotePath, _ string) ([]byte, error) {
		if filepath.Ext(remotePath) == ".md" {
			return []byte(newContent), nil
		}
		return nil, errors.New("not found")
	}

	content := `---
engine: copilot
safe-outputs:
  call-workflow:
    - worker
---

# Orchestrator
`
	spec := &WorkflowSpec{
		RepoSpec: RepoSpec{
			RepoSlug: "github/gh-aw",
			Version:  "v1.0.0",
		},
		WorkflowPath: ".github/workflows/orchestrator.md",
	}

	tracker := &FileTracker{
		OriginalContent: make(map[string][]byte),
		gitRoot:         tmpDir,
	}

	err := fetchAndSaveRemoteCallWorkflows(context.Background(), content, spec, tmpDir, false, true, tracker, downloader)
	require.NoError(t, err)

	// The tracker must have captured the *original* content, not the newly written bytes.
	absWorkerPath, _ := filepath.Abs(workerPath)
	captured, ok := tracker.OriginalContent[absWorkerPath]
	require.True(t, ok, "worker.md should be in OriginalContent")
	assert.Equal(t, []byte(originalContent), captured, "tracker should capture original content before overwrite")
}

// TestFetchAndSaveRemoteCallWorkflows_MapFormConfig verifies that the map-form
// call-workflow configuration (workflows: [...]) is also handled.
func TestFetchAndSaveRemoteCallWorkflows_MapFormConfig(t *testing.T) {
	tmpDir := t.TempDir()

	downloader := func(_ context.Context, _, _, remotePath, _ string) ([]byte, error) {
		if filepath.Ext(remotePath) == ".md" {
			return []byte("# worker\n"), nil
		}
		return nil, errors.New("not found")
	}

	content := `---
engine: copilot
safe-outputs:
  call-workflow:
    workflows:
      - worker-a
      - worker-b
---

# Orchestrator
`
	spec := &WorkflowSpec{
		RepoSpec: RepoSpec{
			RepoSlug: "github/gh-aw",
			Version:  "main",
		},
		WorkflowPath: ".github/workflows/orchestrator.md",
	}

	err := fetchAndSaveRemoteCallWorkflows(context.Background(), content, spec, tmpDir, false, false, nil, downloader)
	require.NoError(t, err)

	for _, name := range []string{"worker-a", "worker-b"} {
		_, statErr := os.Stat(filepath.Join(tmpDir, name+".md"))
		require.NoError(t, statErr, "%s.md should be created", name)
	}
}

func TestFetchAndSaveRemoteResources_InstallsAndRestoresGraderEvaluator(t *testing.T) {
	tmpDir := t.TempDir()
	setupMinimalGitRepo(t, tmpDir)
	workflowsDir := filepath.Join(tmpDir, ".github", "workflows")
	require.NoError(t, os.MkdirAll(workflowsDir, 0o755))

	const evaluatorPath = ".github/workflows/graders/example-operational-value.sh"
	content := `---
on: workflow_dispatch
graders:
  operational-value:
    run: .github/workflows/graders/example-operational-value.sh
---

# Workflow
`
	evaluatorContent := []byte("#!/usr/bin/env bash\necho old\n")
	download := func(_ context.Context, owner, repo, filePath, ref string) ([]byte, error) {
		assert.Equal(t, "workflows/graders/example-operational-value.sh", filePath)
		return evaluatorContent, nil
	}

	spec := &WorkflowSpec{
		RepoSpec:     RepoSpec{RepoSlug: "owner/repo", Version: "main"},
		WorkflowPath: "workflows/graded.md",
	}
	require.NoError(t, fetchAndSaveRemoteResourcesWithDownloader(t.Context(), content, spec, workflowsDir, false, false, nil, download))

	installedPath := filepath.Join(tmpDir, filepath.FromSlash(evaluatorPath))
	installed, err := os.ReadFile(installedPath)
	require.NoError(t, err)
	assert.Equal(t, evaluatorContent, installed)
	require.NoError(t, fetchAndSaveRemoteResourcesWithDownloader(t.Context(), content, spec, workflowsDir, false, false, nil, download))

	evaluatorContent = []byte("#!/usr/bin/env bash\necho conflict\n")
	err = fetchAndSaveRemoteResourcesWithDownloader(t.Context(), content, spec, workflowsDir, false, false, nil, download)
	require.ErrorContains(t, err, evaluatorPath)
	require.ErrorContains(t, err, "--force")

	workflowPath := filepath.Join(workflowsDir, "graded.md")
	require.NoError(t, os.WriteFile(workflowPath, []byte(content), 0o644))
	compiler := workflow.NewCompiler()
	compiler.SetNoEmit(true)
	require.NoError(t, compiler.CompileWorkflow(workflowPath))

	require.NoError(t, os.Remove(installedPath))
	evaluatorContent = []byte("#!/usr/bin/env bash\necho new\n")
	require.NoError(t, fetchAndSaveRemoteResourcesWithDownloader(t.Context(), content, spec, workflowsDir, false, true, nil, download))
	restored, err := os.ReadFile(installedPath)
	require.NoError(t, err)
	assert.Equal(t, evaluatorContent, restored)
}

// TestFetchAndSaveRemoteResources_InstallsRepoRootGraderEvaluatorFromNestedPackage verifies that
// a repository-root anchored grader evaluator (not under ".github/workflows") is fetched from
// its exact repository-root path even when the declaring workflow belongs to a nested package,
// instead of being incorrectly prefixed with the package's own directory.
func TestFetchAndSaveRemoteResources_InstallsRepoRootGraderEvaluatorFromNestedPackage(t *testing.T) {
	tmpDir := t.TempDir()
	setupMinimalGitRepo(t, tmpDir)
	workflowsDir := filepath.Join(tmpDir, ".github", "workflows")
	require.NoError(t, os.MkdirAll(workflowsDir, 0o755))

	const evaluatorPath = ".github/graders/shared-operational-value.sh"
	content := `---
on: workflow_dispatch
graders:
  operational-value:
    run: .github/graders/shared-operational-value.sh
---

# Workflow
`
	evaluatorContent := []byte("#!/usr/bin/env bash\necho shared\n")
	download := func(_ context.Context, owner, repo, filePath, ref string) ([]byte, error) {
		assert.Equal(t, evaluatorPath, filePath)
		return evaluatorContent, nil
	}

	spec := &WorkflowSpec{
		RepoSpec:     RepoSpec{RepoSlug: "owner/repo", Version: "main", PackagePath: "packages/repo-assist"},
		WorkflowPath: "packages/repo-assist/workflows/graded.md",
	}
	require.NoError(t, fetchAndSaveRemoteResourcesWithDownloader(t.Context(), content, spec, workflowsDir, false, false, nil, download))

	installed, err := os.ReadFile(filepath.Join(tmpDir, filepath.FromSlash(evaluatorPath)))
	require.NoError(t, err)
	assert.Equal(t, evaluatorContent, installed)
}

func TestFetchAndSaveRemoteResources_InstallsLocalDotGraderEvaluator(t *testing.T) {
	tmpDir := t.TempDir()
	setupMinimalGitRepo(t, tmpDir)
	workflowsDir := filepath.Join(tmpDir, ".github", "workflows")
	require.NoError(t, os.MkdirAll(workflowsDir, 0o755))

	content := `---
on: workflow_dispatch
graders:
  operational-value:
    run: ./scripts/example-operational-value.sh
---

# Workflow
`
	evaluatorContent := []byte("#!/usr/bin/env bash\necho local\n")
	download := func(_ context.Context, owner, repo, filePath, ref string) ([]byte, error) {
		assert.Equal(t, "workflows/scripts/example-operational-value.sh", filePath)
		return evaluatorContent, nil
	}

	spec := &WorkflowSpec{
		RepoSpec:     RepoSpec{RepoSlug: "owner/repo", Version: "main"},
		WorkflowPath: "workflows/graded.md",
	}
	require.NoError(t, fetchAndSaveRemoteResourcesWithDownloader(t.Context(), content, spec, workflowsDir, false, false, nil, download))

	installed, err := os.ReadFile(filepath.Join(workflowsDir, "scripts", "example-operational-value.sh"))
	require.NoError(t, err)
	assert.Equal(t, evaluatorContent, installed)
}

func TestFetchAndSaveRemoteResources_RejectsGraderEvaluatorSymlinkedParent(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation requires additional privileges on Windows")
	}
	tmpDir := t.TempDir()
	setupMinimalGitRepo(t, tmpDir)
	workflowsDir := filepath.Join(tmpDir, ".github", "workflows")
	require.NoError(t, os.MkdirAll(workflowsDir, 0o755))
	outsideDir := t.TempDir()
	require.NoError(t, os.Symlink(outsideDir, filepath.Join(workflowsDir, "scripts")))

	content := `---
on: workflow_dispatch
graders:
  operational-value:
    run: ./scripts/example-operational-value.sh
---

# Workflow
`
	download := func(_ context.Context, owner, repo, filePath, ref string) ([]byte, error) {
		return []byte("#!/usr/bin/env bash\necho unsafe\n"), nil
	}

	spec := &WorkflowSpec{
		RepoSpec:     RepoSpec{RepoSlug: "owner/repo", Version: "main"},
		WorkflowPath: "workflows/graded.md",
	}
	require.NoError(t, fetchAndSaveRemoteResourcesWithDownloader(t.Context(), content, spec, workflowsDir, false, true, nil, download))
	assert.NoFileExists(t, filepath.Join(outsideDir, "example-operational-value.sh"))
}

// TestFetchAndSaveRemoteResources_NoResources verifies that the function is a no-op when the
// workflow has no resources field.
func TestFetchAndSaveRemoteResources_NoResources(t *testing.T) {
	content := `---
engine: copilot
on: issues
---

# Workflow
`
	spec := &WorkflowSpec{
		RepoSpec: RepoSpec{
			RepoSlug: "github/gh-aw",
			Version:  "main",
		},
		WorkflowPath: ".github/workflows/my-workflow.md",
	}

	tmpDir := t.TempDir()
	err := fetchAndSaveRemoteResources(t.Context(), content, spec, tmpDir, false, false, nil)
	require.NoError(t, err, "should not error when no resources field")

	entries, readErr := os.ReadDir(tmpDir)
	require.NoError(t, readErr)
	assert.Empty(t, entries, "no files should be created when no resources configured")
}

// TestFetchAndSaveRemoteResources_EmptyRepoSlug verifies that the function is a no-op for local workflows.
func TestFetchAndSaveRemoteResources_EmptyRepoSlug(t *testing.T) {
	content := `---
engine: copilot
on: issues
resources:
  - triage-issue.md
---

# Workflow
`
	spec := &WorkflowSpec{
		RepoSpec: RepoSpec{
			RepoSlug: "",
		},
		WorkflowPath: ".github/workflows/my-workflow.md",
	}

	tmpDir := t.TempDir()
	err := fetchAndSaveRemoteResources(t.Context(), content, spec, tmpDir, false, false, nil)
	require.NoError(t, err, "should not error for local workflow")

	entries, readErr := os.ReadDir(tmpDir)
	require.NoError(t, readErr)
	assert.Empty(t, entries, "no files should be created for local workflows")
}

// TestFetchAndSaveRemoteResources_MacroRejected verifies that resources with macro syntax return an error.
func TestFetchAndSaveRemoteResources_MacroRejected(t *testing.T) {
	content := `---
engine: copilot
on: issues
resources:
  - ${{ vars.WORKFLOW }}
---

# Workflow
`
	spec := &WorkflowSpec{
		RepoSpec: RepoSpec{
			RepoSlug: "github/gh-aw",
			Version:  "main",
		},
		WorkflowPath: ".github/workflows/my-workflow.md",
	}

	tmpDir := t.TempDir()
	err := fetchAndSaveRemoteResources(t.Context(), content, spec, tmpDir, false, false, nil)
	require.Error(t, err, "should error when resources contain macro syntax")
	require.ErrorContains(t, err, "${{", "error should mention the disallowed syntax")

	entries, readErr := os.ReadDir(tmpDir)
	require.NoError(t, readErr)
	assert.Empty(t, entries, "no files should be created when resources contain macros")
}

// TestFetchAndSaveRemoteResources_SkipExistingWithoutForce verifies that an existing resource
// file is not re-downloaded when force=false.
func TestFetchAndSaveRemoteResources_SkipExistingWithoutForce(t *testing.T) {
	tmpDir := t.TempDir()
	// Pre-existing file with a matching source field so it is treated as same-source (skip).
	existingContent := []byte(`---
source: github/gh-aw/.github/workflows/triage-issue.md@v1.0.0
engine: copilot
---
# Existing triage issue workflow
`)
	existingFile := filepath.Join(tmpDir, "triage-issue.md")
	require.NoError(t, os.WriteFile(existingFile, existingContent, 0600))

	content := `---
engine: copilot
on: issues
resources:
  - triage-issue.md
---

# Workflow
`
	spec := &WorkflowSpec{
		RepoSpec: RepoSpec{
			RepoSlug: "github/gh-aw",
			Version:  "v1.0.0",
		},
		WorkflowPath: ".github/workflows/my-workflow.md",
	}

	err := fetchAndSaveRemoteResources(t.Context(), content, spec, tmpDir, false, false, nil)
	require.NoError(t, err)

	gotContent, readErr := os.ReadFile(existingFile)
	require.NoError(t, readErr)
	assert.Equal(t, existingContent, gotContent, "pre-existing resource file must not be modified when force=false")
}

// TestFetchAndSaveRemoteResources_PathTraversal verifies that path traversal attempts are rejected.
func TestFetchAndSaveRemoteResources_PathTraversal(t *testing.T) {
	content := `---
engine: copilot
on: issues
resources:
  - ../etc/passwd
  - ../../etc/shadow
---

# Workflow
`
	spec := &WorkflowSpec{
		RepoSpec: RepoSpec{
			RepoSlug: "github/gh-aw",
			Version:  "v1.0.0",
		},
		WorkflowPath: ".github/workflows/my-workflow.md",
	}

	tmpDir := t.TempDir()
	err := fetchAndSaveRemoteResources(t.Context(), content, spec, tmpDir, false, false, nil)
	require.NoError(t, err, "path traversal should be silently rejected")

	entries, readErr := os.ReadDir(tmpDir)
	require.NoError(t, readErr)
	assert.Empty(t, entries, "traversal resources must not write any file")
}

// TestFetchAndSaveRemoteResources_InvalidRepoSlug verifies early return for invalid slug.
func TestFetchAndSaveRemoteResources_InvalidRepoSlug(t *testing.T) {
	content := `---
engine: copilot
on: issues
resources:
  - triage-issue.md
---
# Workflow
`
	spec := &WorkflowSpec{
		RepoSpec: RepoSpec{
			RepoSlug: "not-a-valid-slug",
		},
		WorkflowPath: ".github/workflows/my-workflow.md",
	}

	tmpDir := t.TempDir()
	err := fetchAndSaveRemoteResources(t.Context(), content, spec, tmpDir, false, false, nil)
	require.NoError(t, err, "invalid RepoSlug should return nil without error")

	entries, readErr := os.ReadDir(tmpDir)
	require.NoError(t, readErr)
	assert.Empty(t, entries, "no files should be created for an invalid RepoSlug")
}

// TestFetchAndSaveRemoteResources_TrackerNoOpOnExisting verifies that a pre-existing resource
// that is skipped (force=false) does NOT appear in any tracker list.
func TestFetchAndSaveRemoteResources_TrackerNoOpOnExisting(t *testing.T) {
	tmpDir := t.TempDir()
	existingFile := filepath.Join(tmpDir, "resource.md")
	// Pre-existing file with a matching source field so it is treated as same-source (skip).
	existingContent := `---
source: github/gh-aw/.github/workflows/resource.md@v1.0.0
engine: copilot
---
# Existing resource
`
	require.NoError(t, os.WriteFile(existingFile, []byte(existingContent), 0600))

	tracker := &FileTracker{
		OriginalContent: make(map[string][]byte),
		gitRoot:         tmpDir,
	}

	content := `---
engine: copilot
on: issues
resources:
  - resource.md
---
# Workflow
`
	spec := &WorkflowSpec{
		RepoSpec: RepoSpec{
			RepoSlug: "github/gh-aw",
			Version:  "v1.0.0",
		},
		WorkflowPath: ".github/workflows/my-workflow.md",
	}

	err := fetchAndSaveRemoteResources(t.Context(), content, spec, tmpDir, false, false, tracker)
	require.NoError(t, err)
	assert.Empty(t, tracker.CreatedFiles, "pre-existing file must not appear in CreatedFiles")
	assert.Empty(t, tracker.ModifiedFiles, "pre-existing file must not appear in ModifiedFiles")
}

// --- fetchAndSaveDispatchWorkflowsFromParsedFile tests ---
//
// These tests verify that dispatch workflows discovered via parsed (compiled) workflow data are
// handled correctly — including the key shared agentic workflow scenario where dispatch-workflow
// config comes from an imported shared workflow rather than from the main workflow's own frontmatter.

// TestFetchAndSaveDispatchWorkflowsFromParsedFile_EmptyRepoSlug verifies that a local
// (non-remote) workflow is a no-op.
func TestFetchAndSaveDispatchWorkflowsFromParsedFile_EmptyRepoSlug(t *testing.T) {
	tmpDir := t.TempDir()
	spec := &WorkflowSpec{
		RepoSpec:     RepoSpec{RepoSlug: ""},
		WorkflowPath: ".github/workflows/my-workflow.md",
	}

	fetchAndSaveDispatchWorkflowsFromParsedFile(t.Context(), filepath.Join(tmpDir, "nonexistent.md"), spec, tmpDir, false, false, nil)

	entries, err := os.ReadDir(tmpDir)
	require.NoError(t, err)
	assert.Empty(t, entries, "no files should be created for local workflows")
}

// TestFetchAndSaveDispatchWorkflowsFromParsedFile_InvalidRepoSlug verifies that an invalid
// RepoSlug (not owner/repo format) is a no-op.
func TestFetchAndSaveDispatchWorkflowsFromParsedFile_InvalidRepoSlug(t *testing.T) {
	tmpDir := t.TempDir()
	spec := &WorkflowSpec{
		RepoSpec:     RepoSpec{RepoSlug: "not-valid"},
		WorkflowPath: ".github/workflows/my-workflow.md",
	}

	fetchAndSaveDispatchWorkflowsFromParsedFile(t.Context(), filepath.Join(tmpDir, "nonexistent.md"), spec, tmpDir, false, false, nil)

	entries, err := os.ReadDir(tmpDir)
	require.NoError(t, err)
	assert.Empty(t, entries, "no files should be created for invalid RepoSlug")
}

// TestFetchAndSaveDispatchWorkflowsFromParsedFile_ParseFailure verifies that a missing or
// unparseable workflow file is handled silently (best-effort no-op).
func TestFetchAndSaveDispatchWorkflowsFromParsedFile_ParseFailure(t *testing.T) {
	tmpDir := t.TempDir()
	spec := &WorkflowSpec{
		RepoSpec:     RepoSpec{RepoSlug: "github/gh-aw", Version: "main"},
		WorkflowPath: ".github/workflows/my-workflow.md",
	}

	// Point to a file that does not exist — ParseWorkflowFile will fail.
	fetchAndSaveDispatchWorkflowsFromParsedFile(t.Context(), filepath.Join(tmpDir, "does-not-exist.md"), spec, tmpDir, false, false, nil)

	entries, err := os.ReadDir(tmpDir)
	require.NoError(t, err)
	assert.Empty(t, entries, "parse failures must not create any files")
}

// TestFetchAndSaveDispatchWorkflowsFromParsedFile_NoDispatchWorkflows verifies that a
// workflow with no safe-outputs dispatch-workflow config is a no-op.
func TestFetchAndSaveDispatchWorkflowsFromParsedFile_NoDispatchWorkflows(t *testing.T) {
	tmpDir := t.TempDir()
	workflowsDir := filepath.Join(tmpDir, ".github", "workflows")
	require.NoError(t, os.MkdirAll(workflowsDir, 0755))

	mainPath := filepath.Join(workflowsDir, "my-workflow.md")
	mainContent := `---
on: issues
engine: copilot
permissions:
  issues: read
  contents: read
---

# Workflow with no dispatch-workflow
`
	require.NoError(t, os.WriteFile(mainPath, []byte(mainContent), 0644))

	spec := &WorkflowSpec{
		RepoSpec:     RepoSpec{RepoSlug: "github/gh-aw", Version: "main"},
		WorkflowPath: ".github/workflows/my-workflow.md",
	}

	fetchAndSaveDispatchWorkflowsFromParsedFile(t.Context(), mainPath, spec, workflowsDir, false, false, nil)

	// Only the main workflow itself should be in the directory.
	entries, err := os.ReadDir(workflowsDir)
	require.NoError(t, err)
	assert.Len(t, entries, 1, "only the main workflow file should exist")
}

// TestFetchAndSaveDispatchWorkflowsFromParsedFile_SharedWorkflow_SkipExisting is the key
// shared agentic workflow test: the dispatch-workflow list comes from an *imported* shared
// workflow, and the referenced dispatch workflow file already exists locally.
//
// Setup:
//
//	.github/workflows/
//	  main.md          — imports shared/dispatch-helper.md
//	  shared/
//	    dispatch-helper.md  — shared workflow; defines safe-outputs.dispatch-workflow
//	  triage-issue.md  — dispatch workflow that already exists locally
//
// Expected: fetchAndSaveDispatchWorkflowsFromParsedFile discovers "triage-issue" via the
// parsed (merged) safe-outputs config, finds the file already present, and skips without
// modifying it.
func TestFetchAndSaveDispatchWorkflowsFromParsedFile_SharedWorkflow_SkipExisting(t *testing.T) {
	tmpDir := t.TempDir()
	workflowsDir := filepath.Join(tmpDir, ".github", "workflows")
	sharedDir := filepath.Join(workflowsDir, "shared")
	require.NoError(t, os.MkdirAll(sharedDir, 0755))

	// Shared workflow defines the dispatch-workflow config (no 'on:' field → treated as shared).
	sharedPath := filepath.Join(sharedDir, "dispatch-helper.md")
	sharedContent := `---
safe-outputs:
  dispatch-workflow:
    workflows:
      - triage-issue
---

Shared helper that configures dispatch-workflow.
`
	require.NoError(t, os.WriteFile(sharedPath, []byte(sharedContent), 0644))

	// Main workflow imports the shared workflow and provides its own triggers.
	mainPath := filepath.Join(workflowsDir, "main.md")
	mainContent := `---
on: issues
engine: copilot
permissions:
  issues: write
  contents: read
imports:
  - shared/dispatch-helper.md
---

# Main Workflow

Process incoming issues.
`
	require.NoError(t, os.WriteFile(mainPath, []byte(mainContent), 0644))

	// The dispatch workflow file already exists locally with known content.
	existingContent := []byte("# Triage Issue workflow")
	triagePath := filepath.Join(workflowsDir, "triage-issue.md")
	require.NoError(t, os.WriteFile(triagePath, existingContent, 0644))

	spec := &WorkflowSpec{
		RepoSpec:     RepoSpec{RepoSlug: "github/gh-aw", Version: "v1.0.0"},
		WorkflowPath: ".github/workflows/main.md",
	}

	fetchAndSaveDispatchWorkflowsFromParsedFile(t.Context(), mainPath, spec, workflowsDir, false, false, nil)

	// The pre-existing dispatch workflow must not be modified.
	got, err := os.ReadFile(triagePath)
	require.NoError(t, err)
	assert.Equal(t, existingContent, got, "pre-existing dispatch workflow file must not be modified")
}

// TestFetchAndSaveDispatchWorkflowsFromParsedFile_SharedWorkflow_TrackerNoOpOnExisting verifies
// that a pre-existing dispatch workflow discovered via a shared workflow import does NOT appear
// in the tracker's created or modified lists when force=false.
func TestFetchAndSaveDispatchWorkflowsFromParsedFile_SharedWorkflow_TrackerNoOpOnExisting(t *testing.T) {
	tmpDir := t.TempDir()
	workflowsDir := filepath.Join(tmpDir, ".github", "workflows")
	sharedDir := filepath.Join(workflowsDir, "shared")
	require.NoError(t, os.MkdirAll(sharedDir, 0755))

	// Shared workflow with dispatch-workflow config.
	sharedPath := filepath.Join(sharedDir, "dispatch-helper.md")
	sharedContent := `---
safe-outputs:
  dispatch-workflow:
    workflows:
      - triage-issue
---

Shared dispatch configuration.
`
	require.NoError(t, os.WriteFile(sharedPath, []byte(sharedContent), 0644))

	// Main workflow imports the shared workflow.
	mainPath := filepath.Join(workflowsDir, "main.md")
	mainContent := `---
on: issues
engine: copilot
permissions:
  issues: write
  contents: read
imports:
  - shared/dispatch-helper.md
---

# Main Workflow
`
	require.NoError(t, os.WriteFile(mainPath, []byte(mainContent), 0644))

	// Dispatch workflow already on disk.
	triagePath := filepath.Join(workflowsDir, "triage-issue.md")
	require.NoError(t, os.WriteFile(triagePath, []byte("# Triage"), 0644))

	tracker := &FileTracker{
		OriginalContent: make(map[string][]byte),
		gitRoot:         workflowsDir,
	}

	spec := &WorkflowSpec{
		RepoSpec:     RepoSpec{RepoSlug: "github/gh-aw", Version: "v1.0.0"},
		WorkflowPath: ".github/workflows/main.md",
	}

	fetchAndSaveDispatchWorkflowsFromParsedFile(t.Context(), mainPath, spec, workflowsDir, false, false, tracker)

	assert.Empty(t, tracker.CreatedFiles, "pre-existing dispatch workflow must not appear in CreatedFiles")
	assert.Empty(t, tracker.ModifiedFiles, "pre-existing dispatch workflow must not appear in ModifiedFiles")
}

// TestFetchAndSaveDispatchWorkflowsFromParsedFile_SharedWorkflow_MacroWorkflowSkipped verifies
// that workflow names containing GitHub Actions expression syntax (${{) in the merged
// dispatch-workflow list are silently skipped.
func TestFetchAndSaveDispatchWorkflowsFromParsedFile_SharedWorkflow_MacroWorkflowSkipped(t *testing.T) {
	tmpDir := t.TempDir()
	workflowsDir := filepath.Join(tmpDir, ".github", "workflows")
	sharedDir := filepath.Join(workflowsDir, "shared")
	require.NoError(t, os.MkdirAll(sharedDir, 0755))

	// Shared workflow whose dispatch-workflow list contains a macro-valued entry.
	sharedPath := filepath.Join(sharedDir, "dispatch-helper.md")
	sharedContent := `---
safe-outputs:
  dispatch-workflow:
    workflows:
      - static-workflow
      - ${{ vars.DYNAMIC_WORKFLOW }}
---

Shared dispatch helper with mixed static and macro workflow names.
`
	require.NoError(t, os.WriteFile(sharedPath, []byte(sharedContent), 0644))

	// Main workflow.
	mainPath := filepath.Join(workflowsDir, "main.md")
	mainContent := `---
on: issues
engine: copilot
permissions:
  issues: write
  contents: read
imports:
  - shared/dispatch-helper.md
---

# Main Workflow
`
	require.NoError(t, os.WriteFile(mainPath, []byte(mainContent), 0644))

	// Pre-create static-workflow.md so the fetch is skipped without a network call.
	staticPath := filepath.Join(workflowsDir, "static-workflow.md")
	require.NoError(t, os.WriteFile(staticPath, []byte("# Static"), 0644))

	spec := &WorkflowSpec{
		RepoSpec:     RepoSpec{RepoSlug: "github/gh-aw", Version: "v1.0.0"},
		WorkflowPath: ".github/workflows/main.md",
	}

	fetchAndSaveDispatchWorkflowsFromParsedFile(t.Context(), mainPath, spec, workflowsDir, false, false, nil)

	// No file named after the macro entry should have been created.
	macroFile := filepath.Join(workflowsDir, "${{ vars.DYNAMIC_WORKFLOW }}.md")
	_, err := os.Stat(macroFile)
	assert.True(t, os.IsNotExist(err), "macro-valued dispatch workflow must not create a file")
}

// ---------------------------------------------------------------------------
// readSourceRepoFromFile
// ---------------------------------------------------------------------------

func TestSourceRepoLabel_NonEmpty(t *testing.T) {
	assert.Equal(t, "github/gh-aw", sourceRepoLabel("github/gh-aw"), "non-empty repo should pass through")
}

func TestSourceRepoLabel_Empty(t *testing.T) {
	assert.Equal(t, "(no source field)", sourceRepoLabel(""), "empty repo should return placeholder label")
}

func TestReadSourceRepoFromFile_ValidSource(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "wf.md")
	content := `---
source: github/gh-aw/.github/workflows/wf.md@abc123
---
# Workflow
`
	require.NoError(t, os.WriteFile(f, []byte(content), 0644))
	assert.Equal(t, "github/gh-aw", readSourceRepoFromFile(f), "should return owner/repo")
}

func TestReadSourceRepoFromFile_NoSource(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "wf.md")
	content := "---\non: push\n---\n# No source\n"
	require.NoError(t, os.WriteFile(f, []byte(content), 0644))
	assert.Empty(t, readSourceRepoFromFile(f), "should return empty string when no source field")
}

func TestReadSourceRepoFromFile_MissingFile(t *testing.T) {
	assert.Empty(t, readSourceRepoFromFile("/nonexistent/file.md"), "should return empty string for missing file")
}

func TestReadSourceRepoFromFile_ShortSource(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "wf.md")
	content := "---\nsource: only-one-segment\n---\n# Workflow\n"
	require.NoError(t, os.WriteFile(f, []byte(content), 0644))
	assert.Empty(t, readSourceRepoFromFile(f), "should return empty string for malformed source")
}

// ---------------------------------------------------------------------------
// fetchAndSaveRemoteDispatchWorkflows — conflict detection
// ---------------------------------------------------------------------------

// TestFetchDispatchWorkflows_ConflictDifferentSource verifies that an error is returned
// when a dispatch-workflow target file already exists with a different source repo.
func TestFetchDispatchWorkflows_ConflictDifferentSource(t *testing.T) {
	dir := t.TempDir()
	workflowsDir := dir

	// Pre-existing file from a DIFFERENT source repo
	existingPath := filepath.Join(dir, "target-workflow.md")
	existingContent := `---
source: otherorg/other-repo/.github/workflows/target-workflow.md@v1
---
# Target workflow from a different repo
`
	require.NoError(t, os.WriteFile(existingPath, []byte(existingContent), 0644))

	// Workflow content that references target-workflow as a dispatch-workflow
	content := `---
safe-outputs:
  dispatch-workflow:
    workflows:
      - target-workflow
---
# Main
`
	spec := &WorkflowSpec{
		RepoSpec:     RepoSpec{RepoSlug: "github/gh-aw", Version: "main"},
		WorkflowPath: ".github/workflows/main.md",
	}

	err := fetchAndSaveRemoteDispatchWorkflows(context.Background(), content, spec, workflowsDir, false, false, nil)
	require.Error(t, err, "should error when existing file has a different source repo")
	require.ErrorContains(t, err, "target-workflow", "error should name the conflicting file")
	require.ErrorContains(t, err, "otherorg/other-repo", "error should mention existing source")
	require.ErrorContains(t, err, "github/gh-aw", "error should mention the intended source")
}

// TestFetchDispatchWorkflows_SameSourceSkips verifies that an existing dispatch-workflow
// file from the SAME source repo is silently skipped without error.
func TestFetchDispatchWorkflows_SameSourceSkips(t *testing.T) {
	dir := t.TempDir()
	workflowsDir := dir

	// Pre-existing file from the SAME source repo
	existingPath := filepath.Join(dir, "target-workflow.md")
	existingContent := `---
source: github/gh-aw/.github/workflows/target-workflow.md@v1
---
# Target workflow from the same repo
`
	require.NoError(t, os.WriteFile(existingPath, []byte(existingContent), 0644))

	content := `---
safe-outputs:
  dispatch-workflow:
    workflows:
      - target-workflow
---
# Main
`
	spec := &WorkflowSpec{
		RepoSpec:     RepoSpec{RepoSlug: "github/gh-aw", Version: "main"},
		WorkflowPath: ".github/workflows/main.md",
	}

	err := fetchAndSaveRemoteDispatchWorkflows(context.Background(), content, spec, workflowsDir, false, false, nil)
	require.NoError(t, err, "should not error when existing file is from the same source repo")

	// File must not have been modified
	got, readErr := os.ReadFile(existingPath)
	require.NoError(t, readErr)
	assert.Equal(t, existingContent, string(got), "existing same-source file must be left unchanged")
}

// TestFetchDispatchWorkflows_NoSourceConflict verifies that a file with no source field
// is treated as a conflict (unknown origin).
func TestFetchDispatchWorkflows_NoSourceConflict(t *testing.T) {
	dir := t.TempDir()
	workflowsDir := dir

	// Pre-existing file with NO source field
	existingPath := filepath.Join(dir, "target-workflow.md")
	require.NoError(t, os.WriteFile(existingPath, []byte("# No source field\n"), 0644))

	content := `---
safe-outputs:
  dispatch-workflow:
    workflows:
      - target-workflow
---
# Main
`
	spec := &WorkflowSpec{
		RepoSpec:     RepoSpec{RepoSlug: "github/gh-aw", Version: "main"},
		WorkflowPath: ".github/workflows/main.md",
	}

	err := fetchAndSaveRemoteDispatchWorkflows(context.Background(), content, spec, workflowsDir, false, false, nil)
	require.Error(t, err, "should error when existing file has no source field")
	require.ErrorContains(t, err, "target-workflow", "error should name the conflicting file")
	require.ErrorContains(t, err, "(no source field)", "error should show placeholder for missing source")
}

// TestFetchDispatchWorkflows_ForceOverwritesConflict verifies that --force bypasses conflict detection.
func TestFetchDispatchWorkflows_ForceOverwritesConflict(t *testing.T) {
	dir := t.TempDir()

	// Pre-existing file from a DIFFERENT source — would conflict without force
	existingPath := filepath.Join(dir, "target-workflow.md")
	existingContent := `---
source: otherorg/other-repo/.github/workflows/target-workflow.md@v1
---
# From other repo
`
	require.NoError(t, os.WriteFile(existingPath, []byte(existingContent), 0644))

	content := `---
safe-outputs:
  dispatch-workflow:
    workflows:
      - target-workflow
---
# Main
`
	spec := &WorkflowSpec{
		RepoSpec:     RepoSpec{RepoSlug: "github/gh-aw", Version: "main"},
		WorkflowPath: ".github/workflows/main.md",
	}

	// force=true should not error (even though there would normally be a conflict).
	// The conflict check is bypassed; the download fails immediately via the injected
	// mock downloader (avoids real network calls in unit tests).
	// Download failures are best-effort, so the function returns nil overall.
	mockDownloader := func(_ context.Context, _, _, _, _ string) ([]byte, error) {
		return nil, errors.New("download not available in unit tests")
	}
	err := fetchAndSaveRemoteDispatchWorkflows(context.Background(), content, spec, dir, false, true, nil, mockDownloader)
	require.NoError(t, err, "force=true should bypass conflict detection and return nil (download fails silently)")
}

func TestFetchAndSaveRemoteDispatchWorkflows_FetchesNestedFrontmatterImports(t *testing.T) {
	dir := t.TempDir()
	originalDownloadRemoteImportFile := downloadRemoteImportFile
	t.Cleanup(func() {
		downloadRemoteImportFile = originalDownloadRemoteImportFile
	})

	content := `---
safe-outputs:
  dispatch-workflow:
    workflows:
      - dependent-workflow
---
# Main
`
	spec := &WorkflowSpec{
		RepoSpec:     RepoSpec{RepoSlug: "github/gh-aw", Version: "main"},
		WorkflowPath: ".github/workflows/main.md",
	}

	mockDownloader := func(_ context.Context, _, _, filePath, _ string) ([]byte, error) {
		switch filePath {
		case ".github/workflows/dependent-workflow.md":
			return []byte(`---
imports:
  - uses: shared/helper.md
---
# Dependent
`), nil
		default:
			return nil, errors.New("unexpected workflow path: " + filePath)
		}
	}

	downloadRemoteImportFile = func(_ context.Context, owner, repo, filePath, ref string) ([]byte, error) {
		if owner != "github" || repo != "gh-aw" || ref != "main" {
			return nil, errors.New("unexpected import coordinates")
		}
		if filePath == ".github/workflows/shared/helper.md" {
			return []byte("---\n# Helper\n"), nil
		}
		return nil, errors.New("unexpected import path: " + filePath)
	}

	err := fetchAndSaveRemoteDispatchWorkflows(context.Background(), content, spec, dir, false, false, nil, mockDownloader)
	require.NoError(t, err)

	_, err = os.Stat(filepath.Join(dir, "dependent-workflow.md"))
	require.NoError(t, err)
	_, err = os.Stat(filepath.Join(dir, "shared", "helper.md"))
	require.NoError(t, err)
}

// ---------------------------------------------------------------------------
// fetchAndSaveRemoteResources — conflict detection
// ---------------------------------------------------------------------------

// TestFetchResources_MarkdownConflictDifferentSource verifies that an error is returned
// when a markdown resource file exists from a different source.
func TestFetchResources_MarkdownConflictDifferentSource(t *testing.T) {
	dir := t.TempDir()

	// Pre-existing markdown resource from a DIFFERENT source
	existingPath := filepath.Join(dir, "helper.md")
	existingContent := `---
source: otherorg/other-repo/.github/workflows/helper.md@v1
---
# Helper from different repo
`
	require.NoError(t, os.WriteFile(existingPath, []byte(existingContent), 0644))

	content := `---
resources:
  - helper.md
---
# Main
`
	spec := &WorkflowSpec{
		RepoSpec:     RepoSpec{RepoSlug: "github/gh-aw", Version: "main"},
		WorkflowPath: ".github/workflows/main.md",
	}

	err := fetchAndSaveRemoteResources(t.Context(), content, spec, dir, false, false, nil)
	require.Error(t, err, "should error when markdown resource exists from a different source")
	require.ErrorContains(t, err, "helper.md", "error should name the conflicting resource")
}

// TestFetchResources_NonMarkdownConflict verifies that a non-markdown resource that already
// exists always triggers a conflict error (no source tracking for non-markdown files).
func TestFetchResources_NonMarkdownConflict(t *testing.T) {
	dir := t.TempDir()

	// Pre-existing .yml resource (no source tracking)
	existingPath := filepath.Join(dir, "helper.yml")
	require.NoError(t, os.WriteFile(existingPath, []byte("name: Helper\n"), 0644))

	content := `---
resources:
  - helper.yml
---
# Main
`
	spec := &WorkflowSpec{
		RepoSpec:     RepoSpec{RepoSlug: "github/gh-aw", Version: "main"},
		WorkflowPath: ".github/workflows/main.md",
	}

	err := fetchAndSaveRemoteResources(t.Context(), content, spec, dir, false, false, nil)
	require.Error(t, err, "should error when non-markdown resource already exists")
	require.ErrorContains(t, err, "helper.yml", "error should name the conflicting resource")
}

// TestFetchResources_MarkdownSameSourceSkips verifies that an existing markdown resource
// from the same source repo is silently skipped without error.
func TestFetchResources_MarkdownSameSourceSkips(t *testing.T) {
	dir := t.TempDir()

	// Pre-existing markdown resource from the SAME source
	existingPath := filepath.Join(dir, "helper.md")
	existingContent := `---
source: github/gh-aw/.github/workflows/helper.md@main
---
# Helper from same repo
`
	require.NoError(t, os.WriteFile(existingPath, []byte(existingContent), 0644))

	content := `---
resources:
  - helper.md
---
# Main
`
	spec := &WorkflowSpec{
		RepoSpec:     RepoSpec{RepoSlug: "github/gh-aw", Version: "main"},
		WorkflowPath: ".github/workflows/main.md",
	}

	err := fetchAndSaveRemoteResources(t.Context(), content, spec, dir, false, false, nil)
	require.NoError(t, err, "should not error when markdown resource is from the same source")
}

// ---------------------------------------------------------------------------
// fetchAndSaveDispatchWorkflowsFromParsedFile — conflict detection (warning, not error)
// ---------------------------------------------------------------------------

// TestFetchDispatchWorkflowsFromParsed_ConflictWarnsAndContinues verifies that a conflict
// in the post-write phase emits a warning but does NOT return an error (best-effort).
func TestFetchDispatchWorkflowsFromParsed_ConflictWarnsAndContinues(t *testing.T) {
	workflowsDir := t.TempDir()

	// Main workflow: has dispatch-workflow: [triage-issue]
	mainPath := filepath.Join(workflowsDir, "main.md")
	mainContent := `---
on:
  issues:
permissions:
  issues: write
  contents: read
engine: copilot
safe-outputs:
  dispatch-workflow:
    workflows:
      - triage-issue
---
# Main Workflow
`
	require.NoError(t, os.WriteFile(mainPath, []byte(mainContent), 0644))

	// Pre-existing triage-issue.md from a DIFFERENT source
	conflictPath := filepath.Join(workflowsDir, "triage-issue.md")
	conflictContent := `---
source: otherorg/other-repo/.github/workflows/triage-issue.md@v1
---
# Triage from other repo
`
	require.NoError(t, os.WriteFile(conflictPath, []byte(conflictContent), 0644))

	spec := &WorkflowSpec{
		RepoSpec:     RepoSpec{RepoSlug: "github/gh-aw", Version: "main"},
		WorkflowPath: ".github/workflows/main.md",
	}

	// Must not panic or error — post-write is best-effort.
	fetchAndSaveDispatchWorkflowsFromParsedFile(t.Context(), mainPath, spec, workflowsDir, false, false, nil)

	// The conflicting file must NOT have been overwritten.
	got, readErr := os.ReadFile(conflictPath)
	require.NoError(t, readErr)
	assert.Equal(t, conflictContent, string(got), "conflict file must not be overwritten in post-write phase")
}

// --- fetchAllRemoteDependencies tests ---

// TestFetchAllRemoteDependencies_NoDependencies verifies that a workflow with no
// includes, imports, dispatch workflows, or resources succeeds with no files created.
func TestFetchAllRemoteDependencies_NoDependencies(t *testing.T) {
	content := `---
engine: copilot
---

# Workflow with no remote dependencies
`
	spec := &WorkflowSpec{
		RepoSpec:     RepoSpec{RepoSlug: "", Version: ""},
		WorkflowPath: ".github/workflows/my-workflow.md",
	}

	tmpDir := t.TempDir()
	err := fetchAllRemoteDependencies(context.Background(), content, spec, tmpDir, false, false, nil)
	require.NoError(t, err, "should not error when there are no remote dependencies")

	entries, readErr := os.ReadDir(tmpDir)
	require.NoError(t, readErr)
	assert.Empty(t, entries, "no files should be created when there are no remote dependencies")
}

// TestFetchAllRemoteDependencies_LocalSpecNoOp verifies that when the spec has an empty
// RepoSlug (a local workflow), all fetch operations are skipped and the function succeeds.
func TestFetchAllRemoteDependencies_LocalSpecNoOp(t *testing.T) {
	content := `---
engine: copilot
safe-outputs:
  dispatch-workflow:
    - dependent-workflow
---

# Workflow
`
	spec := &WorkflowSpec{
		RepoSpec:     RepoSpec{RepoSlug: "", Version: ""},
		WorkflowPath: ".github/workflows/my-workflow.md",
	}

	tmpDir := t.TempDir()
	err := fetchAllRemoteDependencies(context.Background(), content, spec, tmpDir, false, false, nil)
	require.NoError(t, err, "should not error for a local (no RepoSlug) workflow")

	entries, readErr := os.ReadDir(tmpDir)
	require.NoError(t, readErr)
	assert.Empty(t, entries, "no files should be created for a local workflow")
}

// TestFetchAllRemoteDependencies_IncludeErrorSwallowed verifies that include-fetch errors
// are treated as best-effort: a non-optional @include that cannot be resolved does not
// cause fetchAllRemoteDependencies to return an error.
func TestFetchAllRemoteDependencies_IncludeErrorSwallowed(t *testing.T) {
	// A non-optional @include with a relative path and no RepoSlug will fail to resolve,
	// but that error should be swallowed by fetchAllRemoteDependencies.
	content := `---
engine: copilot
---

@include shared/helpers.md

# Workflow
`
	spec := &WorkflowSpec{
		RepoSpec:     RepoSpec{RepoSlug: "", Version: ""},
		WorkflowPath: ".github/workflows/my-workflow.md",
	}

	tmpDir := t.TempDir()
	err := fetchAllRemoteDependencies(context.Background(), content, spec, tmpDir, false, false, nil)
	require.NoError(t, err, "include errors should be swallowed (best-effort)")
}

// TestFetchAllRemoteDependencies_DispatchConflictPropagated verifies that a conflict
// detected when fetching dispatch-workflow dependencies is returned to the caller.
// The conflict is detected before any network call, so no real download occurs.
func TestFetchAllRemoteDependencies_DispatchConflictPropagated(t *testing.T) {
	tmpDir := t.TempDir()

	// Pre-existing dispatch workflow from a DIFFERENT source repo.
	conflictPath := filepath.Join(tmpDir, "triage-issue.md")
	conflictContent := `---
source: otherorg/other-repo/.github/workflows/triage-issue.md@v1
---
# Triage from other repo
`
	require.NoError(t, os.WriteFile(conflictPath, []byte(conflictContent), 0644))

	content := `---
engine: copilot
safe-outputs:
  dispatch-workflow:
    - triage-issue
---

# Main Workflow
`
	spec := &WorkflowSpec{
		RepoSpec:     RepoSpec{RepoSlug: "github/gh-aw", Version: "main"},
		WorkflowPath: ".github/workflows/main.md",
	}

	err := fetchAllRemoteDependencies(context.Background(), content, spec, tmpDir, false, false, nil)
	require.Error(t, err, "dispatch workflow conflict should be propagated")
	require.ErrorContains(t, err, "dispatch workflow", "error should mention 'dispatch workflow'")
}

// TestFetchAllRemoteDependencies_ResourceMacroErrorPropagated verifies that a resource
// path containing GitHub Actions expression syntax causes an error that is propagated.
// This check occurs before any network call, so no download is attempted.
func TestFetchAllRemoteDependencies_ResourceMacroErrorPropagated(t *testing.T) {
	content := `---
engine: copilot
resources:
  - "${{ env.DYNAMIC_FILE }}"
---

# Workflow
`
	spec := &WorkflowSpec{
		RepoSpec:     RepoSpec{RepoSlug: "github/gh-aw", Version: "main"},
		WorkflowPath: ".github/workflows/my-workflow.md",
	}

	tmpDir := t.TempDir()
	err := fetchAllRemoteDependencies(context.Background(), content, spec, tmpDir, false, false, nil)
	require.Error(t, err, "resource macro error should be propagated")
	require.ErrorContains(t, err, "failed to fetch resource dependencies", "error should be wrapped with dependency context")
}
