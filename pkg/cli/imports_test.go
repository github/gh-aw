//go:build !integration

package cli

import (
	"strings"
	"testing"
)

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

	result, err := processIncludesWithWorkflowSpec(content, workflow, "", "/tmp/package", false)
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

	result, err := processIncludesWithWorkflowSpec(content, workflow, "", "/tmp/package", false)
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

	result, err := processIncludesWithWorkflowSpec(content, workflow, commitSHA, "/tmp/package", false)
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

	result, err := processIncludesWithWorkflowSpec(content, workflow, "", "/tmp/package", false)
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

	result, err := processIncludesInContent(content, workflow, "", false)
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

	result, err := processIncludesInContent(content, workflow, "", false)
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

	result, err := processIncludesWithWorkflowSpec(content, workflow, commitSHA, "/tmp/package", false)
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
