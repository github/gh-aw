//go:build !integration

package cli

import (
	"io"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMCPCommandUnknownSubcommandReturnsError(t *testing.T) {
	t.Parallel()
	cmd := NewMCPCommand()
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true
	cmd.SetArgs([]string{"remove"})

	err := cmd.Execute()
	require.Error(t, err)
	assert.Equal(t, `unknown command "remove" for "gh aw mcp"`, err.Error())
}
