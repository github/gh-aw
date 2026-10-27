//go:build !integration

package cli

import (
	"context"
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The fake resolves the effective host like gh, so a split-host request fails
// instead of returning a fixture from the wrong server.
func installHostRoutingTestGH(t *testing.T, hostname, repoFlag string) string {
	t.Helper()
	dir := t.TempDir()
	calls := filepath.Join(dir, "calls")
	script := `#!/bin/sh
printf '%s\n' "$*" >> "$HOST_ROUTING_CALLS"
host="${GH_HOST:-github.com}"
command="$1"
endpoint=""
dir=""
while [ "$#" -gt 0 ]; do
  case "$1" in
    --hostname) host="$2"; shift 2 ;;
    -R)
      [ "$2" = "$HOST_ROUTING_REPO" ] || exit 1
      case "$2" in
        */*/*) host="${2%%/*}" ;;
      esac
      shift 2 ;;
    --dir) dir="$2"; shift 2 ;;
    repos/*) endpoint="$1"; shift ;;
    *) shift ;;
  esac
done
[ "$host" = "$HOST_ROUTING_EXPECTED" ] || { echo "HTTP 404: wrong host" >&2; exit 1; }
case "$command:$endpoint" in
  api:repos/owner/repo/actions/runs/123)
    printf '%s\n' '{"databaseId":123,"status":"completed","conclusion":"success","workflowName":".github/workflows/host-routing-test.lock.yml","workflowPath":".github/workflows/host-routing-test.lock.yml"}' ;;
  api:repos/owner/repo/actions/workflows/host-routing-test.lock.yml)
    printf '%s\n' 'Host routing test' ;;
  api:repos/owner/repo/commits*)
    printf '%s\n' '[{"sha":"abc123","commit":{"message":"Update evals results from workflow run 123"}}]' ;;
  api:repos/owner/repo/contents/evals.jsonl)
    printf '%s\n' "$HOST_ROUTING_EVALS" ;;
  api:repos/owner/repo/actions/runs/123/artifacts)
    printf '%s\n' graders ;;
  run:)
    mkdir -p "$dir"
    printf '%s\n' '{"version":2,"results":[{"id":"score","status":"pass","value":0.8}]}' > "$dir/grader_results.json" ;;
  *) echo "unexpected command" >&2; exit 1 ;;
esac
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "gh"), []byte(script), 0o755))
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("HOST_ROUTING_CALLS", calls)
	t.Setenv("HOST_ROUTING_EXPECTED", hostname)
	t.Setenv("HOST_ROUTING_REPO", repoFlag)
	t.Setenv("HOST_ROUTING_EVALS", base64.StdEncoding.EncodeToString([]byte("{\"id\":\"quality\",\"answer\":\"YES\",\"runid\":\"123\"}\n")))
	return calls
}

func TestEnsureEvalsResultsFromBranchHostRouting(t *testing.T) {
	for _, tt := range []struct {
		name     string
		hostname string
		runURL   string
		expected string
	}{
		{name: "ambient enterprise", expected: "ghes.example.com"},
		{name: "explicit public", hostname: "github.com", expected: "github.com"},
		{name: "public run URL", runURL: "https://github.com/owner/repo/actions/runs/123", expected: "github.com"},
		{name: "enterprise run URL", runURL: "https://other.example.com/owner/repo/actions/runs/123", expected: "other.example.com"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("GH_HOST", "ghes.example.com")
			calls := installHostRoutingTestGH(t, tt.expected, "")
			owner, repo := "owner", "repo"
			if tt.runURL != "" {
				owner, repo = "", ""
			}
			runDir := t.TempDir()
			require.True(t, ensureEvalsResultsFromBranch(context.Background(), WorkflowRun{
				DatabaseID: 123, URL: tt.runURL,
			}, runDir, owner, repo, tt.hostname, false))
			content, err := os.ReadFile(filepath.Join(runDir, "evals.jsonl"))
			require.NoError(t, err)
			assert.Contains(t, string(content), `"runid":"123"`)
			commands, err := os.ReadFile(calls)
			require.NoError(t, err)
			assert.Contains(t, string(commands), "repos/owner/repo/actions/runs/123")
			assert.Contains(t, string(commands), "repos/owner/repo/commits?")
			assert.Contains(t, string(commands), "ref=abc123")
			if tt.hostname == "" && tt.runURL == "" {
				assert.NotContains(t, string(commands), "--hostname")
			} else {
				assert.Equal(t, 4, strings.Count(string(commands), "--hostname "+tt.expected))
			}
		})
	}
}

func TestResolveRunStateBranchRef(t *testing.T) {
	t.Run("finds matching commit for run ID", func(t *testing.T) {
		fakeBinDir := t.TempDir()
		fakeGH := filepath.Join(fakeBinDir, "gh")
		script := "#!/bin/sh\ncat <<'EOF'\n[{\"sha\":\"abc123\",\"commit\":{\"message\":\"Update evals results from workflow run 123\"}}]\nEOF\n"
		require.NoError(t, os.WriteFile(fakeGH, []byte(script), 0o755))
		t.Setenv("PATH", fakeBinDir+string(os.PathListSeparator)+os.Getenv("PATH"))

		ref, err := resolveRunStateBranchRef(context.Background(), "github/gh-aw", "evals/workflow", 123, "", "evals results")
		require.NoError(t, err)
		assert.Equal(t, "abc123", ref)
	})

	t.Run("returns not found when no matching commit exists", func(t *testing.T) {
		fakeBinDir := t.TempDir()
		fakeGH := filepath.Join(fakeBinDir, "gh")
		script := "#!/bin/sh\ncat <<'EOF'\n[{\"sha\":\"abc123\",\"commit\":{\"message\":\"Update evals results from workflow run 999\"}}]\nEOF\n"
		require.NoError(t, os.WriteFile(fakeGH, []byte(script), 0o755))
		t.Setenv("PATH", fakeBinDir+string(os.PathListSeparator)+os.Getenv("PATH"))

		_, err := resolveRunStateBranchRef(context.Background(), "github/gh-aw", "evals/workflow", 123, "", "evals results")
		assert.ErrorIs(t, err, errRunStateCommitNotFound)
	})
}

func TestResolveRepoOverrideForRunKeepsUnknownHostUnset(t *testing.T) {
	t.Setenv("GH_HOST", "ghe.example.com")

	repo, hostname := resolveRepoOverrideForRun(WorkflowRun{}, "octo", "repo", "")

	assert.Equal(t, "octo/repo", repo)
	assert.Empty(t, hostname)
}

func TestResolveRunStateBranchRefPreservesExplicitHost(t *testing.T) {
	for _, tt := range []struct {
		name     string
		hostname string
	}{
		{name: "ambient host"},
		{name: "public host", hostname: "github.com"},
		{name: "enterprise host", hostname: "ghe.example.com"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			fakeBinDir := t.TempDir()
			fakeGH := filepath.Join(fakeBinDir, "gh")
			argsFile := filepath.Join(fakeBinDir, "args")
			script := fmt.Sprintf("#!/bin/sh\nprintf '%%s\\n' \"$@\" > %q\nprintf '%%s\\n' '[{\"sha\":\"abc123\",\"commit\":{\"message\":\"Update evals results from workflow run 123\"}}]'\n", argsFile)
			require.NoError(t, os.WriteFile(fakeGH, []byte(script), 0o755))
			t.Setenv("PATH", fakeBinDir+string(os.PathListSeparator)+os.Getenv("PATH"))
			t.Setenv("GH_HOST", "ghe.example.com")

			ref, err := resolveRunStateBranchRef(context.Background(), "octo/repo", "evals/workflow", 123, tt.hostname, "evals results")

			require.NoError(t, err)
			assert.Equal(t, "abc123", ref)
			args, err := os.ReadFile(argsFile)
			require.NoError(t, err)
			expected := []string{"api", "repos/octo/repo/commits?sha=evals%2Fworkflow&per_page=100&page=1", "--method", "GET"}
			if tt.hostname != "" {
				expected = append(expected, "--hostname", tt.hostname)
			}
			assert.Equal(t, expected, strings.Fields(string(args)))
		})
	}
}

func TestWorkflowIDFromRunPath(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		path string
		want string
	}{
		{name: "empty", path: "", want: ""},
		{name: "markdown workflow", path: ".github/workflows/release.md", want: "release"},
		{name: "lock workflow", path: ".github/workflows/release.lock.yml", want: "release"},
		{name: "yaml workflow", path: ".github/workflows/release.yml", want: "release"},
		{name: "yml workflow", path: ".github/workflows/release.yaml", want: "release"},
		{name: "uppercase and hyphen normalized", path: ".github/workflows/My-Release.yaml", want: "myrelease"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, workflowIDFromRunPath(tt.path))
		})
	}
}
