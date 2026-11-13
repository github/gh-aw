//go:build !integration

package cli

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestLogsJSONLConflictExplainsAlternatives(t *testing.T) {
	t.Parallel()
	cmd := NewLogsCommand()
	require.NoError(t, cmd.ParseFlags([]string{"--format", "jsonl", "--json"}))
	_, err := loadCommonLogsOptions(cmd)
	require.EqualError(t, err, "--format jsonl cannot be combined with --json. Use --format jsonl for streaming events or --json for a single JSON document")
}
