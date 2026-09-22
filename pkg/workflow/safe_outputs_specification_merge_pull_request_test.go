//go:build !integration

package workflow

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSafeOutputsSpecificationDocumentsMergePullRequest(t *testing.T) {
	specPath := findRepoFile(t, filepath.Join("docs", "src", "content", "docs", "specs", "safe-outputs-specification.md"))
	specBytes, err := os.ReadFile(specPath)
	require.NoError(t, err, "should read safe outputs specification")

	spec := string(specBytes)
	section := extractSpecTypeSection(t, spec, "merge_pull_request")

	assert.Contains(t, section, "**Purpose**: Merge pull requests only when configured policy gates pass.",
		"spec should define merge_pull_request purpose")
	assert.Contains(t, section, "Base Branch Protection",
		"spec should document base branch restrictions for merge_pull_request")
	assert.Contains(t, section, "repository default branch",
		"spec should explicitly refuse merge_pull_request to repository default branch")
	assert.Contains(t, section, "Target Branch Open PR",
		"spec should document the target-branch open-PR gate for merge_pull_request")
	assert.Contains(t, section, "`required-labels`",
		"spec should document required-labels configuration for merge_pull_request")
	assert.Contains(t, section, "`contents: write`",
		"spec should document contents: write permission for merge_pull_request")
	assert.Contains(t, section, "`pull-requests: write`",
		"spec should document pull-requests: write permission for merge_pull_request")
	assert.Contains(t, section, "temporary ID",
		"spec should document temporary ID support for merge_pull_request pull_request_number")
}

func TestSafeOutputsSpecificationDocumentsMergePullRequestTargetAuthorization(t *testing.T) {
	specPath := findRepoFile(t, filepath.Join("docs", "src", "content", "docs", "specs", "safe-outputs-specification.md"))
	specBytes, err := os.ReadFile(specPath)
	require.NoError(t, err, "should read safe outputs specification")

	section := extractSpecTypeSection(t, string(specBytes), "merge_pull_request")

	assert.Contains(t, section, "**MPR-001**", "spec should define the omitted target default")
	assert.Contains(t, section, "interpret it as `target: \"triggering\"`", "spec should default omitted targets to triggering")
	assert.Contains(t, section, "**MPR-002**", "spec should define triggering target authorization")
	assert.Contains(t, section, "**MPR-003**", "spec should define fixed target authorization")
	assert.Contains(t, section, "**MPR-004**", "spec should define wildcard target authorization")
	assert.Contains(t, section, "Only `target: \"*\"`", "spec should reserve agent-selected targets for wildcard mode")
	assert.Contains(t, section, "**MPR-005**", "spec should require runtime enforcement")
}

func extractSpecTypeSection(t *testing.T, spec, typeName string) string {
	t.Helper()

	header := "#### Type: " + typeName
	start := strings.Index(spec, header)
	require.NotEqual(t, -1, start, "spec should include section header for %s", typeName)

	rest := spec[start+len(header):]
	nextOffset := strings.Index(rest, "\n#### Type: ")
	if nextOffset == -1 {
		return spec[start:]
	}

	return spec[start : start+len(header)+nextOffset]
}

func findRepoFile(t *testing.T, relativePath string) string {
	t.Helper()

	wd, err := os.Getwd()
	require.NoError(t, err, "should get current working directory")

	dir := wd
	for {
		candidate := filepath.Join(dir, relativePath)
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("could not find %s from %s", relativePath, wd)
		}
		dir = parent
	}
}
