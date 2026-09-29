//go:build !integration

package workflow

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSafeOutputsSpecificationDocumentsAssignToAgentTargetAuthorization(t *testing.T) {
	specPath := findRepoFile(t, filepath.Join("docs", "src", "content", "docs", "specs", "safe-outputs-specification.md"))
	specBytes, err := os.ReadFile(specPath)
	require.NoError(t, err, "should read safe outputs specification")

	section := extractSpecTypeSection(t, string(specBytes), "assign_to_agent")

	assert.Contains(t, section, "**ATA-001**", "spec should define the omitted target default")
	assert.Contains(t, section, "interpret it as `target: \"triggering\"`", "spec should default omitted targets to triggering")
	assert.Contains(t, section, "**ATA-002**", "spec should define triggering target authorization")
	assert.Contains(t, section, "**ATA-003**", "spec should define fixed target authorization")
	assert.Contains(t, section, "**ATA-004**", "spec should define wildcard target authorization")
	assert.Contains(t, section, "Only `target: \"*\"`", "spec should reserve agent-selected targets for wildcard mode")
	assert.Contains(t, section, "**ATA-005**", "spec should require runtime enforcement")
	assert.Contains(t, section, "- `required-labels`:", "spec should document the required-labels configuration")
	assert.Contains(t, section, "**ATA-006**", "spec should define write-time label gating")
	assert.Contains(t, section, "check the current labels on the resolved target issue or pull request immediately before assignment", "spec should require a current-label check on the resolved target")
	assert.Contains(t, section, "If any configured label is missing, the processor MUST skip agent assignment", "spec should forbid assignment without all required labels")
}
