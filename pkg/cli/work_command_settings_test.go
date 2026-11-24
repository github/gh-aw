package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/github/gh-aw/pkg/workflow"
	"github.com/stretchr/testify/require"
)

// workSettingsCheckout makes the working directory a checkout whose aw.json
// holds config; an empty config leaves aw.json absent.
func workSettingsCheckout(t *testing.T, config string) {
	t.Helper()
	root := t.TempDir()
	require.NoError(t, os.Mkdir(filepath.Join(root, ".git"), 0o700))
	if config != "" {
		path := filepath.Join(root, workflow.RepoConfigFileName)
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o700))
		require.NoError(t, os.WriteFile(path, []byte(config), 0o600))
	}
	t.Chdir(root)
}

func TestWorkReleaseUsesConfiguredMaxAttempts(t *testing.T) {
	for _, tc := range []struct {
		name, config string
		exhausted    bool
	}{
		{name: "absent configuration defaults to three attempts"},
		{name: "work_queue section omitted", config: `{}`},
		{name: "global retry", config: `{"work_queue":{"retry":{"max_attempts":1}}}`, exhausted: true},
		{name: "pool inherits global retry", config: `{"work_queue":{"retry":{"max_attempts":1},"pools":{"default":{"concurrency":2}}}}`, exhausted: true},
		{name: "pool retry overrides global", config: `{"work_queue":{"retry":{"max_attempts":1},"pools":{"default":{"retry":{"max_attempts":2}}}}}`},
		{name: "other pool retry does not apply", config: `{"work_queue":{"pools":{"other":{"retry":{"max_attempts":1}}}}}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("GH_HOST", "github.com")
			t.Setenv("GH_TOKEN", "test-token")
			workSettingsCheckout(t, tc.config)
			log, ids := workQueueMixedFixture(t)
			queue := newWorkQueueGitRemote(t, log.jsonl())

			cmd := NewWorkCommand()
			var output bytes.Buffer
			cmd.SetOut(&output)
			cmd.SetErr(&output)
			cmd.SetArgs([]string{"--repo", workQueueTestRepository, "--json", "release", "--dispatch-id", ids["running_dispatch"]})
			require.NoError(t, cmd.Execute(), output.String())
			var summary map[string]any
			require.NoError(t, json.Unmarshal(output.Bytes(), &summary))
			want := 1
			if tc.exhausted {
				want = 2
			}
			require.Equal(t, true, summary["committed"])
			require.InDelta(t, want, summary["operations"], 0)
			require.Equal(t, 1, queue.pushes(t))

			work := workClearRead(t, queue).Works[ids["running"]]
			require.Equal(t, 1, work.Attempts)
			if tc.exhausted {
				require.Equal(t, "cancelled", work.State)
				require.Equal(t, "attempts_exhausted", work.CancellationReason)
			} else {
				require.Equal(t, "available", work.State)
			}
		})
	}
}
