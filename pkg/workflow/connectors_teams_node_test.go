//go:build !integration

package workflow

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestTeamsConnectorPublicNodeExecution(t *testing.T) {
	node, err := exec.LookPath("node")
	require.NoError(t, err, "Node.js is required to verify the deployed Teams connector runtime")
	source, _ := teamsPublicDocumentation(t)
	source = strings.Replace(source, `threads: ["1730000000000"]`, `threads: ["1730000000000", "1730000000999"]`, 1)
	_, data, _, err := teamsPublicCompiler(t, source, false)
	require.NoError(t, err)
	config := data.Connectors["teams"]
	reader, err := config.Agent.Encode()
	require.NoError(t, err)
	writer, err := config.SafeOutputs.Encode()
	require.NoError(t, err)
	message, err := os.ReadFile("../connectors/testdata/providers/teams/message.json")
	require.NoError(t, err)
	staged, err := config.SafeOutputs.PrepareSafeOutput("reply-to-activity", teamsPublicWriteArguments("Fixture reply"))
	require.NoError(t, err)
	input, err := json.Marshal(map[string]any{
		"reader": reader, "writer": writer, "message": json.RawMessage(message),
		"readArgs": teamsPublicReadArguments(), "writeArgs": teamsPublicWriteArguments("Fixture reply"),
		"staged": map[string]any{"staged": true, "request": staged},
	})
	require.NoError(t, err)
	runtime, err := filepath.Abs("../../actions/setup/js/connector_runtime.cjs")
	require.NoError(t, err)
	command := exec.CommandContext(t.Context(), node, "-e", teamsNodeExecutionDriver, runtime)
	command.Stdin = strings.NewReader(string(input))
	command.Env = []string{"PATH=" + os.Getenv("PATH")}
	output, err := command.CombinedOutput()
	require.NoError(t, err, "%s", output)
	require.Equal(t, "Teams Node reads, replies, boundaries, and staging verified\n", string(output))
}

func TestTeamsConnectorPublicNodeStagingCLI(t *testing.T) {
	node, err := exec.LookPath("node")
	require.NoError(t, err)
	source, _ := teamsPublicDocumentation(t)
	_, data, _, err := teamsPublicCompiler(t, source, false)
	require.NoError(t, err)
	plan := data.Connectors["teams"].SafeOutputs
	encoded, err := plan.Encode()
	require.NoError(t, err)
	runtime, err := filepath.Abs("../../actions/setup/js/connector_runtime.cjs")
	require.NoError(t, err)
	args := teamsPublicWriteArguments("Fixture reply")
	input, err := json.Marshal(args)
	require.NoError(t, err)
	for _, operation := range plan.SafeOutputNames() {
		t.Run(operation, func(t *testing.T) {
			prepared, err := plan.PrepareSafeOutput(operation, args)
			require.NoError(t, err)
			expected, err := json.Marshal(map[string]any{"staged": true, "request": prepared})
			require.NoError(t, err)
			command := exec.CommandContext(t.Context(), node, runtime, "invoke", "safe-output", operation, "--stage")
			command.Stdin = strings.NewReader(string(input))
			command.Env = []string{"GH_AW_CONNECTOR_PLAN=" + encoded}
			output, err := command.CombinedOutput()
			require.NoError(t, err, "%s", output)
			require.JSONEq(t, string(expected), string(output))
		})
	}
}

const teamsNodeExecutionDriver = `
const assert = require("node:assert/strict");
const {executeConnector} = require(process.argv[1]);
const fixture = JSON.parse(require("node:fs").readFileSync(0, "utf8"));
const plain = value => JSON.parse(JSON.stringify(value));
const response = (body, status = 200) => new Response(
  typeof body === "string" ? body : JSON.stringify(body),
  {status, headers: {"content-type": "application/json"}}
);
const denied = new Proxy({}, {get() { throw new Error("Unexpected credential access"); }});
const noFetch = async () => { throw new Error("Unexpected network access"); };
const readerEnv = {GH_AW_CONNECTOR_SECRET_TEAMS_GRAPH_READ_ACCESS_TOKEN: "fixture-graph-token"};
const writerEnv = {GH_AW_CONNECTOR_SECRET_TEAMS_BOT_ACCESS_TOKEN: "fixture-bot-token"};
const read = (operation, args, fetchImpl, env = readerEnv) =>
  executeConnector(fixture.reader, "mcp", operation, args, {env, fetchImpl});
const write = (operation, args, options = {}) =>
  executeConnector(fixture.writer, "safe-output", operation, args, {env: writerEnv, fetchImpl: noFetch, ...options});
async function main() {
  let calls = 0;
  const root = await read("get-channel-message", fixture.readArgs, async (url, init) => {
    calls++;
    const target = new URL(url);
    assert.equal(target.origin, "https://graph.microsoft.com");
    assert.equal(decodeURIComponent(target.pathname), "/v1.0/teams/" + fixture.readArgs.team +
      "/channels/" + fixture.readArgs.channel + "/messages/" + fixture.readArgs.thread);
    assert.equal(target.search, "");
    assert.equal(init.method, "GET");
    assert.equal(init.headers.Authorization, "Bearer fixture-graph-token");
    assert.equal(init.body, undefined);
    assert.equal(init.redirect, "manual");
    return response(fixture.message);
  });
  assert.equal(root.untrusted, true);
  assert.equal(root.data.id, fixture.readArgs.thread);
  assert.equal(root.data.author, fixture.message.from.user.id);
  assert.equal(JSON.stringify(root).includes("displayName"), false);
  assert.equal(calls, 1);
  await assert.rejects(read("get-channel-message", fixture.readArgs,
    async () => response({...fixture.message, id: "1730000000999"})), /request input/);
  await assert.rejects(read("get-channel-message", fixture.readArgs,
    async () => response({...fixture.message, id: 1730000000000})));
  await assert.rejects(read("get-channel-message", fixture.readArgs,
    async () => response({...fixture.message, body: {content: 1, contentType: "text"}})));
  calls = 0;
  const rawReply = {id: "fixture-reply", from: {user: {displayName: "Fixture participant"}}};
  const replies = await read("list-channel-replies", fixture.readArgs, async (url, init) => {
    calls++;
    const target = new URL(url);
    assert.equal(target.origin, "https://graph.microsoft.com");
    assert.equal(target.searchParams.get("$top"), "20");
    assert.equal(target.searchParams.size, 1);
    assert.equal(init.method, "GET");
    assert.equal(init.headers.Authorization, "Bearer fixture-graph-token");
    return response({value: [rawReply], "@odata.nextLink": "https://untrusted.example/next"});
  });
  assert.deepEqual(plain(replies.data), {replies: [rawReply]});
  assert.equal(calls, 1);
  for (const length of [65536, 65537]) {
    const body = '{"value":[]}' + " ".repeat(length - '{"value":[]}'.length);
    const result = read("list-channel-replies", fixture.readArgs, async () => response(body));
    if (length === 65536) await result;
    else await assert.rejects(result, /byte bound/);
  }
  const prepared = await write("reply-to-activity", fixture.writeArgs,
    {staged: true, env: denied});
  assert.deepEqual(plain(prepared), fixture.staged);
  for (const operation of ["reply-to-activity", "reply-with-review-card"]) {
    calls = 0;
    const result = await write(operation, fixture.writeArgs, {fetchImpl: async (url, init) => {
      calls++;
      const target = new URL(url);
      assert.equal(target.origin, "https://smba.trafficmanager.net");
      assert.equal(decodeURIComponent(target.pathname), "/teams/v3/conversations/" +
        fixture.writeArgs.conversation + "/activities/" + fixture.writeArgs.activity);
      assert.equal(target.search, "");
      assert.equal(init.method, "POST");
      assert.equal(init.headers.Authorization, "Bearer fixture-bot-token");
      const body = JSON.parse(init.body);
      assert.equal(body.type, "message");
      assert.equal(body.replyToId, fixture.writeArgs.activity);
      assert.equal(body.text, fixture.writeArgs.text);
      assert.equal(!!body.attachments, operation === "reply-with-review-card");
      if (body.attachments) assert.deepEqual(body.attachments, [{
        contentType: "application/vnd.microsoft.card.adaptive",
        content: {$schema: "http://adaptivecards.io/schemas/adaptive-card.json",
          type: "AdaptiveCard", version: "1.2",
          body: [{type: "TextBlock", text: "Agentic workflow review", wrap: true}]}
      }]);
      return response({id: "fixture-sent-activity", extra: "omit"}, 201);
    }});
    assert.deepEqual(plain(result.data), {id: "fixture-sent-activity"});
    assert.equal(calls, 1);
    for (const text of ["", " \n", "x".repeat(4001)]) {
      await assert.rejects(write(operation, {...fixture.writeArgs, text}, {env: denied}));
    }
    await write(operation, {...fixture.writeArgs, text: "x".repeat(4000)}, {staged: true, env: denied});
    await write(operation, {...fixture.writeArgs, text: "\u00e9".repeat(4000)}, {staged: true, env: denied});
    await write(operation, {...fixture.writeArgs, text: "\u{1f600}".repeat(4000)}, {staged: true, env: denied});
    await assert.rejects(write(operation, {...fixture.writeArgs, text: "\u00e9".repeat(4001)},
      {staged: true, env: denied}));
    for (const id of ["", null, 123]) {
      await assert.rejects(write(operation, fixture.writeArgs,
        {fetchImpl: async () => response({id})}));
    }
    for (const length of [16384, 16385]) {
      const body = '{"id":"fixture-sent-activity"}' +
        " ".repeat(length - '{"id":"fixture-sent-activity"}'.length);
      const result = write(operation, fixture.writeArgs, {fetchImpl: async () => response(body)});
      if (length === 16384) await result;
      else await assert.rejects(result, /byte bound/);
    }
    calls = 0;
    await assert.rejects(write(operation, fixture.writeArgs, {fetchImpl: async () => {
      calls++;
      return response({}, 429);
    }}), /non-success/);
    assert.equal(calls, 1);
  }
  for (const field of ["conversation", "activity", "service-url"]) {
    await assert.rejects(write("reply-to-activity", {...fixture.writeArgs, [field]: "unapproved"},
      {env: denied}));
  }
  for (const field of ["team", "channel", "thread"]) {
    await assert.rejects(read("get-channel-message", {...fixture.readArgs, [field]: "unapproved"},
      noFetch, denied));
  }
  await assert.rejects(executeConnector(fixture.reader, "safe-output", "reply-to-activity",
    fixture.writeArgs, {env: denied, fetchImpl: noFetch}));
  await assert.rejects(executeConnector(fixture.writer, "mcp", "get-channel-message",
    fixture.readArgs, {env: denied, fetchImpl: noFetch}));
  process.stdout.write("Teams Node reads, replies, boundaries, and staging verified\n");
}
main().catch(error => { console.error(error); process.exitCode = 1; });
`
