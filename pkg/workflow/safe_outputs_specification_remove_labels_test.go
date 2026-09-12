//go:build !integration

package workflow

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSafeOutputsSpecificationDocumentsRemoveLabelsTargetAuthorization(t *testing.T) {
	specPath := findRepoFile(t, filepath.Join("docs", "src", "content", "docs", "specs", "safe-outputs-specification.md"))
	specBytes, err := os.ReadFile(specPath)
	require.NoError(t, err, "should read safe outputs specification")

	section := extractSpecTypeSection(t, string(specBytes), "remove_labels")

	assert.Contains(t, section, "**RML-001**", "spec should define the omitted target default")
	assert.Contains(t, section, "**RML-002**", "spec should restrict triggering targets to event context")
	assert.Contains(t, section, "**RML-003**", "spec should make fixed targets override agent output")
	assert.Contains(t, section, "**RML-004**", "spec should reserve agent-selected targets for wildcard mode")
	assert.Contains(t, section, "**RML-005**", "spec should require runtime enforcement")
}
