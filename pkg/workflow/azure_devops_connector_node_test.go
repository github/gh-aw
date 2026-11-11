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

func azureDevOpsMutationArguments(operation string) map[string]any {
	args := azureDevOpsConsumerArgs()
	switch operation {
	case "create-work-item":
		delete(args, "work-item")
		args["title"] = "literal ${{ text }} $(not-code)"
	case "update-work-item":
		args["title"] = "literal ${{ text }} $(not-code)"
	case "comment-on-work-item":
		args["text"] = "literal ${{ text }} $(not-code)"
	case "assign-work-item":
		args["assignee"] = "approved@example.invalid"
	}
	return args
}

func runAzureDevOpsNode(t *testing.T, script string, input any) {
	t.Helper()
	node, err := exec.LookPath("node")
	require.NoError(t, err, "bundled Azure host checks require Node.js")
	runtime, err := filepath.Abs("../../actions/setup/js/connector_runtime.cjs")
	require.NoError(t, err)
	payload, err := json.Marshal(input)
	require.NoError(t, err)
	command := exec.CommandContext(t.Context(), node, "--unhandled-rejections=strict", "-e", script, runtime)
	command.Env = []string{"PATH=" + os.Getenv("PATH")}
	command.Stdin = strings.NewReader(string(payload))
	output, err := command.CombinedOutput()
	require.NoError(t, err, "%s", output)
	require.Empty(t, string(output), "successful fixture checks must not log credentials or provider content")
}

func TestAzureDevOpsConnectorBundledNodeMutations(t *testing.T) {
	t.Parallel()
	_, data, _ := parseAzureDevOpsConsumer(t, azureDevOpsWriteSelection)
	plan := data.Connectors["azure-devops"].SafeOutputs
	encoded, err := plan.Encode()
	require.NoError(t, err)
	var cases []map[string]any
	for _, operation := range plan.SafeOutputNames() {
		args := azureDevOpsMutationArguments(operation)
		request, err := plan.PrepareSafeOutput(operation, args)
		require.NoError(t, err)
		cases = append(cases, map[string]any{"operation": operation, "args": args, "request": request})
		field, limit := "title", 256
		if operation == "comment-on-work-item" {
			field, limit = "text", 4000
		}
		if operation == "create-work-item" || operation == "update-work-item" || operation == "comment-on-work-item" {
			unicodeArgs := azureDevOpsMutationArguments(operation)
			unicodeArgs[field] = strings.Repeat("\U0001f600", limit)
			request, err := plan.PrepareSafeOutput(operation, unicodeArgs)
			require.NoError(t, err)
			cases = append(cases, map[string]any{"operation": operation, "args": unicodeArgs, "request": request})
		}
	}
	runAzureDevOpsNode(t, `
const assert = require("node:assert/strict");
const { decodePlan, executeConnector } = require(process.argv[1]);
const fixture = JSON.parse(require("node:fs").readFileSync(0, "utf8"));
const plan = decodePlan(fixture.plan);
assert.deepEqual(Object.keys(plan.spec.credentials), ["writer"]);
(async () => {
  for (const {operation, args, request} of fixture.cases) {
    let calls = 0;
    const fetchImpl = async (target, init) => {
      calls++;
      const url = new URL(target);
      assert.equal(url.origin, "https://dev.azure.com");
      assert.equal(init.redirect, "manual");
      assert.equal(init.headers.Authorization, "Bearer fixture-write-token");
      const base = "/contoso/33333333-3333-4333-8333-333333333333/_apis/wit/";
      let response = {id: 9876, fields: {"System.TeamProject": "Approved Project"}};
      const body = JSON.parse(init.body);
      if (operation === "comment-on-work-item") {
        assert.equal(init.method, "POST");
        assert.equal(url.pathname, base + "workItems/9876/comments");
        assert.equal(url.searchParams.get("api-version"), "7.1-preview.4");
        assert.equal(url.searchParams.get("format"), "markdown");
        assert.equal(init.headers["Content-Type"], "application/json");
        assert.deepEqual(body, {text: args.text});
        response = {workItemId: 9876};
      } else {
        assert.equal(init.headers["Content-Type"], "application/json-patch+json");
        assert.equal(url.searchParams.get("api-version"), "7.1");
        assert.equal(init.method, operation === "create-work-item" ? "POST" : "PATCH");
        assert.equal(url.pathname, base + "workitems/" + (operation === "create-work-item" ? "$Task" : "9876"));
        let patch = {op: "add", path: "/fields/System.Title", value: args.title};
        if (operation === "assign-work-item")
          patch = {op: "add", path: "/fields/System.AssignedTo", value: args.assignee};
        if (operation === "link-work-items")
          patch = {op: "add", path: "/relations/-", value: {
            rel: "System.LinkTypes.Related",
            url: "https://dev.azure.com/contoso/33333333-3333-4333-8333-333333333333/_apis/wit/workItems/9877"
          }};
        assert.deepEqual(body, [patch]);
      }
      return new Response(JSON.stringify(response), {status: 200});
    };
    const noSecrets = new Proxy({}, {get() {throw Error("staging accessed a credential");}});
    const staged = await executeConnector(fixture.plan, "safe-output", operation, args, {staged: true, env: noSecrets, fetchImpl});
    assert.deepEqual(JSON.parse(JSON.stringify(staged)), {staged: true, request});
    assert.equal(calls, 0);
    const field = operation === "comment-on-work-item" ? "text" : "title";
    if (args[field] !== undefined) {
      const limit = field === "text" ? 4000 : 256;
      const tooLong = {...args, [field]: String.fromCodePoint(0x1f600).repeat(limit + 1)};
      await assert.rejects(executeConnector(fixture.plan, "safe-output", operation, tooLong, {staged: true, env: noSecrets, fetchImpl}));
      assert.equal(calls, 0);
    }
    const result = await executeConnector(fixture.plan, "safe-output", operation, args, {
      env: {GH_AW_CONNECTOR_SECRET_APPROVED_AZURE_WRITER: "fixture-write-token"}, fetchImpl
    });
    assert.equal(result.untrusted, true);
    assert.equal(calls, 1);
    const expected = operation === "comment-on-work-item" ? {"/workItemId": "work-item"} : {"/id": "work-item"};
    if (operation === "create-work-item") assert.equal(plan.spec.safeOutputs[operation].responseResourceInputs, undefined);
    else assert.deepEqual(JSON.parse(JSON.stringify(plan.spec.safeOutputs[operation].responseResourceInputs)), expected);
  }
})();
`, map[string]any{"plan": encoded, "cases": cases})
}

func TestAzureDevOpsConnectorBundledNodeResponseIdentity(t *testing.T) {
	t.Parallel()
	_, data, _ := parseAzureDevOpsConsumer(t, azureDevOpsWriteSelection)
	encoded, err := data.Connectors["azure-devops"].SafeOutputs.Encode()
	require.NoError(t, err)
	args := map[string]any{}
	for _, operation := range []string{"update-work-item", "comment-on-work-item", "assign-work-item", "link-work-items"} {
		args[operation] = azureDevOpsMutationArguments(operation)
	}
	runAzureDevOpsNode(t, `
const assert = require("node:assert/strict");
const {executeConnector} = require(process.argv[1]);
const fixture = JSON.parse(require("node:fs").readFileSync(0, "utf8"));
(async () => {
  for (const [operation, args] of Object.entries(fixture.args)) {
    for (const token of ["9877", '"9876"', "9876.0", "9.876e3", "9876.0000000000000001",
      "09876", "-9876", "-0", "9007199254740992", "9007199254740993",
      "null", "true", '["9876"]', '{"id":9876}']) {
      let calls = 0;
      const key = operation === "comment-on-work-item" ? "workItemId" : "id";
      const body = '{"' + key + '":' + token + ',"fields":{"System.TeamProject":"Approved Project"}}';
      await assert.rejects(executeConnector(fixture.plan, "safe-output", operation, args, {
        env: {GH_AW_CONNECTOR_SECRET_APPROVED_AZURE_WRITER: "fixture-write-token"},
        fetchImpl: async () => {calls++; return new Response(body, {status: 200});}
      }), token === "9877" ? /exact resource input/ : /Connector|connector/);
      assert.equal(calls, 1);
    }
  }
})();
`, map[string]any{"plan": encoded, "args": args})
}

func TestAzureDevOpsConnectorBundledNodeMetadata(t *testing.T) {
	t.Parallel()
	_, data, _ := parseAzureDevOpsConsumer(t, "tools:\n  azure-devops: [azure-devops]\n")
	encoded, err := data.Connectors["azure-devops"].Agent.Encode()
	require.NoError(t, err)
	runAzureDevOpsNode(t, `
const assert = require("node:assert/strict");
const {decodePlan, callMCP, executeConnector} = require(process.argv[1]);
const {plan: encoded} = JSON.parse(require("node:fs").readFileSync(0, "utf8"));
const plan = decodePlan(encoded);
assert.deepEqual(Object.keys(plan.spec.credentials), ["agent-reader"]);
let calls = 0;
const options = {
  env: {GH_AW_CONNECTOR_SECRET_APPROVED_AZURE_READER: "fixture-read-token"},
  fetchImpl: async (target, init) => {
    calls++;
    assert.equal(target, "https://mcp.dev.azure.com/contoso");
    assert.equal(init.headers.Authorization, "Bearer fixture-read-token");
    assert.equal(init.headers["X-MCP-Readonly"], "true");
    assert.equal(init.headers["X-MCP-Tools"], "core_list_projects");
    assert.equal(init.headers["X-MCP-Toolsets"], undefined);
    const message = JSON.parse(init.body);
    if (message.method === "initialize")
      return Response.json({jsonrpc: "2.0", id: message.id, result: {protocolVersion: "2025-03-26"}});
    if (message.method === "notifications/initialized") return new Response(null, {status: 202});
    assert.equal(message.method, "tools/call");
    assert.deepEqual(message.params, {name: "core_list_projects", arguments: {}});
    return Response.json({jsonrpc: "2.0", id: message.id, result: {content: [{type: "text", text: "project metadata only"}]}});
  }
};
(async () => {
  const result = await callMCP(encoded, "azure-devops", "core_list_projects", {}, options);
  assert.equal(result.content[0].text, "project metadata only");
  assert.equal(calls, 3);
  for (const tool of ["*", "wit_work_item", "wit_work_item_write"])
    await assert.rejects(callMCP(encoded, "azure-devops", tool, {}, options));
  await assert.rejects(callMCP(encoded, "azure-devops", "core_list_projects", {project: "unapproved"}, options));
  await assert.rejects(executeConnector(encoded, "safe-output", "comment-on-work-item", {}, options));
  assert.equal(calls, 3);
  const legacyOnly = {env: {AZURE_DEVOPS_EXT_PAT: "must-not-use", SYSTEM_ACCESSTOKEN: "must-not-use"}, fetchImpl: options.fetchImpl};
  await assert.rejects(callMCP(encoded, "azure-devops", "core_list_projects", {}, legacyOnly), /credential resolution failed/);
  assert.equal(calls, 3);
})();
`, map[string]any{"plan": encoded})
}

func TestAzureDevOpsConnectorBundledNodeStagingCLI(t *testing.T) {
	t.Parallel()
	_, data, _ := parseAzureDevOpsConsumer(t, azureDevOpsWriteSelection)
	plan := data.Connectors["azure-devops"].SafeOutputs
	encoded, err := plan.Encode()
	require.NoError(t, err)
	node, err := exec.LookPath("node")
	require.NoError(t, err)
	for _, operation := range plan.SafeOutputNames() {
		t.Run(operation, func(t *testing.T) {
			args := azureDevOpsMutationArguments(operation)
			request, err := plan.PrepareSafeOutput(operation, args)
			require.NoError(t, err)
			expected, err := json.Marshal(map[string]any{"staged": true, "request": request})
			require.NoError(t, err)
			input, err := json.Marshal(args)
			require.NoError(t, err)
			command := exec.CommandContext(t.Context(), node, "../../actions/setup/js/connector_runtime.cjs", "invoke", "safe-output", operation, "--stage")
			command.Env = []string{"PATH=" + os.Getenv("PATH"), "GH_AW_CONNECTOR_PLAN=" + encoded}
			command.Stdin = strings.NewReader(string(input))
			output, err := command.CombinedOutput()
			require.NoError(t, err, "%s", output)
			require.JSONEq(t, string(expected), string(output))
		})
	}
}
