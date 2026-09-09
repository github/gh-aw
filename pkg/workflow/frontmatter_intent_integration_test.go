//go:build integration

package workflow

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/github/gh-aw/pkg/stringutil"
	"github.com/github/gh-aw/pkg/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIntentRenderedInLockFile(t *testing.T) {
	tmpDir := testutil.TempDir(t, "intent-frontmatter-test")

	testContent := `---
name: Issue Triage
description: Classifies newly opened issues and applies appropriate labels.
intent: Reduce manual issue-triage work while keeping classifications evidence-based.
on:
  issues:
    types: [opened]
permissions:
  contents: read
engine: claude
strict: false
---

# Issue Triage

Classify the issue.
`
	testFile := filepath.Join(tmpDir, "test-workflow.md")
	require.NoError(t, os.WriteFile(testFile, []byte(testContent), 0644))

	compiler := NewCompiler()
	require.NoError(t, compiler.CompileWorkflow(testFile))

	lockContent, err := os.ReadFile(stringutil.MarkdownToLockFile(testFile))
	require.NoError(t, err)

	assert.Contains(t, string(lockContent), "# Classifies newly opened issues and applies appropriate labels.")
	assert.Contains(t, string(lockContent), "# Intent: Reduce manual issue-triage work while keeping classifications evidence-based.")
}
