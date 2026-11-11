package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/google/jsonschema-go/jsonschema"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"
)

func TestEpisodeRetrievalPreservesWorkflowArguments(t *testing.T) {
	for _, args := range [][]string{
		{"workflow-one", "workflow-two"},
		{"--", "episode"},
		{"episode.md"},
		{"org/repo/episode"},
		{"explain"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			cmd := NewLogsCommand()
			var workflows []string
			cmd.RunE = func(_ *cobra.Command, args []string) error {
				workflows = args
				return nil
			}
			cmd.SetArgs(args)
			require.NoError(t, cmd.Execute())
			if len(args) > 0 && args[0] == "--" {
				require.Equal(t, args[1:], workflows)
			} else {
				require.Equal(t, args, workflows)
			}
		})
	}
}

func TestEpisodeRetrievalCommandAndMCPArguments(t *testing.T) {
	t.Setenv("GITHUB_REPOSITORY", "org/one")
	args := logsArgs{
		EpisodeRun: "123", AllowRepositories: []string{"org/two"},
		Count: 25, MaxAPIRequests: 70, StartDate: "2026-10-18", EndDate: "2026-10-20", Timeout: 2,
	}
	require.NoError(t, validateMCPEpisodeLogsArgs(args))
	cmdArgs, count, timeout := buildLogsCommandArgs(context.Background(), args)
	require.Equal(t, 25, count)
	require.Equal(t, 2, timeout)
	require.Equal(t, []string{
		"logs", "episode", "--json", "--output", constants.TmpAwMcpLogsDir,
		"--max-runs", "25", "--max-api-requests", "70", "--timeout", "2m",
		"--repo", "org/one", "--allow-repo", "org/two",
		"--start-date", "2026-10-18", "--end-date", "2026-10-20", "--", "123",
	}, cmdArgs)
	root := NewLogsCommand()
	cmd, remaining, err := root.Find(cmdArgs[1:])
	require.NoError(t, err)
	require.Equal(t, "episode", cmd.Name())
	require.NoError(t, cmd.ParseFlags(remaining))
	require.Equal(t, []string{"123"}, cmd.Flags().Args())
	require.Contains(t, cmd.Long, "not")
	args.Refresh = true
	cmdArgs, _, _ = buildLogsCommandArgs(context.Background(), args)
	require.Contains(t, cmdArgs, "--refresh")
}

func TestEpisodeRetrievalMCPRejectsUnsupportedOptions(t *testing.T) {
	for _, args := range []logsArgs{
		{AllowRepositories: []string{"org/two"}},
		{MaxAPIRequests: 4},
		{EpisodeRepository: "org/one"},
		{Refresh: true},
		{EpisodeRun: "1", WorkflowName: "example"},
		{EpisodeRun: "1", Engine: "copilot"},
		{EpisodeRun: "1", Artifacts: []string{"all"}},
		{EpisodeRun: "1", MaxGitHubAPIRateLimit: -2000},
		{EpisodeRun: "1", Count: -1},
		{EpisodeRun: "1", MaxAPIRequests: -1},
		{EpisodeRun: "1", Timeout: -1},
	} {
		require.Error(t, validateMCPEpisodeLogsArgs(args), "%+v", args)
	}
	args := logsArgs{EpisodeRun: "https://github.com/other/repo/actions/runs/123"}
	cmdArgs, _, _ := buildLogsCommandArgs(context.Background(), args)
	require.NotContains(t, cmdArgs, "--repo")
}

func TestEpisodeRetrievalSeedParsingAndBoundsValidation(t *testing.T) {
	for _, tc := range []struct {
		seed, repo, expected string
	}{
		{"123", "org/one", "org/one"},
		{"https://github.com/org/one/actions/runs/123", "", "github.com/org/one"},
		{"https://github.com/org/one/actions/runs/123", "github.com/org/one", "github.com/org/one"},
		{"https://ghe.example/org/one/actions/runs/123", "", "ghe.example/org/one"},
	} {
		var opts EpisodeRetrievalOptions
		require.NoError(t, resolveEpisodeSeed(&opts, tc.seed, tc.repo))
		require.Equal(t, tc.expected, opts.Repository)
		require.EqualValues(t, 123, opts.SeedRunID)
	}
	for _, tc := range []struct{ seed, repo string }{
		{"0", "org/one"}, {"-1", "org/one"}, {"123", ""},
		{"https://github.com/org/one/actions/runs/123", "other/repo"},
	} {
		var opts EpisodeRetrievalOptions
		require.Error(t, resolveEpisodeSeed(&opts, tc.seed, tc.repo))
	}
	for _, change := range []func(*EpisodeRetrievalOptions){
		func(o *EpisodeRetrievalOptions) { o.MaxRuns = 0 },
		func(o *EpisodeRetrievalOptions) { o.MaxAPIRequests = 0 },
		func(o *EpisodeRetrievalOptions) { o.Timeout = 0 },
		func(o *EpisodeRetrievalOptions) { o.StartDate = "2026-10-21" },
		func(o *EpisodeRetrievalOptions) { o.Repository = "../one" },
		func(o *EpisodeRetrievalOptions) { o.Repository = "org/one?path=other" },
		func(o *EpisodeRetrievalOptions) { o.AllowRepositories = []string{"ghe.example/org/two"} },
		func(o *EpisodeRetrievalOptions) { o.AllowRepositories = []string{"org/.."} },
	} {
		opts := episodeRetrievalTestOptions(t)
		change(&opts)
		_, err := retrieveEpisodeLogs(context.Background(), opts, func(context.Context, string, string) ([]byte, error) {
			t.Fatal("invalid bounds must fail before any API request")
			return nil, nil
		})
		require.Error(t, err)
	}
}

type episodeFailWriter struct{}

func (episodeFailWriter) Write([]byte) (int, error) { return 0, io.ErrClosedPipe }

func TestEpisodeRetrievalOutputAndOutcomeUsagePath(t *testing.T) {
	f := newEpisodeRetrievalFixture(t)
	f.add("org/one", 2, "canonical", "org/one", 2)
	opts := episodeRetrievalTestOptions(t)
	data, err := retrieveEpisodeLogs(context.Background(), opts, f.request)
	require.NoError(t, err)
	var console bytes.Buffer
	require.NoError(t, renderEpisodeRetrieval(&console, data))
	require.Contains(t, console.String(), "Episode canonical")
	require.Contains(t, console.String(), "membership coverage is not complete")
	require.ErrorIs(t, renderEpisodeRetrieval(episodeFailWriter{}, data), io.ErrClosedPipe)
	encoded, err := json.Marshal(data)
	require.NoError(t, err)
	require.Contains(t, string(encoded), `"episode_retrieval"`)
	require.NotContains(t, string(encoded), `"status":"complete"`)
	schemaJSON, err := GenerateNamedOutputSchema("logs")
	require.NoError(t, err)
	var schema jsonschema.Schema
	require.NoError(t, json.Unmarshal(schemaJSON, &schema))
	resolved, err := schema.Resolve(nil)
	require.NoError(t, err)
	var value any
	require.NoError(t, json.Unmarshal(encoded, &value))
	require.NoError(t, resolved.Validate(value))
	usage, err := analyzeTokenUsageAICOnly(data.Runs[0].LogsPath, false)
	require.NoError(t, err)
	require.NotNil(t, usage)
	require.InDelta(t, 0.25, usage.TotalAIC, 0.00001)
}

func TestEpisodeRetrievalConflictingCompactContextsAndIdentities(t *testing.T) {
	_, err := selectEpisodeInfo([]*AwInfo{
		{Context: &AwContext{EpisodeID: "one"}},
		{Context: &AwContext{EpisodeID: "two"}},
	})
	require.ErrorContains(t, err, "conflicting canonical episode")
	for _, repository := range []string{"org/two", "attacker/../../outside"} {
		f := newEpisodeRetrievalFixture(t)
		f.add("org/one", 2, "canonical", "org/one", 2)
		run := f.runs["org/one/2"]
		run.Repository.FullName = repository
		f.runs["org/one/2"] = run
		_, err := retrieveEpisodeLogs(context.Background(), episodeRetrievalTestOptions(t), f.request)
		require.ErrorContains(t, err, "does not match")
		require.Len(t, f.calls, 1)
	}
}

func TestEpisodeRetrievalArtifactPagesShareRequestBudget(t *testing.T) {
	f := newEpisodeRetrievalFixture(t)
	f.add("org/one", 2, "canonical", "org/one", 2)
	opts := episodeRetrievalTestOptions(t)
	opts.MaxAPIRequests = 4
	request := func(ctx context.Context, host, endpoint string) ([]byte, error) {
		if strings.Contains(endpoint, "/artifacts?") && strings.HasSuffix(endpoint, "page=1") {
			f.calls = append(f.calls, endpoint)
			return json.Marshal(map[string]any{"artifacts": make([]episodeArtifact, 100)})
		}
		return f.request(ctx, host, endpoint)
	}
	data, err := retrieveEpisodeLogs(context.Background(), opts, request)
	require.NoError(t, err)
	require.Len(t, f.calls, 4)
	require.Contains(t, f.calls[2], "page=2")
	require.Equal(t, 4, data.EpisodeRetrieval.APIRequests)
	require.Contains(t, data.EpisodeRetrieval.StopReasons, "api_request_limit")
}

func TestEpisodeRetrievalAlreadyScannedNonmemberCannotVerifyParent(t *testing.T) {
	f := newEpisodeRetrievalFixture(t)
	f.add("org/one", 2, "canonical", "org/one", 2)
	f.add("org/one", 3, "different", "org/one", 3)
	f.add("org/one", 4, "canonical", "org/one", 3)
	data, err := retrieveEpisodeLogs(context.Background(), episodeRetrievalTestOptions(t), f.request)
	require.NoError(t, err)
	require.ElementsMatch(t, []int64{2, 4}, data.Episodes[0].RunIDs)
	require.Contains(t, data.Episodes[0].Coverage.Reasons, "retrieval_conflicting_recorded_identity")
}

func TestEpisodeRetrievalFailedRequestConsumesBudget(t *testing.T) {
	f := newEpisodeRetrievalFixture(t)
	f.add("org/one", 2, "canonical", "org/one", 1)
	f.add("org/two", 3, "canonical", "org/two", 3)
	opts := episodeRetrievalTestOptions(t)
	opts.AllowRepositories = []string{"org/two"}
	opts.MaxAPIRequests = 4
	data, err := retrieveEpisodeLogs(context.Background(), opts, f.request)
	require.NoError(t, err)
	require.Len(t, f.calls, 4)
	require.Contains(t, data.EpisodeRetrieval.StopReasons, "api_request_limit")
	require.Contains(t, data.EpisodeRetrieval.Issues[0].Message, "404")
	require.Equal(t, "partial", data.Episodes[0].Coverage.Status)
	require.NotContains(t, strings.Join(f.calls, "\n"), "repos/org/two/")
}

func TestEpisodeRetrievalRejectsMissingBoundsInRenderer(t *testing.T) {
	err := renderEpisodeRetrieval(io.Discard, LogsData{})
	require.Error(t, err)
	require.NotErrorIs(t, err, io.ErrClosedPipe)
}

func TestEpisodeRetrievalCLIUsesOnlyBudgetedRequests(t *testing.T) {
	f := newEpisodeRetrievalFixture(t)
	f.add("org/one", 2, "canonical", "org/one", 2)
	dir := t.TempDir()
	run, err := json.Marshal(f.runs["org/one/2"])
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "run.json"), run, 0600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "usage.zip"), f.archive(2), 0600))
	callsPath := filepath.Join(dir, "calls")
	script := `#!/bin/sh
set -eu
printf '%s\n' "$*" >> '` + callsPath + `'
case "$4" in
  repos/org/one/actions/runs/2) cat '` + filepath.Join(dir, "run.json") + `' ;;
  repos/org/one/actions/runs/2/artifacts*) printf '{"artifacts":[{"id":20,"name":"usage"}]}' ;;
  repos/org/one/actions/artifacts/20/zip) cat '` + filepath.Join(dir, "usage.zip") + `' ;;
  repos/org/one/actions/runs\?*) printf '{"total_count":0,"workflow_runs":[]}' ;;
  *) printf 'unexpected unbudgeted command: %s\n' "$*" >&2; exit 1 ;;
esac
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "gh"), []byte(script), 0700))
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	cmd := NewLogsCommand()
	args := []string{"episode", "2", "--repo", "github.com/org/one", "--start-date", "2026-10-18", "--end-date", "2026-10-20",
		"--max-runs", "1", "--max-api-requests", "4", "--output", filepath.Join(dir, "artifacts")}
	cmd.SetArgs(append(append([]string{}, args...), "--json"))
	var stdout, stderr bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	require.NoError(t, cmd.Execute())
	var data LogsData
	require.NoError(t, json.Unmarshal(stdout.Bytes(), &data))
	require.Equal(t, "canonical", data.EpisodeRetrieval.EpisodeID)
	require.Equal(t, 4, data.EpisodeRetrieval.APIRequests)
	require.False(t, data.EpisodeRetrieval.CollectedAt.IsZero())
	calls, err := os.ReadFile(callsPath)
	require.NoError(t, err)
	require.Len(t, strings.Split(strings.TrimSpace(string(calls)), "\n"), 4)
	require.Empty(t, stderr.String())
	checkEpisodeCLIViewCache(t, args, callsPath)
}

func checkEpisodeCLIViewCache(t *testing.T, args []string, callsPath string) {
	t.Helper()
	cmd := NewLogsCommand()
	cmd.SetArgs(append(append([]string{}, args...), "--view", "hops"))
	var output bytes.Buffer
	cmd.SetOut(&output)
	require.NoError(t, cmd.Execute())
	require.Contains(t, output.String(), "Cached episode snapshot")
	require.Contains(t, output.String(), "no GitHub requests")
	require.Contains(t, output.String(), "Run-level accounting")
	calls, err := os.ReadFile(callsPath)
	require.NoError(t, err)
	require.Len(t, strings.Split(strings.TrimSpace(string(calls)), "\n"), 4)
	cmd = NewLogsCommand()
	cmd.SetArgs(append(append([]string{}, args...), "--refresh", "--json"))
	output.Reset()
	cmd.SetOut(&output)
	require.NoError(t, cmd.Execute())
	var data LogsData
	require.NoError(t, json.Unmarshal(output.Bytes(), &data))
	require.False(t, data.EpisodeRetrieval.Cached)
	require.Equal(t, 3, data.EpisodeRetrieval.APIRequests, "refresh still reuses immutable archives")
	calls, err = os.ReadFile(callsPath)
	require.NoError(t, err)
	require.Len(t, strings.Split(strings.TrimSpace(string(calls)), "\n"), 7)
}

func TestEpisodeRetrievalRejectsInvalidViewBeforeRetrieval(t *testing.T) {
	cmd := newLogsEpisodeCommand()
	cmd.SetArgs([]string{"2", "--repo", "org/one", "--view", "unknown", "--output", t.TempDir()})
	cmd.SetErr(io.Discard)
	require.ErrorContains(t, cmd.Execute(), "invalid --view")
	for _, child := range NewLogsCommand().Commands() {
		require.NotEqual(t, "explain", child.Name())
	}
}

func TestEpisodeRetrievalJSONFlagHasShortAlias(t *testing.T) {
	flag := newLogsEpisodeCommand().Flags().Lookup("json")
	require.NotNil(t, flag)
	require.Equal(t, "j", flag.Shorthand)
}
