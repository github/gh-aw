//go:build !integration

package workflow

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type rs05aBridgeScenario struct {
	AwContextProvided bool   `json:"awContextProvided"`
	AwContextRaw      string `json:"awContextRaw"`
	RepositoryFork    bool   `json:"repositoryFork"`
	Actor             string `json:"actor"`
	SenderType        string `json:"senderType"`
	Permission        string `json:"permission"`
	HeadRef           string `json:"headRef"`
	HeadRepoFullName  string `json:"headRepoFullName"`
	BaseRepoFullName  string `json:"baseRepoFullName"`
	CommitCount       int    `json:"commitCount"`
}

type rs05aBridgeResult struct {
	Info            []string            `json:"info"`
	Warnings        []string            `json:"warnings"`
	Errors          []string            `json:"errors"`
	SetFailed       []string            `json:"setFailed"`
	SetOutput       []rs05aOutputCall   `json:"setOutput"`
	ExportVariable  []rs05aOutputCall   `json:"exportVariable"`
	Exec            []rs05aExecCall     `json:"exec"`
	PermissionCalls []map[string]string `json:"permissionCalls"`
	UserCalls       []map[string]string `json:"userCalls"`
	PullCalls       []map[string]any    `json:"pullCalls"`
	Thrown          string              `json:"thrown"`
}

type rs05aOutputCall struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

type rs05aExecCall struct {
	Command     string   `json:"command"`
	Args        []string `json:"args"`
	ArgsIsArray bool     `json:"argsIsArray"`
}

func TestFormalRS05a_CheckoutGateConjunctionAllPass(t *testing.T) {
	result := runRS05aBridge(t, rs05aDefaultBridgeScenario(t, map[string]any{
		"item_type":   "pull_request",
		"item_number": 123,
		"repo":        "test-owner/test-repo",
	}))

	assert.Contains(t, result.Info, "Detected workflow_dispatch event for PR #123 via aw_context, will fetch PR ref")
	assertRS05aCheckoutSucceeded(t, result)
	assertRS05aFetchedPullHeadRef(t, result, 123)
	assert.Len(t, result.PermissionCalls, 1, "RS-05a actor trust gate must run before checkout")
	assert.Empty(t, result.Warnings, "RS-05a all-pass case must not emit warnings")
}

func TestFormalRS05a_RepoScopeMismatchBlocksCheckout(t *testing.T) {
	result := runRS05aBridge(t, rs05aDefaultBridgeScenario(t, map[string]any{
		"item_type":   "pull_request",
		"item_number": 123,
		"repo":        "other-owner/other-repo",
	}))

	assertRS05aCheckoutSkipped(t, result)
	assert.Empty(t, result.PermissionCalls, "RS-05a repository scope mismatch must block before actor trust")
	assertRS05aWarningContains(t, result, "Cross-repository workflow_dispatch is not supported")
	assertRS05aWarningContains(t, result, "other-owner/other-repo")
}

func TestFormalRS05a_RepoScopeAbsentAllowsCheckoutContinuation(t *testing.T) {
	result := runRS05aBridge(t, rs05aDefaultBridgeScenario(t, map[string]any{
		"item_type":   "pull_request",
		"item_number": 123,
	}))

	assertRS05aCheckoutSucceeded(t, result)
	assertRS05aFetchedPullHeadRef(t, result, 123)
	assert.Len(t, result.PermissionCalls, 1, "RS-05a absent repo field must fall through to actor trust")
}

func TestFormalRS05a_ActorTrustForkRejected(t *testing.T) {
	scenario := rs05aDefaultBridgeScenario(t, map[string]any{
		"item_type":   "pull_request",
		"item_number": 123,
		"repo":        "test-owner/test-repo",
	})
	scenario.RepositoryFork = true

	result := runRS05aBridge(t, scenario)

	assertRS05aNoFetch(t, result)
	assert.Empty(t, result.PermissionCalls, "RS-05a forked runtime repository must block before permission lookup")
	assertRS05aFailedContains(t, result, "Refusing PR checkout in forked repository runtime context")
	assertRS05aOutput(t, result, "checkout_pr_success", "false")
}

func TestFormalRS05a_ActorTrustInsufficientPermissionRejected(t *testing.T) {
	scenario := rs05aDefaultBridgeScenario(t, map[string]any{
		"item_type":   "pull_request",
		"item_number": 123,
		"repo":        "test-owner/test-repo",
	})
	scenario.Permission = "read"

	result := runRS05aBridge(t, scenario)

	assertRS05aNoFetch(t, result)
	assert.Len(t, result.PermissionCalls, 1, "RS-05a must query actor permission for non-bot actors")
	assertRS05aFailedContains(t, result, "requires write or higher")
	assertRS05aOutput(t, result, "checkout_pr_success", "false")
}

func TestFormalRS05a_ActorTrustVerifiedBotAllowed(t *testing.T) {
	scenario := rs05aDefaultBridgeScenario(t, map[string]any{
		"item_type":   "pull_request",
		"item_number": 123,
		"repo":        "test-owner/test-repo",
	})
	scenario.Actor = "copilot-swe-agent[bot]"
	scenario.SenderType = "Bot"
	scenario.Permission = "write"

	result := runRS05aBridge(t, scenario)

	assertRS05aCheckoutSucceeded(t, result)
	assertRS05aFetchedPullHeadRef(t, result, 123)
	assert.Len(t, result.PermissionCalls, 1, "RS-05a verified bot/app actor must satisfy the repository permission floor")
	assert.Contains(t, result.Info, "Runtime safety check passed for actor 'copilot-swe-agent[bot]' with 'write' permission")
}

func TestFormalRS05a_ActorTrustBotWithoutWritePermissionRejected(t *testing.T) {
	scenario := rs05aDefaultBridgeScenario(t, map[string]any{
		"item_type":   "pull_request",
		"item_number": 123,
		"repo":        "test-owner/test-repo",
	})
	scenario.Actor = "third-party-bot[bot]"
	scenario.SenderType = "Bot"
	scenario.Permission = "read"

	result := runRS05aBridge(t, scenario)

	assertRS05aNoFetch(t, result)
	assert.Len(t, result.PermissionCalls, 1, "RS-05a must query repository permission even for bot/app actors")
	assertRS05aFailedContains(t, result, "requires write or higher")
	assertRS05aOutput(t, result, "checkout_pr_success", "false")
}

func TestFormalRS05a_CentralizedDispatchValidatesOriginatingActor(t *testing.T) {
	scenario := rs05aDefaultBridgeScenario(t, map[string]any{
		"command_name": "triage",
		"actor":        "trusted-maintainer",
		"item_type":    "pull_request",
		"item_number":  123,
		"repo":         "test-owner/test-repo",
	})
	scenario.Actor = "github-actions[bot]"
	scenario.SenderType = "Bot"

	result := runRS05aBridge(t, scenario)

	assertRS05aCheckoutSucceeded(t, result)
	assertRS05aFetchedPullHeadRef(t, result, 123)
	require.Len(t, result.PermissionCalls, 1)
	assert.Equal(t, "trusted-maintainer", result.PermissionCalls[0]["username"], "centralized dispatch must validate the originating actor, not the dispatcher bot")
	assert.Contains(t, result.Info, "Validating centralized workflow_dispatch against originating actor 'trusted-maintainer'")
}

func TestFormalRS05a_CentralizedDispatchWithoutOriginatingActorRejected(t *testing.T) {
	scenario := rs05aDefaultBridgeScenario(t, map[string]any{
		"command_name": "triage",
		"item_type":    "pull_request",
		"item_number":  123,
		"repo":         "test-owner/test-repo",
	})
	scenario.Actor = "github-actions[bot]"
	scenario.SenderType = "Bot"

	result := runRS05aBridge(t, scenario)

	assertRS05aNoFetch(t, result)
	assert.Empty(t, result.PermissionCalls, "missing originating actor must fail before permission lookup")
	assertRS05aFailedContains(t, result, "unable to determine originating actor")
	assertRS05aOutput(t, result, "checkout_pr_success", "false")
}

func TestFormalRS05a_CentralizedDispatchRequiresBotSenderSignal(t *testing.T) {
	scenario := rs05aDefaultBridgeScenario(t, map[string]any{
		"command_name": "triage",
		"actor":        "trusted-maintainer",
		"item_type":    "pull_request",
		"item_number":  123,
		"repo":         "test-owner/test-repo",
	})
	scenario.Actor = "github-actions[bot]"
	scenario.SenderType = "User"
	scenario.Permission = "read"

	result := runRS05aBridge(t, scenario)

	assertRS05aNoFetch(t, result)
	require.Len(t, result.PermissionCalls, 1)
	assert.Equal(t, "github-actions[bot]", result.PermissionCalls[0]["username"], "propagated actor requires GitHub's Bot sender signal")
	assertRS05aFailedContains(t, result, "requires write or higher")
	assertRS05aOutput(t, result, "checkout_pr_success", "false")
}

func TestFormalRS05a_CentralizedDispatchWithoutRouterMarkersRejected(t *testing.T) {
	scenario := rs05aDefaultBridgeScenario(t, map[string]any{
		"actor":       "trusted-maintainer",
		"item_type":   "pull_request",
		"item_number": 123,
		"repo":        "test-owner/test-repo",
	})
	scenario.Actor = "github-actions[bot]"
	scenario.SenderType = "Bot"

	result := runRS05aBridge(t, scenario)

	assertRS05aNoFetch(t, result)
	assert.Empty(t, result.PermissionCalls, "missing centralized router markers must fail before permission lookup")
	assertRS05aFailedContains(t, result, "unable to identify centralized workflow_dispatch")
	assertRS05aOutput(t, result, "checkout_pr_success", "false")
}

func TestFormalRS05a_MalformedJSONSkipsCheckoutWithoutPanic(t *testing.T) {
	scenario := rs05aDefaultBridgeScenario(t, nil)
	scenario.AwContextRaw = "{not-valid-json"

	result := runRS05aBridge(t, scenario)

	assertRS05aCheckoutSkipped(t, result)
	assert.Empty(t, result.Thrown, "RS-05a malformed aw_context JSON must be caught by the implementation")
	assertRS05aWarningContains(t, result, "Failed to parse aw_context:")
}

func TestFormalRS05a_RefIsolationUsesPullHeadRef(t *testing.T) {
	result := runRS05aBridge(t, rs05aDefaultBridgeScenario(t, map[string]any{
		"item_type":   "pull_request",
		"item_number": 456,
		"repo":        "test-owner/test-repo",
	}))

	assertRS05aCheckoutSucceeded(t, result)
	assertRS05aFetchedPullHeadRef(t, result, 456)
	assert.NotContains(t, strings.Join(rs05aAllExecArgs(result), " "), "fork-owner",
		"RS-05a ref isolation must fetch the pull head ref from the current repository origin, not a caller-supplied remote")
}

func TestFormalRS05a_RefIsolationRejectsShellInterpolation(t *testing.T) {
	result := runRS05aBridge(t, rs05aDefaultBridgeScenario(t, map[string]any{
		"item_type":   "pull_request",
		"item_number": 789,
		"repo":        "test-owner/test-repo",
	}))

	fetch := rs05aFindGitSubcommand(result, "fetch")
	require.NotNil(t, fetch, "RS-05a checkout must fetch the PR head")
	assert.True(t, fetch.ArgsIsArray, "RS-05a fetch must use array-based exec arguments")
	assert.Equal(t, "git", fetch.Command)
	assert.Contains(t, fetch.Args, "+refs/pull/789/head:refs/remotes/origin/pr-head")
	assert.NotContains(t, strings.Join(fetch.Args, " "), "$(")
	assert.NotContains(t, strings.Join(fetch.Args, " "), "${")
	assert.NotContains(t, strings.Join(fetch.Args, " "), ";")
}

func TestFormalRS05a_MissingItemNumberBlocksCheckout(t *testing.T) {
	result := runRS05aBridge(t, rs05aDefaultBridgeScenario(t, map[string]any{
		"item_type": "pull_request",
		"repo":      "test-owner/test-repo",
	}))

	assertRS05aCheckoutSkipped(t, result)
	assert.Empty(t, result.PermissionCalls, "RS-05a missing item_number must block before actor trust")
}

func TestFormalRS05a_ZeroItemNumberTreatedAsFalsy(t *testing.T) {
	for _, itemNumber := range []any{"0", ""} {
		t.Run(fmt.Sprintf("item_number=%q", itemNumber), func(t *testing.T) {
			result := runRS05aBridge(t, rs05aDefaultBridgeScenario(t, map[string]any{
				"item_type":   "pull_request",
				"item_number": itemNumber,
				"repo":        "test-owner/test-repo",
			}))

			assertRS05aCheckoutSkipped(t, result)
			assert.Empty(t, result.PermissionCalls, "RS-05a falsy item_number must block before actor trust")
		})
	}
}

func TestFormalRS05a_NonPullRequestItemTypeBypassesGate(t *testing.T) {
	scenario := rs05aDefaultBridgeScenario(t, map[string]any{
		"item_type":   "issue",
		"item_number": 123,
		"repo":        "test-owner/test-repo",
	})
	scenario.RepositoryFork = true

	result := runRS05aBridge(t, scenario)

	assertRS05aCheckoutSkipped(t, result)
	assert.Empty(t, result.PermissionCalls, "non-pull_request aw_context item types are outside RS-05a checkout scope")
	assert.Empty(t, result.SetFailed, "non-pull_request aw_context item types must bypass the checkout trust gate entirely")
}

func rs05aDefaultBridgeScenario(t *testing.T, awContext map[string]any) rs05aBridgeScenario {
	t.Helper()

	scenario := rs05aBridgeScenario{
		AwContextProvided: true,
		RepositoryFork:    false,
		Actor:             "trusted-maintainer",
		Permission:        "write",
		HeadRef:           "feature-branch",
		HeadRepoFullName:  "test-owner/test-repo",
		BaseRepoFullName:  "test-owner/test-repo",
		CommitCount:       1,
	}
	if awContext != nil {
		raw, err := json.Marshal(awContext)
		require.NoError(t, err)
		scenario.AwContextRaw = string(raw)
	}
	return scenario
}

func runRS05aBridge(t *testing.T, scenario rs05aBridgeScenario) rs05aBridgeResult {
	t.Helper()

	_, file, _, ok := runtime.Caller(0)
	require.True(t, ok)
	repoRoot := filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
	jsDir := filepath.Join(repoRoot, "actions", "setup", "js")

	scenarioJSON, err := json.Marshal(scenario)
	require.NoError(t, err)

	cmd := exec.Command("node", "-e", rs05aBridgeScript, string(scenarioJSON))
	cmd.Dir = jsDir
	cmd.Env = append(cmd.Environ(), "GH_AW_PROMPTS_DIR="+filepath.Join(repoRoot, "actions", "setup", "md"))
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	require.NoError(t, cmd.Run(), "RS-05a JS bridge failed\nstdout:\n%s\nstderr:\n%s", stdout.String(), stderr.String())

	var result rs05aBridgeResult
	require.NoError(t, json.Unmarshal(bytes.TrimSpace(stdout.Bytes()), &result), "RS-05a JS bridge returned invalid JSON: %s", stdout.String())
	require.Empty(t, result.Thrown, "RS-05a JS bridge main() threw unexpectedly")
	return result
}

const rs05aBridgeScript = `
const scenario = JSON.parse(process.argv[1]);
const records = {
  info: [],
  warnings: [],
  errors: [],
  setFailed: [],
  setOutput: [],
  exportVariable: [],
  exec: [],
  permissionCalls: [],
  userCalls: [],
  pullCalls: [],
  thrown: "",
};

global.core = {
  info: message => records.info.push(String(message)),
  warning: message => records.warnings.push(String(message)),
  error: message => records.errors.push(String(message)),
  setFailed: message => records.setFailed.push(String(message)),
  setOutput: (name, value) => records.setOutput.push({ name: String(name), value: String(value) }),
  exportVariable: (name, value) => records.exportVariable.push({ name: String(name), value: String(value) }),
  startGroup: message => records.info.push(String(message)),
  endGroup: () => records.info.push("::endgroup::"),
  summary: {
    addRaw(message) {
      records.info.push(String(message));
      return this;
    },
    async write() {},
  },
};

global.exec = {
  async exec(command, args) {
    records.exec.push({
      command: String(command),
      args: Array.isArray(args) ? args.map(String) : [String(args)],
      argsIsArray: Array.isArray(args),
    });
    return 0;
  },
  async getExecOutput() {
    return { stdout: "true\n", stderr: "", exitCode: 0 };
  },
};

global.context = {
  eventName: "workflow_dispatch",
  actor: scenario.actor || "trusted-maintainer",
  sha: "abc123",
  repo: { owner: "test-owner", repo: "test-repo" },
  payload: {
    repository: { fork: Boolean(scenario.repositoryFork) },
    sender: {
      login: scenario.actor || "trusted-maintainer",
      type: scenario.senderType || "User",
    },
    inputs: {},
  },
};
if (scenario.awContextProvided) {
  global.context.payload.inputs.aw_context = scenario.awContextRaw;
}

global.github = {
  rest: {
    repos: {
      async getCollaboratorPermissionLevel(args) {
        records.permissionCalls.push({ owner: String(args.owner), repo: String(args.repo), username: String(args.username) });
        return { data: { permission: scenario.permission || "write" } };
      },
    },
    users: {
      async getByUsername(args) {
        records.userCalls.push({ username: String(args.username) });
        return { data: { login: String(args.username) } };
      },
    },
    pulls: {
      async get(args) {
        records.pullCalls.push({ owner: String(args.owner), repo: String(args.repo), pull_number: Number(args.pull_number) });
        return {
          data: {
            state: "open",
            commits: Number(scenario.commitCount || 1),
            head: {
              ref: scenario.headRef || "feature-branch",
              repo: { full_name: scenario.headRepoFullName || "test-owner/test-repo", owner: { login: "test-owner" } },
            },
            base: {
              ref: "main",
              repo: { full_name: scenario.baseRepoFullName || "test-owner/test-repo", owner: { login: "test-owner" } },
            },
          },
        };
      },
    },
  },
};

(async () => {
  try {
    await require("./checkout_pr_branch.cjs").main();
  } catch (error) {
    records.thrown = error instanceof Error ? error.message : String(error);
  } finally {
    process.stdout.write(JSON.stringify(records));
  }
})();
`

func assertRS05aCheckoutSucceeded(t *testing.T, result rs05aBridgeResult) {
	t.Helper()
	assert.Empty(t, result.SetFailed, "RS-05a all checkout gates passed; checkout must not fail")
	assertRS05aOutput(t, result, "checkout_pr_success", "true")
}

func assertRS05aCheckoutSkipped(t *testing.T, result rs05aBridgeResult) {
	t.Helper()
	assertRS05aNoFetch(t, result)
	assertRS05aOutput(t, result, "checkout_pr_success", "true")
	assert.Empty(t, result.SetFailed, "RS-05a skipped checkout must be fail-secure without failing the step")
}

func assertRS05aFetchedPullHeadRef(t *testing.T, result rs05aBridgeResult, prNumber int) {
	t.Helper()
	fetch := rs05aFindGitSubcommand(result, "fetch")
	require.NotNil(t, fetch, "RS-05a checkout must fetch the PR head")
	assert.Equal(t, []string{"fetch", "origin", fmt.Sprintf("+refs/pull/%d/head:refs/remotes/origin/pr-head", prNumber), "--depth=2"}, fetch.Args)
}

func assertRS05aNoFetch(t *testing.T, result rs05aBridgeResult) {
	t.Helper()
	assert.Nil(t, rs05aFindGitSubcommand(result, "fetch"), "RS-05a blocked checkout must not fetch any PR ref")
}

func assertRS05aWarningContains(t *testing.T, result rs05aBridgeResult, want string) {
	t.Helper()
	for _, warning := range result.Warnings {
		if strings.Contains(warning, want) {
			return
		}
	}
	assert.Failf(t, "missing warning", "expected warning containing %q in %#v", want, result.Warnings)
}

func assertRS05aFailedContains(t *testing.T, result rs05aBridgeResult, want string) {
	t.Helper()
	for _, failure := range result.SetFailed {
		if strings.Contains(failure, want) {
			return
		}
	}
	assert.Failf(t, "missing failure", "expected setFailed containing %q in %#v", want, result.SetFailed)
}

func assertRS05aOutput(t *testing.T, result rs05aBridgeResult, name, value string) {
	t.Helper()
	for _, output := range result.SetOutput {
		if output.Name == name && output.Value == value {
			return
		}
	}
	assert.Failf(t, "missing output", "expected output %s=%s in %#v", name, value, result.SetOutput)
}

func rs05aFindGitSubcommand(result rs05aBridgeResult, subcommand string) *rs05aExecCall {
	for i := range result.Exec {
		call := &result.Exec[i]
		if call.Command == "git" && len(call.Args) > 0 && call.Args[0] == subcommand {
			return call
		}
	}
	return nil
}

func rs05aAllExecArgs(result rs05aBridgeResult) []string {
	var args []string
	for _, call := range result.Exec {
		args = append(args, call.Command)
		args = append(args, call.Args...)
	}
	return args
}
