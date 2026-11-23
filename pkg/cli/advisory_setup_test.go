package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAdvisorySetupPersonalRepository(t *testing.T) {
	dir := t.TempDir()
	const script = `#!/bin/sh
[ "$1" = api ] && [ "$2" = --hostname ] && [ "$3" = github.com ] || exit 1
case "$4" in
user) printf '%s' '{"id":42,"login":"owner","type":"User"}' ;;
repos/owner/gh-aw-advisories)
  if [ "$REPO_EXISTS" = no ]; then
    echo 'gh: Not Found (HTTP 404)' >&2
    exit 1
  fi
  printf '%s' "$REPO_JSON"
  ;;
user/repos)
  [ "$REPO_EXISTS" = no ] && [ "$*" = 'api --hostname github.com user/repos --method POST --raw-field name=gh-aw-advisories --field private=true --field has_issues=false --field has_wiki=false' ] || exit 1
  echo created >> "$CALL_LOG"
  printf '%s' "$REPO_JSON"
  ;;
'repos/owner/gh-aw-advisories/collaborators?per_page=2') printf '%s' "$COLLABORATORS" ;;
'repos/owner/gh-aw-advisories/invitations?per_page=1') printf '%s' "$INVITATIONS" ;;
*) echo unexpected-call >> "$CALL_LOG"; exit 1 ;;
esac
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "gh"), []byte(script), 0o700))
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	const private = `{"id":30,"full_name":"owner/gh-aw-advisories","visibility":"private","owner":{"id":42,"type":"User"}}`
	for _, tc := range []struct {
		name, exists, repo, collaborators, invitations, wantErr string
	}{
		{"creates privately", "no", private, `[{"id":42}]`, `[]`, ""},
		{"reuses without writing", "yes", private, `[{"id":42}]`, `[]`, ""},
		{"rejects shared repo", "yes", private, `[{"id":42},{"id":99}]`, `[]`, "shared"},
		{"rejects pending invitation", "yes", private, `[{"id":42}]`, `[{"id":99}]`, "pending invitations"},
		{"rejects public collision", "yes", `{"id":30,"full_name":"owner/gh-aw-advisories","visibility":"public","owner":{"id":42,"type":"User"}}`, `[]`, `[]`, "must be private"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			logPath := filepath.Join(t.TempDir(), "calls")
			require.NoError(t, os.WriteFile(logPath, nil, 0o600))
			t.Setenv("CALL_LOG", logPath)
			t.Setenv("REPO_EXISTS", tc.exists)
			t.Setenv("REPO_JSON", tc.repo)
			t.Setenv("COLLABORATORS", tc.collaborators)
			t.Setenv("INVITATIONS", tc.invitations)
			cmd := NewAdvisoryCommand()
			var output bytes.Buffer
			cmd.SetOut(&output)
			cmd.SetErr(&output)
			cmd.SetArgs([]string{"setup"})
			err := cmd.ExecuteContext(t.Context())
			if tc.wantErr != "" {
				require.ErrorContains(t, err, tc.wantErr)
			} else {
				require.NoError(t, err)
				require.Contains(t, output.String(), "https://github.com/owner/gh-aw-advisories")
			}
			calls, err := os.ReadFile(logPath)
			require.NoError(t, err)
			if tc.exists == "no" {
				require.Equal(t, "created\n", string(calls))
			} else {
				require.Empty(t, calls)
			}
		})
	}
}
