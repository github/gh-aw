//go:build !integration

package cli

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/github/gh-aw/pkg/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEnrichItemsFromAgentOutput(t *testing.T) {
	t.Parallel()
	tmpDir := testutil.TempDir(t, "test-enrich-*")

	rawOutput := `{"agent":"copilot","issue_number":100,"type":"assign_to_agent"}
{"agent":"copilot","issue_number":200,"type":"assign_to_agent"}
{"body":"comment body","item_number":100,"type":"add_comment","temporary_id":"aw_abc"}
`
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "safeoutputs.jsonl"), []byte(rawOutput), 0o600))

	items := []CreatedItemReport{
		{Type: "assign_to_agent", Timestamp: "2026-05-12T00:00:00Z"},
		{Type: "assign_to_agent", Timestamp: "2026-05-12T00:01:00Z"},
		{Type: "add_comment", URL: "https://github.com/owner/repo/issues/100#issuecomment-999", Repo: "owner/repo", Timestamp: "2026-05-12T00:02:00Z"},
	}

	enriched := enrichItemsFromAgentOutput(items, tmpDir, "default/repo")

	assert.Equal(t, 100, enriched[0].Number, "first assign_to_agent should get issue_number from raw output")
	assert.Equal(t, 200, enriched[1].Number, "second assign_to_agent should get issue_number from raw output")
	assert.Equal(t, "default/repo", enriched[0].Repo, "should fill repo from default when empty")
	// Third item already has a number from URL, should keep it
	assert.Equal(t, "owner/repo", enriched[2].Repo, "should keep existing repo")
}

func TestEnrichItemsFromAgentOutputMissingFile(t *testing.T) {
	t.Parallel()
	tmpDir := testutil.TempDir(t, "test-enrich-missing-*")

	items := []CreatedItemReport{
		{Type: "assign_to_agent", Timestamp: "2026-05-12T00:00:00Z"},
	}

	enriched := enrichItemsFromAgentOutput(items, tmpDir, "default/repo")
	assert.Equal(t, 0, enriched[0].Number, "should not crash on missing file")
}

func TestEnrichItemsFromAgentOutputSkipsItemsWithNumbers(t *testing.T) {
	t.Parallel()
	tmpDir := testutil.TempDir(t, "test-enrich-skip-*")

	rawOutput := `{"issue_number":999,"type":"assign_to_agent"}
`
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "safeoutputs.jsonl"), []byte(rawOutput), 0o600))

	items := []CreatedItemReport{
		{Type: "assign_to_agent", Number: 42, Timestamp: "2026-05-12T00:00:00Z"},
	}

	enriched := enrichItemsFromAgentOutput(items, tmpDir, "default/repo")
	assert.Equal(t, 42, enriched[0].Number, "should not overwrite existing number")
}
