//go:build !integration

package workflow

import (
	"encoding/json"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/connectors"
	"github.com/stretchr/testify/require"
)

const slackNodeHarness = `
const assert = require("node:assert/strict");
const { executeConnector } = require(process.argv[1]);
const input = JSON.parse(require("node:fs").readFileSync(0, "utf8"));
let calls = 0;
const env = input.staged ? new Proxy({}, { get() { throw new Error("staging resolved credentials"); } }) : {
  GH_AW_CONNECTOR_SECRET_SLACK_READ_TOKEN: "fixture-reader",
  GH_AW_CONNECTOR_SECRET_SLACK_WRITE_TOKEN: "fixture-writer",
};
const fetchImpl = async (url, init) => {
  calls++;
  assert.equal(input.staged, false);
  const parsed = new URL(url);
  assert.equal(parsed.origin, "https://slack.com");
  assert.equal(init.redirect, "manual");
  assert.equal(init.credentials, "omit");
  if (input.capability === "mcp") {
    assert.equal(init.headers.Authorization, "Bearer fixture-reader");
    assert.equal(init.method, "GET");
    assert.equal(parsed.pathname, "/api/conversations.replies");
    assert.deepEqual(Object.fromEntries(parsed.searchParams), {
      channel: "C123ABC456", ts: "1515449522.000016", limit: "15",
    });
  } else {
    assert.equal(init.headers.Authorization, "Bearer fixture-writer");
    assert.equal(init.method, "POST");
    assert.equal(parsed.pathname, "/api/chat.postMessage");
    assert.equal(parsed.search, "");
    assert.deepEqual(JSON.parse(init.body), {
      channel: "C123ABC456", thread_ts: "1515449522.000016", text: input.arguments.text,
      reply_broadcast: false, unfurl_links: false, unfurl_media: false, mrkdwn: false, parse: "none",
    });
  }
  return new Response(input.response, { status: input.status, headers: { "content-type": "application/json" } });
};
executeConnector(input.plan, input.capability, input.operation, input.arguments, { env, fetchImpl, staged: input.staged })
  .then(result => process.stdout.write(JSON.stringify({ result, calls })))
  .catch(error => {
    process.stdout.write(JSON.stringify({ error: error.message, calls }));
    process.exitCode = 1;
  });
`

type slackNodeResult struct {
	Result json.RawMessage `json:"result"`
	Error  string          `json:"error"`
	Calls  int             `json:"calls"`
}

func runSlackNode(t *testing.T, plan *connectors.Plan, capability, operation string, args map[string]any, body string, status int, staged bool) (slackNodeResult, error) {
	t.Helper()
	node, err := exec.LookPath("node")
	require.NoError(t, err, "Slack bundled-host tests require Node.js")
	runtime, err := filepath.Abs("../../actions/setup/js/connector_runtime.cjs")
	require.NoError(t, err)
	encoded, err := plan.Encode()
	require.NoError(t, err)
	input, err := json.Marshal(map[string]any{
		"plan": encoded, "capability": capability, "operation": operation,
		"arguments": args, "response": body, "status": status, "staged": staged,
	})
	require.NoError(t, err)
	command := exec.CommandContext(t.Context(), node, "-e", slackNodeHarness, runtime)
	command.Env = []string{}
	command.Stdin = strings.NewReader(string(input))
	output, executionError := command.CombinedOutput()
	var result slackNodeResult
	require.NoError(t, json.Unmarshal(output, &result), "%s", output)
	return result, executionError
}

func TestSlackBundledNodeReadContract(t *testing.T) {
	config := slackDocumentationPlan(t)
	body := `{"ok":true,"has_more":false,"messages":[{"ts":"1515449522.000016","text":"Untrusted fixture"}],"response_metadata":{"next_cursor":""},"private":"excluded"}`
	result, err := runSlackNode(t, config.Agent, "mcp", "read-thread", slackDocumentationArguments(), body, 200, false)
	require.NoError(t, err, "%s", result.Error)
	require.Equal(t, 1, result.Calls)
	var actual connectors.Result
	require.NoError(t, json.Unmarshal(result.Result, &actual))
	require.True(t, actual.Untrusted)
	require.JSONEq(t, `{"has-more":false,"messages":[{"ts":"1515449522.000016","text":"Untrusted fixture"}]}`, string(actual.Data))
}

func TestSlackBundledNodeCursorEvidence(t *testing.T) {
	config := slackDocumentationPlan(t)
	for _, cursor := range []string{`""`, `null`, `"next"`, `" "`, `false`, `0`, `[]`, `{}`} {
		t.Run(cursor, func(t *testing.T) {
			body := `{"ok":true,"has_more":false,"messages":[{"ts":"1515449522.000016"}],"response_metadata":{"next_cursor":` + cursor + `}}`
			result, err := runSlackNode(t, config.Agent, "mcp", "read-thread", slackDocumentationArguments(), body, 200, false)
			if cursor == `""` || cursor == `null` {
				require.NoError(t, err, "%s", result.Error)
			} else {
				require.Error(t, err)
				require.Contains(t, result.Error, "optional evidence")
			}
			require.Equal(t, 1, result.Calls)
		})
	}
}

func TestSlackBundledNodeCompletePageByteBound(t *testing.T) {
	config := slackDocumentationPlan(t)
	body := `{"ok":true,"has_more":false,"messages":[{"ts":"1515449522.000016"}]}`
	body += strings.Repeat(" ", 65536-len(body))
	result, err := runSlackNode(t, config.Agent, "mcp", "read-thread", slackDocumentationArguments(), body, 200, false)
	require.NoError(t, err, "%s", result.Error)
	result, err = runSlackNode(t, config.Agent, "mcp", "read-thread", slackDocumentationArguments(), body+" ", 200, false)
	require.Error(t, err)
	require.Contains(t, result.Error, "byte bound")
	require.Equal(t, 1, result.Calls)
}

func TestSlackBundledNodeReplyAndStaging(t *testing.T) {
	config := slackDocumentationPlan(t)
	args := slackDocumentationArguments()
	args["text"] = "Reviewed."
	staged, err := runSlackNode(t, config.SafeOutputs, "safe-output", "reply-in-thread", args, "", 200, true)
	require.NoError(t, err, "%s", staged.Error)
	require.Equal(t, 0, staged.Calls)
	require.Contains(t, string(staged.Result), `"staged":true`)
	body := `{"ok":true,"channel":"C123ABC456","ts":"1515449599.000001","message":{"thread_ts":"1515449522.000016"},"private":"excluded"}`
	result, err := runSlackNode(t, config.SafeOutputs, "safe-output", "reply-in-thread", args, body, 200, false)
	require.NoError(t, err, "%s", result.Error)
	require.Equal(t, 1, result.Calls)
	var actual connectors.Result
	require.NoError(t, json.Unmarshal(result.Result, &actual))
	require.JSONEq(t, `{"channel":"C123ABC456","message-id":"1515449599.000001","thread-id":"1515449522.000016"}`, string(actual.Data))
}

func TestSlackBundledNodeUnicodeTextLimit(t *testing.T) {
	config := slackDocumentationPlan(t)
	for _, character := range []string{"x", "\u00e9", "\U0001f600"} {
		args := slackDocumentationArguments()
		args["text"] = strings.Repeat(character, 4000)
		_, err := config.SafeOutputs.PrepareSafeOutput("reply-in-thread", args)
		require.NoError(t, err)
		result, err := runSlackNode(t, config.SafeOutputs, "safe-output", "reply-in-thread", args, "", 200, true)
		require.NoError(t, err, "%s", result.Error)
		require.Equal(t, 0, result.Calls)
		args["text"] = strings.Repeat(character, 4001)
		_, err = config.SafeOutputs.PrepareSafeOutput("reply-in-thread", args)
		require.Error(t, err)
		result, err = runSlackNode(t, config.SafeOutputs, "safe-output", "reply-in-thread", args, "", 200, true)
		require.Error(t, err)
		require.Equal(t, 0, result.Calls)
	}
}

func TestSlackBundledNodeWriteErrorsNeverRetry(t *testing.T) {
	config := slackDocumentationPlan(t)
	for _, failure := range []struct {
		body   string
		status int
	}{
		{`{"ok":false,"error":"internal_error"}`, 200},
		{`{"ok":true,"channel":"COTHER","ts":"1515449599.000001","message":{"thread_ts":"1515449522.000016"}}`, 200},
		{`{"ok":true,"channel":"C123ABC456","ts":"1515449599.000001","message":{"thread_ts":"1515449522.000017"}}`, 200},
		{`{"ok":false,"error":"ratelimited"}`, 429},
		{`{"ok":false}`, 503},
		{"", 307},
	} {
		args := slackDocumentationArguments()
		args["text"] = "Reviewed."
		result, err := runSlackNode(t, config.SafeOutputs, "safe-output", "reply-in-thread", args, failure.body, failure.status, false)
		require.Error(t, err)
		require.NotEmpty(t, result.Error)
		require.Equal(t, 1, result.Calls)
	}
}
