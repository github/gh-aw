//go:build !integration

package workflow

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"maps"
	"net/http"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/connectors"
	"github.com/stretchr/testify/require"
)

const (
	lookupIssue   = "22222222-2222-4222-8222-222222222222"
	lookupTeam    = "33333333-3333-4333-8333-333333333333"
	lookupProject = "44444444-4444-4444-8444-444444444444"
	lookupOther   = "55555555-5555-4555-8555-555555555555"
)

const linearLookupImport = linearConnectorImport + `      issue-references: [ENG-123]
      team-references: [ENG, Engineering]
`

func lookupIssueEvidence() map[string]any {
	return map[string]any{"data": map[string]any{
		"organization": map[string]any{"id": "11111111-1111-4111-8111-111111111111"},
		"issue": map[string]any{"id": lookupIssue, "identifier": "ENG-123", "title": "A reviewed connector title",
			"team": map[string]any{"id": lookupTeam}, "project": map[string]any{"id": lookupProject}},
	}}
}

func lookupTeamEvidence() map[string]any {
	return map[string]any{"data": map[string]any{
		"organization": map[string]any{"id": "11111111-1111-4111-8111-111111111111"},
		"teams":        map[string]any{"nodes": []any{map[string]any{"id": lookupTeam, "key": "ENG", "name": "Engineering"}}, "pageInfo": map[string]any{"hasNextPage": false}},
	}}
}

func lookupProjectEvidence() map[string]any {
	return map[string]any{"data": map[string]any{
		"organization": map[string]any{"id": "11111111-1111-4111-8111-111111111111"},
		"projects":     map[string]any{"nodes": []any{map[string]any{"id": lookupProject, "slugId": "abcdef123456"}}, "pageInfo": map[string]any{"hasNextPage": false}},
	}}
}

func lookupScopeEvidence() map[string]any {
	return map[string]any{"data": map[string]any{
		"organization": map[string]any{"id": "11111111-1111-4111-8111-111111111111"},
		"team":         map[string]any{"id": lookupTeam},
		"project": map[string]any{"id": lookupProject, "teams": map[string]any{
			"nodes": []any{map[string]any{"id": lookupTeam}}, "pageInfo": map[string]any{"hasNextPage": false}}},
	}}
}

func lookupEffectEvidence(operation string) map[string]any {
	if operation == "add-comment" {
		return map[string]any{"data": map[string]any{"commentCreate": map[string]any{
			"success": true, "comment": map[string]any{"id": "comment-id", "issue": map[string]any{"id": lookupIssue}},
		}}}
	}
	field := "issueUpdate"
	if operation == "create-issue" {
		field = "issueCreate"
	}
	return map[string]any{"data": map[string]any{field: map[string]any{
		"success": true, "issue": map[string]any{"id": lookupIssue, "identifier": "ENG-123",
			"team": map[string]any{"id": lookupTeam}, "project": map[string]any{"id": lookupProject}},
	}}}
}

type lookupExchange struct {
	Query     string            `json:"query"`
	Variables map[string]string `json:"variables"`
}

type lookupHostOutcome struct {
	Error   string           `json:"error"`
	Calls   []lookupExchange `json:"calls"`
	Secrets []string         `json:"secrets"`
}

func runGoLinearLookup(t *testing.T, plan *connectors.Plan, capability, operation string, args map[string]any, responses []any, status int) lookupHostOutcome {
	t.Helper()
	outcome := lookupHostOutcome{Calls: []lookupExchange{}, Secrets: []string{}}
	phase := "safe-output"
	if capability == "mcp" {
		phase = "agent"
	}
	runtime := connectors.Runtime{Phase: phase, AllowWrites: phase == "safe-output", AllowedOrigins: plan.Origins(),
		ResolveSecret: func(_ context.Context, actualPhase, name string) (string, error) {
			require.Equal(t, phase, actualPhase)
			outcome.Secrets = append(outcome.Secrets, name)
			return "fixture-key", nil
		},
		Client: &http.Client{Transport: connectorSmokeTransport(func(request *http.Request) (*http.Response, error) {
			require.Equal(t, "https://api.linear.app/graphql", request.URL.String())
			require.Equal(t, "fixture-key", request.Header.Get("Authorization"))
			var exchange lookupExchange
			require.NoError(t, json.NewDecoder(request.Body).Decode(&exchange))
			index := len(outcome.Calls)
			outcome.Calls = append(outcome.Calls, exchange)
			if status == -1 {
				return nil, errors.New("uncertain transport failure")
			}
			require.Less(t, index, len(responses), "no undeclared requests or retries")
			data, err := json.Marshal(responses[index])
			require.NoError(t, err)
			if raw, ok := responses[index].(string); ok {
				data = []byte(raw)
			}
			return &http.Response{StatusCode: status, Header: http.Header{}, Request: request, Body: io.NopCloser(strings.NewReader(string(data)))}, nil
		})},
	}
	_, err := plan.Execute(t.Context(), runtime, capability, operation, args)
	if err != nil {
		outcome.Error = err.Error()
	}
	return outcome
}

func runNodeLinearLookup(t *testing.T, plan *connectors.Plan, capability, operation string, args map[string]any, responses []any, status int) lookupHostOutcome {
	t.Helper()
	encoded, err := plan.Encode()
	require.NoError(t, err)
	payload, err := json.Marshal(map[string]any{"plan": encoded, "capability": capability, "operation": operation, "args": args, "responses": responses, "status": status})
	require.NoError(t, err)
	runtimePath, err := filepath.Abs("../../actions/setup/js/connector_runtime.cjs")
	require.NoError(t, err)
	command := exec.CommandContext(t.Context(), "node", "-e", `
const runtime = require(process.argv[1]);
const input = JSON.parse(require("node:fs").readFileSync(0, "utf8"));
const outcome = {error: "", calls: [], secrets: []};
const env = new Proxy({}, {get(_, key) {
  if (key === "GH_AW_CONNECTOR_BINDING_RUN") return undefined;
  if (!key.startsWith("GH_AW_CONNECTOR_SECRET_")) throw new Error("unexpected environment access");
  outcome.secrets.push(key.slice("GH_AW_CONNECTOR_SECRET_".length));
  return "fixture-key";
}});
runtime.executeConnector(input.plan, input.capability, input.operation, input.args, {
  env, fetchImpl: async (url, init) => {
    if (url !== "https://api.linear.app/graphql" || init.headers.Authorization !== "fixture-key" || init.redirect !== "manual") throw new Error("endpoint or credential changed");
    const index = outcome.calls.length;
    outcome.calls.push(JSON.parse(init.body));
    if (input.status === -1) throw new Error("uncertain transport failure");
    if (index >= input.responses.length) throw new Error("undeclared request or retry");
    const data = input.responses[index];
    return new Response(typeof data === "string" ? data : JSON.stringify(data), {status: input.status, headers: {"content-type": "application/json"}});
  }
}).catch(error => {outcome.error = error.message;}).then(() => process.stdout.write(JSON.stringify(outcome)));
`, runtimePath)
	command.Env = []string{}
	command.Stdin = strings.NewReader(string(payload))
	output, err := command.CombinedOutput()
	require.NoError(t, err, "%s", output)
	var outcome lookupHostOutcome
	require.NoError(t, json.Unmarshal(output, &outcome), "%s", output)
	return outcome
}

func TestLinearAutomaticLookupFrozenHosts(t *testing.T) {
	for _, scenario := range []string{
		"comment", "update", "read", "create-key", "create-name", "create-team-only", "create-project-only", "direct",
		"ungranted-alias", "ungranted-uuid", "missing-boundary", "wrong-identifier", "wrong-organization", "unapproved-result",
		"null-project", "ambiguous-team", "paginated-team", "empty-team", "wrong-team-name", "wrong-project-slug",
		"ambiguous-project", "paginated-project", "wrong-membership", "wrong-project-identity", "paginated-membership", "oversized-membership",
		"wrong-final-issue", "wrong-final-project", "graphql-errors", "malformed-response", "oversized-response", "authorization", "redirect", "transport",
	} {
		t.Run(scenario, func(t *testing.T) {
			imports, capability, operation := linearLookupImport, "safe-output", "add-comment"
			args := map[string]any{"issue-id": "ENG-123", "body": "Untrusted prose $(not-code)"}
			responses := []any{lookupIssueEvidence(), lookupEffectEvidence(operation)}
			status, expectedCalls, success := 200, 1, false
			switch scenario {
			case "comment", "update", "read":
				success, expectedCalls = true, 2
				if scenario == "update" {
					operation, args["title"] = "update-issue", "A reviewed connector title"
					responses[1] = lookupEffectEvidence(operation)
				}
				if scenario == "read" {
					capability, operation = "mcp", "get-issue"
					delete(args, "body")
					responses[1] = lookupIssueEvidence()
				}
			case "direct":
				args["issue-id"], responses = lookupIssue, []any{lookupEffectEvidence(operation)}
				imports = strings.ReplaceAll(imports, "      issue-references: [ENG-123]\n", "")
				success = true
			case "ungranted-alias":
				args["issue-id"], expectedCalls = "ENG-999", 0
			case "ungranted-uuid":
				args["issue-id"], expectedCalls = lookupOther, 0
			case "missing-boundary":
				imports = strings.Replace(imports, "      projects: ["+lookupProject+"]\n", "", 1)
				expectedCalls = 0
			case "wrong-identifier":
				responses[0].(map[string]any)["data"].(map[string]any)["issue"].(map[string]any)["identifier"] = "ENG-999"
			case "wrong-organization":
				responses[0].(map[string]any)["data"].(map[string]any)["organization"] = map[string]any{"id": lookupOther}
			case "unapproved-result":
				responses[0].(map[string]any)["data"].(map[string]any)["issue"].(map[string]any)["id"] = lookupOther
			case "null-project":
				responses[0].(map[string]any)["data"].(map[string]any)["issue"].(map[string]any)["project"] = nil
			case "wrong-final-issue":
				imports = strings.Replace(imports, "issues: ["+lookupIssue+"]", "issues: ["+lookupIssue+", "+lookupOther+"]", 1)
				responses[1].(map[string]any)["data"].(map[string]any)["commentCreate"].(map[string]any)["comment"].(map[string]any)["issue"] = map[string]any{"id": lookupOther}
				expectedCalls = 2
			case "graphql-errors":
				responses[0].(map[string]any)["errors"] = []any{map[string]any{"message": "partial failure"}}
			case "malformed-response":
				responses[0] = "{"
			case "oversized-response":
				responses[0] = strings.Repeat("x", 16385)
			case "authorization":
				status = 401
			case "redirect":
				status = 302
			case "transport":
				status = -1
			default:
				operation = "create-issue"
				args = map[string]any{"team-id": "ENG", "project-id": "abcdef123456", "title": "A reviewed connector title", "body": "Untrusted prose $(not-code)"}
				responses = []any{lookupTeamEvidence(), lookupProjectEvidence(), lookupScopeEvidence(), lookupEffectEvidence(operation)}
				team := responses[0].(map[string]any)["data"].(map[string]any)["teams"].(map[string]any)
				project := responses[1].(map[string]any)["data"].(map[string]any)["projects"].(map[string]any)
				scope := responses[2].(map[string]any)["data"].(map[string]any)["project"].(map[string]any)
				switch scenario {
				case "create-key", "create-name", "create-team-only", "create-project-only":
					success, expectedCalls = true, 4
					if scenario == "create-name" {
						args["team-id"] = "Engineering"
					}
					if scenario == "create-team-only" {
						args["project-id"], responses, expectedCalls = lookupProject, []any{responses[0], responses[2], responses[3]}, 3
					}
					if scenario == "create-project-only" {
						args["team-id"], responses, expectedCalls = lookupTeam, responses[1:], 3
					}
				case "ambiguous-team":
					team["nodes"] = append(team["nodes"].([]any), team["nodes"].([]any)[0])
				case "paginated-team":
					team["pageInfo"] = map[string]any{"hasNextPage": true}
				case "empty-team":
					team["nodes"] = []any{}
				case "wrong-team-name":
					team["nodes"].([]any)[0].(map[string]any)["key"] = "OTHER"
				case "wrong-project-slug":
					project["nodes"].([]any)[0].(map[string]any)["slugId"] = "123456abcdef"
					expectedCalls = 2
				case "ambiguous-project":
					project["nodes"] = append(project["nodes"].([]any), project["nodes"].([]any)[0])
					expectedCalls = 2
				case "paginated-project":
					project["pageInfo"] = map[string]any{"hasNextPage": true}
					expectedCalls = 2
				case "wrong-membership":
					scope["teams"].(map[string]any)["nodes"] = []any{map[string]any{"id": lookupOther}}
					expectedCalls = 3
				case "wrong-project-identity":
					scope["id"], expectedCalls = lookupOther, 3
				case "paginated-membership":
					scope["teams"].(map[string]any)["pageInfo"] = map[string]any{"hasNextPage": true}
					expectedCalls = 3
				case "oversized-membership":
					nodes := []any{}
					for range 101 {
						nodes = append(nodes, map[string]any{"id": lookupTeam})
					}
					scope["teams"].(map[string]any)["nodes"], expectedCalls = nodes, 3
				case "wrong-final-project":
					imports = strings.Replace(imports, "projects: ["+lookupProject+"]", "projects: ["+lookupProject+", "+lookupOther+"]", 1)
					responses[3].(map[string]any)["data"].(map[string]any)["issueCreate"].(map[string]any)["issue"].(map[string]any)["project"] = map[string]any{"id": lookupOther}
					expectedCalls = 4
				default:
					t.Fatalf("unhandled scenario %s", scenario)
				}
			}
			selection := "safe-outputs:\n  linear-" + operation + ":\n"
			if capability == "mcp" {
				selection = "tools:\n  linear: [" + operation + "]\n"
			}
			_, data, _, err := parseLinearConnectorWorkflow(t, imports, selection)
			require.NoError(t, err)
			plan := data.Connectors["linear"].SafeOutputs
			if capability == "mcp" {
				plan = data.Connectors["linear"].Agent
			}
			encoded, err := plan.Encode()
			require.NoError(t, err)
			plan, err = connectors.DecodePlan(encoded)
			require.NoError(t, err)
			goOutcome := runGoLinearLookup(t, plan, capability, operation, args, responses, status)
			nodeOutcome := runNodeLinearLookup(t, plan, capability, operation, args, responses, status)
			require.Equal(t, success, goOutcome.Error == "", goOutcome.Error)
			require.Equal(t, success, nodeOutcome.Error == "", nodeOutcome.Error)
			require.Len(t, goOutcome.Calls, expectedCalls)
			require.Equal(t, goOutcome.Calls, nodeOutcome.Calls)
			require.Equal(t, goOutcome.Secrets, nodeOutcome.Secrets)
			if success {
				final := goOutcome.Calls[len(goOutcome.Calls)-1].Variables
				if operation == "create-issue" {
					require.Equal(t, lookupTeam, final["team"])
					require.Equal(t, lookupProject, final["project"])
				} else {
					require.Equal(t, lookupIssue, final["id"])
				}
			}
		})
	}
}

func TestLinearShorthandStagingAndSchemas(t *testing.T) {
	_, data, _, err := parseLinearConnectorWorkflow(t, linearLookupImport, "tools:\n  linear: [get-issue]\nsafe-outputs:\n  staged: true\n  linear-create-issue:\n  linear-add-comment:\n")
	require.NoError(t, err)
	plan := data.Connectors["linear"].SafeOutputs
	for operation, args := range map[string]map[string]any{
		"add-comment":  {"issue-id": "ENG-123", "body": "Body"},
		"create-issue": {"team-id": "Engineering", "project-id": "abcdef123456", "title": "A reviewed connector title", "body": "Body"},
	} {
		intent, err := plan.PrepareSafeOutputIntent(operation, args)
		require.NoError(t, err)
		require.NotEmpty(t, intent.UnresolvedInputs)
		require.Equal(t, args, intent.Arguments, "staging must not claim aliases resolved")
		runtimePath, err := filepath.Abs("../../actions/setup/js/connector_runtime.cjs")
		require.NoError(t, err)
		checkLinearBundledIntent(t, "node", runtimePath, plan, operation, args)
		schema, err := plan.InputSchema("safe-output", operation)
		require.NoError(t, err)
		properties := schema["properties"].(map[string]any)
		for _, input := range intent.UnresolvedInputs {
			pattern := properties[input].(map[string]any)["pattern"].(string)
			require.Contains(t, pattern, "|")
		}
	}
	// No persisted grant expansion: the original plan still rejects another UUID.
	args := map[string]any{"issue-id": lookupOther, "body": "Body"}
	_, err = plan.PrepareSafeOutputIntent("add-comment", args)
	require.Error(t, err)
	original, err := plan.PrepareSafeOutputIntent("add-comment", map[string]any{"issue-id": "ENG-123", "body": "Body"})
	require.NoError(t, err)
	modified := *original
	modified.Arguments = maps.Clone(original.Arguments)
	modified.Arguments["issue-id"] = lookupIssue
	_, err = plan.ExecuteSafeOutput(t.Context(), connectors.Runtime{}, modified)
	require.ErrorContains(t, err, "unresolved inputs")
}

func TestLinearShorthandBundledReadToolSchema(t *testing.T) {
	_, data, _, err := parseLinearConnectorWorkflow(t, linearLookupImport, "tools:\n  linear: [get-issue]\n")
	require.NoError(t, err)
	encoded, err := data.Connectors["linear"].Agent.Encode()
	require.NoError(t, err)
	runtimePath, err := filepath.Abs("../../actions/setup/js/connector_runtime.cjs")
	require.NoError(t, err)
	command := exec.CommandContext(t.Context(), "node", "-e", `
const { Readable, Writable } = require("node:stream");
const runtime = require(process.argv[1]);
const plan = require("node:fs").readFileSync(0, "utf8");
const request = JSON.stringify({jsonrpc: "2.0", id: 1, method: "tools/list"}) + "\n";
let response = "";
const output = new Writable({write(chunk, _, callback) { response += chunk; callback(); }});
const blocked = () => { throw new Error("tool discovery must not resolve secrets or contact provider"); };
runtime.serve(plan, Readable.from([request]), output, {
  env: new Proxy({}, {get: blocked}), fetchImpl: blocked,
}).then(() => process.stdout.write(response)).catch(error => {process.stderr.write(error.message); process.exitCode = 1;});
`, runtimePath)
	command.Env = []string{}
	command.Stdin = strings.NewReader(encoded)
	output, err := command.CombinedOutput()
	require.NoError(t, err, "%s", output)
	var response struct {
		Result struct {
			Tools []struct {
				Name        string         `json:"name"`
				InputSchema map[string]any `json:"inputSchema"`
			} `json:"tools"`
		} `json:"result"`
	}
	require.NoError(t, json.Unmarshal(output, &response))
	require.Len(t, response.Result.Tools, 1)
	require.Equal(t, "get-issue", response.Result.Tools[0].Name)
	goSchema, err := data.Connectors["linear"].Agent.InputSchema("mcp", "get-issue")
	require.NoError(t, err)
	goField := goSchema["properties"].(map[string]any)["issue-id"].(map[string]any)
	nodeField := response.Result.Tools[0].InputSchema["properties"].(map[string]any)["issue-id"].(map[string]any)
	require.Equal(t, goField["pattern"], nodeField["pattern"])
	require.Contains(t, nodeField["pattern"], "|")
}
