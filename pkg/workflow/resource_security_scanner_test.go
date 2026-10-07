package workflow

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFormatResourceSecurityFindings(t *testing.T) {
	findings := ScanResourceSecurity([]byte("const value = 'safe\u202Eunsafe';"))
	require.NotEmpty(t, findings)

	result := FormatResourceSecurityFindings(findings, ".github/aw/template.mjs")
	assert.Contains(t, result, "resource content")
	assert.Contains(t, result, ".github/aw/template.mjs")
	assert.NotContains(t, result, "workflow markdown")
	assert.Contains(t, result, "This resource")
}
