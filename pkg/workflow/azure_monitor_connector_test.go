//go:build !integration

package workflow

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/connectors"
	"github.com/stretchr/testify/require"
)

const monitorWorkspace = "11111111-2222-4333-8444-555555555555"
const monitorResponse = `{"tables":[{"name":"PrimaryResult","columns":[{"name":"EventCount","type":"long"}],"rows":[[42]]}]}`

func azureMonitorPlan(t *testing.T) *connectors.Plan {
	t.Helper()
	compiler, path := prepareConnectorScenario(t, connectorCompilerScenario{provider: "azure-monitor"}, false, nil)
	data, err := compiler.ParseWorkflowFile(path)
	require.NoError(t, err)
	encoded, err := data.Connectors["azure-monitor"].Agent.Encode()
	require.NoError(t, err)
	plan, err := connectors.DecodePlan(encoded)
	require.NoError(t, err)
	return plan
}

// Exercise the published federated reader with the environment produced by
// the compiler. Substitute a local socket for the gateway's read-only mount.
func TestAzureMonitorOIDC(t *testing.T) {
	compiler, path := prepareConnectorScenario(t, connectorCompilerScenario{provider: "azure-monitor"}, false, func(consumer, _ map[string]any) {
		consumer["tools"].(map[string]any)["azure-monitor"] = []any{"get-activity-count-federated"}
	})
	data, err := compiler.ParseWorkflowFile(path)
	require.NoError(t, err)
	config := data.Connectors["azure-monitor"]
	env, err := connectorEnv(config.Agent, config)
	require.NoError(t, err)
	socketKey := connectorAzureSocketPrefix + connectorAppSecret(config.Alias, "federated-reader")
	require.Contains(t, env, socketKey)
	require.Len(t, env, 2, "federation must not provision the stored-token reader")
	payload, err := json.Marshal(map[string]any{"env": env, "key": socketKey, "workspace": monitorWorkspace, "response": monitorResponse})
	require.NoError(t, err)
	runtime, err := filepath.Abs("../../actions/setup/js/connector_runtime.cjs")
	require.NoError(t, err)
	command := exec.CommandContext(t.Context(), "node", "--unhandled-rejections=strict", "-e", `
const assert = require("node:assert/strict");
const {once} = require("node:events");
const fs = require("node:fs");
const net = require("node:net");
const {join} = require("node:path");
const {executeConnector} = require(process.argv[1]);
const {env, key, workspace, response} = JSON.parse(fs.readFileSync(0, "utf8"));
(async () => {
  const dir = fs.mkdtempSync(join(require("node:os").tmpdir(), "monitor-"));
  env[key] = join(dir, "token.sock");
  let requests = 0, connections = 0;
  const server = net.createServer(s => { connections++; s.end("synthetic-access"); });
  try {
    server.listen(env[key]);
    await once(server, "listening");
    const read = id => executeConnector(env.GH_AW_CONNECTOR_PLAN, "mcp", "get-activity-count-federated",
      {"workspace-id": id}, {env, fetchImpl: async (target, init) => {
        requests++;
        assert.equal(new URL(target).origin, "https://api.loganalytics.azure.com");
        assert.equal(new Headers(init.headers).get("authorization"), ["Bearer", "synthetic-access"].join(" "));
        return new Response(response);
      }});
    await assert.rejects(read("aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee"));
    assert.equal(connections, 0);
    const result = await read(workspace);
    assert.deepEqual({...result.data}, {"event-count": 42});
    assert.equal(requests, 1);
    assert.equal(connections, 1);
  } finally {
    server.close();
    fs.rmSync(dir, {recursive:true, force:true});
  }
})();`, runtime)
	command.Env = []string{"PATH=" + os.Getenv("PATH"), "TMPDIR=" + os.TempDir()}
	command.Stdin = strings.NewReader(string(payload))
	output, err := command.CombinedOutput()
	require.NoError(t, err, "%s", output)
}

func azureMonitorInvalidArguments() []map[string]any {
	return []map[string]any{
		{},
		{"workspace-id": 123},
		{"workspace-id": "../" + monitorWorkspace},
		{"workspace-id": "aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee"},
		{"workspace-id": monitorWorkspace, "query": "search *"},
		{"workspace-id": monitorWorkspace, "timespan": "P30D"},
		{"workspace-id": monitorWorkspace, "workspaces": []string{monitorWorkspace}},
		{"workspace-id": monitorWorkspace, "url": "https://untrusted.invalid"},
	}
}

func azureMonitorInvalidResponses() map[string]string {
	return map[string]string{
		"partial":      strings.TrimSuffix(monitorResponse, "}") + `,"error":{"code":"PartialError"}}`,
		"null-error":   strings.TrimSuffix(monitorResponse, "}") + `,"error":null}`,
		"missing":      `{}`,
		"malformed":    `{`,
		"wrong-table":  strings.Replace(monitorResponse, "PrimaryResult", "OtherResult", 1),
		"wrong-column": strings.Replace(monitorResponse, "EventCount", "SecretText", 1),
		"wrong-type":   strings.Replace(monitorResponse, "long", "string", 1),
		"text-count":   strings.Replace(monitorResponse, "[42]", `["42"]`, 1),
		"null-count":   strings.Replace(monitorResponse, "[42]", "[null]", 1),
		"fraction":     strings.Replace(monitorResponse, "[42]", "[0.5]", 1),
		"empty":        strings.Replace(monitorResponse, "[[42]]", "[]", 1),
		"extra-row":    strings.Replace(monitorResponse, "[[42]]", "[[42],[43]]", 1),
		"extra-cell":   strings.Replace(monitorResponse, "[42]", "[42,43]", 1),
		"extra-table":  strings.TrimSuffix(monitorResponse, "]}") + `,{}]}`,
		"oversize":     strings.TrimSuffix(monitorResponse, "}") + `,"private":"` + strings.Repeat("x", 16384) + `"}`,
	}
}

func TestAzureMonitorConnectorArgumentsBeforeCredentials(t *testing.T) {
	plan := azureMonitorPlan(t)
	runtime := connectors.Runtime{Phase: "agent", AllowedOrigins: plan.Origins(),
		ResolveSecret: func(context.Context, string, string) (string, error) {
			t.Fatal("invalid call resolved credentials")
			return "", nil
		},
		Client: &http.Client{Transport: connectorSmokeTransport(func(*http.Request) (*http.Response, error) {
			t.Fatal("invalid call accessed HTTP")
			return nil, nil
		})},
	}
	for _, args := range azureMonitorInvalidArguments() {
		_, err := plan.Execute(t.Context(), runtime, "mcp", "get-activity-count", args)
		require.Error(t, err, "%v", args)
	}
	args := map[string]any{"workspace-id": monitorWorkspace}
	_, err := plan.Execute(t.Context(), runtime, "mcp", "query", args)
	require.Error(t, err)
	runtime.Phase = "safe-output"
	_, err = plan.Execute(t.Context(), runtime, "mcp", "get-activity-count", args)
	require.Error(t, err)
	runtime.Phase, runtime.AllowedOrigins = "agent", []string{"https://untrusted.invalid"}
	_, err = plan.Execute(t.Context(), runtime, "mcp", "get-activity-count", args)
	require.Error(t, err)
}

func TestAzureMonitorConnectorResponseEvidence(t *testing.T) {
	plan := azureMonitorPlan(t)
	cases := azureMonitorInvalidResponses()
	cases["valid"], cases["zero"], cases["private-unprojected"] = monitorResponse,
		strings.Replace(monitorResponse, "[42]", "[0]", 1),
		strings.TrimSuffix(monitorResponse, "}")+`,"private":"sensitive fixture"}`
	statuses := map[string]int{"redirect": 302, "unauthenticated": 401, "denied": 403, "rate-limit": 429, "server-error": 500}
	for name := range statuses {
		cases[name] = monitorResponse
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			secrets, requests := 0, 0
			runtime := connectors.Runtime{Phase: "agent", AllowedOrigins: plan.Origins(),
				ResolveSecret: func(_ context.Context, phase, secret string) (string, error) {
					secrets++
					require.Equal(t, "agent", phase)
					require.Equal(t, "AZURE_MONITOR_READ_ACCESS_TOKEN", secret)
					return "synthetic-token", nil
				},
				Client: &http.Client{Transport: connectorSmokeTransport(func(request *http.Request) (*http.Response, error) {
					requests++
					require.Equal(t, http.MethodGet, request.Method)
					require.Equal(t, "api.loganalytics.azure.com", request.URL.Host)
					require.Equal(t, "/v1/workspaces/"+monitorWorkspace+"/query", request.URL.Path)
					require.Equal(t, "Bearer synthetic-token", request.Header.Get("Authorization"))
					require.Equal(t, []string{"AzureActivity | summarize EventCount=count()"}, request.URL.Query()["query"])
					require.Equal(t, []string{"PT1H"}, request.URL.Query()["timespan"])
					require.Len(t, request.URL.Query(), 2)
					require.True(t, request.Body == nil || request.Body == http.NoBody)
					status := http.StatusOK
					if failureStatus, ok := statuses[name]; ok {
						status = failureStatus
					}
					return &http.Response{StatusCode: status, Request: request, Body: io.NopCloser(strings.NewReader(body)),
						Header: http.Header{"Location": {"https://untrusted.invalid"}, "Retry-After": {"1"}}}, nil
				})},
			}
			result, err := plan.Execute(t.Context(), runtime, "mcp", "get-activity-count", map[string]any{"workspace-id": monitorWorkspace})
			if name == "valid" || name == "zero" || name == "private-unprojected" {
				require.NoError(t, err)
				count := "42"
				if name == "zero" {
					count = "0"
				}
				require.JSONEq(t, `{"event-count":`+count+`}`, string(result.Data))
				require.True(t, result.Untrusted)
			} else {
				require.Error(t, err)
			}
			require.Equal(t, 1, secrets)
			require.Equal(t, 1, requests)
		})
	}
}

func TestAzureMonitorConnectorBundledNodeParity(t *testing.T) {
	plan := azureMonitorPlan(t)
	encoded, err := plan.Encode()
	require.NoError(t, err)
	payload, err := json.Marshal(map[string]any{
		"plan": encoded, "workspace": monitorWorkspace, "valid": monitorResponse,
		"invalidArgs": azureMonitorInvalidArguments(), "invalidResponses": azureMonitorInvalidResponses(),
	})
	require.NoError(t, err)
	runtime, err := filepath.Abs("../../actions/setup/js/connector_runtime.cjs")
	require.NoError(t, err)
	command := exec.CommandContext(t.Context(), "node", "--unhandled-rejections=strict", "-e", azureMonitorNodeScript, runtime)
	command.Env = []string{"PATH=" + os.Getenv("PATH")}
	command.Stdin = strings.NewReader(string(payload))
	output, err := command.CombinedOutput()
	require.NoError(t, err, "%s", output)
	require.Empty(t, string(output))
}

const azureMonitorNodeScript = `
const assert = require("node:assert/strict");
const {executeConnector} = require(process.argv[1]);
const fixture = JSON.parse(require("node:fs").readFileSync(0, "utf8"));
const args = {"workspace-id":fixture.workspace};
const env = {GH_AW_CONNECTOR_SECRET_AZURE_MONITOR_READ_ACCESS_TOKEN:"synthetic-token"};
(async () => {
  let forbiddenSecrets = 0, forbiddenHTTP = 0;
  const noSecrets = new Proxy({}, {get() {forbiddenSecrets++; throw Error("unexpected secret access");}});
  const noHTTP = async () => {forbiddenHTTP++; throw Error("unexpected HTTP");};
  for (const invalid of fixture.invalidArgs)
    await assert.rejects(executeConnector(fixture.plan,"mcp","get-activity-count",invalid,{env:noSecrets,fetchImpl:noHTTP}));
  await assert.rejects(executeConnector(fixture.plan,"mcp","query",args,{env:noSecrets,fetchImpl:noHTTP}));
  await assert.rejects(executeConnector(fixture.plan,"safe-output","get-activity-count",args,{env:noSecrets,fetchImpl:noHTTP}));
  assert.equal(forbiddenSecrets,0);
  assert.equal(forbiddenHTTP,0);
  async function execute(body,status=200) {
    let calls = 0;
    try {
      return await executeConnector(fixture.plan,"mcp","get-activity-count",args,{env,fetchImpl:async (target,init) => {
        calls++;
        const url = new URL(target);
        assert.equal(url.origin,"https://api.loganalytics.azure.com");
        assert.equal(url.pathname,"/v1/workspaces/"+fixture.workspace+"/query");
        assert.equal(url.searchParams.get("query"),"AzureActivity | summarize EventCount=count()");
        assert.equal(url.searchParams.get("timespan"),"PT1H");
        assert.equal(url.searchParams.size,2);
        assert.equal(init.method,"GET");
        assert.equal(init.redirect,"manual");
        assert.equal(init.credentials,"omit");
        assert.equal(new Headers(init.headers).get("Authorization"),"Bearer synthetic-token");
        assert.equal(init.body,undefined);
        return new Response(body,{status,headers:{Location:"https://untrusted.invalid", "Retry-After":"1"}});
      }});
    } finally { assert.equal(calls,1); }
  }
  for (const count of [0,42]) {
    const body = JSON.parse(fixture.valid);
    body.tables[0].rows[0][0] = count;
    body.private = "sensitive fixture";
    const result = await execute(JSON.stringify(body));
    assert.deepEqual({...result.data},{"event-count":count});
    assert.equal(result.untrusted,true);
  }
  const rejectsWithoutAssertionFailure = error => !(error instanceof assert.AssertionError);
  for (const [name,body] of Object.entries(fixture.invalidResponses))
    await assert.rejects(execute(body),rejectsWithoutAssertionFailure,name);
  for (const status of [302,401,403,429,500])
    await assert.rejects(execute(fixture.valid,status),rejectsWithoutAssertionFailure);
})();
`
