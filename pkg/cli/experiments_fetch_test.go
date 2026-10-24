//go:build !integration

package cli

import (
	"encoding/base64"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func installExperimentFetchFakeGH(t *testing.T, workflowContent, state string) string {
	t.Helper()
	binDir := t.TempDir()
	argsLog := filepath.Join(binDir, "gh-args.log")
	t.Setenv("EXPERIMENT_GH_ARGS_LOG", argsLog)
	script := fmt.Sprintf(`#!/bin/sh
printf '%%s\n' "$@" >> "$EXPERIMENT_GH_ARGS_LOG"
printf '\n' >> "$EXPERIMENT_GH_ARGS_LOG"
for arg in "$@"; do
  case "$arg" in
    --repo|*'{owner}'*|*'{repo}'*) echo "invalid gh api argument: $arg" >&2; exit 1 ;;
  esac
done
case "$*" in
  *"/branches "*)
    echo '["experiments/ci-coach"]'
    if [ "$EXPERIMENT_GH_PAGED_BRANCHES" = "1" ]; then echo '["experiments/second"]'; fi ;;
  *"/branches/experiments%%2Fci-coach"*) echo "experiments/ci-coach" ;;
  *"contents/.github/workflows "*) echo '["ci-coach.md"]' ;;
  *"ci-coach.md"*) echo %q ;;
  *"state.jsonl"*) echo %q ;;
  *"evals.jsonl"*) echo 'eyJpZCI6InF1YWxpdHkiLCJhbnN3ZXIiOiJZRVMiLCJydW5pZCI6IjEifQo=' ;;
  *) exit 1 ;;
esac
`, base64.StdEncoding.EncodeToString([]byte(workflowContent)), base64.StdEncoding.EncodeToString([]byte(state)))
	require.NoError(t, os.WriteFile(filepath.Join(binDir, "gh"), []byte(script), 0o755))
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("GH_TOKEN", "")
	t.Setenv("GITHUB_TOKEN", "")
	t.Setenv("GH_REPO", "unrelated/checkout")
	t.Setenv("EXPERIMENT_GH_PAGED_BRANCHES", "")
	return argsLog
}

func TestLoadRemoteExperimentConfigsResolvesWorkflowFilename(t *testing.T) {
	argsLog := installExperimentFetchFakeGH(t, `---
experiments:
  style:
    variants: [concise, detailed]
---
# Workflow
`, "")

	result := loadRemoteExperimentConfigs("octo/repo", "cicoach")

	require.Contains(t, result.ExperimentConfigs, "style")
	assert.Equal(t, []string{"concise", "detailed"}, result.ExperimentConfigs["style"].Variants)
	assert.Equal(t, [][]string{
		{"api", "repos/octo/repo/contents/.github/workflows", "--jq", `[.[] | select(.name | endswith(".md")) | .name]`},
		{"api", "repos/octo/repo/contents/.github/workflows/ci-coach.md", "--jq", ".content"},
	}, readExperimentGHArgs(t, argsLog))
}

func TestLoadRemoteExperimentConfigsRejectsUnsafeNames(t *testing.T) {
	for _, name := range []string{"../../x", "nested/../../../x", "../x", "/x", ".."} {
		t.Run(name, func(t *testing.T) {
			argsLog := installExperimentFetchFakeGH(t, "", "")

			result := loadRemoteExperimentConfigs("octo/repo", name)

			assert.Empty(t, result)
			_, err := os.Stat(argsLog)
			assert.ErrorIs(t, err, os.ErrNotExist, "unsafe experiment names must not invoke gh")
		})
	}
}

func TestFetchRemoteExperimentDetailsLoadsState(t *testing.T) {
	installExperimentFetchFakeGH(t, "", `{"run_id":"1","timestamp":"2026-08-18T12:00:00Z","assignments":{"style":"concise"}}`)

	details, err := fetchRemoteExperimentDetails("octo/repo", "experiments/ci-coach", "ci-coach")

	require.NoError(t, err)
	assert.Equal(t, "ci-coach", details.WorkflowID)
	assert.Equal(t, 1, details.TotalRuns)
	require.Len(t, details.Experiments, 1)
	assert.Equal(t, "style", details.Experiments[0].Name)
	assert.Equal(t, map[string]int{"concise": 1}, details.Experiments[0].Variants)
}

func readExperimentGHArgs(t *testing.T, path string) [][]string {
	t.Helper()
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	var calls [][]string
	for call := range strings.SplitSeq(strings.TrimSuffix(string(data), "\n\n"), "\n\n") {
		calls = append(calls, strings.Split(call, "\n"))
	}
	return calls
}

func TestRemoteExperimentAPIRequests(t *testing.T) {
	for _, tt := range []struct {
		name string
		repo string
		host string
	}{
		{name: "owner/repo", repo: "octo/repo"},
		{name: "enterprise", repo: "github.example.com/octo/repo", host: "github.example.com"},
		{name: "explicit github.com", repo: "github.com/octo/repo", host: "github.com"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			argsLog := installExperimentFetchFakeGH(t, `---
experiments:
  style:
    variants: [concise, detailed]
---
# Workflow
`, `{"run_id":"1","timestamp":"2026-08-18T12:00:00Z","assignments":{"style":"concise"}}`)
			t.Setenv("GH_HOST", "ambient.example.com")
			t.Setenv("EXPERIMENT_GH_PAGED_BRANCHES", "1")

			experiments, err := fetchRemoteExperiments(tt.repo)
			require.NoError(t, err)
			require.Len(t, experiments, 2, "all paginated branch arrays must be read")
			assert.Equal(t, "ci-coach", experiments[0].WorkflowID)
			assert.Equal(t, "second", experiments[1].WorkflowID)
			for _, experiment := range experiments {
				assert.Equal(t, 1, experiment.TotalRuns)
			}

			frontmatter, details, evals, err := loadExperimentAnalysisInputs(ExperimentsAnalyzeConfig{
				RepoOverride: tt.repo, ExperimentName: "ci-coach",
			})
			require.NoError(t, err)
			require.Contains(t, frontmatter.ExperimentConfigs, "style")
			assert.Equal(t, 1, details.TotalRuns)
			require.Contains(t, evals, "quality")
			assert.Equal(t, 1, evals["quality"].Yes)

			expected := [][]string{
				{"api", "repos/octo/repo/branches", "--paginate", "--jq", `[.[] | select(.name | startswith("experiments/")) | .name]`},
				{"api", "repos/octo/repo/contents/state.jsonl", "--method", "GET", "--raw-field", "ref=experiments/ci-coach", "--jq", ".content"},
				{"api", "repos/octo/repo/contents/state.jsonl", "--method", "GET", "--raw-field", "ref=experiments/second", "--jq", ".content"},
				{"api", "repos/octo/repo/contents/.github/workflows", "--jq", `[.[] | select(.name | endswith(".md")) | .name]`},
				{"api", "repos/octo/repo/contents/.github/workflows/ci-coach.md", "--jq", ".content"},
				{"api", "repos/octo/repo/branches/experiments%2Fci-coach", "--jq", ".name"},
				{"api", "repos/octo/repo/contents/state.jsonl", "--method", "GET", "--raw-field", "ref=experiments/ci-coach", "--jq", ".content"},
				{"api", "repos/octo/repo/contents/evals.jsonl", "--method", "GET", "--raw-field", "ref=evals/cicoach", "--jq", ".content"},
			}

			if tt.host != "" {
				for i := range expected {
					expected[i] = append(expected[i], "--hostname", tt.host)
				}
			}
			assert.Equal(t, expected, readExperimentGHArgs(t, argsLog))
		})
	}
}

func TestReadRemoteExperimentStateLegacyFallback(t *testing.T) {
	binDir := t.TempDir()
	argsLog := filepath.Join(binDir, "gh-args.log")
	t.Setenv("EXPERIMENT_GH_ARGS_LOG", argsLog)
	script := `#!/bin/sh
printf '%s\n' "$@" >> "$EXPERIMENT_GH_ARGS_LOG"
printf '\n' >> "$EXPERIMENT_GH_ARGS_LOG"
case "$2" in
  repos/octo/repo/contents/state.jsonl) echo 'HTTP 404: Not Found' >&2; exit 1 ;;
  repos/octo/repo/contents/state.json) echo 'eyJjb3VudHMiOnsic3R5bGUiOnsiY29uY2lzZSI6Mn19fQ==' ;;
  *) exit 1 ;;
esac
`
	require.NoError(t, os.WriteFile(filepath.Join(binDir, "gh"), []byte(script), 0o755))
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	state := readRemoteExperimentState("octo/repo", "experiments/legacy")
	assert.Equal(t, map[string]map[string]int{"style": {"concise": 2}}, state.Counts)
	assert.Equal(t, [][]string{
		{"api", "repos/octo/repo/contents/state.jsonl", "--method", "GET", "--raw-field", "ref=experiments/legacy", "--jq", ".content"},
		{"api", "repos/octo/repo/contents/state.json", "--method", "GET", "--raw-field", "ref=experiments/legacy", "--jq", ".content"},
	}, readExperimentGHArgs(t, argsLog))
}

func TestLocalExperimentsDoNotCallGitHubAPI(t *testing.T) {
	argsLog := installExperimentFetchFakeGH(t, "", "")
	t.Chdir(t.TempDir())
	require.NoError(t, os.WriteFile("state.jsonl", []byte(`{"run_id":"1","timestamp":"2026-08-18T12:00:00Z","assignments":{"style":"concise"}}`), 0o644))
	for _, args := range [][]string{
		{"init", "-b", "experiments/local"},
		{"add", "state.jsonl"},
		{"-c", "user.name=Test", "-c", "user.email=test@example.com", "-c", "commit.gpgsign=false", "commit", "-m", "Initial commit"},
	} {
		out, err := exec.Command("git", args...).CombinedOutput()
		require.NoError(t, err, "%s", out)
	}

	experiments, err := fetchLocalExperiments()
	require.NoError(t, err)
	require.Len(t, experiments, 1)
	assert.Equal(t, "local", experiments[0].WorkflowID)
	assert.Equal(t, 1, experiments[0].TotalRuns)
	details, err := fetchLocalExperimentDetails("experiments/local", "local")
	require.NoError(t, err)
	assert.Equal(t, 1, details.TotalRuns)
	_, err = os.Stat(argsLog)
	assert.ErrorIs(t, err, os.ErrNotExist, "local mode must not invoke gh")
}
