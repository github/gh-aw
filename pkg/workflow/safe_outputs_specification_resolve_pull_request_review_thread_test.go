//go:build !integration

package workflow

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSafeOutputsSpecificationDocumentsResolvePullRequestReviewThreadTargetAuthorization(t *testing.T) {
	specPath := findRepoFile(t, filepath.Join("docs", "src", "content", "docs", "specs", "safe-outputs-specification.md"))
	specBytes, err := os.ReadFile(specPath)
	require.NoError(t, err, "should read safe outputs specification")

	section := extractSpecTypeSection(t, string(specBytes), "resolve_pull_request_review_thread")

	assert.Contains(t, section, "**RPT-001**", "spec should define the omitted target default")
	assert.Contains(t, section, "interpret it as `target: \"triggering\"`", "spec should default omitted targets to triggering")
	assert.Contains(t, section, "**RPT-002**", "spec should define triggering target authorization")
	assert.Contains(t, section, "**RPT-003**", "spec should define fixed target authorization")
	assert.Contains(t, section, "**RPT-004**", "spec should define wildcard target authorization")
	assert.Contains(t, section, "Only `target: \"*\"`", "spec should reserve agent-selected targets for wildcard mode")
	assert.Contains(t, section, "**RPT-005**", "spec should require runtime enforcement")
	assert.Contains(t, section, "`PullRequestReviewComment`", "spec should describe review-comment node resolution")
	assert.Contains(t, section, "`resolveReviewThread`", "spec should document the GraphQL mutation")
	assert.Contains(t, section, "following GraphQL cursors", "spec should require complete comment lookup pagination")
	assert.Contains(t, section, "successful no-ops", "spec should describe stale and already-resolved thread handling")
	assert.Contains(t, section, "without calling the mutation", "spec should forbid mutation in staged mode")
}
