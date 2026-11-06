//go:build !integration

package cli

import (
	"context"
	"crypto/sha256"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/github"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLogsOutcomeEnterpriseRepositoryHost(t *testing.T) {
	t.Parallel()
	run := WorkflowRun{
		URL:        "https://github.example.com/acme/repo/actions/runs/42",
		Repository: "acme/repo",
	}
	assert.Equal(t, "github.example.com/acme/repo", logsOutcomeRepository(run))
	run.Repository = "other.example.com/target/repo"
	assert.Equal(t, run.Repository, logsOutcomeRepository(run))
}

func TestLogsOutcomeItemRepositoryHost(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		item CreatedItemReport
		want string
	}{
		{name: "manifest repository", item: CreatedItemReport{Repo: "acme/repo"}, want: "github.example.com/acme/repo"},
		{name: "cross repository", item: CreatedItemReport{Repo: "other/repo"}, want: "github.example.com/other/repo"},
		{name: "explicit enterprise host", item: CreatedItemReport{Repo: "other.example.com/acme/repo"}, want: "other.example.com/acme/repo"},
		{name: "explicit public host", item: CreatedItemReport{Repo: "github.com/acme/repo"}, want: "github.com/acme/repo"},
		{name: "explicit repository host overrides URL", item: CreatedItemReport{Repo: "other.example.com/acme/repo", URL: "https://github.com/acme/repo/issues/42"}, want: "other.example.com/acme/repo"},
		{name: "public object URL", item: CreatedItemReport{Repo: "acme/repo", URL: "https://github.com/acme/repo/issues/42"}, want: "github.com/acme/repo"},
		{name: "public URL only", item: CreatedItemReport{URL: "https://github.com/other/repo/issues/42"}, want: "github.com/other/repo"},
		{name: "enterprise object URL", item: CreatedItemReport{Repo: "acme/repo", URL: "https://other.example.com/acme/repo/issues/42"}, want: "other.example.com/acme/repo"},
		{name: "URL only", item: CreatedItemReport{URL: "https://other.example.com/other/repo/issues/42"}, want: "other.example.com/other/repo"},
		{name: "fallback", want: "github.example.com/acme/repo"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			actual := resolveItemRepo(tc.item, "github.example.com/acme/repo")
			assert.Equal(t, tc.want, actual)
			assert.Equal(t, actual, resolveItemRepo(tc.item, actual), "evaluators resolve the selected repository again")
		})
	}
}

func TestLogsOutcomeEnterpriseManifestRepository(t *testing.T) {
	calls := installOutcomeAPIFixture(t, `case "$*" in
"api repos/acme/repo/issues/42 --hostname github.example.com") printf '%s' '{"state":"closed","state_reason":"completed","labels":[]}' ;;
*) printf 'wrong host or endpoint: %s\n' "$*" >&2; exit 1 ;;
esac
`)
	dir := t.TempDir()
	manifest := "{\"type\":\"create_issue\",\"repo\":\"acme/repo\",\"number\":42}\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, safeOutputItemsManifestFilename), []byte(manifest), 0o600))
	report, err := collectRunOutcomes(context.Background(), ProcessedRun{Run: WorkflowRun{
		DatabaseID: 42, Repository: "acme/repo", LogsPath: dir,
		URL: "https://github.example.com/acme/repo/actions/runs/42",
	}}, github.DefaultObjectiveMapping(), false)
	require.NoError(t, err)
	require.Len(t, report.Items, 1)
	assert.Equal(t, OutcomeStatusAccepted, report.Items[0].OutcomeStatus)
	assert.Equal(t, "github.example.com/acme/repo", report.Items[0].Repo)
	assert.Empty(t, report.Items[0].EvalError)
	assert.Len(t, outcomeAPICalls(t, calls), 1)
	items, err := decodeLogsOutcomeItems(strings.NewReader(manifest))
	require.NoError(t, err)
	assert.Equal(t, outcomeItemID(items[0], make(map[[sha256.Size]byte]int)), report.Items[0].ItemID,
		"host routing must not alter source action identity")
}

func TestEvaluateOutcomesRejectsManifestHostsOutsideRunHost(t *testing.T) {
	calls := installOutcomeAPIFixture(t, `printf '%s\n' "$*" >> "$GH_AW_TEST_OUTCOME_CALLS"; exit 1`)
	items := []CreatedItemReport{
		{Type: "create_issue", Repo: "attacker.example.com/acme/repo", Number: 1},
		{Type: "create_issue", Repo: "acme/repo", URL: "https://attacker.example.com/acme/repo/issues/2"},
	}
	reports := EvaluateOutcomes(context.Background(), items, "github.example.com/acme/repo", nil)
	require.Len(t, reports, 2)
	for _, report := range reports {
		assert.Equal(t, OutcomeStatusError, report.OutcomeStatus)
		assert.Equal(t, "item repository host does not match workflow run host", report.EvalError)
	}
	_, err := os.Stat(calls)
	assert.ErrorIs(t, err, os.ErrNotExist, "untrusted artifact hosts must not trigger API calls")
}

func TestExperimentOutcomeSourcePreservesTrustedHost(t *testing.T) {
	for _, tc := range []struct {
		name, repository, manifest string
		status                     OutcomeStatus
	}{
		{
			name:       "enterprise repository host",
			repository: "github.example.com/acme/repo",
			manifest:   `{"type":"create_pull_request","repo":"attacker.example.com/acme/repo","number":42}`,
			status:     OutcomeStatusError,
		},
		{
			name:       "enterprise object URL host",
			repository: "github.example.com/acme/repo",
			manifest:   `{"type":"create_pull_request","repo":"acme/repo","url":"https://attacker.example.com/acme/repo/pull/42"}`,
			status:     OutcomeStatusError,
		},
		{
			name:       "enterprise URL only",
			repository: "github.example.com/acme/repo",
			manifest:   `{"type":"create_pull_request","url":"https://attacker.example.com/acme/repo/pull/42"}`,
			status:     OutcomeStatusError,
		},
		{
			name:       "public host is not authorized for enterprise run",
			repository: "github.example.com/acme/repo",
			manifest:   `{"type":"create_pull_request","repo":"github.com/acme/repo","number":42}`,
			status:     OutcomeStatusError,
		},
		{
			name:       "public run rejects enterprise artifact host",
			repository: "github.com/acme/repo",
			manifest:   `{"type":"create_pull_request","repo":"attacker.example.com/acme/repo","number":42}`,
			status:     OutcomeStatusError,
		},
		{
			name:       "hostless repository inherits source host",
			repository: "github.example.com/acme/repo",
			manifest:   `{"type":"create_pull_request","repo":"acme/repo","number":42}`,
			status:     OutcomeStatusAccepted,
		},
		{
			name:       "explicit trusted repository makes foreign URL inert",
			repository: "github.example.com/acme/repo",
			manifest:   `{"type":"create_pull_request","repo":"github.example.com/acme/repo","url":"https://attacker.example.com/acme/repo/pull/42"}`,
			status:     OutcomeStatusAccepted,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := installExperimentOutcomeFixture(t, tc.manifest+"\n")
			t.Setenv("GH_HOST", "ambient.example.com")
			source := &githubOutcomeRunSource{artifacts: newGitHubGraderRunArtifactSource(t.TempDir(), tc.repository)}
			data := source.Load(context.Background(), "101")
			require.Empty(t, data.ExclusionReason)
			require.Len(t, data.Report.Items, 1)
			report := data.Report.Items[0]
			assert.Equal(t, tc.status, report.OutcomeStatus)
			commands, err := os.ReadFile(calls)
			require.NoError(t, err)
			assert.Contains(t, string(commands), "-R "+tc.repository)
			assert.NotContains(t, string(commands), "attacker.example.com")
			assert.NotContains(t, string(commands), "ambient.example.com")
			if tc.status == OutcomeStatusError {
				assert.Equal(t, "item repository host does not match workflow run host", report.EvalError)
				assert.NotContains(t, string(commands), "repos/acme/repo/pulls/42",
					"rejected artifacts must not trigger outcome API calls on any host")
			} else {
				assert.Empty(t, report.EvalError)
				assert.Contains(t, string(commands), "api repos/acme/repo/pulls/42 --hostname github.example.com")
			}
		})
	}
}

func TestCollectRunOutcomesTrustsRunURLOverRepositoryHost(t *testing.T) {
	calls := installOutcomeAPIFixture(t, "exit 1\n")
	dir := t.TempDir()
	manifest := "{\"type\":\"create_issue\",\"repo\":\"attacker.example.com/acme/repo\",\"number\":42}\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, safeOutputItemsManifestFilename), []byte(manifest), 0o600))
	report, err := collectRunOutcomes(context.Background(), ProcessedRun{Run: WorkflowRun{
		DatabaseID: 42, Repository: "attacker.example.com/acme/repo", LogsPath: dir,
		URL: "https://github.example.com/acme/repo/actions/runs/42",
	}}, github.DefaultObjectiveMapping(), false)
	require.NoError(t, err)
	require.Len(t, report.Items, 1)
	assert.Equal(t, OutcomeStatusError, report.Items[0].OutcomeStatus)
	assert.Equal(t, "item repository host does not match workflow run host", report.Items[0].EvalError)
	_, err = os.Stat(calls)
	assert.ErrorIs(t, err, os.ErrNotExist)
}
