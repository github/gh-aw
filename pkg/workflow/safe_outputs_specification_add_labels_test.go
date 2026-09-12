//go:build !integration

package workflow

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSafeOutputsSpecificationDocumentsAddLabelsTargetAuthorization(t *testing.T) {
	specPath := findRepoFile(t, filepath.Join("docs", "src", "content", "docs", "specs", "safe-outputs-specification.md"))
	specBytes, err := os.ReadFile(specPath)
	require.NoError(t, err, "should read safe outputs specification")

	section := extractSpecTypeSection(t, string(specBytes), "add_labels")

	assert.Contains(t, section, "**AL-001**", "spec should define the omitted target default")
	assert.Contains(t, section, "interpreted as `target: \"triggering\"`", "spec should default omitted targets to triggering")
	assert.Contains(t, section, "**AL-002**", "spec should define triggering target authorization")
	assert.Contains(t, section, "only the issue or pull request number from trusted triggering-event context", "spec should restrict triggering targets to event context")
	assert.Contains(t, section, "**AL-003**", "spec should define fixed target authorization")
	assert.Contains(t, section, "ignore conflicting agent-supplied target identifiers", "spec should make fixed targets override agent output")
	assert.Contains(t, section, "**AL-004**", "spec should define wildcard target authorization")
	assert.Contains(t, section, "Only `target: \"*\"` MAY select", "spec should reserve agent-selected targets for wildcard mode")
	assert.Contains(t, section, "**AL-005**", "spec should require runtime enforcement")
	assert.Contains(t, section, "MUST NOT replace runtime enforcement", "spec should not rely on schema shaping or prompts for authorization")
}
