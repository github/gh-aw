//go:build !integration

package workflow

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSafeOutputsSpecificationDocumentsAssignToUserTargetAuthorization(t *testing.T) {
	specPath := findRepoFile(t, filepath.Join("docs", "src", "content", "docs", "specs", "safe-outputs-specification.md"))
	specBytes, err := os.ReadFile(specPath)
	require.NoError(t, err, "should read safe outputs specification")

	section := extractSpecTypeSection(t, string(specBytes), "assign_to_user")

	assert.Contains(t, section, "**ATU-001**", "spec should define the omitted target default")
	assert.Contains(t, section, "interpret it as `target: \"triggering\"`", "spec should default omitted targets to triggering")
	assert.Contains(t, section, "**ATU-002**", "spec should define triggering target authorization")
	assert.Contains(t, section, "**ATU-003**", "spec should define fixed target authorization")
	assert.Contains(t, section, "**ATU-004**", "spec should define wildcard target authorization")
	assert.Contains(t, section, "Only `target: \"*\"`", "spec should reserve agent-selected targets for wildcard mode")
	assert.Contains(t, section, "**ATU-005**", "spec should require runtime enforcement")
}
