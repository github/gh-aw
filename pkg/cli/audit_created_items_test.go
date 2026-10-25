//go:build !integration

package cli

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/stretchr/testify/require"
)

func TestAuditCreatedItemIdentities(t *testing.T) {
	logsPath := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(logsPath, constants.TemporaryIdMapFilename.String()),
		[]byte(`{"aw_issue":{"repo":"owner/repo","number":17}}`), 0o600))

	items := []CreatedItemReport{
		{Type: "create_issue", Provider: "github"},
		{Type: "create_issue", TemporaryID: "aw_issue"},
		{Type: "create_issue", TemporaryID: "aw_issue", Repo: "other/repo", Number: 99, URL: "https://example.com/other/repo/issues/99"},
		{Type: "linear_create_issue", Provider: "linear", TemporaryID: "aw_issue", Identifier: "ENG-42"},
	}
	resolved := resolveAuditItemIdentities(items, logsPath)
	require.Contains(t, resolved[0].IdentityUnavailableReason, "Execution artifacts")
	require.Equal(t, "owner/repo", resolved[1].Repo)
	require.Equal(t, 17, resolved[1].Number)
	require.Empty(t, resolved[1].IdentityUnavailableReason)
	require.Equal(t, items[2], resolved[2], "must not replace authoritative manifest identity")
	require.Equal(t, items[3], resolved[3], "must not borrow a GitHub mapping for another provider")
	require.Empty(t, items[1].Repo, "cached input items must not be mutated")
	_, output := captureOutput(t, func() error {
		renderConsoleCreatedItems(resolved)
		return nil
	})
	require.Contains(t, output, "identity unavailable:")
	require.Contains(t, output, "owner/repo#17")
	require.Contains(t, output, "https://example.com/other/repo/issues/99")
	require.Contains(t, output, "ENG-42")
}

func TestAuditReportsUnavailableCreatedItemIdentity(t *testing.T) {
	for _, mapping := range []string{"", "{}", "invalid JSON"} {
		t.Run(mapping, func(t *testing.T) {
			logsPath := t.TempDir()
			require.NoError(t, os.WriteFile(filepath.Join(logsPath, safeOutputItemsManifestFilename),
				[]byte("{\"type\":\"create_issue\",\"provider\":\"github\",\"timestamp\":\"2026-10-23T04:59:00Z\"}\n"), 0o600))
			if mapping != "" {
				require.NoError(t, os.WriteFile(filepath.Join(logsPath, constants.TemporaryIdMapFilename.String()), []byte(mapping), 0o600))
			}
			audit, _ := buildLocalAuditData(ProcessedRun{Run: WorkflowRun{LogsPath: logsPath}}, LogMetrics{}, nil)
			require.Len(t, audit.CreatedItems, 1)
			require.NotEmpty(t, audit.CreatedItems[0].IdentityUnavailableReason)
			require.Empty(t, audit.CreatedItems[0].URL, "must not invent a URL for an unidentified resource")
			require.Zero(t, audit.CreatedItems[0].Number)
		})
	}
}
