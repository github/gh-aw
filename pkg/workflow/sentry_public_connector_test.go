//go:build !integration

package workflow

import (
	"context"
	"encoding/json"
	"encoding/xml"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/github/gh-aw/pkg/connectors"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

func sentryPublicConsumer(t *testing.T, content string) (*Compiler, *WorkflowData, string, error) {
	t.Helper()
	root := t.TempDir()
	path := filepath.Join(root, ".github", "workflows", "sentry-review.md")
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0700))
	require.NoError(t, os.WriteFile(path, []byte(content), 0600))
	compiler := NewCompiler(WithNoEmit(true), WithSkipValidation(true))
	compiler.gitRoot = root
	installConnectorTestSources(t, root)
	compiler.SetActionMode(ActionModeDev)
	compiler.version = "v0.0.0-1-g683ddf9617"
	data, err := compiler.ParseWorkflowFile(path)
	_, statErr := os.Stat(filepath.Join(root, ".github", "workflows", "connectors"))
	require.NoError(t, statErr, "normal imports use the local declaration")
	return compiler, data, path, err
}

func sentryPublicSource(selection string) string {
	return awConnectorHeader + `imports:
  - uses: ".github/workflows/connectors/sentry.md"
    with:
      reader-secret: APPROVED_SENTRY_READER
      hosted-reader-secret: APPROVED_SENTRY_MCP
      writer-secret: APPROVED_SENTRY_WRITER
` + selection + "---\nInspect approved metadata only.\n"
}

func TestSentryPublicAlertGatewayNumericProject(t *testing.T) {
	compiler, root := awConnectorCompiler(t)
	compiler.repoConfig, compiler.repoConfigLoaded = &RepoConfig{GatewayDispatch: true}, true
	source, err := os.ReadFile("../../.github/workflows/connectors/sentry.md")
	require.NoError(t, err)
	path := filepath.Join(root, ".github", "workflows", "sentry-gateway.md")
	declaration := filepath.Join(filepath.Dir(path), "connectors", "sentry.md")
	require.NoError(t, os.MkdirAll(filepath.Dir(declaration), 0700))
	require.NoError(t, os.WriteFile(declaration, source, 0600))
	consumer := strings.Replace(sentryPublicSource(""), "on: workflow_dispatch",
		"on:\n  gateway-dispatch:\n    sentry: [issue-alert-triggered]", 1)
	require.NoError(t, os.WriteFile(path, []byte(consumer), 0600))
	data, err := compiler.ParseWorkflowFile(path)
	require.NoError(t, err)
	require.Len(t, data.WebhookSubscriptions, 1)
	require.Nil(t, data.Connectors["sentry"].Agent)
	require.Nil(t, data.Connectors["sentry"].SafeOutputs)
	output, err := compiler.CompileToYAML(data, path)
	require.NoError(t, err)
	require.Contains(t, output, "gateway_provenance:")
	require.Contains(t, output, "gateway_admission:")
	require.Contains(t, output, "needs.gateway_provenance.outputs.verified == 'true'")
	require.Contains(t, output, "needs.gateway_admission.outputs.verified == 'true'")
	var compiled map[string]any
	require.NoError(t, yaml.Unmarshal([]byte(output), &compiled))
	activation := compiled["jobs"].(map[string]any)["activation"].(map[string]any)
	subscriptions := ""
	for _, raw := range activation["steps"].([]any) {
		step := raw.(map[string]any)
		if step["id"] == "validate-webhook" {
			subscriptions = step["env"].(map[string]any)["GH_AW_WEBHOOK_SUBSCRIPTIONS"].(string)
		}
	}
	require.NotEmpty(t, subscriptions)
	require.NoFileExists(t, strings.TrimSuffix(path, ".md")+".lock.yml")
	fixture, err := os.ReadFile("../connectors/testdata/providers/sentry/alert-triggered.json")
	require.NoError(t, err)
	input, err := json.Marshal(map[string]any{
		"subscriptions": json.RawMessage(subscriptions),
		"fixture":       json.RawMessage(fixture),
	})
	require.NoError(t, err)
	assets, err := filepath.Abs("../../actions/setup/js")
	require.NoError(t, err)
	node, err := exec.LookPath("node")
	require.NoError(t, err)
	driver := `
const assert = require("node:assert/strict");
const {validateWebhook} = require(process.argv[1] + "/validate_webhook.cjs");
const input = JSON.parse(require("node:fs").readFileSync(0, "utf8"));
const subscription = input.subscriptions[0];
const projectCondition = subscription.conditions.find(condition => condition.path.join("/") === "data/event/project");
assert(projectCondition);
const project = input.fixture.data.event.project;
assert.equal(typeof project, "number");
const filters = Object.fromEntries(subscription.conditions.map(condition => [
  condition.filter, [condition.path.reduce((value, key) => value[key], input.fixture)]
]));
const event = {
  action: subscription.dispatch.event_type,
  client_payload: {schema_version:2, aw_context:{webhook:{
    event:subscription.event, deliveries:[{
      delivery_id:"offline-sentry-alert-fixture",
      payload:{
        url:"https://sentry.io/organizations/example-org/issues/1234567890/",
        type:[subscription.connector.trigger], filters
      }
    }]
  }}}
};
const env = {GITHUB_EVENT_NAME:"repository_dispatch", GH_AW_WEBHOOK_SUBSCRIPTIONS:JSON.stringify(input.subscriptions)};
assert.equal(validateWebhook(event, env).skipped, false);
assert.equal(projectCondition.value_type, "integer");
assert.deepEqual(projectCondition.values, [project]);
for (const value of [String(project), project + 1]) {
  const invalid = structuredClone(event);
  invalid.client_payload.aw_context.webhook.deliveries[0].payload.filters[projectCondition.filter] = [value];
  assert.throws(() => validateWebhook(invalid, env), /webhook payload filters/);
}
`
	command := exec.CommandContext(t.Context(), node, "-e", driver, assets)
	command.Env = []string{}
	command.Stdin = strings.NewReader(string(input))
	result, err := command.CombinedOutput()
	require.NoError(t, err, "%s", result)
}

func TestSentryPublicDocumentationConsumer(t *testing.T) {
	source, err := os.ReadFile("../../.github/workflows/connectors/sentry.md")
	require.NoError(t, err)
	comment := commentedConnectorDocumentation(t, string(source))
	require.Equal(t, 1, strings.Count(comment, `<connector-documentation id="sentry" version="1">`))
	type section struct {
		Text string `xml:",innerxml"`
	}
	var documentation struct {
		XMLName        xml.Name `xml:"connector-documentation"`
		ID             string   `xml:"id,attr"`
		Version        string   `xml:"version,attr"`
		Purpose        string   `xml:"purpose"`
		Authentication section  `xml:"authentication"`
		Endpoints      section  `xml:"endpoints"`
		Resources      section  `xml:"resources"`
		Imports        section  `xml:"imports"`
		Triggers       section  `xml:"triggers"`
		Reads          section  `xml:"reads"`
		Effects        section  `xml:"effects"`
		Privacy        string   `xml:"privacy"`
		Unsupported    string   `xml:"unsupported"`
		Consumer       struct {
			Path        string `xml:"path,attr"`
			Frontmatter string `xml:"frontmatter"`
			Body        string `xml:"body"`
		} `xml:"consumer"`
	}
	require.NoError(t, xml.Unmarshal([]byte(comment), &documentation))
	assert.Equal(t, "sentry", documentation.ID)
	assert.Equal(t, "1", documentation.Version)
	for _, section := range []string{
		documentation.Purpose, documentation.Authentication.Text, documentation.Endpoints.Text,
		documentation.Resources.Text, documentation.Imports.Text, documentation.Triggers.Text,
		documentation.Reads.Text, documentation.Effects.Text, documentation.Privacy, documentation.Unsupported,
	} {
		assert.NotEmpty(t, strings.TrimSpace(section))
	}
	require.Equal(t, ".github/workflows/sentry-review.md", documentation.Consumer.Path)
	require.Equal(t, 2, strings.Count(comment, "<![CDATA["))
	content := "---\n" + strings.TrimSpace(documentation.Consumer.Frontmatter) + "\n---\n" +
		strings.TrimSpace(documentation.Consumer.Body) + "\n"
	compiler, data, path, err := sentryPublicConsumer(t, content)
	require.NoError(t, err)
	require.NotNil(t, data.Connectors["sentry"].Agent)
	require.NotNil(t, data.Connectors["sentry"].SafeOutputs)
	assert.Nil(t, data.Connectors["sentry"].Context)
	output, err := compiler.CompileToYAML(data, path)
	require.NoError(t, err)
	assert.Contains(t, output, "sentry_resolve_issue")
	assert.NotContains(t, output, "connector_6_sentry_")
	assert.NotContains(t, output, "${{ secrets.SENTRY_STATUS_TOKEN }}")
	assert.NotContains(t, output, "${{ secrets.SENTRY_MCP_OAUTH_TOKEN }}")
	assert.Contains(t, output, "connector_runtime.cjs")
	for _, legacy := range []string{"go build", "CGO_ENABLED=0 make build", ".gh-aw-connector-runtime", "Checkout trusted connector runtime"} {
		assert.NotContains(t, output, legacy)
	}
	var embedded map[string]any
	require.NoError(t, yaml.Unmarshal([]byte(documentation.Consumer.Frontmatter), &embedded))
	fixture, err := os.ReadFile("../connectors/testdata/providers/sentry/workflow.md")
	require.NoError(t, err)
	fixtureParts := strings.SplitN(strings.TrimPrefix(string(fixture), "---\n"), "\n---\n", 2)
	require.Len(t, fixtureParts, 2)
	var positive map[string]any
	require.NoError(t, yaml.Unmarshal([]byte(fixtureParts[0]), &positive))
	assert.Equal(t, embedded, positive, "the positive fixture must match the documented consumer")
	assert.Equal(t, strings.TrimSpace(documentation.Consumer.Body), strings.TrimSpace(fixtureParts[1]))
}

func TestSentryPublicIndependentSelections(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name, selection     string
		read, hosted, write bool
	}{
		{"import-only", "", false, false, false},
		{"rest", "tools:\n  sentry: [get-issue]\n", true, false, false},
		{"hosted", "tools:\n  sentry: [sentry]\n", false, true, false},
		{"writer", "safe-outputs:\n  sentry-resolve-issue:\n", false, false, true},
		{"both", "tools:\n  sentry: [get-issue]\nsafe-outputs:\n  sentry-resolve-issue:\n", true, false, true},
		{"staged", "tools:\n  sentry: [get-issue]\nsafe-outputs:\n  staged: true\n  sentry-resolve-issue:\n", true, false, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			compiler, data, path, err := sentryPublicConsumer(t, sentryPublicSource(test.selection))
			require.NoError(t, err)
			if !test.read && !test.hosted && !test.write {
				assert.Empty(t, data.Connectors)
			} else {
				config := data.Connectors["sentry"]
				require.NotNil(t, config)
				assert.Equal(t, test.read || test.hosted, config.Agent != nil)
				assert.Equal(t, test.write, config.SafeOutputs != nil)
				assert.Nil(t, config.Context)
				if test.read || test.hosted {
					secret := "APPROVED_SENTRY_READER"
					if test.hosted {
						secret = "APPROVED_SENTRY_MCP"
					}
					assert.Equal(t, []string{secret}, config.Agent.Secrets())
					frozen, err := config.Agent.Encode()
					require.NoError(t, err)
					plan, err := connectors.DecodePlan(frozen)
					require.NoError(t, err)
					assert.Equal(t, config.Agent.Secrets(), plan.Secrets())
				}
				if test.write {
					assert.Equal(t, []string{"APPROVED_SENTRY_WRITER"}, config.SafeOutputs.Secrets())
					assert.Equal(t, []string{"resolve-issue"}, config.SafeOutputs.SafeOutputNames())
				}
			}
			output, err := compiler.CompileToYAML(data, path)
			require.NoError(t, err)
			assert.Equal(t, test.read, strings.Contains(output, "${{ secrets.APPROVED_SENTRY_READER }}"))
			assert.Equal(t, test.hosted, strings.Contains(output, "${{ secrets.APPROVED_SENTRY_MCP }}"))
			assert.Equal(t, test.write && test.name != "staged", strings.Contains(output, "${{ secrets.APPROVED_SENTRY_WRITER }}"))
			assert.NotContains(t, output, "SENTRY_CONTEXT_TOKEN")
			assert.NotContains(t, output, "connector_6_sentry_")
			for _, legacy := range []string{"go build", "CGO_ENABLED=0 make build", ".gh-aw-connector-runtime", "Checkout trusted connector runtime"} {
				assert.NotContains(t, output, legacy)
			}
			var lock map[string]any
			require.NoError(t, yaml.Unmarshal([]byte(output), &lock))
			jobs := lock["jobs"].(map[string]any)
			agent, err := json.Marshal(jobs["agent"])
			require.NoError(t, err)
			assert.NotContains(t, string(agent), "${{ secrets.APPROVED_SENTRY_WRITER }}")
		})
	}
}

func TestSentryPublicUnsupportedSelections(t *testing.T) {
	t.Parallel()
	for _, selection := range []string{
		"tools:\n  sentry: [search-issues]\n",
		"tools:\n  sentry: [issue-event]\n",
		"safe-outputs:\n  sentry-add-comment:\n",
		"safe-outputs:\n  sentry-resolve-issue: {status: ignored}\n",
		"context:\n  sentry: {issue-event: {issue: '1234567890'}}\n",
	} {
		t.Run(selection, func(t *testing.T) {
			_, _, _, err := sentryPublicConsumer(t, sentryPublicSource(selection))
			require.Error(t, err)
		})
	}
	for _, trigger := range []string{"sentry-error-created", "sentry-error-regressed", "sentry-issue-alert-triggered"} {
		t.Run(trigger, func(t *testing.T) {
			source := strings.Replace(sentryPublicSource(""), "on: workflow_dispatch", "on:\n  "+trigger+":", 1)
			_, _, _, err := sentryPublicConsumer(t, source)
			require.ErrorContains(t, err, "gateway-dispatch")
		})
	}
}

func sentryPublicPlan(t *testing.T) *WorkflowConnector {
	t.Helper()
	_, data, _, err := sentryPublicConsumer(t, sentryPublicSource("tools:\n  sentry: [get-issue, sentry]\nsafe-outputs:\n  sentry-resolve-issue:\n"))
	require.NoError(t, err)
	return data.Connectors["sentry"]
}

func sentryPublicArguments() map[string]any {
	return map[string]any{"organization": "example-org", "issue": "1234567890"}
}

func sentryPublicRuntime(t *testing.T, phase, response string, status int, calls, resolutions *int) connectors.Runtime {
	t.Helper()
	return connectors.Runtime{
		Phase: phase, AllowWrites: phase == "safe-output", AllowedOrigins: []string{"https://sentry.io"},
		ResolveSecret: func(_ context.Context, actualPhase, name string) (string, error) {
			*resolutions++
			assert.Equal(t, phase, actualPhase)
			expected := "APPROVED_SENTRY_READER"
			if phase == "safe-output" {
				expected = "APPROVED_SENTRY_WRITER"
			}
			require.Equal(t, expected, name)
			return "fixture-token", nil
		},
		Client: &http.Client{Transport: connectorSmokeTransport(func(request *http.Request) (*http.Response, error) {
			*calls++
			assert.Equal(t, "https://sentry.io/api/0/organizations/example-org/issues/1234567890/", request.URL.String())
			assert.Equal(t, "Bearer fixture-token", request.Header.Get("Authorization"))
			if phase == "safe-output" {
				assert.Equal(t, http.MethodPut, request.Method)
				assert.Equal(t, "application/json", request.Header.Get("Content-Type"))
				body, err := io.ReadAll(request.Body)
				require.NoError(t, err)
				assert.JSONEq(t, `{"status":"resolved"}`, string(body))
			} else {
				assert.Equal(t, http.MethodGet, request.Method)
				assert.Equal(t, http.NoBody, request.Body)
			}
			return &http.Response{StatusCode: status, Header: http.Header{"Location": []string{"https://untrusted.invalid"}},
				Body: io.NopCloser(strings.NewReader(response)), Request: request}, nil
		})},
	}
}

func TestSentryPublicScalarReadAndResolution(t *testing.T) {
	t.Parallel()
	config := sentryPublicPlan(t)
	fixture, err := os.ReadFile("../connectors/testdata/providers/sentry/issue-details.json")
	require.NoError(t, err)
	for _, phase := range []string{"agent", "safe-output"} {
		t.Run(phase, func(t *testing.T) {
			status, operation, capability, plan := "unresolved", "get-issue", "mcp", config.Agent
			if phase == "safe-output" {
				status, operation, capability, plan = "resolved", "resolve-issue", "safe-output", config.SafeOutputs
			}
			response := strings.Replace(string(fixture), `"status": "unresolved"`, `"status": "`+status+`"`, 1)
			calls, resolutions := 0, 0
			runtime := sentryPublicRuntime(t, phase, response, http.StatusOK, &calls, &resolutions)
			if phase == "safe-output" {
				_, err := plan.PrepareSafeOutput(operation, sentryPublicArguments())
				require.NoError(t, err)
				assert.Zero(t, calls)
				assert.Zero(t, resolutions)
			}
			result, err := plan.Execute(t.Context(), runtime, capability, operation, sentryPublicArguments())
			require.NoError(t, err)
			assert.True(t, result.Untrusted)
			assert.JSONEq(t, `{"issue":"1234567890","project":"4500000000000001","status":"`+status+`"}`, string(result.Data))
			assert.Equal(t, 1, calls)
			assert.Equal(t, 1, resolutions)
		})
	}
}

func TestSentryPublicRejectsInputsBeforeCredentials(t *testing.T) {
	t.Parallel()
	config := sentryPublicPlan(t)
	calls, resolutions := 0, 0
	runtime := sentryPublicRuntime(t, "agent", "{}", http.StatusOK, &calls, &resolutions)
	for field, value := range map[string]any{
		"organization": "other-org", "issue": "9999999999", "status": "ignored",
		"issueUrl": "https://untrusted.invalid", "project": "4500000000000002",
	} {
		for _, phase := range []string{"agent", "safe-output"} {
			args := sentryPublicArguments()
			args[field] = value
			plan, capability, operation := config.Agent, "mcp", "get-issue"
			if phase == "safe-output" {
				plan, capability, operation = config.SafeOutputs, "safe-output", "resolve-issue"
			}
			denied := runtime
			denied.Phase, denied.AllowWrites = phase, phase == "safe-output"
			_, err := plan.Execute(t.Context(), denied, capability, operation, args)
			require.Error(t, err)
		}
	}
	for _, issue := range []any{1234567890, "../1234567890", strings.Repeat("1", 33), "EXAMPLE-1", nil} {
		args := sentryPublicArguments()
		args["issue"] = issue
		_, err := config.Agent.Execute(t.Context(), runtime, "mcp", "get-issue", args)
		require.Error(t, err)
	}
	_, err := config.SafeOutputs.Execute(t.Context(), runtime, "safe-output", "resolve-issue", sentryPublicArguments())
	require.Error(t, err)
	runtime.AllowedOrigins = []string{"https://untrusted.invalid"}
	_, err = config.Agent.Execute(t.Context(), runtime, "mcp", "get-issue", sentryPublicArguments())
	require.Error(t, err)
	assert.Zero(t, calls)
	assert.Zero(t, resolutions)
}

func TestSentryPublicResponseFailuresAndByteBoundary(t *testing.T) {
	t.Parallel()
	config := sentryPublicPlan(t)
	const valid = `{"id":"1234567890","project":{"id":"4500000000000001"},"issueCategory":"error","status":"unresolved"}`
	for _, fixture := range []struct {
		name, body string
		status     int
	}{
		{"unauthorized", valid, 401}, {"forbidden", valid, 403}, {"throttled", valid, 429},
		{"server-error", valid, 500}, {"redirect", valid, 307}, {"empty", "", 200},
		{"invalid-json", "{", 200}, {"application-error", `{"detail":"failure"}`, 200},
		{"wrong-issue", strings.Replace(valid, "1234567890", "9999999999", 1), 200},
		{"wrong-project", strings.Replace(valid, "4500000000000001", "4500000000000002", 1), 200},
		{"missing-project", strings.Replace(valid, `"id":"4500000000000001"`, "", 1), 200},
		{"wrong-category", strings.Replace(valid, `"error"`, `"outage"`, 1), 200},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			for _, phase := range []string{"agent", "safe-output"} {
				plan, capability, operation := config.Agent, "mcp", "get-issue"
				response := fixture.body
				if phase == "safe-output" {
					plan, capability, operation = config.SafeOutputs, "safe-output", "resolve-issue"
					response = strings.ReplaceAll(response, "unresolved", "resolved")
				}
				calls, resolutions := 0, 0
				runtime := sentryPublicRuntime(t, phase, response, fixture.status, &calls, &resolutions)
				_, err := plan.Execute(t.Context(), runtime, capability, operation, sentryPublicArguments())
				require.Error(t, err)
				assert.Equal(t, 1, calls, "failed requests must not retry")
			}
		})
	}
	for _, size := range []int{65536, 65537} {
		base := strings.TrimSuffix(valid, "}") + `,"unprojected":""}`
		response := strings.TrimSuffix(base, `"}`) + strings.Repeat("x", size-len(base)) + `"}`
		require.Len(t, response, size)
		calls, resolutions := 0, 0
		runtime := sentryPublicRuntime(t, "agent", response, 200, &calls, &resolutions)
		result, err := config.Agent.Execute(t.Context(), runtime, "mcp", "get-issue", sentryPublicArguments())
		if size == 65536 {
			require.NoError(t, err)
			assert.Less(t, len(result.Data), 128)
		} else {
			require.Error(t, err)
		}
	}
	for _, phase := range []string{"agent", "safe-output"} {
		calls, resolutions := 0, 0
		response := valid
		plan, capability, operation := config.SafeOutputs, "safe-output", "resolve-issue"
		if phase == "agent" {
			plan, capability, operation = config.Agent, "mcp", "get-issue"
			response = strings.Replace(valid, "unresolved", "resolved", 1)
		}
		runtime := sentryPublicRuntime(t, phase, response, 200, &calls, &resolutions)
		_, err := plan.Execute(t.Context(), runtime, capability, operation, sentryPublicArguments())
		require.Error(t, err, "read status restrictions and write evidence must not be relaxed")
	}
}

func TestSentryPublicHostedAllowlistAndNoCredentialFallback(t *testing.T) {
	t.Parallel()
	config := sentryPublicPlan(t)
	resolutions := 0
	runtime := connectors.Runtime{Phase: "agent", AllowedOrigins: []string{"https://mcp.sentry.dev"},
		ResolveSecret: func(_ context.Context, phase, name string) (string, error) {
			resolutions++
			assert.Equal(t, "agent", phase)
			assert.Equal(t, "APPROVED_SENTRY_MCP", name)
			return "", errors.New("fixture MCP OAuth token unavailable")
		},
		Client: &http.Client{Transport: connectorSmokeTransport(func(*http.Request) (*http.Response, error) {
			t.Error("credential failure must prevent network access")
			return nil, errors.New("network forbidden")
		})},
	}
	args := map[string]any{"organizationSlug": "example-org", "issueId": "1234567890"}
	for _, tool := range []string{"*", "update_issue", "analyze_issue_with_seer", "search_issues"} {
		_, err := config.Agent.CallMCP(t.Context(), runtime, "sentry", tool, args)
		require.Error(t, err)
	}
	for field, value := range map[string]any{
		"organizationSlug": "other-org", "issueId": "9999999999",
		"issueUrl": "https://untrusted.invalid", "eventId": "0123456789abcdef0123456789abcdef",
		"regionUrl": "https://untrusted.invalid",
	} {
		invalid := map[string]any{"organizationSlug": "example-org", "issueId": "1234567890", field: value}
		_, err := config.Agent.CallMCP(t.Context(), runtime, "sentry", "get_issue_details", invalid)
		require.Error(t, err)
	}
	assert.Zero(t, resolutions)
	_, err := config.Agent.CallMCP(t.Context(), runtime, "sentry", "get_issue_details", args)
	require.Error(t, err)
	assert.Positive(t, resolutions, "only the explicitly named hosted credential may be resolved")
}

func TestSentryPublicHostedMCP(t *testing.T) {
	t.Parallel()
	config := sentryPublicPlan(t)
	for _, size := range []int{32, 65537} {
		t.Run(strconv.Itoa(size), func(t *testing.T) {
			var calls atomic.Int32
			upstream := mcp.NewServer(&mcp.Implementation{Name: "sentry-public-fixture", Version: "1"}, nil)
			upstream.AddTool(&mcp.Tool{Name: "get_issue_details", InputSchema: map[string]any{"type": "object"}},
				func(_ context.Context, request *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
					calls.Add(1)
					assert.JSONEq(t, `{"organizationSlug":"example-org","issueId":"1234567890"}`, string(request.Params.Arguments))
					return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: strings.Repeat("x", size)}}}, nil
				})
			handler := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return upstream }, nil)
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, "/mcp/example-org/example-project", r.URL.Path)
				assert.Equal(t, "Bearer fixture-mcp-token", r.Header.Get("Authorization"))
				handler.ServeHTTP(w, r)
			}))
			defer server.Close()
			destination, err := url.Parse(server.URL)
			require.NoError(t, err)
			client := server.Client()
			transport := client.Transport
			client.Transport = connectorSmokeTransport(func(request *http.Request) (*http.Response, error) {
				require.Equal(t, "https://mcp.sentry.dev/mcp/example-org/example-project", request.URL.String())
				local := request.Clone(request.Context())
				local.URL.Scheme, local.URL.Host = destination.Scheme, destination.Host
				local.Host = destination.Host
				return transport.RoundTrip(local)
			})
			runtime := connectors.Runtime{Phase: "agent", AllowedOrigins: []string{"https://mcp.sentry.dev"}, Client: client,
				ResolveSecret: func(_ context.Context, phase, name string) (string, error) {
					assert.Equal(t, "agent", phase)
					assert.Equal(t, "APPROVED_SENTRY_MCP", name)
					return "fixture-mcp-token", nil
				},
			}
			result, err := config.Agent.CallMCP(t.Context(), runtime, "sentry", "get_issue_details",
				map[string]any{"organizationSlug": "example-org", "issueId": "1234567890"})
			if size == 32 {
				require.NoError(t, err)
				require.NotNil(t, result)
			} else {
				require.Error(t, err)
			}
			assert.EqualValues(t, 1, calls.Load())
		})
	}
}

func TestSentryPublicBundledNodeRuntime(t *testing.T) {
	node, err := exec.LookPath("node")
	require.NoError(t, err, "Node.js is required for the bundled connector host")
	runtime, err := filepath.Abs("../../actions/setup/js/connector_runtime.cjs")
	require.NoError(t, err)
	config := sentryPublicPlan(t)
	reader, err := config.Agent.Encode()
	require.NoError(t, err)
	writer, err := config.SafeOutputs.Encode()
	require.NoError(t, err)
	request, err := config.SafeOutputs.PrepareSafeOutput("resolve-issue", sentryPublicArguments())
	require.NoError(t, err)
	fixture, err := os.ReadFile("../connectors/testdata/providers/sentry/issue-details.json")
	require.NoError(t, err)
	input, err := json.Marshal(map[string]any{
		"reader": reader, "writer": writer, "args": sentryPublicArguments(),
		"staged": map[string]any{"staged": true, "request": request}, "response": string(fixture),
	})
	require.NoError(t, err)
	driver := `
const assert = require("node:assert/strict");
const {executeConnector, callMCP} = require(process.argv[1]);
const input = JSON.parse(require("node:fs").readFileSync(0, "utf8"));
let forbiddenCredentials = 0, forbiddenRequests = 0;
const noSecrets = new Proxy({}, {get() { forbiddenCredentials++; throw new Error("credential access forbidden"); }});
const noNetwork = async () => { forbiddenRequests++; throw new Error("network access forbidden"); };
const credential = name => new Proxy({["GH_AW_CONNECTOR_SECRET_" + name]: "fixture-token"}, {
  get(target, key) {
    if (typeof key === "string" && key.startsWith("GH_AW_CONNECTOR_SECRET_"))
      assert.equal(key, "GH_AW_CONNECTOR_SECRET_" + name);
    return target[key];
  }
});
(async () => {
  const staged = await executeConnector(input.writer, "safe-output", "resolve-issue", input.args,
    {staged:true, env:noSecrets, fetchImpl:noNetwork});
  assert.deepEqual(staged, input.staged);
  for (const phase of ["agent", "safe-output"]) {
    const writing = phase === "safe-output";
    let calls = 0;
    const env = credential(writing ? "APPROVED_SENTRY_WRITER" : "APPROVED_SENTRY_READER");
    const response = writing ? input.response.replace('"status": "unresolved"', '"status": "resolved"') : input.response;
    const fetchImpl = async (url, init) => {
      calls++;
      assert.equal(url, "https://sentry.io/api/0/organizations/example-org/issues/1234567890/");
      assert.equal(new Headers(init.headers).get("Authorization"), "Bearer fixture-token");
      assert.equal(init.redirect, "manual");
      assert.equal(init.method, writing ? "PUT" : "GET");
      if (writing) assert.deepEqual(JSON.parse(init.body), {status:"resolved"});
      return new Response(response, {status:200});
    };
    const result = await executeConnector(writing ? input.writer : input.reader,
      writing ? "safe-output" : "mcp", writing ? "resolve-issue" : "get-issue", input.args, {env, fetchImpl});
    assert.equal(result.untrusted, true);
    assert.deepEqual(JSON.parse(JSON.stringify(result.data)),
      {issue:"1234567890", project:"4500000000000001", status:writing ? "resolved" : "unresolved"});
    assert.equal(calls, 1);
  }
  const base = JSON.stringify({...JSON.parse(input.response), padding:""});
  const boundary = base.slice(0, -2) + "x".repeat(65536 - Buffer.byteLength(base)) + '"}';
  assert.equal(Buffer.byteLength(boundary), 65536);
  const bounded = await executeConnector(input.reader, "mcp", "get-issue", input.args,
    {env:credential("APPROVED_SENTRY_READER"), fetchImpl:async () => new Response(boundary, {status:200})});
  assert.deepEqual(JSON.parse(JSON.stringify(bounded.data)),
    {issue:"1234567890", project:"4500000000000001", status:"unresolved"});
  for (const [body, status] of [
    [input.response.replace("4500000000000001", "4500000000000002"), 200],
    [input.response.replace("unresolved", "resolved"), 200],
    [input.response, 429], ["x".repeat(65537), 200]
  ]) {
    let calls = 0;
    await assert.rejects(executeConnector(input.reader, "mcp", "get-issue", input.args,
      {env:credential("APPROVED_SENTRY_READER"), fetchImpl:async () => {
        calls++;
        return new Response(body, {status});
      }}));
    assert.equal(calls, 1);
  }
  await assert.rejects(executeConnector(input.reader, "mcp", "get-issue",
    {...input.args, issue:"9999999999"}, {env:noSecrets, fetchImpl:noNetwork}));
  const hostedArgs = {organizationSlug:"example-org", issueId:"1234567890"};
  const remoteResult = {content:[{type:"text", text:"synthetic hosted detail, not a local scalar projection"}]};
  let hostedCalls = 0;
  const fetchHosted = async (url, init) => {
    hostedCalls++;
    assert.equal(url, "https://mcp.sentry.dev/mcp/example-org/example-project");
    assert.equal(new Headers(init.headers).get("Authorization"), "Bearer fixture-token");
    const message = JSON.parse(init.body);
    if (message.method === "notifications/initialized") return new Response(null, {status:202});
    const result = message.method === "initialize" ? {protocolVersion:"2025-03-26", capabilities:{}, serverInfo:{name:"fixture", version:"1"}} : remoteResult;
    if (message.method === "tools/call") {
      assert.equal(message.params.name, "get_issue_details");
      assert.deepEqual(message.params.arguments, hostedArgs);
    }
    return new Response(JSON.stringify({jsonrpc:"2.0", id:message.id, result}), {status:200, headers:{"Content-Type":"application/json"}});
  };
  assert.deepEqual(JSON.parse(JSON.stringify(await callMCP(input.reader, "sentry", "get_issue_details", hostedArgs,
    {env:credential("APPROVED_SENTRY_MCP"), fetchImpl:fetchHosted}))), remoteResult);
  assert.equal(hostedCalls, 3);
  await assert.rejects(callMCP(input.reader, "sentry", "update_issue", hostedArgs, {env:noSecrets, fetchImpl:noNetwork}));
  assert.equal(forbiddenCredentials, 0);
  assert.equal(forbiddenRequests, 0);
  process.stdout.write("sentry bundled runtime parity passed");
})().catch(error => { console.error(error); process.exitCode = 1; });
`
	command := exec.CommandContext(t.Context(), node, "-e", driver, runtime)
	command.Env = []string{}
	command.Stdin = strings.NewReader(string(input))
	output, err := command.CombinedOutput()
	require.NoError(t, err, "%s", output)
	assert.Equal(t, "sentry bundled runtime parity passed", string(output))
}
