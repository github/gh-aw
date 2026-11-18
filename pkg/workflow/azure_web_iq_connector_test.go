//go:build !integration

package workflow

import (
	"context"
	"encoding/json"
	"encoding/xml"
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

const webIQQuery = "Microsoft Web IQ availability"
const webIQURL = "https://learn.microsoft.com/en-us/web-iq/overview"

func webIQPlan(t *testing.T, methods ...string) *connectors.Plan {
	t.Helper()
	compiler, path := prepareConnectorScenario(t, connectorCompilerScenario{provider: "azure-web-iq"}, false, func(consumer, _ map[string]any) {
		consumer["tools"].(map[string]any)["azure-web-iq"] = map[string]any{"search": methods}
		entry := consumer["imports"].([]any)[0].(map[string]any)
		// Invalid URLs are granted too so missing grants cannot mask unsafe patterns.
		urls := append(webIQValidBrowseURLs(), webIQInvalidBrowseURLs()...)
		entry["with"].(map[string]any)["urls"] = append(urls, webIQPrivateBrowseURLs()...)
	})
	data, err := compiler.ParseWorkflowFile(path)
	require.NoError(t, err)
	encoded, err := data.Connectors["azure-web-iq"].Agent.Encode()
	require.NoError(t, err)
	plan, err := connectors.DecodePlan(encoded)
	require.NoError(t, err, "exercise the frozen plan rather than mutable source")
	return plan
}

func webIQArguments(tool string) map[string]any {
	args := map[string]any{"query": webIQQuery, "contentFormat": "passage", "maxLength": 1500}
	if tool == "browse" {
		args["url"], args["liveCrawl"] = webIQURL, "none"
	} else {
		args["maxResults"] = 5
		if tool == "web" {
			args["safeSearch"] = "strict"
		}
	}
	return args
}

type webIQArgumentCase struct {
	Tool          string         `json:"tool"`
	Args          map[string]any `json:"args"`
	WantError     string         `json:"wantError,omitempty"`
	WantNodeError string         `json:"wantNodeError,omitempty"`
}

func webIQValidBrowseURLs() []string {
	return []string{
		webIQURL,
		"https://example.com",
		"https://example.com:8443/path",
		"https://[2001:db8::1]:8443/path",
		"https://example.com/@public",
		"https://example.com/?contact=public@example.com",
		"https://example.com/path%23section?query=public%23value",
		"https://example.com/?query=public&lang=en",
	}
}

func webIQInvalidBrowseURLs() []string {
	return []string{
		"http://example.com/",
		"https://",
		"https:///path",
		"https://?query=public",
		"https://#fragment",
		"https://:443/path",
		"https://user@example.com/",
		"https://user:password@example.com/",
		"https://@example.com/",
		"https://user%3Apassword@example.com/",
		"https://user:password@[2001:db8::1]/",
		"https:///user:password@example.com/",
		"https://example.com\\path",
		"https://exa\nmple.com/",
		"https://example.com/\n",
		"https://example.com/#@public",
		"https://example.com#fragment",
		"https://example.com/#",
		"https://example.com/#access_token=synthetic-secret",
		"https://example.com/?query=public#token=synthetic-secret",
	}
}

func webIQPrivateBrowseURLs() []string {
	return []string{
		"https://127.0.0.1/",
		"https://10.0.0.1/",
		"https://172.16.0.1/",
		"https://192.168.0.1/",
		"https://169.254.169.254/",
		"https://[::1]/",
		"https://[fe80::1]/",
		"https://[fd00::1]/",
		"https://[::ffff:127.0.0.1]/",
		"https://localhost/",
	}
}

func webIQValidArguments() []webIQArgumentCase {
	cases := []webIQArgumentCase{{Tool: "web", Args: webIQArguments("web")}, {Tool: "news", Args: webIQArguments("news")}}
	for _, url := range webIQValidBrowseURLs() {
		args := webIQArguments("browse")
		args["url"] = url
		cases = append(cases, webIQArgumentCase{Tool: "browse", Args: args})
	}
	return cases
}

func webIQInvalidArguments() []webIQArgumentCase {
	type change struct {
		value                    any
		wantError, wantNodeError string
	}
	var cases []webIQArgumentCase
	for _, tool := range []string{"web", "news", "browse"} {
		changes := map[string]change{
			"query":         {"unapproved query", "outside approved resources", ""},
			"contentFormat": {"html", "declared enum", ""},
			"maxLength":     {3001, "declared bounds", ""},
			"APPID":         {"unapproved-app", "unknown connector argument", ""},
			"scopeName":     {"unapproved-scope", "unknown connector argument", ""},
		}
		if tool == "browse" {
			changes["url"] = change{"https://example.com/unapproved", "outside approved resources", ""}
			changes["liveCrawl"] = change{"fallback", "bounded UTF-8 string", "bounded Unicode string"}
		} else {
			changes["maxResults"] = change{11, "declared bounds", ""}
		}
		if tool == "web" {
			changes["safeSearch"] = change{"off", "declared enum", ""}
		}
		if tool == "news" {
			changes["freshness"] = change{"forever", "bounded UTF-8 string", "bounded Unicode string"}
		}
		for key, change := range changes {
			args := webIQArguments(tool)
			args[key] = change.value
			cases = append(cases, webIQArgumentCase{Tool: tool, Args: args, WantError: change.wantError, WantNodeError: change.wantNodeError})
		}
		for key := range webIQArguments(tool) {
			args := webIQArguments(tool)
			delete(args, key)
			cases = append(cases, webIQArgumentCase{Tool: tool, Args: args, WantError: "required connector argument"})
		}
	}
	for _, url := range webIQInvalidBrowseURLs() {
		args := webIQArguments("browse")
		args["url"] = url
		test := webIQArgumentCase{Tool: "browse", Args: args, WantError: "declared pattern"}
		if url == "https://" {
			test.WantError, test.WantNodeError = "bounded UTF-8 string", "bounded Unicode string"
		}
		cases = append(cases, test)
	}
	// These exact grants cannot enable live fetching, regardless of destination.
	// This is an indexed-only mode check, not public-address validation.
	for _, url := range webIQPrivateBrowseURLs() {
		for _, mode := range []string{"fallback", ""} {
			args := webIQArguments("browse")
			args["url"] = url
			test := webIQArgumentCase{Tool: "browse", Args: args, WantError: "bounded UTF-8 string", WantNodeError: "bounded Unicode string"}
			if mode == "" {
				delete(args, "liveCrawl")
				test.WantError, test.WantNodeError = "required connector argument", ""
			} else {
				args["liveCrawl"] = mode
			}
			cases = append(cases, test)
		}
	}
	cases = append(cases, webIQArgumentCase{Tool: "images", Args: map[string]any{}, WantError: "exact allowlist"})
	return cases
}

func TestAzureWebIQConnectorArgumentsBeforeCredentials(t *testing.T) {
	plan := webIQPlan(t, "web", "news", "browse")
	runtime := connectors.Runtime{Phase: "agent", AllowedOrigins: plan.Origins(),
		ResolveSecret: func(context.Context, string, string) (string, error) {
			t.Fatal("invalid call accessed credentials")
			return "", nil
		},
		Client: &http.Client{Transport: connectorSmokeTransport(func(*http.Request) (*http.Response, error) {
			t.Fatal("invalid call accessed HTTP")
			return nil, nil
		})},
	}
	for _, test := range webIQInvalidArguments() {
		_, err := plan.CallMCP(t.Context(), runtime, "search", test.Tool, test.Args)
		require.ErrorContains(t, err, test.WantError, "%s: %v", test.Tool, test.Args)
	}
	runtime.Phase = "safe-output"
	_, err := plan.CallMCP(t.Context(), runtime, "search", "web", webIQArguments("web"))
	require.ErrorContains(t, err, "runtime phase is not agent")
	runtime.Phase, runtime.AllowedOrigins = "agent", []string{"https://example.com"}
	_, err = plan.CallMCP(t.Context(), runtime, "search", "web", webIQArguments("web"))
	require.ErrorContains(t, err, "connector endpoint origin is not approved by the host")
}

func TestAzureWebIQConnectorTargetedSelection(t *testing.T) {
	plan := webIQPlan(t, "web")
	require.Equal(t, []string{"search__web"}, plan.ToolNames())
	require.Equal(t, []string{"AZURE_WEB_IQ_READ_ACCESS_TOKEN"}, plan.Secrets())
	encoded, err := plan.EncodeJSON()
	require.NoError(t, err)
	require.Contains(t, encoded, `"web"`)
	for _, omitted := range []string{`"news"`, `"browse"`, `"urls"`, webIQURL, `"safeOutputs"`} {
		require.NotContains(t, encoded, omitted)
	}
	newsPlan := webIQPlan(t, "news")
	require.Equal(t, []string{"search__news"}, newsPlan.ToolNames())
	newsJSON, err := newsPlan.EncodeJSON()
	require.NoError(t, err)
	require.Contains(t, newsJSON, `"news"`)
	require.NotContains(t, newsJSON, `"web"`)
	runtime := connectors.Runtime{Phase: "agent", AllowedOrigins: plan.Origins(),
		ResolveSecret: func(context.Context, string, string) (string, error) {
			t.Fatal("unselected method resolved credentials")
			return "", nil
		},
	}
	for _, tool := range []string{"news", "browse", "auto", "*"} {
		_, err := plan.CallMCP(t.Context(), runtime, "search", tool, webIQArguments(tool))
		require.ErrorContains(t, err, "exact allowlist")
	}
}

func webIQMockRuntime(t *testing.T, result string, status int, calls *int, wantArgs map[string]any) connectors.Runtime {
	t.Helper()
	return connectors.Runtime{Phase: "agent", AllowedOrigins: []string{"https://api.microsoft.ai"},
		ResolveSecret: func(_ context.Context, phase, name string) (string, error) {
			require.Equal(t, "agent", phase)
			require.Equal(t, "AZURE_WEB_IQ_READ_ACCESS_TOKEN", name)
			return "synthetic-entra-token", nil
		},
		Client: &http.Client{Transport: connectorSmokeTransport(func(request *http.Request) (*http.Response, error) {
			require.Equal(t, "https://api.microsoft.ai/v3/mcp", request.URL.String())
			require.Equal(t, "Bearer synthetic-entra-token", request.Header.Get("Authorization"))
			require.Empty(t, request.Header.Get("x-apikey"))
			var message struct {
				ID     json.RawMessage `json:"id"`
				Method string          `json:"method"`
				Params struct {
					Name      string         `json:"name"`
					Arguments map[string]any `json:"arguments"`
				} `json:"params"`
			}
			require.NoError(t, json.NewDecoder(request.Body).Decode(&message))
			body := `{"protocolVersion":"2025-03-26","capabilities":{},"serverInfo":{"name":"offline-web-iq","version":"1"}}`
			code := http.StatusOK
			if len(message.ID) == 0 {
				return &http.Response{StatusCode: http.StatusAccepted, Header: http.Header{}, Body: http.NoBody, Request: request}, nil
			}
			if message.Method == "tools/call" {
				(*calls)++
				want, err := json.Marshal(wantArgs)
				require.NoError(t, err)
				got, err := json.Marshal(message.Params.Arguments)
				require.NoError(t, err)
				require.JSONEq(t, string(want), string(got))
				body, code = result, status
			}
			envelope := `{"jsonrpc":"2.0","id":` + string(message.ID) + `,"result":` + body + `}`
			return &http.Response{StatusCode: code, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(envelope)), Request: request}, nil
		})},
	}
}

func TestAzureWebIQConnectorHostedCalls(t *testing.T) {
	plan := webIQPlan(t, "web", "news", "browse")
	for _, test := range webIQValidArguments() {
		t.Run(test.Tool, func(t *testing.T) {
			calls := 0
			runtime := webIQMockRuntime(t, `{"content":[{"type":"text","text":"Untrusted passage fixture"}]}`, 200, &calls, test.Args)
			result, err := plan.CallMCP(t.Context(), runtime, "search", test.Tool, test.Args)
			require.NoError(t, err, "%v", test.Args)
			require.Len(t, result.Content, 1)
			require.Equal(t, 1, calls, "the native method is forwarded once, without direct origin fetches")
		})
	}
	for name, fixture := range map[string]struct {
		body   string
		status int
	}{
		"denied":     {`{}`, 403},
		"tool-error": {`{"isError":true,"content":[{"type":"text","text":"private-error-fixture"}]}`, 200},
		"oversize":   {`{"content":[{"type":"text","text":"` + strings.Repeat("x", 131073) + `"}]}`, 200},
	} {
		t.Run(name, func(t *testing.T) {
			calls := 0
			args := webIQArguments("web")
			_, err := plan.CallMCP(t.Context(), webIQMockRuntime(t, fixture.body, fixture.status, &calls, args), "search", "web", args)
			require.Error(t, err)
			require.NotContains(t, err.Error(), "private-error-fixture")
			require.Equal(t, 1, calls, "failures do not retry or fall back")
		})
	}
}

func TestAzureWebIQConnectorXMLConsumer(t *testing.T) {
	source, err := os.ReadFile("../../.github/workflows/connectors/azure-web-iq.md")
	require.NoError(t, err)
	document := commentedConnectorDocumentation(t, string(source))
	var docs struct {
		ID       string `xml:"id,attr"`
		Examples []struct {
			YAML string `xml:"workflow-yaml"`
			Body string `xml:"workflow-body"`
		} `xml:"examples>example"`
	}
	require.NoError(t, xml.Unmarshal([]byte(document), &docs))
	require.Equal(t, "azure-web-iq", docs.ID)
	require.Len(t, docs.Examples, 1)
	example := docs.Examples[0]
	consumer := "---\n" + strings.TrimSpace(example.YAML) + "\n---\n" + strings.TrimSpace(example.Body) + "\n"
	compiler, data, path, err := parseConnectorConsumer(t, "azure-web-iq-docs.md", consumer)
	require.NoError(t, err)
	require.Equal(t, []string{"search__web"}, data.Connectors["azure-web-iq"].Agent.ToolNames())
	require.Nil(t, data.Connectors["azure-web-iq"].SafeOutputs)
	require.True(t, templatableBoolIsTrue(data.SafeOutputs.Staged))
	output, err := compiler.CompileToYAML(data, path)
	require.NoError(t, err)
	_, effects := connectorPhaseJobJSON(t, output)
	require.NotContains(t, effects, "${{ secrets.AZURE_WEB_IQ_READ_ACCESS_TOKEN }}")
}

func TestAzureWebIQConnectorBundledNode(t *testing.T) {
	plan := webIQPlan(t, "web", "news", "browse")
	encoded, err := plan.Encode()
	require.NoError(t, err)
	payload, err := json.Marshal(map[string]any{"plan": encoded, "valid": webIQValidArguments(), "invalid": webIQInvalidArguments()})
	require.NoError(t, err)
	runtime, err := filepath.Abs("../../actions/setup/js/connector_runtime.cjs")
	require.NoError(t, err)
	command := exec.CommandContext(t.Context(), "node", "--unhandled-rejections=strict", "-e", webIQNodeScript, runtime)
	command.Env = []string{"PATH=" + os.Getenv("PATH")}
	command.Stdin = strings.NewReader(string(payload))
	output, err := command.CombinedOutput()
	require.NoError(t, err, "%s", output)
	require.Empty(t, string(output), "offline checks must not log provider content or credentials")
}

const webIQNodeScript = `
const assert = require("node:assert/strict");
const {callMCP} = require(process.argv[1]);
const fixture = JSON.parse(require("node:fs").readFileSync(0, "utf8"));
let forbiddenSecrets = 0, forbiddenHTTP = 0;
const noSecrets = new Proxy({}, {get() {forbiddenSecrets++; throw Error("invalid call accessed a credential");}});
const noHTTP = async () => {forbiddenHTTP++; throw Error("invalid call accessed HTTP");};
const env = {GH_AW_CONNECTOR_SECRET_AZURE_WEB_IQ_READ_ACCESS_TOKEN:"synthetic-entra-token"};
(async () => {
  for (const {tool,args,wantError,wantNodeError} of fixture.invalid)
    await assert.rejects(callMCP(fixture.plan,"search",tool,args,{env:noSecrets,fetchImpl:noHTTP}),
      new RegExp(wantNodeError || wantError,"i"),JSON.stringify({tool,args}));
  assert.equal(forbiddenSecrets,0);
  assert.equal(forbiddenHTTP,0);
  for (const {tool,args} of fixture.valid) {
    let calls = 0;
    const fetchImpl = async (target,init) => {
      assert.equal(target,"https://api.microsoft.ai/v3/mcp");
      assert.equal(init.redirect,"manual");
      assert.equal(init.headers.Authorization,"Bearer synthetic-entra-token");
      assert.equal(init.headers["x-apikey"],undefined);
      const message = JSON.parse(init.body);
      if (message.method === "initialize")
        return Response.json({jsonrpc:"2.0",id:message.id,result:{protocolVersion:"2025-03-26"}});
      if (message.method === "notifications/initialized") return new Response(null,{status:202});
      calls++;
      assert.equal(message.method,"tools/call");
      assert.deepEqual(message.params,{name:tool,arguments:args});
      return Response.json({jsonrpc:"2.0",id:message.id,result:{content:[{type:"text",text:"untrusted passage"}]}});
    };
    const result = await callMCP(fixture.plan,"search",tool,args,{env,fetchImpl});
    assert.equal(result.content[0].text,"untrusted passage");
    assert.equal(calls,1);
    await assert.rejects(callMCP(fixture.plan,"search",tool,args,{env:{},fetchImpl:noHTTP}));
  }
  assert.equal(forbiddenHTTP,0);
  for (const failure of ["denied","oversize","tool-error"]) {
    let calls = 0;
    await assert.rejects(callMCP(fixture.plan,"search","web",fixture.valid[0].args,{env,fetchImpl:async (target,init) => {
      assert.equal(target,"https://api.microsoft.ai/v3/mcp");
      const message = JSON.parse(init.body);
      if (message.method === "initialize")
        return Response.json({jsonrpc:"2.0",id:message.id,result:{protocolVersion:"2025-03-26"}});
      if (message.method === "notifications/initialized") return new Response(null,{status:202});
      calls++;
      if (failure === "denied") return Response.json({error:"private fixture"},{status:403});
      const result = {content:[{type:"text",text:failure === "oversize" ? "x".repeat(131073) : "private fixture"}]};
      if (failure === "tool-error") result.isError = true;
      return Response.json({jsonrpc:"2.0",id:message.id,result});
    }}));
    assert.equal(calls,1);
  }
})();
`
