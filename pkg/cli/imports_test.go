//go:build !integration

package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/parser"
	"github.com/stretchr/testify/require"
)

func TestProcessImportsWithWorkflowSpec_PreservesFormatting(t *testing.T) {
	localDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(localDir, "local.md"), []byte("# Local\n"), 0644))
	workflow := &WorkflowSpec{
		RepoSpec:     RepoSpec{RepoSlug: "owner/repo", Version: "v1"},
		WorkflowPath: ".github/workflows/test.md",
	}
	for _, tt := range []struct {
		name    string
		imports string
		rewrite bool
	}{
		{"unchanged", "  - local.md # keep local\n  - uses: other/repo/shared.md@abc123 # pinned", false},
		{"plain", "  - shared/config.md # shared/config.md stays in this comment", true},
		{"single quoted", "  - 'shared/config.md' # keep quotes", true},
		{"double quoted", "  - \"shared/config.md\" # keep quotes", true},
		{"uses", "  - uses: shared/config.md # explanation\n    with:\n      path: shared/config.md", true},
		{"path", "  - path: shared/config.md\n    inputs:\n      value: shared/config.md", true},
		{"aw", "  aw:\n    - shared/config.md # explanation\n  other: [shared/config.md]", true},
		{"flow", "  [shared/config.md, 'shared/config.md'] # keep flow style", true},
		{"unrelated anchor", "  - shared/config.md\n  - uses: other/repo/shared.md@abc123\n    with:\n      value: *config", true},
		{"unicode columns", "  [{with: {label: café}, uses: shared/config.md}]", true},
		{"mixed", "  - local.md # keep local\n  - shared/config.md # rewrite only this\n  - uses: other/repo/shared.md@abc123 # pinned", true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			content := "---\n# Keep field order\nengine: copilot\non: workflow_dispatch # trigger\nconfig: &config shared/config.md\nimports:\n" +
				tt.imports + "\n# Keep this too\npermissions: {contents: read}\n---\n\n# Test\n\n  Body stays unchanged.  \n"
			expected := content
			if tt.rewrite {
				start := strings.Index(content, "imports:\n")
				prefix, rest := content[:start], content[start:]
				count := 1
				if tt.name == "flow" {
					count = 2
				}
				expected = prefix + strings.Replace(rest, "shared/config.md", "owner/repo/.github/workflows/shared/config.md@sha", count)
			}
			for _, newline := range []string{"\n", "\r\n"} {
				actual, err := processImportsWithWorkflowSpec(strings.ReplaceAll(content, "\n", newline), workflow, "sha", localDir, false)
				require.NoError(t, err)
				require.Equal(t, strings.ReplaceAll(expected, "\n", newline), actual)
			}
		})
	}
}

func TestProcessImportsWithWorkflowSpec_FormattingFallback(t *testing.T) {
	workflow := &WorkflowSpec{
		RepoSpec:     RepoSpec{RepoSlug: "owner/repo", Version: "v1"},
		WorkflowPath: ".github/workflows/test.md",
	}
	for _, imports := range []string{
		"  - >-\n    shared/config.md",
		"  - &shared shared/config.md\n  - *shared",
		"  - shared/config.md\n  - <<: {uses: shared/config.md}",
	} {
		t.Run(imports, func(t *testing.T) {
			content := "---\nimports:\n" + imports + "\n---\n\n# Test\n"
			actual, err := processImportsWithWorkflowSpec(content, workflow, "sha", "", false)
			require.NoError(t, err)
			parsed, err := parser.ExtractFrontmatterFromContent(actual)
			require.NoError(t, err)
			for _, entry := range parsed.Frontmatter["imports"].([]any) {
				if object, ok := entry.(map[string]any); ok {
					entry = object["uses"]
				}
				require.Equal(t, "owner/repo/.github/workflows/shared/config.md@sha", entry)
			}
		})
	}
}

func TestProcessIncludesWithWorkflowSpec_NewSyntax(t *testing.T) {
	// Test with new {{#import}} syntax
	content := `---
engine: claude
---

# Test Workflow

Some content here.

{{#import? agentics/weekly-research.config}}

More content.
`

	workflow := &WorkflowSpec{
		RepoSpec: RepoSpec{
			RepoSlug: "githubnext/agentics",
			Version:  "main",
		},
	}

	result, err := processIncludesWithWorkflowSpec(content, workflow, "", "/tmp/package", "", false)
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	// Should convert to @include with workflowspec
	expectedInclude := "{{#import? githubnext/agentics/agentics/weekly-research.config@main}}"
	if !strings.Contains(result, expectedInclude) {
		t.Errorf("Expected result to contain '%s'\nGot:\n%s", expectedInclude, result)
	}

	// Should NOT contain the malformed path
	malformedPath := "githubnext/agentics/@"
	if strings.Contains(result, malformedPath) {
		t.Errorf("Result should NOT contain malformed path '%s'\nGot:\n%s", malformedPath, result)
	}
}

func TestProcessIncludesWithWorkflowSpec_LegacySyntax(t *testing.T) {
	// Test with legacy @include syntax
	content := `---
engine: claude
---

# Test Workflow

Some content here.

@include? shared/config.md

More content.
`

	workflow := &WorkflowSpec{
		RepoSpec: RepoSpec{
			RepoSlug: "githubnext/agentics",
			Version:  "main",
		},
	}

	result, err := processIncludesWithWorkflowSpec(content, workflow, "", "/tmp/package", "", false)
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	// Should convert to @include with workflowspec
	expectedInclude := "{{#import? githubnext/agentics/shared/config.md@main}}"
	if !strings.Contains(result, expectedInclude) {
		t.Errorf("Expected result to contain '%s'\nGot:\n%s", expectedInclude, result)
	}
}

func TestProcessIncludesWithWorkflowSpec_WithCommitSHA(t *testing.T) {
	// Test with commit SHA
	content := `---
engine: claude
---

# Test Workflow

{{#import agentics/config.md}}
`

	workflow := &WorkflowSpec{
		RepoSpec: RepoSpec{
			RepoSlug: "githubnext/agentics",
		},
	}

	commitSHA := "e2770974a7eaccb58ddafd5606c38a05ba52c631"

	result, err := processIncludesWithWorkflowSpec(content, workflow, commitSHA, "/tmp/package", "", false)
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	// Should use commit SHA instead of version
	expectedInclude := "{{#import githubnext/agentics/agentics/config.md@e2770974a7eaccb58ddafd5606c38a05ba52c631}}"
	if !strings.Contains(result, expectedInclude) {
		t.Errorf("Expected result to contain '%s'\nGot:\n%s", expectedInclude, result)
	}
}

func TestProcessIncludesWithWorkflowSpec_EmptyFilePath(t *testing.T) {
	// Test with section-only reference (should be skipped/passed through)
	content := `---
engine: claude
---

# Test Workflow

{{#import? #SectionName}}

More content.
`

	workflow := &WorkflowSpec{
		RepoSpec: RepoSpec{
			RepoSlug: "githubnext/agentics",
			Version:  "main",
		},
	}

	result, err := processIncludesWithWorkflowSpec(content, workflow, "", "/tmp/package", "", false)
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	// Should preserve the original line when filePath is empty
	if !strings.Contains(result, "{{#import? #SectionName}}") {
		t.Errorf("Expected result to preserve original line\nGot:\n%s", result)
	}

	// Should NOT generate malformed workflowspec
	malformedPath := "githubnext/agentics/@"
	if strings.Contains(result, malformedPath) {
		t.Errorf("Result should NOT contain malformed path '%s'\nGot:\n%s", malformedPath, result)
	}
}

func TestProcessIncludesInContent_NewSyntax(t *testing.T) {
	// Test processIncludesInContent with new syntax
	content := `---
engine: claude
---

# Test Workflow

{{#import? config/settings.md}}
`

	workflow := &WorkflowSpec{
		RepoSpec: RepoSpec{
			RepoSlug: "owner/repo",
			Version:  "v1.0.0",
		},
	}

	result, err := processIncludesInContent(content, workflow, "", "", false)
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	// Should convert to workflowspec format
	expectedInclude := "{{#import? owner/repo/config/settings.md@v1.0.0}}"
	if !strings.Contains(result, expectedInclude) {
		t.Errorf("Expected result to contain '%s'\nGot:\n%s", expectedInclude, result)
	}
}

func TestProcessIncludesInContent_EmptyFilePath(t *testing.T) {
	// Test processIncludesInContent with empty file path
	content := `---
engine: claude
---

# Test Workflow

@include? #JustASection
`

	workflow := &WorkflowSpec{
		RepoSpec: RepoSpec{
			RepoSlug: "owner/repo",
			Version:  "v1.0.0",
		},
	}

	result, err := processIncludesInContent(content, workflow, "", "", false)
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	// Should preserve the original line
	if !strings.Contains(result, "@include? #JustASection") {
		t.Errorf("Expected result to preserve original line\nGot:\n%s", result)
	}

	// Should NOT generate malformed workflowspec
	malformedPath := "owner/repo/@"
	if strings.Contains(result, malformedPath) {
		t.Errorf("Result should NOT contain malformed path '%s'\nGot:\n%s", malformedPath, result)
	}
}

func TestProcessIncludesWithWorkflowSpec_RealWorldScenario(t *testing.T) {
	// Test the exact scenario from the weekly-research workflow bug report
	// The workflow has: {{#import? agentics/weekly-research.config}}
	// Previously this would generate: githubnext/agentics/@e2770974...
	// Now it should generate: githubnext/agentics/agentics/weekly-research.config@e2770974...

	content := `---
on:
  schedule:
    - cron: "0 9 * * 1"

tools:
  web-fetch:
  web-search:
---

# Weekly Research

Do research.

{{#import? agentics/weekly-research.config}}
`

	workflow := &WorkflowSpec{
		RepoSpec: RepoSpec{
			RepoSlug: "githubnext/agentics",
		},
	}

	commitSHA := "e2770974a7eaccb58ddafd5606c38a05ba52c631"

	result, err := processIncludesWithWorkflowSpec(content, workflow, commitSHA, "/tmp/package", "", false)
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	// Should convert to proper workflowspec
	expectedInclude := "{{#import? githubnext/agentics/agentics/weekly-research.config@e2770974a7eaccb58ddafd5606c38a05ba52c631}}"
	if !strings.Contains(result, expectedInclude) {
		t.Errorf("Expected result to contain '%s'\nGot:\n%s", expectedInclude, result)
	}

	// Should NOT contain the malformed path from the bug report
	malformedPath := "githubnext/agentics/@e2770974"
	if strings.Contains(result, malformedPath) {
		t.Errorf("Result should NOT contain malformed path '%s' (the original bug)\nGot:\n%s", malformedPath, result)
	}
}

// TestProcessIncludesWithWorkflowSpec_PathResolution tests that body-level {{#import}}
// paths are resolved relative to the workflow file's location, not the repo root.
// Regression test for: gh aw add rewrites {{#import shared/X.md}} with incorrect
// cross-repo path (resolves from repo root instead of .github/workflows/).
func TestProcessIncludesWithWorkflowSpec_PathResolution(t *testing.T) {
	content := `---
engine: copilot
---

# My Workflow

{{#import shared/config.md}}
`

	workflow := &WorkflowSpec{
		RepoSpec: RepoSpec{
			RepoSlug: "github/source-repo",
		},
		WorkflowPath: ".github/workflows/my-workflow.md",
	}

	commitSHA := "abc123def456"

	result, err := processIncludesWithWorkflowSpec(content, workflow, commitSHA, "", "", false)
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	// Path should be resolved relative to .github/workflows/, not the repo root
	expectedInclude := "{{#import github/source-repo/.github/workflows/shared/config.md@abc123def456}}"
	if !strings.Contains(result, expectedInclude) {
		t.Errorf("Expected result to contain '%s'\nGot:\n%s", expectedInclude, result)
	}

	// The old wrong form (resolving from repo root) must not appear
	wrongPath := "{{#import github/source-repo/shared/config.md@abc123def456}}"
	if strings.Contains(result, wrongPath) {
		t.Errorf("Result must NOT contain repo-root-relative path '%s'\nGot:\n%s", wrongPath, result)
	}
}

// TestProcessIncludesWithWorkflowSpec_PreservesLocalIncludes tests that body-level
// {{#import}} directives are preserved as-is when the target file exists in the
// local workflow directory. This is the add-command equivalent of the update-command's
// local preservation fix.
func TestProcessIncludesWithWorkflowSpec_PreservesLocalIncludes(t *testing.T) {
	// Create a temporary directory to act as the local workflow directory
	tmpDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(tmpDir, "shared"), 0755); err != nil {
		t.Fatalf("Failed to create shared dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(tmpDir, "shared", "config.md"), []byte("# Local config"), 0644); err != nil {
		t.Fatalf("Failed to create local config file: %v", err)
	}

	content := `---
engine: copilot
---

# My Workflow

{{#import shared/config.md}}
`

	workflow := &WorkflowSpec{
		RepoSpec: RepoSpec{
			RepoSlug: "github/source-repo",
		},
		WorkflowPath: ".github/workflows/my-workflow.md",
	}

	commitSHA := "abc123def456"

	result, err := processIncludesWithWorkflowSpec(content, workflow, commitSHA, "", tmpDir, false)
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	// The import must be preserved as-is since the file exists locally
	if !strings.Contains(result, "{{#import shared/config.md}}") {
		t.Errorf("Expected local import to be preserved, got:\n%s", result)
	}

	// Cross-repo ref must NOT appear
	if strings.Contains(result, "github/source-repo") {
		t.Errorf("Cross-repo ref should NOT appear when local file exists, got:\n%s", result)
	}
}

// TestProcessIncludesWithWorkflowSpec_RewritesBodyWhenLocalMissing tests that body-level
// {{#import}} directives are rewritten to cross-repo refs when the target does not
// exist in the local workflow directory.
func TestProcessIncludesWithWorkflowSpec_RewritesBodyWhenLocalMissing(t *testing.T) {
	tmpDir := t.TempDir() // empty — no shared files present

	content := `---
engine: copilot
---

# My Workflow

{{#import shared/config.md}}
`

	workflow := &WorkflowSpec{
		RepoSpec: RepoSpec{
			RepoSlug: "github/source-repo",
		},
		WorkflowPath: ".github/workflows/my-workflow.md",
	}

	commitSHA := "abc123def456"

	result, err := processIncludesWithWorkflowSpec(content, workflow, commitSHA, "", tmpDir, false)
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	// File not present locally → must be rewritten with correct full path
	expectedRef := "{{#import github/source-repo/.github/workflows/shared/config.md@abc123def456}}"
	if !strings.Contains(result, expectedRef) {
		t.Errorf("Expected cross-repo ref '%s' when file is missing locally, got:\n%s", expectedRef, result)
	}

	// Relative path must not remain
	if strings.Contains(result, "{{#import shared/config.md}}") {
		t.Errorf("Relative path should have been rewritten when file is missing locally, got:\n%s", result)
	}
}

// TestProcessIncludesWithWorkflowSpec_DuplicateInclude verifies that a body-level
// {{#import}} directive that appears more than once is preserved in full for each
// occurrence, rather than being silently dropped on the second occurrence by the
// cycle-detection guard.
func TestProcessIncludesWithWorkflowSpec_DuplicateInclude(t *testing.T) {
	content := `---
engine: copilot
---

# My Workflow

{{#import shared/config.md}}

Some text.

{{#import shared/config.md}}
`

	workflow := &WorkflowSpec{
		RepoSpec: RepoSpec{
			RepoSlug: "acme/repo",
		},
		WorkflowPath: ".github/workflows/my-workflow.md",
	}

	result, err := processIncludesWithWorkflowSpec(content, workflow, "deadbeef", "", "", false)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	expectedRef := "{{#import acme/repo/.github/workflows/shared/config.md@deadbeef}}"
	count := strings.Count(result, expectedRef)
	if count != 2 {
		t.Errorf("Expected rewritten import to appear 2 times, got %d\nOutput:\n%s", count, result)
	}
}

// TestProcessIncludesWithWorkflowSpec_PathTypes exercises every meaningful import-path
// variant for the body-level {{#import}} processor used by `gh aw add`.
// The workflow file is assumed to live at `.github/workflows/my-workflow.md` inside
// "acme/repo", pinned to commit "deadbeef".
func TestProcessIncludesWithWorkflowSpec_PathTypes(t *testing.T) {
	workflowPath := ".github/workflows/my-workflow.md"
	repoSlug := "acme/repo"
	sha := "deadbeef"

	tests := []struct {
		name     string
		line     string // body line to process
		wantLine string // expected line in output
		notWant  string // substring that must NOT appear (optional)
	}{
		{
			// Simple two-segment relative path: resolved relative to workflow dir
			name:     "simple relative shared/file.md",
			line:     "{{#import shared/config.md}}",
			wantLine: "{{#import acme/repo/.github/workflows/shared/config.md@deadbeef}}",
			notWant:  "acme/repo/shared/config.md@deadbeef",
		},
		{
			// Optional flag must be preserved
			name:     "optional relative path",
			line:     "{{#import? shared/config.md}}",
			wantLine: "{{#import? acme/repo/.github/workflows/shared/config.md@deadbeef}}",
		},
		{
			// Deep nested relative path
			name:     "deep relative path shared/mcp/deep/file.md",
			line:     "{{#import shared/mcp/deep/file.md}}",
			wantLine: "{{#import acme/repo/.github/workflows/shared/mcp/deep/file.md@deadbeef}}",
		},
		{
			// Absolute path (starts with /): strips leading slash, repo-root relative
			name:     "absolute path /tools/config.md",
			line:     "{{#import /tools/config.md}}",
			wantLine: "{{#import acme/repo/tools/config.md@deadbeef}}",
		},
		{
			// Path with section reference: section preserved after @sha
			name:     "relative path with section",
			line:     "{{#import shared/config.md#Introduction}}",
			wantLine: "{{#import acme/repo/.github/workflows/shared/config.md@deadbeef#Introduction}}",
		},
		{
			// Optional path with section
			name:     "optional relative path with section",
			line:     "{{#import? shared/config.md#Setup}}",
			wantLine: "{{#import? acme/repo/.github/workflows/shared/config.md@deadbeef#Setup}}",
		},
		{
			// Already a workflowspec (contains @): must be passed through unchanged
			name:     "already a workflowspec",
			line:     "{{#import other/repo/file.md@abc123}}",
			wantLine: "{{#import other/repo/file.md@abc123}}",
		},
		{
			// Already a workflowspec with section: pass through unchanged
			name:     "already a workflowspec with section",
			line:     "{{#import other/repo/file.md@abc123#Section}}",
			wantLine: "{{#import other/repo/file.md@abc123#Section}}",
		},
		{
			// Section-only reference (empty file path): preserved as-is
			name:     "section-only #SectionName",
			line:     "{{#import? #SectionName}}",
			wantLine: "{{#import? #SectionName}}",
			notWant:  "acme/repo",
		},
		{
			// Parent directory traversal: resolves up from .github/workflows/
			// ../shared/config.md from .github/workflows/ → .github/shared/config.md
			name:     "parent dir ../shared/config.md",
			line:     "{{#import ../shared/config.md}}",
			wantLine: "{{#import acme/repo/.github/shared/config.md@deadbeef}}",
		},
		{
			// Explicit current-dir prefix ./
			name:     "current dir ./config.md",
			line:     "{{#import ./config.md}}",
			wantLine: "{{#import acme/repo/.github/workflows/config.md@deadbeef}}",
		},
		{
			// Legacy @include syntax: output must use new {{#import}} syntax
			name:     "legacy @include shared/config.md",
			line:     "@include shared/config.md",
			wantLine: "{{#import acme/repo/.github/workflows/shared/config.md@deadbeef}}",
		},
		{
			// Legacy optional @include? syntax
			name:     "legacy @include? optional",
			line:     "@include? shared/config.md",
			wantLine: "{{#import? acme/repo/.github/workflows/shared/config.md@deadbeef}}",
		},
		{
			// Three-segment path that has no @: treated as local relative, not a workflowspec
			name:     "three-segment path shared/mcp/arxiv.md",
			line:     "{{#import shared/mcp/arxiv.md}}",
			wantLine: "{{#import acme/repo/.github/workflows/shared/mcp/arxiv.md@deadbeef}}",
		},
		{
			// Plain filename (single segment)
			name:     "plain filename config.md",
			line:     "{{#import config.md}}",
			wantLine: "{{#import acme/repo/.github/workflows/config.md@deadbeef}}",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Wrap the single import line in a minimal workflow
			content := "---\nengine: copilot\n---\n\n" + tt.line + "\n"

			workflow := &WorkflowSpec{
				RepoSpec: RepoSpec{
					RepoSlug: repoSlug,
				},
				WorkflowPath: workflowPath,
			}

			result, err := processIncludesWithWorkflowSpec(content, workflow, sha, "", "", false)
			if err != nil {
				t.Fatalf("Unexpected error: %v", err)
			}

			if !strings.Contains(result, tt.wantLine) {
				t.Errorf("Expected output to contain:\n  %q\nGot:\n%s", tt.wantLine, result)
			}

			if tt.notWant != "" && strings.Contains(result, tt.notWant) {
				t.Errorf("Output must NOT contain %q\nGot:\n%s", tt.notWant, result)
			}
		})
	}
}

// TestProcessImportsWithWorkflowSpec_PathTypes exercises every meaningful import-path
// variant for the frontmatter imports: field processor used by both `gh aw add` and
// `gh aw update`.
func TestProcessImportsWithWorkflowSpec_PathTypes(t *testing.T) {
	workflowPath := ".github/workflows/my-workflow.md"
	repoSlug := "acme/repo"
	sha := "deadbeef"

	tests := []struct {
		name       string
		importPath string // raw value in the imports: list
		wantRef    string // expected substring in YAML output
		notWant    string // substring that must NOT appear (optional)
	}{
		{
			// Simple two-segment relative path
			name:       "simple relative shared/config.md",
			importPath: "shared/config.md",
			wantRef:    "acme/repo/.github/workflows/shared/config.md@deadbeef",
			notWant:    "acme/repo/shared/config.md@deadbeef",
		},
		{
			// Deep nested relative path
			name:       "deep relative shared/mcp/file.md",
			importPath: "shared/mcp/file.md",
			wantRef:    "acme/repo/.github/workflows/shared/mcp/file.md@deadbeef",
		},
		{
			// Absolute path (starts with /)
			name:       "absolute /tools/setup.md",
			importPath: "/tools/setup.md",
			wantRef:    "acme/repo/tools/setup.md@deadbeef",
		},
		{
			// Already a workflowspec: must be passed through unchanged
			name:       "already workflowspec other/repo/file.md@v1",
			importPath: "other/repo/file.md@v1",
			wantRef:    "other/repo/file.md@v1",
			notWant:    "acme/repo/other/repo",
		},
		{
			// Three-segment path with no @: treated as relative, NOT a workflowspec
			name:       "three-segment shared/mcp/arxiv.md",
			importPath: "shared/mcp/arxiv.md",
			wantRef:    "acme/repo/.github/workflows/shared/mcp/arxiv.md@deadbeef",
		},
		{
			// Parent dir traversal
			name:       "parent dir ../shared/config.md",
			importPath: "../shared/config.md",
			wantRef:    "acme/repo/.github/shared/config.md@deadbeef",
		},
		{
			// Current-dir prefix
			name:       "current dir ./config.md",
			importPath: "./config.md",
			wantRef:    "acme/repo/.github/workflows/config.md@deadbeef",
		},
		{
			// Plain filename
			name:       "plain filename config.md",
			importPath: "config.md",
			wantRef:    "acme/repo/.github/workflows/config.md@deadbeef",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			content := "---\nengine: copilot\nimports:\n  - " + tt.importPath + "\n---\n\n# Test\n"

			workflow := &WorkflowSpec{
				RepoSpec: RepoSpec{
					RepoSlug: repoSlug,
				},
				WorkflowPath: workflowPath,
			}

			result, err := processImportsWithWorkflowSpec(content, workflow, sha, "", false)
			if err != nil {
				t.Fatalf("Unexpected error: %v", err)
			}

			if !strings.Contains(result, tt.wantRef) {
				t.Errorf("Expected output to contain:\n  %q\nGot:\n%s", tt.wantRef, result)
			}

			if tt.notWant != "" && strings.Contains(result, tt.notWant) {
				t.Errorf("Output must NOT contain %q\nGot:\n%s", tt.notWant, result)
			}
		})
	}
}

func TestIsWorkflowSpecFormat(t *testing.T) {
	tests := []struct {
		name     string
		path     string
		expected bool
	}{
		{
			name:     "workflowspec with SHA",
			path:     "owner/repo/path/file.md@abc123",
			expected: true,
		},
		{
			name:     "workflowspec with version tag",
			path:     "owner/repo/file.md@v1.0.0",
			expected: true,
		},
		{
			name:     "workflowspec without version",
			path:     "owner/repo/path/file.md",
			expected: true,
		},
		{
			name:     "three-part relative path - NOT a workflowspec",
			path:     "shared/mcp/arxiv.md",
			expected: false, // Local path, not a workflowspec
		},
		{
			name:     "two-part relative path",
			path:     "shared/file.md",
			expected: false,
		},
		{
			name:     "relative path with ./",
			path:     "./shared/file.md",
			expected: false,
		},
		{
			name:     "absolute path",
			path:     "/shared/file.md",
			expected: false,
		},
		{
			name:     "workflowspec with section and version",
			path:     "owner/repo/path/file.md@sha#section",
			expected: true,
		},
		{
			name:     "local path with section containing at-sign",
			path:     "shared/mcp/file.md#user@example",
			expected: false,
		},
		{
			name:     "malformed workflowspec with empty repo segment",
			path:     "owner//path/file.md",
			expected: false,
		},
		{
			name:     "simple filename",
			path:     "file.md",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isWorkflowSpecFormat(tt.path)
			if result != tt.expected {
				t.Errorf("isWorkflowSpecFormat(%q) = %v, want %v", tt.path, result, tt.expected)
			}
		})
	}
}

func TestProcessImportsWithWorkflowSpec_ThreePartPath(t *testing.T) {
	// Test that three-part paths like "shared/mcp/arxiv.md" are correctly converted
	// to workflowspecs, not skipped as if they were already workflowspecs
	content := `---
engine: copilot
imports:
  - shared/mcp/arxiv.md
  - shared/reporting.md
  - shared/mcp/brave.md
---

# Test Workflow

Test content.
`

	workflow := &WorkflowSpec{
		RepoSpec: RepoSpec{
			RepoSlug: "github/gh-aw",
			Version:  "main",
		},
		WorkflowPath: ".github/workflows/test-workflow.md",
	}

	commitSHA := "abc123def456"

	result, err := processImportsWithWorkflowSpec(content, workflow, commitSHA, "", false)
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	// All imports should be converted to workflowspecs with the commit SHA
	expectedImports := []string{
		"github/gh-aw/.github/workflows/shared/mcp/arxiv.md@abc123def456",
		"github/gh-aw/.github/workflows/shared/reporting.md@abc123def456",
		"github/gh-aw/.github/workflows/shared/mcp/brave.md@abc123def456",
	}

	for _, expected := range expectedImports {
		if !strings.Contains(result, expected) {
			t.Errorf("Expected result to contain '%s'\nGot:\n%s", expected, result)
		}
	}

	// The original paths should NOT appear unchanged
	unchangedPaths := []string{
		"- shared/mcp/arxiv.md",
		"- shared/reporting.md",
		"- shared/mcp/brave.md",
	}

	for _, unchanged := range unchangedPaths {
		if strings.Contains(result, unchanged) {
			t.Errorf("Did not expect result to contain unchanged path '%s'\nGot:\n%s", unchanged, result)
		}
	}
}

// TestProcessImportsWithWorkflowSpec_ObjectFormPreserved tests that object-form
// import entries (using "uses"/"with" or "path"/"inputs") are preserved rather
// than silently dropped when the imports field is rewritten to workflowspec
// format. This is a regression test for: gh aw update replaces valid workflow
// imports with `imports: []` when the only entries are object-form.
func TestProcessImportsWithWorkflowSpec_ObjectFormPreserved(t *testing.T) {
	content := `---
engine: copilot
imports:
  - uses: shared/control.md
    with:
      role: orchestrator
      rollout_mode: ${{ vars.CENTRAL_AGENTIC_OPS_DEPENDABOT_MODE || 'preview' }}
      review_repo: ${{ vars.CENTRAL_AGENTIC_OPS_DEPENDABOT_REVIEW_REPO || '' }}
---

# Test Workflow

Test content.
`

	workflow := &WorkflowSpec{
		RepoSpec: RepoSpec{
			RepoSlug: "github/gh-aw",
			Version:  "main",
		},
		WorkflowPath: ".github/workflows/dependabot.md",
	}

	commitSHA := "abc123def456"

	result, err := processImportsWithWorkflowSpec(content, workflow, commitSHA, "", false)
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	// The imports field must NOT be replaced with an empty array.
	if strings.Contains(result, "imports: []") {
		t.Fatalf("Expected imports to be preserved, but got empty imports array:\n%s", result)
	}

	// The uses path should be rewritten to workflowspec format.
	expectedUses := "uses: github/gh-aw/.github/workflows/shared/control.md@abc123def456"
	if !strings.Contains(result, expectedUses) {
		t.Errorf("Expected result to contain '%s'\nGot:\n%s", expectedUses, result)
	}

	// The "with" subfields must be preserved.
	for _, expected := range []string{"role: orchestrator", "rollout_mode:", "review_repo:"} {
		if !strings.Contains(result, expected) {
			t.Errorf("Expected result to contain '%s'\nGot:\n%s", expected, result)
		}
	}
}

// TestProcessImportsWithWorkflowSpec_PreservesLocalRelativePaths tests that when
// localWorkflowDir is provided and import files exist on disk, the relative paths
// are kept as-is and NOT rewritten to cross-repo workflowspec references.
// This is the fix for: gh aw update rewrites local imports: to cross-repo paths.
func TestProcessImportsWithWorkflowSpec_PreservesLocalRelativePaths(t *testing.T) {
	// Create a temporary directory to act as the local workflow directory
	tmpDir := t.TempDir()

	// Create the shared import files locally
	for _, rel := range []string{"shared/team-config.md", "shared/aor-index.md"} {
		dir := filepath.Join(tmpDir, rel[:strings.LastIndex(rel, "/")])
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatalf("Failed to create dir %s: %v", dir, err)
		}
		if err := os.WriteFile(filepath.Join(tmpDir, rel), []byte("# Shared content"), 0644); err != nil {
			t.Fatalf("Failed to create file %s: %v", rel, err)
		}
	}

	content := `---
engine: copilot
imports:
  - shared/team-config.md
  - shared/aor-index.md
---

# Investigate
`

	workflow := &WorkflowSpec{
		RepoSpec: RepoSpec{
			RepoSlug: "github/identity-core",
			Version:  "cd32c168",
		},
		WorkflowPath: ".github/workflows/investigate.md",
	}

	result, err := processImportsWithWorkflowSpec(content, workflow, "cd32c168", tmpDir, false)
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	// Local paths must be preserved as-is
	if !strings.Contains(result, "- shared/team-config.md") {
		t.Errorf("Expected local import 'shared/team-config.md' to be preserved, got:\n%s", result)
	}
	if !strings.Contains(result, "- shared/aor-index.md") {
		t.Errorf("Expected local import 'shared/aor-index.md' to be preserved, got:\n%s", result)
	}

	// Cross-repo refs must NOT appear
	if strings.Contains(result, "github/identity-core") {
		t.Errorf("Cross-repo ref should NOT appear when local file exists, got:\n%s", result)
	}
}

func TestProcessImportsWithWorkflowSpec_UnchangedImportsPreserveFormatting(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "shared"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "shared/local.md"), []byte("# Local\n"), 0644); err != nil {
		t.Fatal(err)
	}

	workflow := &WorkflowSpec{
		RepoSpec:     RepoSpec{RepoSlug: "owner/repo"},
		WorkflowPath: ".github/workflows/test.md",
	}
	for _, tt := range []struct {
		name    string
		imports string
	}{
		{"local list", "  - shared/local.md\n  - owner/repo/shared/remote.md@abc123\n"},
		{"object form", "  aw:\n    - uses: shared/local.md\n      with:\n        role: reviewer\n"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			content := "---\n# Keep this setup note\nname: Example\nimports:\n" + tt.imports +
				"description: |\n  Keep this block's indentation.\n# Optional import:\n#   - shared/optional.md\n---\n\n# Body\n"
			got, err := processImportsWithWorkflowSpec(content, workflow, "abc123", dir, false)
			if err != nil {
				t.Fatal(err)
			}
			if got != content {
				t.Errorf("unchanged imports must preserve the workflow byte-for-byte:\nwant:\n%s\ngot:\n%s", content, got)
			}
		})
	}
}

func TestProcessImportsWithWorkflowSpec_RewritePreservesUnrelatedFrontmatter(t *testing.T) {
	content := `---
# Keep this setup note
description: |
  Preserve this block's indentation.
name: Example
imports:
  - shared/team-config.md
engine: copilot
---

# Body
`
	workflow := &WorkflowSpec{
		RepoSpec:     RepoSpec{RepoSlug: "owner/repo", Version: "v1"},
		WorkflowPath: ".github/workflows/example.md",
	}

	got, err := processImportsWithWorkflowSpec(content, workflow, "v1", t.TempDir(), false)
	if err != nil {
		t.Fatal(err)
	}
	want := strings.Replace(content, "shared/team-config.md", "owner/repo/.github/workflows/shared/team-config.md@v1", 1)
	if got != want {
		t.Errorf("rewriting an import must preserve unrelated frontmatter formatting:\nwant:\n%s\ngot:\n%s", want, got)
	}
}

// TestProcessImportsWithWorkflowSpec_RewritesWhenLocalMissing verifies that imports
// for files that do NOT exist locally are still rewritten to cross-repo refs.
func TestProcessImportsWithWorkflowSpec_RewritesWhenLocalMissing(t *testing.T) {
	// Use a temp dir that has NO shared files
	tmpDir := t.TempDir()

	content := `---
engine: copilot
imports:
  - shared/team-config.md
---

# Investigate
`

	workflow := &WorkflowSpec{
		RepoSpec: RepoSpec{
			RepoSlug: "github/identity-core",
			Version:  "cd32c168",
		},
		WorkflowPath: ".github/workflows/investigate.md",
	}

	result, err := processImportsWithWorkflowSpec(content, workflow, "cd32c168", tmpDir, false)
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	// File does NOT exist locally → must be rewritten to cross-repo ref
	expectedRef := "github/identity-core/.github/workflows/shared/team-config.md@cd32c168"
	if !strings.Contains(result, expectedRef) {
		t.Errorf("Expected cross-repo ref '%s' when file is missing locally, got:\n%s", expectedRef, result)
	}

	// Original relative path must be gone
	if strings.Contains(result, "- shared/team-config.md") {
		t.Errorf("Relative path should have been rewritten when file is missing locally, got:\n%s", result)
	}
}

// TestProcessIncludesInContent_PreservesLocalIncludeDirectives tests that @include
// directives whose files exist locally are not rewritten to cross-repo refs.
func TestProcessIncludesInContent_PreservesLocalIncludeDirectives(t *testing.T) {
	// Create a temporary directory with the shared include file
	tmpDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(tmpDir, "shared"), 0755); err != nil {
		t.Fatalf("Failed to create shared dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(tmpDir, "shared", "config.md"), []byte("# Config"), 0644); err != nil {
		t.Fatalf("Failed to create config file: %v", err)
	}

	content := `---
engine: copilot
---

# Test Workflow

{{#import shared/config.md}}
`

	workflow := &WorkflowSpec{
		RepoSpec: RepoSpec{
			RepoSlug: "github/identity-core",
			Version:  "abc123",
		},
		WorkflowPath: ".github/workflows/test.md",
	}

	result, err := processIncludesInContent(content, workflow, "abc123", tmpDir, false)
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	// Local include directive must be preserved
	if !strings.Contains(result, "{{#import shared/config.md}}") {
		t.Errorf("Expected local @include to be preserved, got:\n%s", result)
	}

	// Cross-repo ref must NOT appear
	if strings.Contains(result, "github/identity-core") {
		t.Errorf("Cross-repo ref should NOT appear when local file exists, got:\n%s", result)
	}
}

// TestIsLocalFileForUpdate_PathTraversal ensures that traversal attempts (e.g.
// "../../etc/passwd") are rejected even if the target path happens to exist.
func TestIsLocalFileForUpdate_PathTraversal(t *testing.T) {
	tmpDir := t.TempDir()

	// Traversal path that would escape tmpDir
	traversal := "../../etc/passwd"
	if isLocalFileForUpdate(tmpDir, traversal) {
		t.Errorf("isLocalFileForUpdate should reject path traversal attempt: %s", traversal)
	}

	// A normal path within tmpDir that doesn't exist should return false
	if isLocalFileForUpdate(tmpDir, "nonexistent.md") {
		t.Errorf("isLocalFileForUpdate should return false for non-existent file")
	}

	// A normal path within tmpDir that DOES exist should return true
	validFile := "shared/file.md"
	if err := os.MkdirAll(filepath.Join(tmpDir, "shared"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tmpDir, validFile), []byte("content"), 0644); err != nil {
		t.Fatal(err)
	}
	if !isLocalFileForUpdate(tmpDir, validFile) {
		t.Errorf("isLocalFileForUpdate should return true for an existing file within tmpDir")
	}
}
