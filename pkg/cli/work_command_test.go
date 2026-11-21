package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os/exec"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/workqueue"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"
)

type workAPIRoundTripper func(*http.Request) (*http.Response, error)

func (f workAPIRoundTripper) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

func TestWorkCommandClosedOperatorSurfaces(t *testing.T) {
	command := NewWorkCommand()
	names := []string{}
	for _, subcommand := range command.Commands() {
		names = append(names, subcommand.Name())
	}
	for _, required := range []string{"replay", "stats", "state", "inspect", "tui", "submit-work", "submit-graph", "cancel-work", "release", "clear", "compact", "dispatch"} {
		require.Contains(t, names, required, "required operator surface is absent")
	}
	for _, forbidden := range []string{"claim", "finish", "bind", "launch", "force-release", "dispatch-next", "policy", "control", "trace", "explain", "reprioritize", "cancel-claim", "sync-issues", "reconcile"} {
		require.NotContains(t, names, forbidden, "operator retained a removed or worker-impersonation surface")
	}
	for _, args := range [][]string{
		{"--storage", "issues", "stats"},
		{"dispatch-next", "--work-id", "chosen"},
		{"reconcile", "--force"},
		{"trace", "--claim-id", "chosen"},
		{"explain", "--before-claim", "chosen"},
		{"cancel-claim", "--claim-id", "claim", "--reason", "operator"},
		{"reprioritize", "--work-id", "work", "--priority", "1"},
		{"sync-issues", "--repo", "owner/repo"},
		{"policy", "--from-config", "--repo", "owner/repo"},
		{"--repo", "owner/repo", "clear", "--hard"},
		{"--repo", "owner/repo", "clear", "--reason", "operator"},
		{"--repo", "owner/repo", "submit-work", "--request-id", "stable"},
		{"--repo", "owner/repo", "cancel-work", "--work-id", "work", "--reason", "abandoned"},
		{"--repo", "owner/repo", "cancel-work"},
		{"--repo", "owner/repo", "release", "--dispatch-id", "dispatch", "--reason", "launch_failed_typo"},
		{"--repo", "owner/repo", "release"},
		{"--repo", "owner/repo", "compact", "--min-lines", "0"},
		{"--repo", "owner/repo", "dispatch", "../dispatcher"},
		{"--repo", "owner/repo", "inspect", "--work-id", "work", "--claim-id", "claim"},
		{"--repo", "owner/repo", "inspect"},
		{"--repo", "owner/repo", "state", "--limit", "257"},
		{"--repo", "owner/repo", "state", "--state", "barrier_pending"},
		{"--repo", "owner/repo", "replay", "--pool", "default"},
		{"--repo", "owner/repo", "tui", "--json"},
	} {
		cmd := NewWorkCommand()
		cmd.SetOut(io.Discard)
		cmd.SetErr(io.Discard)
		cmd.SetArgs(args)
		require.Error(t, cmd.Execute(), "unsupported operator input accepted: %v", args)
	}
	require.Equal(t, workqueue.DefaultBranch, command.PersistentFlags().Lookup("branch").DefValue, "CLI default does not match runtime branch")
}

func TestWorkCommandPersistentOutputAndRepositoryFlags(t *testing.T) {
	command := NewWorkCommand()
	for _, tc := range []struct {
		name, shorthand, usage string
	}{
		{"repo", "r", "Target repository (owner/repo format)"},
		{"json", "j", "Output results in JSON format"},
	} {
		flag := command.PersistentFlags().Lookup(tc.name)
		if flag == nil || flag.Shorthand != tc.shorthand || flag.Usage != tc.usage {
			t.Fatalf("--%s should be a persistent -%s flag with usage %q; got %v", tc.name, tc.shorthand, tc.usage, flag)
		}
	}
	child, _, err := command.Find([]string{"stats"})
	if err != nil {
		t.Fatal(err)
	}
	for _, shorthand := range []string{"r", "j"} {
		if child.InheritedFlags().ShorthandLookup(shorthand) == nil {
			t.Errorf("stats does not inherit -%s", shorthand)
		}
	}
}

func TestWorkCommandInfersRepositoryFromLocalOrigin(t *testing.T) {
	t.Chdir(t.TempDir())
	t.Setenv("GH_REPO", "")
	t.Setenv("GITHUB_SERVER_URL", "https://github.com")
	ClearCurrentRepoSlugCache()
	t.Cleanup(ClearCurrentRepoSlugCache)
	require.NoError(t, exec.Command("git", "init", "--quiet", "--template=").Run())
	require.NoError(t, exec.Command("git", "remote", "add", "origin", "https://github.com/local/repo.git").Run())
	for _, tc := range []struct {
		use, invocation string
	}{
		{"work-queue", "work-queue"},
		{"renamed-queue", "renamed-queue"},
		{"work-queue", "work"},
		{"renamed-queue", "work"},
	} {
		for _, repo := range []string{"", "explicit/repo"} {
			t.Run(tc.use+"/"+tc.invocation+"/"+repo, func(t *testing.T) {
				if repo != "" {
					t.Setenv("GH_REPO", "environment/repo")
				}
				command := NewWorkCommand()
				command.Use = tc.use
				child, _, err := command.Find([]string{"replay"})
				require.NoError(t, err)
				child.RunE = func(cmd *cobra.Command, _ []string) error {
					actual, err := cmd.Flags().GetString("repo")
					require.NoError(t, err)
					expected := repo
					if expected == "" {
						expected = "local/repo"
					}
					require.Equal(t, expected, actual)
					return nil
				}
				parent := &cobra.Command{Use: "aw"}
				parent.AddCommand(command)
				args := []string{tc.invocation, "replay"}
				if repo != "" {
					args = append(args, "--repo", repo)
				}
				parent.SetArgs(args)
				require.NoError(t, parent.Execute())
			})
		}
	}
}

func TestWorkCommandRootHelpDoesNotInferRepository(t *testing.T) {
	t.Chdir(t.TempDir())
	for _, embedded := range []bool{false, true} {
		command := NewWorkCommand()
		executor := command
		args := []string{}
		if embedded {
			executor = &cobra.Command{Use: "aw"}
			executor.AddCommand(command)
			args = append(args, "work")
		}
		var output bytes.Buffer
		executor.SetOut(&output)
		executor.SetArgs(args)
		require.NoError(t, executor.Execute())
		require.Contains(t, output.String(), command.Long)
		repo, err := command.Flags().GetString("repo")
		require.NoError(t, err)
		require.Empty(t, repo)
	}
}

func TestWorkCommandSubmitDefaultsRequireOmittedIdentityFlags(t *testing.T) {
	t.Setenv("GH_HOST", "github.com")
	// Without credentials a ledger read would fail with a different error.
	t.Setenv("GH_TOKEN", "")
	original := http.DefaultTransport
	t.Cleanup(func() { http.DefaultTransport = original })
	http.DefaultTransport = workAPIRoundTripper(func(r *http.Request) (*http.Response, error) {
		t.Errorf("submit validation made an API request: %s", r.URL.Path)
		return nil, fmt.Errorf("unexpected API request %s", r.URL.Path)
	})
	for _, args := range [][]string{
		{"--graph-id", ""},
		{"--node-key", ""},
		{"--graph-id", "", "--node-key", "distinct"},
		{"--graph-id", "distinct", "--node-key", ""},
	} {
		cmd := NewWorkCommand()
		cmd.SetIn(strings.NewReader(`{"a":1,"b":2}`))
		cmd.SetOut(io.Discard)
		cmd.SetErr(io.Discard)
		cmd.SetArgs(append([]string{"--repo", "owner/repo", "submit-work", "--file", "-", "--worker-profile", "worker"}, args...))
		err := cmd.Execute()
		require.ErrorContains(t, err, "work_invalid:", "explicit empty identity %v", args)
		require.ErrorContains(t, err, "omit --graph-id for the payload-hash default", "explicit empty identity %v", args)
	}
	cmd := NewWorkCommand()
	cmd.SetIn(strings.NewReader(`{"a":1}`))
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"--repo", "owner/repo", "submit-work", "--file", "-"})
	require.ErrorContains(t, cmd.Execute(), "work_invalid: --worker-profile is required")
}

// TestWorkCommandCurrentProtocolWithoutCheckout drives the operator mutations
// end to end against a Git ledger: admission, release, cancellation, checkpoint.
func TestWorkCommandCurrentProtocolWithoutCheckout(t *testing.T) {
	t.Setenv("GH_HOST", "github.com")
	t.Setenv("GH_TOKEN", "test-token")
	t.Chdir(t.TempDir())
	original := http.DefaultTransport
	t.Cleanup(func() { http.DefaultTransport = original })
	http.DefaultTransport = workAPIRoundTripper(func(r *http.Request) (*http.Response, error) {
		t.Errorf("operator command made an API request: %s %s", r.Method, r.URL.Path)
		return nil, fmt.Errorf("unexpected API request %s", r.URL.Path)
	})
	const remote = workQueueTestRepository
	queue := newWorkQueueGitRemote(t, "")
	run := func(input string, args ...string) map[string]any {
		t.Helper()
		cmd := NewWorkCommand()
		var output bytes.Buffer
		cmd.SetIn(strings.NewReader(input))
		cmd.SetOut(&output)
		cmd.SetErr(&output)
		cmd.SetArgs(append([]string{"--repo", remote, "--json"}, args...))
		require.NoError(t, cmd.Execute(), "command %v: %s", args, output.String())
		var result map[string]any
		require.NoError(t, json.Unmarshal(output.Bytes(), &result), "command %v output %q", args, output.String())
		return result
	}
	unchanged := func(body func()) {
		t.Helper()
		before, pushes := queue.read(t), queue.pushes(t)
		body()
		require.Equal(t, before, queue.read(t), "ledger changed")
		require.Equal(t, pushes, queue.pushes(t), "ledger was pushed")
	}
	reject := func(input, code string, args ...string) {
		t.Helper()
		unchanged(func() {
			cmd := NewWorkCommand()
			cmd.SetIn(strings.NewReader(input))
			cmd.SetOut(io.Discard)
			cmd.SetErr(io.Discard)
			cmd.SetArgs(append([]string{"--repo", remote, "--json"}, args...))
			require.ErrorContains(t, cmd.Execute(), code, "command %v", args)
		})
	}
	commits := func() []workqueue.QueueCommit {
		t.Helper()
		parsed, err := workqueue.Parse([]byte(queue.read(t)))
		require.NoError(t, err)
		return parsed
	}

	// Admission: the payload-hash root identity is stable across protocol versions.
	const rootGraphID = "b6bd7716590080313f39cf62b6867584109cd484ac4c78a48d8a4160ecac4785"
	submitted := run(`{"task":"review"}`, "submit-work", "--file", "-", "--worker-profile", "worker")
	id := workqueue.NodeID(rootGraphID, "root")
	require.Equal(t, "f096f37384d5cea26021021248638442ed2bb42a3b0b01e10e85224c130f97c5", id)
	require.Equal(t, true, submitted["created"])
	require.Equal(t, id, submitted["work_id"])
	require.Equal(t, 1, queue.pushes(t))
	genesis := commits()
	require.Len(t, genesis, 1)
	require.Nil(t, genesis[0].Previous)
	require.Equal(t, workqueue.Version, genesis[0].Version)
	require.Equal(t, "submit", genesis[0].Request.Kind)
	require.Equal(t, workqueue.Actor{Role: "administrator", Repository: remote}, genesis[0].Actor)
	require.Len(t, genesis[0].Operations, 1)
	var node workqueue.WorkDefinition
	require.NoError(t, json.Unmarshal(genesis[0].Operations[0], &node))
	require.Equal(t, workqueue.WorkDefinition{Kind: "Work", WorkID: id, GraphID: rootGraphID, NodeKey: "root", Pool: "default", Priority: 3,
		WorkerProfile: "worker", Payload: json.RawMessage(`{"task":"review"}`), DependsOn: []workqueue.Dependency{}}, node)

	// Work identity is immutable: equal or changed payloads at an admitted ID are no-ops.
	unchanged(func() {
		require.Equal(t, false, run(`{"task":"\u0072eview"}`, "submit-work", "--file", "-", "--worker-profile", "worker")["created"])
		require.Equal(t, false, run(`{"task":"changed"}`, "submit-work", "--file", "-", "--worker-profile", "worker",
			"--graph-id", rootGraphID, "--node-key", "root")["created"])
		graph, err := json.Marshal([]workqueue.WorkDefinition{node})
		require.NoError(t, err)
		// The same submit request (actor, kind, Work IDs) is recovered, not re-published.
		require.Equal(t, true, run(string(graph), "submit-graph", "--file", "-")["recovered"])
	})
	reject(`[]`, "graph_invalid", "submit-graph", "--file", "-")
	nodeGraph, err := json.Marshal([]workqueue.WorkDefinition{node})
	require.NoError(t, err)
	withExpiry := strings.Replace(string(nodeGraph), `"kind":"Work"`, `"kind":"Work","expires_at":1`, 1)
	require.NotEqual(t, string(nodeGraph), withExpiry)
	reject(withExpiry, "graph_invalid", "submit-graph", "--file", "-")
	forged := node
	forged.NodeKey = "forged"
	forgedGraph, err := json.Marshal([]workqueue.WorkDefinition{forged})
	require.NoError(t, err)
	reject(string(forgedGraph), "work_identity_invalid", "submit-graph", "--file", "-")

	// Distinct node keys and graph IDs over the same payload are distinct Work.
	distinct := run(`{"task":"review"}`, "submit-work", "--file", "-", "--worker-profile", "worker", "--node-key", "distinct")
	require.Equal(t, true, distinct["created"])
	require.Equal(t, workqueue.NodeID(rootGraphID, "distinct"), distinct["work_id"])
	distinctGraph := run(`{"task":"review"}`, "submit-work", "--file", "-", "--worker-profile", "worker", "--graph-id", "distinct")
	require.Equal(t, workqueue.NodeID("distinct", "root"), distinctGraph["work_id"])

	// A dispatcher claims and launches the oldest Work and a worker binds to it.
	log := newWorkQueueTestLog(t)
	log.commits = commits()
	dispatchID := log.dispatch(1)[0]
	log.launch(dispatchID)
	log.bind(dispatchID, workQueueTestWorker("301"))
	queue.write(t, log.jsonl())
	require.Equal(t, "claimed", log.state().Works[id].State, "FIFO dispatch must claim the oldest Work")

	reject("", "ownership_conflict", "cancel-work", "--work-id", id)
	reject("", "reason_invalid", "release", "--dispatch-id", dispatchID, "--reason", "abandoned")
	reject("", "dispatch_missing", "release", "--dispatch-id", "d_missing")
	released := run("", "release", "--dispatch-id", dispatchID)
	require.Equal(t, true, released["committed"])
	require.Equal(t, false, released["recovered"])
	require.InDelta(t, 1, released["operations"], 0)
	state := workClearRead(t, queue)
	require.Equal(t, "available", state.Works[id].State)
	require.Equal(t, 1, state.Works[id].Attempts)
	require.Equal(t, "closed", state.Dispatches[dispatchID].State)
	unchanged(func() {
		again := run("", "release", "--dispatch-id", dispatchID)
		require.Equal(t, true, again["recovered"], "release retry did not recover the committed request")
		require.Equal(t, released["commit_id"], again["commit_id"])
		require.Equal(t, false, run("", "release", "--dispatch-id", dispatchID, "--reason", "run_missing")["committed"])
	})

	// Cancellation of exact available Work.
	reject("", "work_missing", "cancel-work", "--work-id", "missing")
	reject("", "reason_invalid", "cancel-work", "--work-id", id, "--reason", "abandoned")
	cancelled := run("", "cancel-work", "--work-id", id+","+id)
	require.Equal(t, true, cancelled["committed"])
	require.InDelta(t, 1, cancelled["operations"], 0)
	unchanged(func() {
		require.Equal(t, true, run("", "cancel-work", "--work-id", id)["recovered"])
	})
	stats := run("", "stats")
	require.InDelta(t, 3, stats["work"], 0)
	require.InDelta(t, 2, stats["available"], 0)
	require.InDelta(t, 1, stats["cancelled"], 0)
	require.Equal(t, map[string]any{"operator": float64(1)}, stats["work_cancellation_reasons"])
	require.Equal(t, map[string]any{"reserved": float64(0), "started": float64(0), "bound": float64(0), "closed": float64(1)}, stats["dispatches_by_state"])

	// Checkpoint: the whole log becomes one commit; Git history keeps the prior log.
	priorHead := queue.head(t)
	priorState := workClearRead(t, queue)
	priorLines := len(commits())
	unchanged(func() {
		require.Equal(t, false, run("", "compact", "--min-lines", "100")["checkpointed"])
	})
	t.Setenv("WORK_QUEUE_TEST_INVALID_REF", "true")
	reject("", "invalid queue branch reference", "compact")
	t.Setenv("WORK_QUEUE_TEST_INVALID_REF", "false")
	checkpointed := run("", "compact")
	require.Equal(t, true, checkpointed["checkpointed"])
	require.InDelta(t, priorLines, checkpointed["lines"], 0)
	compacted := commits()
	require.Len(t, compacted, 1)
	require.Equal(t, compacted[0].ID, checkpointed["tip"])
	require.Equal(t, "checkpoint", compacted[0].Request.Kind)
	var checkpoint workqueue.CheckpointOperation
	require.NoError(t, json.Unmarshal(compacted[0].Operations[0], &checkpoint))
	require.Equal(t, priorState.Tip, checkpoint.PriorTip)
	require.Equal(t, priorHead, strings.TrimSpace(queue.run(t, "", "rev-parse", queue.head(t)+"^")), "checkpoint dropped the prior log from Git history")
	after := workClearRead(t, queue)
	for _, pair := range [][2]any{{priorState.Works, after.Works}, {priorState.Claims, after.Claims}, {priorState.Dispatches, after.Dispatches}} {
		expected, err := json.Marshal(pair[0])
		require.NoError(t, err)
		actual, err := json.Marshal(pair[1])
		require.NoError(t, err)
		require.JSONEq(t, string(expected), string(actual), "checkpoint changed queue state")
	}
	unchanged(func() {
		require.Equal(t, false, run("", "compact")["checkpointed"])
		require.Equal(t, true, run("", "release", "--dispatch-id", dispatchID)["recovered"], "checkpoint lost the committed request")
		require.Equal(t, false, run(`{"task":"review"}`, "submit-work", "--file", "-", "--worker-profile", "worker")["created"])
	})

	// A v3 ledger is refused until a dispatcher or maintenance run converts it.
	queue.write(t, `{"previous":null,"version":3}`+"\n")
	for _, args := range [][]string{{"replay"}, {"compact"}, {"cancel-work", "--work-id", id}} {
		reject(`{"task":"review"}`, "unsupported_protocol", args...)
	}
}
