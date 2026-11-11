//go:build !integration

package workflow

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/connectors"
	"github.com/stretchr/testify/require"
)

const asanaTask = "120000000000001"
const asanaWorkspace = "120000000000002"
const asanaOtherTask = "120000000000003"
const asanaOtherWorkspace = "120000000000004"
const asanaTaskResponse = `{"data":{"gid":"120000000000001","resource_type":"task","name":"Review release plan","notes":"Confirm the rollout prerequisites.","completed":false,"modified_at":"2026-11-11T00:00:00Z","workspace":{"gid":"120000000000002","resource_type":"workspace"},"followers":[{"name":"unprojected-fixture"}]}}`
const asanaCommentResponse = `{"data":{"gid":"120000000000005","resource_type":"story","resource_subtype":"comment_added","type":"comment","text":"Prepared next step.","target":{"gid":"120000000000001","resource_type":"task"}}}`

func asanaPlans(t *testing.T) (*connectors.Plan, *connectors.Plan) {
	t.Helper()
	compiler, path := prepareConnectorScenario(t, connectorCompilerScenario{provider: "asana"}, false, func(consumer, _ map[string]any) {
		for _, raw := range consumer["imports"].([]any) {
			bindings := raw.(map[string]any)["with"].(map[string]any)
			bindings["tasks"] = []any{asanaTask, asanaOtherTask}
			bindings["workspaces"] = []any{asanaWorkspace, asanaOtherWorkspace}
		}
	})
	data, err := compiler.ParseWorkflowFile(path)
	require.NoError(t, err)
	return data.Connectors["asana"].Agent, data.Connectors["asana"].SafeOutputs
}

type asanaTransport func(*http.Request) (int, string)

func (transport asanaTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	status, body := transport(request)
	return &http.Response{StatusCode: status, Request: request, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(body))}, nil
}

func asanaArguments() map[string]any {
	return map[string]any{"task": asanaTask, "workspace": asanaWorkspace, "text": "Prepared next step."}
}

func TestAsanaConnectorRequestsAndStaging(t *testing.T) {
	reader, writer := asanaPlans(t)
	calls, secrets := 0, 0
	expectedPhase := "agent"
	runtime := connectors.Runtime{Phase: "agent", AllowedOrigins: []string{"https://app.asana.com"},
		ResolveSecret: func(_ context.Context, phase, name string) (string, error) {
			secrets++
			require.Equal(t, expectedPhase, phase)
			expected := "ASANA_READ_ACCESS_TOKEN"
			if phase == "safe-output" {
				expected = "ASANA_WRITE_ACCESS_TOKEN"
			}
			require.Equal(t, expected, name)
			return "synthetic-" + phase, nil
		},
		Client: &http.Client{Transport: asanaTransport(func(request *http.Request) (int, string) {
			calls++
			require.Equal(t, "app.asana.com", request.URL.Host)
			require.Equal(t, "Bearer synthetic-"+expectedPhase, request.Header.Get("Authorization"))
			if request.Method == http.MethodGet {
				require.Equal(t, "/api/1.0/tasks/"+asanaTask, request.URL.Path)
				fields := "resource_type,workspace.resource_type"
				if calls == 1 {
					fields = "resource_type,name,notes,completed,modified_at,workspace.resource_type"
				}
				require.Equal(t, fields, request.URL.Query().Get("opt_fields"))
				require.Len(t, request.URL.Query(), 1)
				return http.StatusOK, asanaTaskResponse
			}
			require.Equal(t, http.MethodPost, request.Method)
			require.Equal(t, "/api/1.0/tasks/"+asanaTask+"/stories", request.URL.Path)
			require.Empty(t, request.URL.RawQuery, "JSON POST field options belong beside data, not in GET-style query parameters")
			require.Equal(t, "application/json", request.Header.Get("Content-Type"))
			body, err := io.ReadAll(request.Body)
			require.NoError(t, err)
			require.JSONEq(t, `{"data":{"text":"Prepared next step."},"options":{"fields":["resource_type","resource_subtype","type","text","target.resource_type"]}}`, string(body))
			return http.StatusCreated, asanaCommentResponse
		})},
	}
	result, err := reader.Execute(t.Context(), runtime, "mcp", "get-task", map[string]any{"task": asanaTask, "workspace": asanaWorkspace})
	require.NoError(t, err)
	require.True(t, result.Untrusted)
	require.JSONEq(t, `{"task":"120000000000001","workspace":"120000000000002","name":"Review release plan","notes":"Confirm the rollout prerequisites.","completed":false,"modified-at":"2026-11-11T00:00:00Z"}`, string(result.Data))
	require.NotContains(t, string(result.Data), "unprojected-fixture")
	intent, err := writer.PrepareSafeOutput("add-comment", asanaArguments())
	require.NoError(t, err)
	require.Equal(t, 1, calls, "staging must not make a membership check or POST")
	require.Equal(t, 1, secrets, "staging must not resolve the writer credential")
	runtime.Phase, runtime.AllowWrites = "safe-output", true
	expectedPhase = "safe-output"
	result, err = writer.ExecuteSafeOutput(t.Context(), runtime, *intent)
	require.NoError(t, err)
	require.JSONEq(t, `{"task":"120000000000001","story":"120000000000005"}`, string(result.Data))
	require.Equal(t, 3, calls, "reader GET followed by writer policy GET and one POST")
	require.Equal(t, 3, secrets)
}

type asanaResponseCase struct {
	Name, Phase, Body string
	Status            int
	Policy            bool
}

func asanaResponseCases() []asanaResponseCase {
	return []asanaResponseCase{
		{"wrong-approved-task", "agent", strings.Replace(asanaTaskResponse, asanaTask, asanaOtherTask, 1), 200, false},
		{"wrong-approved-workspace", "agent", strings.Replace(asanaTaskResponse, asanaWorkspace, asanaOtherWorkspace, 1), 200, false},
		{"wrong-field-type", "agent", strings.Replace(asanaTaskResponse, `"completed":false`, `"completed":"false"`, 1), 200, false},
		{"missing-description", "agent", strings.Replace(asanaTaskResponse, `"notes":"Confirm the rollout prerequisites.",`, "", 1), 200, false},
		{"oversize-read", "agent", strings.Repeat("x", 1048577), 200, false},
		{"policy-workspace-mismatch", "safe-output", strings.Replace(asanaTaskResponse, asanaWorkspace, asanaOtherWorkspace, 1), 200, true},
		{"policy-denied", "safe-output", `{"errors":[{"message":"private-fixture"}]}`, 403, true},
		{"wrong-approved-comment-target", "safe-output", strings.Replace(asanaCommentResponse, asanaTask, asanaOtherTask, 1), 201, false},
		{"system-story", "safe-output", strings.Replace(asanaCommentResponse, `"type":"comment"`, `"type":"system"`, 1), 201, false},
		{"wrong-story-subtype", "safe-output", strings.Replace(asanaCommentResponse, "comment_added", "assigned", 1), 201, false},
		{"changed-comment-text", "safe-output", strings.Replace(asanaCommentResponse, "Prepared next step.", "Altered text.", 1), 201, false},
		{"missing-story-id", "safe-output", strings.Replace(asanaCommentResponse, `"gid":"120000000000005",`, "", 1), 201, false},
		{"oversize-write", "safe-output", strings.Repeat("x", 131073), 201, false},
		{"unauthorized", "safe-output", `{"errors":[{"message":"private-fixture"}]}`, 401, false},
		{"rate-limit", "safe-output", `{"errors":[{"message":"private-fixture"}]}`, 429, false},
		{"uncertain-server-error", "safe-output", `{"errors":[{"message":"private-fixture"}]}`, 503, false},
	}
}

func TestAsanaConnectorResponseEvidence(t *testing.T) {
	reader, writer := asanaPlans(t)
	for _, test := range asanaResponseCases() {
		t.Run(test.Name, func(t *testing.T) {
			calls := 0
			runtime := connectors.Runtime{Phase: test.Phase, AllowWrites: true, AllowedOrigins: []string{"https://app.asana.com"},
				ResolveSecret: func(context.Context, string, string) (string, error) { return "synthetic", nil },
				Client: &http.Client{Transport: asanaTransport(func(request *http.Request) (int, string) {
					calls++
					if test.Phase == "safe-output" && !test.Policy && request.Method == http.MethodGet {
						return http.StatusOK, asanaTaskResponse
					}
					return test.Status, test.Body
				})},
			}
			plan, capability, name, args := reader, "mcp", "get-task", map[string]any{"task": asanaTask, "workspace": asanaWorkspace}
			expectedCalls := 1
			if test.Phase == "safe-output" {
				plan, capability, name, args = writer, "safe-output", "add-comment", asanaArguments()
				if !test.Policy {
					expectedCalls++
				}
			}
			_, err := plan.Execute(t.Context(), runtime, capability, name, args)
			require.Error(t, err)
			require.NotContains(t, err.Error(), "private-fixture")
			require.Equal(t, expectedCalls, calls, "failed policy reads must prevent mutation; failed appends must never replay")
		})
	}
}

func TestAsanaConnectorArgumentAndEmptyDescriptionBoundaries(t *testing.T) {
	reader, writer := asanaPlans(t)
	runtime := connectors.Runtime{Phase: "agent", AllowedOrigins: []string{"https://app.asana.com"},
		ResolveSecret: func(context.Context, string, string) (string, error) {
			t.Fatal("invalid arguments accessed credentials")
			return "", nil
		},
		Client: &http.Client{Transport: asanaTransport(func(*http.Request) (int, string) { t.Fatal("invalid arguments sent HTTP"); return 0, "" })},
	}
	for _, args := range []map[string]any{
		{"task": "unapproved", "workspace": asanaWorkspace},
		{"task": asanaTask, "workspace": "unapproved"},
		{"task": asanaTask},
		{"task": 120000000000001, "workspace": asanaWorkspace},
		{"task": "../tasks", "workspace": asanaWorkspace},
		{"task": asanaTask, "workspace": asanaWorkspace, "url": "https://example.test"},
	} {
		_, err := reader.Execute(t.Context(), runtime, "mcp", "get-task", args)
		require.Error(t, err)
		args["text"] = "Prepared next step."
		_, err = writer.PrepareSafeOutput("add-comment", args)
		require.Error(t, err)
	}
	for _, text := range []string{"", " \n\t", strings.Repeat("\U0001f600", 4001)} {
		args := asanaArguments()
		args["text"] = text
		_, err := writer.PrepareSafeOutput("add-comment", args)
		require.Error(t, err)
	}
	runtime.ResolveSecret = func(context.Context, string, string) (string, error) { return "synthetic", nil }
	runtime.Client.Transport = asanaTransport(func(*http.Request) (int, string) {
		return http.StatusOK, strings.Replace(asanaTaskResponse, "Confirm the rollout prerequisites.", "", 1)
	})
	result, err := reader.Execute(t.Context(), runtime, "mcp", "get-task", map[string]any{"task": asanaTask, "workspace": asanaWorkspace})
	require.NoError(t, err, "an empty existing description is valid evidence, not a missing field")
	require.Contains(t, string(result.Data), `"notes":""`)
}

func TestAsanaConnectorBundledNode(t *testing.T) {
	reader, writer := asanaPlans(t)
	readPlan, err := reader.Encode()
	require.NoError(t, err)
	writePlan, err := writer.Encode()
	require.NoError(t, err)
	payload, err := json.Marshal(map[string]any{"reader": readPlan, "writer": writePlan, "task": asanaTaskResponse, "story": asanaCommentResponse, "cases": asanaResponseCases()})
	require.NoError(t, err)
	runtime, err := filepath.Abs("../../actions/setup/js/connector_runtime.cjs")
	require.NoError(t, err)
	command := exec.CommandContext(t.Context(), "node", "--unhandled-rejections=strict", "-e", `
const assert = require("node:assert/strict");
const {join, dirname} = require("node:path");
const {executeConnector, decodePlan} = require(process.argv[1]);
const {formatConnectorArguments} = require(join(dirname(process.argv[1]), "connector_content.cjs"));
const input = JSON.parse(require("node:fs").readFileSync(0, "utf8"));
const task = JSON.parse(input.task), story = JSON.parse(input.story);
const args = {task:"120000000000001", workspace:"120000000000002", text:"Prepared next step."};
const env = {GH_AW_CONNECTOR_SECRET_ASANA_READ_ACCESS_TOKEN:"synthetic-reader", GH_AW_CONNECTOR_SECRET_ASANA_WRITE_ACCESS_TOKEN:"synthetic-writer"};
let forbiddenSecrets = 0, forbiddenCalls = 0;
const secretFree = new Proxy({}, {get() { forbiddenSecrets++; throw Error("staging read a credential"); }});
const response = (body,status=200) => new Response(typeof body === "string" ? body : JSON.stringify(body), {status, headers:{"content-type":"application/json"}});
async function main() {
  const read = await executeConnector(input.reader,"mcp","get-task",{task:args.task,workspace:args.workspace},
    {env,fetchImpl:async(url,init) => {
      assert.equal(new URL(url).pathname,"/api/1.0/tasks/"+args.task);
      assert.equal(new URL(url).searchParams.get("opt_fields"),"resource_type,name,notes,completed,modified_at,workspace.resource_type");
      assert.equal(init.headers.Authorization,"Bearer synthetic-reader");
      return response(task);
    }});
  assert.equal(read.data.notes,task.data.notes);
  assert.equal(read.data.completed,false);
  assert.ok(!JSON.stringify(read.data).includes("unprojected-fixture"));
  for (const text of ["Observed x < y && y > z.", "😀".repeat(4000), '\\"'.repeat(2000)]) {
    const formatted = formatConnectorArguments(decodePlan(input.writer),"add-comment",{...args,text},{config:{footer:false}});
    assert.ok(formatted.text.includes("gh-aw-agentic-workflow:"));
    assert.ok(!formatted.text.includes("<!--"));
    assert.ok(Array.from(formatted.text).length <= 4000);
    if (text.startsWith("Observed")) assert.ok(formatted.text.includes(text));
    const staged = await executeConnector(input.writer,"safe-output","add-comment",formatted,
      {staged:true,env:secretFree,fetchImpl:async() => { forbiddenCalls++; throw Error("staging sent HTTP"); }});
    assert.deepEqual(JSON.parse(JSON.stringify(staged.request.arguments)),formatted);
    let calls = 0;
    const result = await executeConnector(input.writer,"safe-output","add-comment",formatted,
      {env,fetchImpl:async(url,init) => {
        calls++;
        const parsed = new URL(url);
        assert.equal(parsed.origin,"https://app.asana.com");
        assert.equal(init.headers.Authorization,"Bearer synthetic-writer");
        if (init.method === "GET") {
          assert.equal(parsed.pathname,"/api/1.0/tasks/"+args.task);
          assert.equal(parsed.searchParams.get("opt_fields"),"resource_type,workspace.resource_type");
          return response(task);
        }
        assert.equal(init.method,"POST");
        assert.equal(parsed.pathname,"/api/1.0/tasks/"+args.task+"/stories");
        assert.equal(parsed.search,"");
        const body = JSON.parse(init.body);
        assert.deepEqual(body,{data:{text:formatted.text},options:{fields:["resource_type","resource_subtype","type","text","target.resource_type"]}});
        return response(JSON.stringify({data:{...story.data,text:formatted.text}}).replaceAll("😀","\\ud83d\\ude00"),201);
      }});
    assert.equal(calls,2);
    assert.deepEqual(JSON.parse(JSON.stringify(result.data)),{task:args.task,story:story.data.gid});
  }
  for (const invalid of [{...args,task:"unapproved"},{...args,workspace:"unapproved"},{...args,task:120000000000001},
    {...args,task:"../tasks"},{...args,url:"https://example.test"},{...args,text:""},{...args,text:" \n\t"},{...args,text:"😀".repeat(4001)}]) {
    for (const staged of [false,true]) {
      await assert.rejects(() => executeConnector(input.writer,"safe-output","add-comment",invalid,
        {staged,env:secretFree,fetchImpl:async() => { forbiddenCalls++; throw Error("invalid arguments sent HTTP"); }}));
    }
  }
  assert.equal(forbiddenSecrets,0);
  assert.equal(forbiddenCalls,0);
  for (const test of input.cases) {
    let calls = 0;
    const options = {env,fetchImpl:async(_url,init) => {
      calls++;
      if (test.Phase === "safe-output" && !test.Policy && init.method === "GET") return response(task);
      return response(test.Body,test.Status);
    }};
    const reading = test.Phase === "agent";
    await assert.rejects(() => executeConnector(input[reading ? "reader":"writer"],reading ? "mcp":"safe-output",reading ? "get-task":"add-comment",
      reading ? {task:args.task,workspace:args.workspace}:args,options),error => !error.message.includes("private-fixture"));
    assert.equal(calls,reading || test.Policy ? 1:2,test.Name);
  }
}
main().catch(error => { console.error(error); process.exitCode = 1; });
`, runtime)
	command.Env = []string{}
	command.Stdin = strings.NewReader(string(payload))
	output, err := command.CombinedOutput()
	require.NoError(t, err, "%s", output)
	require.Empty(t, string(output))
}
