//go:build !integration

package workflow

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/connectors"
	"github.com/stretchr/testify/require"
)

func confluenceNodeCommand(t *testing.T, script string, payload any) []byte {
	t.Helper()
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("Node.js is required for bundled Confluence runtime tests")
	}
	runtime, err := filepath.Abs("../../actions/setup/js/connector_runtime.cjs")
	require.NoError(t, err)
	input, err := json.Marshal(payload)
	require.NoError(t, err)
	command := exec.CommandContext(t.Context(), node, "-e", script, runtime)
	command.Env = []string{}
	command.Stdin = bytes.NewReader(input)
	output, err := command.CombinedOutput()
	require.NoError(t, err, "%s", output)
	return output
}

func TestConfluenceBundledNodeCompilation(t *testing.T) {
	compiler, data, path, err := confluencePublicWorkflow(t, confluencePublicImport, confluencePublicSelections)
	require.NoError(t, err)
	require.NotContains(t, data.Tools, "connector-confluence")
	tool := data.Tools["confluence"].(map[string]any)
	require.Equal(t, "node", tool["entrypoint"])
	require.Equal(t, []string{"get-page"}, data.Connectors["confluence"].Agent.ToolNames())
	require.Contains(t, fmt.Sprint(tool["entrypointArgs"]), "/actions/connector_runtime.cjs serve")
	for operation, qualified := range map[string]string{
		"create-runbook": "confluence_create_runbook", "add-review-comment": "confluence_add_review_comment",
	} {
		script := data.SafeOutputs.Scripts["confluence-"+operation]
		require.NotNil(t, script)
		definition := generateCustomScriptToolDefinition("confluence-"+operation, script)
		require.Equal(t, strings.ReplaceAll(qualified, "_", "-"), definition["name"])
		schema := definition["inputSchema"].(map[string]any)
		require.Equal(t, false, schema["additionalProperties"])
		properties := schema["properties"].(map[string]any)
		require.Contains(t, properties, "cloud-id")
		require.NotContains(t, properties, "body")
		require.NotContains(t, properties, "title")
		require.Contains(t, script.Script, "spawnSync(process.execPath")
		require.Contains(t, script.Script, "'actions', 'connector_runtime.cjs'")
		wrapped := generateSafeOutputScriptContent("confluence-"+operation, script)
		confluenceNodeCommand(t, `
const source = JSON.parse(require("node:fs").readFileSync(0,"utf8"));
new (require("node:vm").Script)(source);
`, wrapped)
	}
	output, err := compiler.CompileToYAML(data, path)
	require.NoError(t, err)
	require.Contains(t, output, "confluence_create_runbook")
	require.Contains(t, output, "confluence_add_review_comment")
	for _, removed := range []string{"connector_10_confluence_", "connector-confluence", "Build connector runtime", "go build", "get-confluence-page", "CONFLUENCE_CONTEXT"} {
		require.NotContains(t, output, removed)
	}
}

func TestConfluenceBundledNodeSafeOutputs(t *testing.T) {
	_, data, _, err := confluencePublicWorkflow(t, confluencePublicImport, confluencePublicSelections)
	require.NoError(t, err)
	plan := data.Connectors["confluence"].SafeOutputs
	encoded, err := plan.Encode()
	require.NoError(t, err)
	for _, operation := range []string{"create-runbook", "add-review-comment"} {
		for _, staged := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/staged=%t", operation, staged), func(t *testing.T) {
				args := map[string]any{"cloud-id": confluenceFixtureCloudID}
				response := map[string]any{"id": "789", "version": map[string]int{"number": 1}}
				if operation == "create-runbook" {
					args["space-id"], args["parent-id"] = "123", "456"
					response["spaceId"], response["parentId"] = "123", "456"
				} else {
					args["page-id"], response["pageId"] = "456", "456"
				}
				expected, err := plan.PrepareSafeOutput(operation, args)
				require.NoError(t, err)
				output := confluenceNodeCommand(t, `
const assert = require("node:assert/strict");
const input = JSON.parse(require("node:fs").readFileSync(0, "utf8"));
const {executeConnector} = require(process.argv[1]);
let calls = 0, secrets = 0, envReads = 0;
const env = new Proxy({}, {get(_, key) {
  envReads++;
  assert.equal(input.staged, false);
  if (key === "GH_AW_CONNECTOR_BINDING_RUN") return undefined;
  secrets++;
  assert.equal(key, input.operation === "create-runbook"
    ? "GH_AW_CONNECTOR_SECRET_CONFLUENCE_PAGE_WRITE_ACCESS_TOKEN"
    : "GH_AW_CONNECTOR_SECRET_CONFLUENCE_COMMENT_WRITE_ACCESS_TOKEN");
  return "fixture-token";
}});
const fetchImpl = async (url, init) => {
  calls++;
  assert.equal(input.staged, false);
  const create = input.operation === "create-runbook";
  assert.equal(url, "https://api.atlassian.com/ex/confluence/" + input.args["cloud-id"] +
    "/wiki/api/v2/" + (create ? "pages" : "footer-comments"));
  assert.equal(init.method, "POST");
  assert.equal(init.headers.Authorization, "Bearer fixture-token");
  assert.equal(init.redirect, "manual");
  assert.equal(init.credentials, "omit");
  const body = JSON.parse(init.body);
  assert.deepEqual(body.body, {representation:"storage", value:"<p>Review the workflow result before taking action.</p>"});
  assert.deepEqual(body, create
    ? {body:body.body, spaceId:"123", parentId:"456", status:"current", title:"Agentic workflow runbook"}
    : {body:body.body, pageId:"456"});
  return new Response(JSON.stringify(input.response), {status:create ? 200 : 201});
};
executeConnector(input.encoded, "safe-output", input.operation, input.args,
  {env, fetchImpl, staged:input.staged}).then(result => {
    assert.equal(calls, input.staged ? 0 : 1);
    assert.equal(secrets, input.staged ? 0 : 1);
    assert.equal(envReads, input.staged ? 0 : 2);
    process.stdout.write(JSON.stringify(result));
  }).catch(error => { console.error(error); process.exitCode = 1; });
`, map[string]any{"encoded": encoded, "operation": operation, "args": args, "staged": staged, "response": response})
				if staged {
					want, err := json.Marshal(map[string]any{"staged": true, "request": expected})
					require.NoError(t, err)
					require.JSONEq(t, string(want), string(output))
				} else {
					var result connectors.Result
					require.NoError(t, json.Unmarshal(output, &result))
					require.True(t, result.Untrusted)
					want, err := json.Marshal(response)
					require.NoError(t, err)
					require.JSONEq(t, string(want), string(result.Data))
				}
			})
		}
	}
}

func TestConfluenceBundledNodeMCP(t *testing.T) {
	_, data, _, err := confluencePublicWorkflow(t, confluencePublicImport, "tools:\n  confluence: [get-page]\n")
	require.NoError(t, err)
	encoded, err := data.Connectors["confluence"].Agent.Encode()
	require.NoError(t, err)
	response, err := os.ReadFile("../connectors/testdata/providers/confluence/pages-response.json")
	require.NoError(t, err)
	output := confluenceNodeCommand(t, `
const assert = require("node:assert/strict");
const {Readable, Writable} = require("node:stream");
const input = JSON.parse(require("node:fs").readFileSync(0,"utf8"));
const {serve} = require(process.argv[1]);
const args = {"cloud-id":"11111111-2222-4333-8444-555555555555","space-id":"123","page-id":"456"};
const messages = [
  {jsonrpc:"2.0",id:1,method:"initialize",params:{protocolVersion:"2025-03-26"}},
  {jsonrpc:"2.0",id:2,method:"tools/list"},
  {jsonrpc:"2.0",id:3,method:"tools/call",params:{name:"get-page",arguments:args}},
  {jsonrpc:"2.0",id:4,method:"tools/call",params:{name:"get-page",arguments:{...args,"page-id":"999"}}},
  {jsonrpc:"2.0",id:5,method:"tools/call",params:{name:"create-runbook",arguments:args}}
];
let calls = 0, output = "";
const sink = new Writable({write(chunk, encoding, done) {output += chunk.toString(); done();}});
const fetchImpl = async (url, init) => {
  calls++;
  assert.equal(url, "https://api.atlassian.com/ex/confluence/" + args["cloud-id"] +
    "/wiki/api/v2/pages?body-format=storage&id=456&limit=1&space-id=123&status=current");
  assert.equal(init.method,"GET");
  assert.equal(init.headers.Authorization,"Bearer fixture-read-token");
  return new Response(input.response);
};
serve(input.encoded, Readable.from(messages.map(value => JSON.stringify(value)+"\n")), sink,
  {env:{GH_AW_CONNECTOR_SECRET_CONFLUENCE_READ_ACCESS_TOKEN:"fixture-read-token"},fetchImpl})
.then(() => {
  const packets = output.trim().split("\n").map(line => JSON.parse(line));
  assert.equal(calls,1);
  assert.deepEqual(packets[1].result.tools.map(tool => tool.name),["get-page"]);
  const schema = packets[1].result.tools[0].inputSchema;
  assert.equal(schema.additionalProperties,false);
  assert.deepEqual(schema.required,["cloud-id","page-id","space-id"]);
  const result = JSON.parse(packets[2].result.content[0].text);
  assert.equal(result.untrusted,true);
  assert.equal(result.data.page,"456");
  assert.deepEqual(Object.keys(result.data).sort(),["content","page","representation","space","title","version"]);
  assert.equal(packets[3].result.isError,true);
  assert.equal(packets[4].result.isError,true);
  process.stdout.write("confluence/get-page");
}).catch(error => {console.error(error);process.exitCode=1;});
`, map[string]any{"encoded": encoded, "response": string(response)})
	require.Equal(t, "confluence/get-page", string(output))
}

func TestConfluenceBundledNodeResponseGuards(t *testing.T) {
	_, data, _, err := confluencePublicWorkflow(t, confluencePublicImport, "tools:\n  confluence: [get-page]\n")
	require.NoError(t, err)
	encoded, err := data.Connectors["confluence"].Agent.Encode()
	require.NoError(t, err)
	fixture, err := os.ReadFile("../connectors/testdata/providers/confluence/pages-response.json")
	require.NoError(t, err)
	cases := []map[string]any{
		{"response": strings.ReplaceAll(string(fixture), `"456"`, `"999"`)},
		{"response": strings.ReplaceAll(string(fixture), `"123"`, `"999"`)},
		{"response": strings.ReplaceAll(string(fixture), `"storage"`, `"view"`)},
		{"response": `{"results":[]}`},
		{"response": string(fixture), "status": 429},
	}
	for _, size := range []int{65535, 65536, 65537} {
		cases = append(cases, map[string]any{
			"response": strings.Repeat(" ", size-len(fixture)) + string(fixture), "valid": size <= 65536,
		})
	}
	output := confluenceNodeCommand(t, `
const assert = require("node:assert/strict");
const input = JSON.parse(require("node:fs").readFileSync(0,"utf8"));
const {executeConnector} = require(process.argv[1]);
(async () => {
  for (const test of input.cases) {
    let calls = 0;
    const fetchImpl = async () => {calls++;return new Response(test.response,{status:test.status || 200});};
    const run = executeConnector(input.encoded,"mcp","get-page",
      {"cloud-id":"11111111-2222-4333-8444-555555555555","space-id":"123","page-id":"456"},
      {env:{GH_AW_CONNECTOR_SECRET_CONFLUENCE_READ_ACCESS_TOKEN:"fixture-read-token"},fetchImpl});
    if (test.valid) await run; else await assert.rejects(run);
    assert.equal(calls,1);
  }
  process.stdout.write(String(input.cases.length));
})().catch(error => {console.error(error);process.exitCode=1;});
`, map[string]any{"encoded": encoded, "cases": cases})
	require.Equal(t, strconv.Itoa(len(cases)), string(output))
}

func TestConfluenceBundledNodeDeniesBeforeCredentials(t *testing.T) {
	_, data, _, err := confluencePublicWorkflow(t, confluencePublicImport, confluencePublicSelections)
	require.NoError(t, err)
	encoded, err := data.Connectors["confluence"].SafeOutputs.Encode()
	require.NoError(t, err)
	output := confluenceNodeCommand(t, `
const assert = require("node:assert/strict");
const input = JSON.parse(require("node:fs").readFileSync(0,"utf8"));
const {executeConnector} = require(process.argv[1]);
let secrets = 0, calls = 0;
const env = new Proxy({}, {get() {secrets++;throw new Error("must not resolve");}});
const fetchImpl = async () => {calls++;throw new Error("must not send");};
(async () => {
  for (const [operation,args] of [
    ["add-review-comment",{"cloud-id":"11111111-2222-4333-8444-555555555555","page-id":"456"}],
    ["create-runbook",{"cloud-id":"11111111-2222-4333-8444-555555555555","parent-id":"456","space-id":"123"}]
  ]) {
    for (const [key,value] of [
      ...Object.keys(args).map(key => [key,key === "cloud-id" ? "22222222-2222-4222-8222-222222222222" : "999"]),
      ...Object.keys(args).map(key => [key,"../unapproved"]),
      ["body","<script>not supported</script>"], ["title","not supported"], ["version",2]
    ]) await assert.rejects(executeConnector(input.encoded,"safe-output",operation,
      {...args,[key]:value},{env,fetchImpl}));
  }
  assert.equal(secrets,0);
  assert.equal(calls,0);
  process.stdout.write("denied");
})().catch(error => {console.error(error);process.exitCode=1;});
`, map[string]string{"encoded": encoded})
	require.Equal(t, "denied", string(output))
}
