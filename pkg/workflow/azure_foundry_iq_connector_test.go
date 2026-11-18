//go:build !integration

package workflow

import (
	"context"
	"encoding/json"
	"encoding/xml"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/github/gh-aw/pkg/connectors"
	"github.com/github/gh-aw/pkg/parser"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const foundryIQQuestion = "What deployment rollback checks are documented?"
const foundryIQEndpoint = "https://example-search.search.windows.net/knowledgebases/operations-kb/mcp?api-version=2026-11-01-preview"

func foundryIQPlan(t *testing.T) *connectors.Plan {
	t.Helper()
	compiler, path := prepareConnectorScenario(t, azureFoundryIQCompilerScenario, false, nil)
	data, err := compiler.ParseWorkflowFile(path)
	require.NoError(t, err)
	require.Nil(t, data.Connectors["azure-foundry-iq"].SafeOutputs)
	return data.Connectors["azure-foundry-iq"].Agent
}

func TestFoundryIQDocumentationConsumer(t *testing.T) {
	source, err := os.ReadFile("../../.github/workflows/connectors/azure-foundry-iq.md")
	require.NoError(t, err)
	comment := commentedConnectorDocumentation(t, string(source))
	var documentation struct {
		ID       string `xml:"id,attr"`
		Consumer struct {
			Frontmatter string `xml:"frontmatter"`
			Body        string `xml:"body"`
		} `xml:"consumer"`
	}
	require.NoError(t, xml.Unmarshal([]byte(comment), &documentation))
	require.Equal(t, "azure-foundry-iq", documentation.ID)
	golden, err := os.ReadFile("../cli/workflows/azure-foundry-iq-golden.md")
	require.NoError(t, err)
	embedded := "---\n" + strings.TrimSpace(documentation.Consumer.Frontmatter) + "\n---\n\n" + strings.TrimSpace(documentation.Consumer.Body) + "\n"
	require.Equal(t, string(golden), embedded, "the extracted XML consumer must be the golden compiled in local and pinned import tests")
	body, err := parser.ExtractMarkdownContent(parser.RemoveConnectorDocumentation(string(source)))
	require.NoError(t, err)
	require.Empty(t, strings.TrimSpace(body))
}

func TestFoundryIQInvalidCallsBeforeCredentials(t *testing.T) {
	plan := foundryIQPlan(t)
	runtime := connectors.Runtime{
		Phase: "agent", AllowedOrigins: plan.Origins(),
		ResolveSecret: func(context.Context, string, string) (string, error) {
			t.Fatal("invalid arguments must not resolve credentials")
			return "", errors.New("forbidden")
		},
		Client: &http.Client{Transport: connectorSmokeTransport(func(*http.Request) (*http.Response, error) {
			t.Fatal("invalid arguments must not make HTTP requests")
			return nil, errors.New("forbidden")
		})},
	}
	for _, args := range []map[string]any{
		{}, {"queries": nil}, {"queries": foundryIQQuestion}, {"queries": []any{}},
		{"queries": []any{foundryIQQuestion, foundryIQQuestion}},
		{"queries": []any{1}}, {"queries": []any{[]any{foundryIQQuestion}}},
		{"queries": []any{"An unapproved question"}},
		{"queries": []any{strings.Repeat("x", 2049)}},
		{"queries": []any{foundryIQQuestion}, "knowledgeBase": "other-kb"},
	} {
		_, err := plan.CallMCP(t.Context(), runtime, "knowledge-base", "knowledge_base_retrieve", args)
		require.Error(t, err)
	}
	for _, tool := range []string{"*", "retrieve", "create_knowledge_base", "custom-retrieve"} {
		_, err := plan.CallMCP(t.Context(), runtime, "knowledge-base", tool, map[string]any{"queries": []string{foundryIQQuestion}})
		require.Error(t, err)
	}
	runtime.AllowedOrigins = []string{"https://other.search.windows.net"}
	_, err := plan.CallMCP(t.Context(), runtime, "knowledge-base", "knowledge_base_retrieve", map[string]any{"queries": []string{foundryIQQuestion}})
	require.Error(t, err, "importing an endpoint is not network approval")
}

func TestFoundryIQHostedMCP(t *testing.T) {
	plan := foundryIQPlan(t)
	for _, test := range []struct {
		name        string
		size        int
		status      int
		toolError   bool
		wantSuccess bool
	}{
		{"grounding", 100, 200, false, true},
		{"oversize", 262145, 200, false, false},
		{"tool-error", 100, 200, true, false},
		{"authorization-denial", 100, 403, false, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			var calls, failures atomic.Int32
			upstream := mcp.NewServer(&mcp.Implementation{Name: "azure-foundry-iq-offline", Version: "1"}, nil)
			upstream.AddTool(&mcp.Tool{Name: "knowledge_base_retrieve", InputSchema: map[string]any{
				"type": "object", "properties": map[string]any{"queries": map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "minItems": 1, "maxItems": 1}},
			}}, func(_ context.Context, request *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
				calls.Add(1)
				var args map[string]any
				if err := json.Unmarshal(request.Params.Arguments, &args); err != nil {
					return nil, err
				}
				assert.Equal(t, []any{foundryIQQuestion}, args["queries"])
				return &mcp.CallToolResult{IsError: test.toolError, Content: []mcp.Content{&mcp.TextContent{Text: strings.Repeat("x", test.size)}}}, nil
			})
			handler := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return upstream }, nil)
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, "/knowledgebases/operations-kb/mcp", r.URL.Path)
				assert.Equal(t, "api-version=2026-11-01-preview", r.URL.RawQuery)
				assert.Equal(t, "Bearer fixture-search-token", r.Header.Get("Authorization"))
				if test.status != 200 {
					var message struct{ Method string }
					assert.NoError(t, json.NewDecoder(r.Body).Decode(&message))
					assert.Contains(t, []string{"server/discover", "initialize"}, message.Method)
					if message.Method == "initialize" {
						failures.Add(1)
					}
					w.WriteHeader(test.status)
					return
				}
				handler.ServeHTTP(w, r)
			}))
			defer server.Close()
			destination, err := url.Parse(server.URL)
			require.NoError(t, err)
			client := server.Client()
			transport := client.Transport
			client.Transport = connectorSmokeTransport(func(request *http.Request) (*http.Response, error) {
				assert.Equal(t, foundryIQEndpoint, request.URL.String())
				local := request.Clone(request.Context())
				local.URL.Scheme, local.URL.Host = destination.Scheme, destination.Host
				local.Host = destination.Host
				return transport.RoundTrip(local)
			})
			runtime := connectors.Runtime{
				Phase: "agent", AllowedOrigins: plan.Origins(), Client: client,
				ResolveSecret: func(_ context.Context, phase, name string) (string, error) {
					assert.Equal(t, "agent", phase)
					assert.Equal(t, "FOUNDRY_IQ_SEARCH_ACCESS_TOKEN", name)
					return "fixture-search-token", nil
				},
			}
			result, err := plan.CallMCP(t.Context(), runtime, "knowledge-base", "knowledge_base_retrieve", map[string]any{"queries": []string{foundryIQQuestion}})
			if test.wantSuccess {
				require.NoError(t, err)
				require.Len(t, result.Content, 1)
			} else {
				require.Error(t, err)
			}
			if test.status == 403 {
				require.EqualValues(t, 1, failures.Load(), "authorization denial must not retry")
				require.Zero(t, calls.Load())
			} else {
				require.EqualValues(t, 1, calls.Load())
			}
		})
	}
}

func TestFoundryIQBundledNodeRuntime(t *testing.T) {
	plan := foundryIQPlan(t)
	encoded, err := plan.Encode()
	require.NoError(t, err)
	node, err := exec.LookPath("node")
	require.NoError(t, err)
	runtime, err := filepath.Abs("../../actions/setup/js/connector_runtime.cjs")
	require.NoError(t, err)
	input, err := json.Marshal(map[string]any{"plan": encoded, "question": foundryIQQuestion, "endpoint": foundryIQEndpoint})
	require.NoError(t, err)
	driver := `
const assert = require("node:assert/strict");
const {callMCP, decodePlan, serve} = require(process.argv[1]);
const {Readable, Writable} = require("node:stream");
const input = JSON.parse(require("node:fs").readFileSync(0, "utf8"));
const noSecrets = new Proxy({}, {get() { throw new Error("credential access forbidden"); }});
let network = 0;
const noNetwork = async () => { network++; throw new Error("network access forbidden"); };
(async () => {
  for (const args of [
    {}, {queries:null}, {queries:input.question}, {queries:[]}, {queries:[input.question,input.question]},
    {queries:[1]}, {queries:[[input.question]]}, {queries:["Unapproved"]}, {queries:["x".repeat(2049)]},
    {queries:[input.question],knowledgeBase:"other-kb"}
  ]) await assert.rejects(callMCP(input.plan, "knowledge-base", "knowledge_base_retrieve", args,
    {env:noSecrets, fetchImpl:noNetwork}), error => !/access forbidden/.test(error.message));
  assert.equal(network, 0);
  const messages = [];
  const wire = JSON.stringify({jsonrpc:"2.0",id:1,method:"tools/list",params:{}}) + "\n";
  await serve(input.plan, Readable.from([wire]), new Writable({write(chunk, _, done) {messages.push(String(chunk));done();}}));
  const tools = JSON.parse(messages.join("").trim()).result.tools;
  assert.equal(tools.length, 1);
  assert.equal(tools[0].name, "knowledge-base__knowledge_base_retrieve");
  assert.deepEqual(tools[0].inputSchema.properties.queries, {
    type:"array", minItems:1, maxItems:1, items:{type:"string", minLength:1, maxLength:2048}
  });
  for (const mode of ["success","oversize","tool-error","denied"]) {
    let calls = 0, requests = 0;
    const fetchImpl = async (url, options) => {
      requests++;
      assert.equal(url, input.endpoint);
      assert.equal(options.headers.Authorization, "Bearer fixture-search-token");
      assert.equal(options.redirect, "manual");
      if (mode === "denied") return new Response("denied", {status:403});
      const request = JSON.parse(options.body);
      let result;
      if (request.method === "initialize") result = {
        protocolVersion:"2025-03-26",capabilities:{tools:{}},serverInfo:{name:"offline",version:"1"}
      };
      else if (request.method === "notifications/initialized") return new Response(null, {status:202});
      else {
        calls++;
        assert.equal(request.method, "tools/call");
        assert.deepEqual(request.params, {name:"knowledge_base_retrieve",arguments:{queries:[input.question]}});
        result = {content:[{type:"text",text:"x".repeat(mode === "oversize" ? 262145 : 100)}],isError:mode === "tool-error"};
      }
      return new Response(JSON.stringify({jsonrpc:"2.0",id:request.id,result}), {headers:{"content-type":"application/json"}});
    };
    const options = {env:{GH_AW_CONNECTOR_SECRET_FOUNDRY_IQ_SEARCH_ACCESS_TOKEN:"fixture-search-token"},fetchImpl};
    const call = callMCP(input.plan, "knowledge-base", "knowledge_base_retrieve", {queries:[input.question]}, options);
    if (mode === "success") assert.equal((await call).content[0].text.length,100);
    else await assert.rejects(call);
    assert.equal(calls, mode === "denied" ? 0 : 1);
    if (mode === "denied") assert.equal(requests,1);
  }
  assert.equal(decodePlan(input.plan).spec.safeOutputs, undefined);
})().catch(error => {console.error(error);process.exit(1);});
`
	command := exec.CommandContext(t.Context(), node, "-e", driver, runtime)
	command.Env = []string{}
	command.Stdin = strings.NewReader(string(input))
	output, err := command.CombinedOutput()
	require.NoError(t, err, "%s", output)
}
