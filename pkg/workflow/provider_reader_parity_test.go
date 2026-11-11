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
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/connectors"
	"github.com/github/gh-aw/pkg/parser"
	"github.com/stretchr/testify/require"
)

const readerOrganization = "11111111-1111-4111-8111-111111111111"
const readerOtherOrganization = "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
const readerTeam = "33333333-3333-4333-8333-333333333333"
const readerProject = "44444444-4444-4444-8444-444444444444"

// These hashes identify the public provider-owned schemas consulted on 2026-11-02,
// not the schema of a similarly named hosted MCP tool. Tests perform no discovery.
const linearReaderSchemaSHA256 = "73fac69ae08c0cf8329bbc85a3728a9e24e6e66116ac2f5d6aebbef922b30daf"
const jiraReaderSchemaSHA256 = "30d96390297b247d3e734110a689c9314209021b00badacea12fdf2a34f97ad4"

func readerParityService(t *testing.T, provider string) (*connectors.Service, ConnectorDeclaration, map[string][]string) {
	t.Helper()
	source, err := os.ReadFile(filepath.Join("../../.github/workflows/connectors", provider+".md"))
	require.NoError(t, err)
	parsed, err := parser.ExtractFrontmatterFromContent(string(source))
	require.NoError(t, err)
	grants := map[string][]string{
		"organizations": {readerOrganization, readerOtherOrganization},
		"teams":         {readerTeam, "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb"},
		"projects":      {readerProject, "cccccccc-cccc-4ccc-8ccc-cccccccccccc"},
		"issues":        {"22222222-2222-4222-8222-222222222222"},
		"documents":     {"55555555-5555-4555-8555-555555555555"},
		"initiatives":   {"66666666-6666-4666-8666-666666666666"},
		"milestones":    {"77777777-7777-4777-8777-777777777777"},
		"statuses":      {"88888888-8888-4888-8888-888888888888"},
		"users":         {"99999999-9999-4999-8999-999999999999"},
		"queries":       {"latency"},
	}
	if provider == "jira" {
		grants["issues"] = []string{"APP-1", "OTHER-1"}
		grants["projects"] = []string{"APP", "OTHER"}
		grants["issue-type-ids"] = []string{"10001"}
		grants["discovery-scopes"] = []string{"site-metadata"}
		grants["account-queries"] = []string{"Alice"}
	}
	text := string(source)
	for name, raw := range parsed.Frontmatter["import-schema"].(map[string]any) {
		value := raw.(map[string]any)["default"]
		if grant, exists := grants[name]; exists {
			value = grant
		}
		if name == "site" {
			value = "https://approved.atlassian.net"
		}
		encoded, err := json.Marshal(value)
		require.NoError(t, err)
		text = strings.ReplaceAll(text, "${{ github.aw.import-inputs."+name+" }}", string(encoded))
	}
	parsed, err = parser.ExtractFrontmatterFromContent(text)
	require.NoError(t, err)
	raw, err := json.Marshal(parsed.Frontmatter["connectors"].(map[string]any)[provider])
	require.NoError(t, err)
	var declaration ConnectorDeclaration
	require.NoError(t, connectors.Decode(raw, &declaration))
	credential := "reader"
	if provider == "jira" {
		credential = "rest-reader"
	}
	spec := connectors.ConnectorSpec{
		APIVersion: connectors.APIVersion, Kind: "ConnectorSpec", ID: "aw.github.com/" + provider,
		Endpoints: declaration.Endpoints, MCP: declaration.Tools,
		Credentials: map[string]connectors.Credential{credential: declaration.Credentials[credential].Credential},
	}
	connection := connectors.Connection{
		APIVersion: connectors.APIVersion, Kind: "ConnectorConnection", Connector: spec.ID,
		Resources: nonemptyConnectorResources(declaration.Resources),
		Credentials: map[string]connectors.SecretRef{
			credential: {Secret: declaration.Credentials[credential].Secret},
		},
	}
	service, err := connectors.NewService(spec, connection)
	require.NoError(t, err)
	return service, declaration, grants
}

func readerParityPlan(t *testing.T, service *connectors.Service, name string) *connectors.Plan {
	t.Helper()
	plan, err := service.Select(connectors.Selection{MCP: []string{name}})
	require.NoError(t, err)
	plan, err = plan.ForPhase("agent")
	require.NoError(t, err)
	require.Equal(t, []string{name}, plan.ToolNames())
	require.Empty(t, plan.SafeOutputNames())
	return plan
}

func readerParityArguments(op connectors.ReadTool, grants map[string][]string) map[string]any {
	args := map[string]any{}
	for name, input := range op.Inputs {
		switch {
		case name == "after":
			args[name] = "explicit-cursor-2"
		case name == "start-at":
			args[name] = 0
		case input.Type == "integer":
			args[name] = 3
		case op.Resources[name] != "":
			args[name] = grants[op.Resources[name]][0]
		}
	}
	return args
}

func readerParitySet(document map[string]any, pointer string, value any) {
	tokens := strings.Split(strings.TrimPrefix(pointer, "/"), "/")
	current := document
	for index, token := range tokens {
		if index == len(tokens)-1 {
			current[token] = value
			return
		}
		if tokens[index+1] == "*" {
			array, ok := current[token].([]any)
			if !ok || len(array) == 0 {
				array = []any{map[string]any{}}
				current[token] = array
			}
			rest := "/" + strings.Join(tokens[index+2:], "/")
			readerParitySet(array[0].(map[string]any), rest, value)
			return
		}
		child, ok := current[token].(map[string]any)
		if !ok {
			child = map[string]any{}
			current[token] = child
		}
		current = child
	}
}

func readerParityReplace(document any, pointer string, value any) any {
	if pointer == "" {
		return value
	}
	readerParitySet(document.(map[string]any), pointer, value)
	return document
}

func readerParityAppendMismatch(t *testing.T, document any, pointer string, value any) any {
	t.Helper()
	prefix, suffix, _ := strings.Cut(pointer, "/*")
	current := document
	for token := range strings.SplitSeq(strings.TrimPrefix(prefix, "/"), "/") {
		current = current.(map[string]any)[token]
	}
	items := current.([]any)
	encoded, err := json.Marshal(items[0])
	require.NoError(t, err)
	var item map[string]any
	require.NoError(t, json.Unmarshal(encoded, &item))
	readerParitySet(item, suffix, value)
	return readerParityReplace(document, prefix, append(items, item))
}

func readerParityFixture(op connectors.ReadTool, args map[string]any, grants map[string][]string) any {
	var response any = map[string]any{}
	for pointer := range op.ResponseLimits {
		response = readerParityReplace(response, pointer, []any{})
	}
	for pointer, input := range op.ResponseInputs {
		response = readerParityReplace(response, pointer, args[input])
	}
	for pointer, resource := range op.ResponseResources {
		response = readerParityReplace(response, pointer, grants[resource][0])
	}
	for pointer, input := range op.ResponseContains {
		response = readerParityReplace(response, pointer, args[input])
	}
	for pointer, expected := range op.ResponseChecks {
		response = readerParityReplace(response, pointer, expected)
	}
	for name, pointer := range op.Project {
		if name == "total" {
			response = readerParityReplace(response, pointer, 1)
		}
	}
	if op.Tool == "get-issue-remote-links" {
		response = []any{map[string]any{
			"id": 10000, "globalId": "system=https://tracker.example&id=1",
			"object": map[string]any{"title": "External evidence", "url": "https://tracker.example/1"},
		}}
	}
	if op.Tool == "lookup-account-id" {
		response = []any{map[string]any{"accountId": "account-alice", "displayName": "Alice"}}
	}
	return response
}

func executeReaderParity(t *testing.T, plan *connectors.Plan, name string, args map[string]any, response any) (*connectors.Result, int, error) {
	t.Helper()
	calls := 0
	encoded, err := json.Marshal(response)
	require.NoError(t, err)
	result, executionErr := plan.Execute(context.Background(), connectors.Runtime{
		Phase: "agent", AllowedOrigins: plan.Origins(),
		ResolveSecret: func(_ context.Context, phase, secret string) (string, error) {
			require.Equal(t, "agent", phase)
			require.NotContains(t, secret, "WRITE")
			return "fixture-read-only", nil
		},
		Client: &http.Client{Transport: connectorSmokeTransport(func(request *http.Request) (*http.Response, error) {
			calls++
			require.NotEmpty(t, request.Header.Get("Authorization"))
			require.Contains(t, []string{"GET", "POST"}, request.Method)
			require.NotContains(t, request.URL.String(), "mcp.")
			if expected := map[string]string{
				"get-issue-remote-links":    "/rest/api/3/issue/APP-1/remotelink",
				"lookup-account-id":         "/rest/api/3/user/search",
				"get-issue-link-types":      "/rest/api/3/issueLinkType",
				"get-transitions-for-issue": "/rest/api/3/issue/APP-1/transitions",
				"get-project-issue-types":   "/rest/api/3/issue/createmeta/APP/issuetypes",
				"get-issue-type-fields":     "/rest/api/3/issue/createmeta/APP/issuetypes/10001",
				"get-visible-projects":      "/rest/api/3/project/search",
			}[name]; expected != "" {
				require.Equal(t, http.MethodGet, request.Method)
				require.Equal(t, expected, request.URL.Path)
				if name == "lookup-account-id" {
					require.Equal(t, "Alice", request.URL.Query().Get("query"))
					require.Empty(t, request.URL.Query().Get("jql"))
				}
				if name == "get-visible-projects" {
					require.Equal(t, "APP", request.URL.Query().Get("keys"))
					require.Equal(t, "browse", request.URL.Query().Get("action"))
				}
			}
			if request.Method == http.MethodPost {
				body, err := io.ReadAll(request.Body)
				require.NoError(t, err)
				var document map[string]any
				require.NoError(t, json.Unmarshal(body, &document))
				require.True(t, strings.HasPrefix(document["query"].(string), "query "))
				require.NotContains(t, document["query"], "mutation")
				variables := document["variables"].(map[string]any)
				if first, present := args["first"]; present {
					require.EqualValues(t, first, variables["first"])
					require.Equal(t, args["after"], variables["after"])
				}
			} else if maximum, present := args["max-results"]; present {
				require.Equal(t, strconv.Itoa(maximum.(int)), request.URL.Query().Get("maxResults"))
				require.Equal(t, "0", request.URL.Query().Get("startAt"))
			}
			return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(string(encoded)))}, nil
		})},
	}, "mcp", name, args)
	return result, calls, executionErr
}

func TestProviderReaderParityDeclarationsAndGoRuntime(t *testing.T) {
	for _, provider := range []string{"linear", "jira"} {
		t.Run(provider, func(t *testing.T) {
			service, declaration, grants := readerParityService(t, provider)
			for name, op := range declaration.Tools {
				if slices.Contains([]string{"get-issue", "resolve-team", "resolve-project"}, name) {
					continue
				}
				t.Run(name, func(t *testing.T) {
					plan := readerParityPlan(t, service, name)
					args := readerParityArguments(op, grants)
					response := readerParityFixture(op, args, grants)
					result, calls, err := executeReaderParity(t, plan, name, args, response)
					require.NoError(t, err)
					require.NotEmpty(t, result.Data)
					require.Equal(t, 1, calls, "a cursor never starts automatic pagination")
					for pointer, input := range op.ResponseInputs {
						if strings.Contains(pointer, "/*/") {
							mismatch := "different-returned-parent"
							if approved := grants[op.Resources[input]]; len(approved) > 1 {
								mismatch = approved[1]
							}
							bad := readerParityAppendMismatch(t, readerParityFixture(op, args, grants), pointer, mismatch)
							_, _, err = executeReaderParity(t, plan, name, args, bad)
							require.Error(t, err, "even an independently approved second parent must exactly match at %s", pointer)
							continue
						}
						bad := readerParityFixture(op, args, grants)
						if input == "organization-id" {
							bad = readerParityReplace(bad, pointer, readerOtherOrganization)
						} else {
							bad = readerParityReplace(bad, pointer, "different-request-identity")
						}
						_, _, err = executeReaderParity(t, plan, name, args, bad)
						require.Error(t, err, "exact request identity is required at %s", pointer)
					}
					for pointer := range op.ResponseLimits {
						empty := readerParityFixture(op, args, grants)
						empty = readerParityReplace(empty, pointer, []any{})
						_, _, err = executeReaderParity(t, plan, name, args, empty)
						requiresMembership := false
						for membership := range op.ResponseContains {
							requiresMembership = requiresMembership || strings.HasPrefix(membership, pointer+"/*/")
						}
						if requiresMembership {
							require.Error(t, err, "empty team membership is not authority")
						} else {
							require.NoError(t, err, "an empty bounded page is a valid result")
						}
						bad := readerParityFixture(op, args, grants)
						items := make([]any, 26)
						for index := range items {
							items[index] = map[string]any{"id": "unapproved"}
						}
						bad = readerParityReplace(bad, pointer, items)
						_, _, err = executeReaderParity(t, plan, name, args, bad)
						require.Error(t, err, "oversize collection must fail at %s", pointer)
					}
					badArgs := readerParityArguments(op, grants)
					badArgs["fields"] = "*"
					_, calls, err = executeReaderParity(t, plan, name, badArgs, response)
					require.Error(t, err)
					require.Zero(t, calls, "unknown fields are rejected before credentials/network")
					for _, field := range []string{"first", "max-results"} {
						if _, exists := args[field]; exists {
							oversize := readerParityArguments(op, grants)
							oversize[field] = 26
							_, calls, err = executeReaderParity(t, plan, name, oversize, response)
							require.Error(t, err)
							require.Zero(t, calls)
						}
					}
					if _, exists := args["query"]; exists {
						unapproved := readerParityArguments(op, grants)
						unapproved["query"] = "unapproved query"
						_, calls, err = executeReaderParity(t, plan, name, unapproved, response)
						require.Error(t, err)
						require.Zero(t, calls, "a parent grant never grants an arbitrary query")
					}
				})
			}
		})
	}
}

func TestProviderReaderParityBundledNode(t *testing.T) {
	node, err := exec.LookPath("node")
	require.NoError(t, err)
	runtime, err := filepath.Abs("../../actions/setup/js/connector_runtime.cjs")
	require.NoError(t, err)
	for _, provider := range []string{"linear", "jira"} {
		service, declaration, grants := readerParityService(t, provider)
		for name, op := range declaration.Tools {
			if slices.Contains([]string{"get-issue", "resolve-team", "resolve-project"}, name) {
				continue
			}
			t.Run(provider+"/"+name, func(t *testing.T) {
				plan := readerParityPlan(t, service, name)
				encoded, err := plan.Encode()
				require.NoError(t, err)
				args := readerParityArguments(op, grants)
				response := readerParityFixture(op, args, grants)
				cases := []map[string]any{{"args": args, "response": response, "success": true, "calls": 1}}
				for pointer := range op.ResponseLimits {
					empty := readerParityReplace(readerParityFixture(op, args, grants), pointer, []any{})
					requiresMembership := false
					for membership := range op.ResponseContains {
						requiresMembership = requiresMembership || strings.HasPrefix(membership, pointer+"/*/")
					}
					cases = append(cases, map[string]any{"args": args, "response": empty, "success": !requiresMembership, "calls": 1})
					items := make([]any, 26)
					for index := range items {
						items[index] = map[string]any{"id": "unapproved"}
					}
					oversize := readerParityReplace(readerParityFixture(op, args, grants), pointer, items)
					cases = append(cases, map[string]any{"args": args, "response": oversize, "success": false, "calls": 1})
				}
				for pointer, field := range op.ResponseInputs {
					var mismatch any
					if strings.Contains(pointer, "/*/") {
						value := "different-returned-parent"
						if approved := grants[op.Resources[field]]; len(approved) > 1 {
							value = approved[1]
						}
						mismatch = readerParityAppendMismatch(t, readerParityFixture(op, args, grants), pointer, value)
					} else {
						mismatch = readerParityReplace(readerParityFixture(op, args, grants), pointer, "different-returned-parent")
					}
					cases = append(cases, map[string]any{"args": args, "response": mismatch, "success": false, "calls": 1})
				}
				unknown := readerParityArguments(op, grants)
				unknown["fields"] = "*"
				cases = append(cases, map[string]any{"args": unknown, "response": response, "success": false, "calls": 0})
				input, err := json.Marshal(map[string]any{"plan": encoded, "name": name, "cases": cases})
				require.NoError(t, err)
				command := exec.CommandContext(t.Context(), node, "-e", `
const { executeConnector } = require(process.argv[1]);
const input = JSON.parse(require("node:fs").readFileSync(0, "utf8"));
const env = new Proxy({}, { get: () => "fixture-read-only" });
(async () => {
  for (const test of input.cases) {
    let calls = 0, success = false;
    try {
      await executeConnector(input.plan, "mcp", input.name, test.args, {
        allowedOrigins: ["https://api.linear.app", "https://approved.atlassian.net"], env,
        fetchImpl: async (url, init) => {
          calls++;
          if (!["GET", "POST"].includes(init.method)) throw new Error("Unexpected mutation");
          return new Response(JSON.stringify(test.response), { status: 200 });
        }
      });
      success = true;
    } catch (error) {
      if (test.success) throw error;
    }
    if (success !== test.success || calls !== test.calls) throw new Error("Reader boundary case failed");
  }
  process.stdout.write("ok");
})().catch(error => { process.stderr.write(error.message); process.exitCode = 1; });
`, runtime)
				command.Stdin = strings.NewReader(string(input))
				output, err := command.CombinedOutput()
				require.NoError(t, err, "%s", output)
				require.Equal(t, "ok", string(output))
			})
		}
	}
}

func TestProviderReaderParityOfficialEvidenceAndUnsupportedPayloads(t *testing.T) {
	for provider, hash := range map[string]string{"linear": linearReaderSchemaSHA256, "jira": jiraReaderSchemaSHA256} {
		source, err := os.ReadFile(filepath.Join("../../.github/workflows/connectors", provider+".md"))
		require.NoError(t, err)
		require.Contains(t, string(source), hash)
		require.Contains(t, string(source), "<known-tool-coverage>")
	}
	_, linear, _ := readerParityService(t, "linear")
	for _, name := range []string{"extract-images", "get-attachment", "get-diff", "get-diff-threads", "list-diffs", "search-documentation"} {
		require.NotContains(t, linear.Tools, name, "metadata is not an unverified content capability")
	}
	for name, expected := range map[string][]string{
		"search-issues": {"searchIssues(teamId: $team, term: $term, first: $first, after: $after)", "team { id }"},
		"get-document":  {"document(id: $id)", "id title content url"},
		"list-comments": {"comments(first: $first, after: $after)", "id body url"},
	} {
		for _, text := range expected {
			require.Contains(t, linear.Tools[name].Request.GraphQL, text)
		}
	}

}

func TestProviderReaderParityDefaultsGrantNothing(t *testing.T) {
	for _, provider := range []string{"linear", "jira"} {
		service, _, _ := readerParityService(t, provider)
		plan, err := service.Select(connectors.Selection{})
		require.ErrorContains(t, err, "explicitly select")
		require.Nil(t, plan)
		source, err := os.ReadFile(filepath.Join("../../.github/workflows/connectors", provider+".md"))
		require.NoError(t, err)
		parsed, err := parser.ExtractFrontmatterFromContent(string(source))
		require.NoError(t, err)
		resources := []string{"documents", "initiatives", "milestones", "queries", "statuses", "users"}
		if provider == "jira" {
			resources = []string{"account-queries", "discovery-scopes", "issue-type-ids"}
		}
		for _, name := range resources {
			schema := parsed.Frontmatter["import-schema"].(map[string]any)[name].(map[string]any)
			require.Empty(t, schema["default"], "%s must not grant %s by default", provider, name)
		}
	}
}
