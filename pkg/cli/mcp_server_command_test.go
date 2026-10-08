//go:build !integration

package cli

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewMCPServerCommand_PortExampleMentionsSSE(t *testing.T) {
	t.Parallel()
	cmd := NewMCPServerCommand()
	require.NotNil(t, cmd)

	assert.Contains(t, cmd.Example, "Run HTTP server on port 8080 with SSE transport", "Port example should match SSE transport behavior")
}

// TestNewMCPServerCommand_AuditToolListedInHelp verifies that the audit tool
// is documented in the mcp-server command's Long description.
// This is a regression test for https://github.com/github/gh-aw/issues/47209
// where audit was absent from the MCP server tool list.
func TestNewMCPServerCommand_AuditToolListedInHelp(t *testing.T) {
	t.Parallel()
	cmd := NewMCPServerCommand()
	require.NotNil(t, cmd)

	assert.Contains(t, cmd.Long, "audit", "mcp-server long description should list the audit tool")
}

func TestNewMCPServerCommand_WorkQueueToolListedInHelp(t *testing.T) {
	t.Parallel()
	cmd := NewMCPServerCommand()
	require.NotNil(t, cmd)

	assert.Contains(t, cmd.Long, "  - work-queue  - Read Git-backed work queue state or inspect Work/Claim metadata (read-only)")
}
