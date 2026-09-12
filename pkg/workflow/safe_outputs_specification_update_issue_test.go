//go:build !integration

package workflow

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSafeOutputsSpecificationDocumentsUpdateIssueTargetAuthorization(t *testing.T) {
	specPath := findRepoFile(t, filepath.Join("docs", "src", "content", "docs", "specs", "safe-outputs-specification.md"))
	specBytes, err := os.ReadFile(specPath)
	require.NoError(t, err, "should read safe outputs specification")

	section := extractSpecTypeSection(t, string(specBytes), "update_issue")

	assert.Contains(t, section, "**UI-001**", "spec should define the omitted target default")
	assert.Contains(t, section, "**UI-002**", "spec should restrict triggering targets to event context")
	assert.Contains(t, section, "**UI-003**", "spec should make fixed targets override agent output")
	assert.Contains(t, section, "**UI-004**", "spec should reserve agent-selected targets for wildcard mode")
	assert.Contains(t, section, "**UI-005**", "spec should require runtime target authorization before temporary-ID resolution")
}
