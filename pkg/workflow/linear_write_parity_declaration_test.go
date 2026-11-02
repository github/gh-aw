//go:build !integration

package workflow

import (
	"context"
	"encoding/json"
	"io"
	"maps"
	"net/http"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/connectors"
	"github.com/stretchr/testify/require"
)

const linearParityIssue = "22222222-2222-4222-8222-222222222222"
const linearParityTeam = "33333333-3333-4333-8333-333333333333"
const linearParityTitle = "A sufficiently long reviewed title"

func TestLinearWriteParitySparseDeclarations(t *testing.T) {
	compiler, data, path, err := parseLinearConnectorWorkflow(t, linearConnectorImport,
		"safe-outputs:\n  linear-update-issue:\n  linear-update-issue-title:\n"+
			"  linear-update-issue-body:\n  linear-clear-issue-body:\n")
	require.NoError(t, err)
	compiled, err := compiler.CompileToYAML(data, path)
	require.NoError(t, err)
	require.Contains(t, compiled, "linear_clear_issue_body")
	plan := data.Connectors["linear"].SafeOutputs
	require.Equal(t, []string{"LINEAR_WRITE_API_KEY"}, plan.Secrets())
	response := `{"data":{"issueUpdate":{"success":true,"issue":{"id":"` + linearParityIssue + `","identifier":"ENG-123","title":"Existing title"}}}}`
	for _, test := range []struct {
		name, input string
		fields      map[string]any
	}{
		{"update-issue-title", "input: { title: $title }", map[string]any{"title": linearParityTitle}},
		{"update-issue-body", "input: { description: $body }", map[string]any{"body": "Reviewed description"}},
		{"clear-issue-body", `input: { description: "" }`, map[string]any{}},
		{"update-issue", "input: { title: $title, description: $body }", map[string]any{"title": linearParityTitle, "body": "Reviewed description"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			args := maps.Clone(test.fields)
			args["issue-id"] = linearParityIssue
			intent, err := plan.PrepareSafeOutput(test.name, args)
			require.NoError(t, err)
			calls := 0
			runtime := parityProviderRuntime(t, "https://api.linear.app", "LINEAR_WRITE_API_KEY", func(request *http.Request) string {
				calls++
				require.Equal(t, "POST", request.Method)
				require.Equal(t, "/graphql", request.URL.Path)
				var document struct {
					Query     string         `json:"query"`
					Variables map[string]any `json:"variables"`
				}
				require.NoError(t, json.NewDecoder(request.Body).Decode(&document))
				require.Contains(t, document.Query, test.input)
				variables := maps.Clone(test.fields)
				variables["id"] = linearParityIssue
				require.Equal(t, variables, document.Variables, "untouched content must never be read or sent")
				return response
			})
			result, err := plan.ExecuteSafeOutput(t.Context(), runtime, *intent)
			require.NoError(t, err)
			require.True(t, result.Untrusted)
			require.Equal(t, 1, calls, "UUID updates perform only the mutation, never read-and-rewrite")
			for _, field := range []string{"body", "title"} {
				if _, exists := test.fields[field]; !exists {
					bad := maps.Clone(args)
					bad[field] = "Unselected field"
					_, err := plan.PrepareSafeOutput(test.name, bad)
					require.ErrorContains(t, err, "unknown connector argument")
				}
			}
			for _, badResponse := range []string{
				strings.Replace(response, `"success":true`, `"success":false`, 1),
				strings.Replace(response, linearParityIssue, "99999999-9999-4999-8999-999999999999", 1),
				strings.Replace(response, `"identifier":"ENG-123",`, "", 1),
			} {
				runtime := parityProviderRuntime(t, "https://api.linear.app", "LINEAR_WRITE_API_KEY", func(*http.Request) string { return badResponse })
				_, err := plan.ExecuteSafeOutput(t.Context(), runtime, *intent)
				require.Error(t, err, "success, canonical identity, and identifier evidence remain mandatory")
			}
		})
	}
	for _, args := range []map[string]any{
		{"issue-id": linearParityIssue, "title": "short"},
		{"issue-id": linearParityIssue},
		{"issue-id": "99999999-9999-4999-8999-999999999999", "title": linearParityTitle},
	} {
		_, err := plan.PrepareSafeOutput("update-issue-title", args)
		require.Error(t, err)
	}
	for _, body := range []string{"", " \n"} {
		_, err := plan.PrepareSafeOutput("update-issue-body", map[string]any{"issue-id": linearParityIssue, "body": body})
		require.Error(t, err, "body publication is deliberately distinct from clearing")
	}
}

func TestLinearWriteParityProjectlessCreation(t *testing.T) {
	imports := `imports:
  - uses: ".github/workflows/connectors/linear.md"
    with:
      teams: [` + linearParityTeam + `]
`
	_, data, _, err := parseLinearConnectorWorkflow(t, imports, "safe-outputs:\n  linear-create-issue-in-team:\n")
	require.NoError(t, err, "team-only creation requires neither projects nor project-slugs")
	plan := data.Connectors["linear"].SafeOutputs
	args := map[string]any{"team-id": linearParityTeam, "title": linearParityTitle, "body": "Reviewed description"}
	intent, err := plan.PrepareSafeOutput("create-issue-in-team", args)
	require.NoError(t, err)
	calls := 0
	response := `{"data":{"issueCreate":{"success":true,"issue":{"id":"` + linearParityIssue + `","identifier":"ENG-123","team":{"id":"` + linearParityTeam + `"}}}}}`
	runtime := parityProviderRuntime(t, "https://api.linear.app", "LINEAR_API_KEY", func(request *http.Request) string {
		calls++
		var document struct {
			Query     string         `json:"query"`
			Variables map[string]any `json:"variables"`
		}
		require.NoError(t, json.NewDecoder(request.Body).Decode(&document))
		require.NotContains(t, document.Query, "project")
		require.Equal(t, map[string]any{"team": linearParityTeam, "title": linearParityTitle, "body": "Reviewed description"}, document.Variables)
		return response
	})
	_, err = plan.ExecuteSafeOutput(t.Context(), runtime, *intent)
	require.NoError(t, err)
	require.Equal(t, 1, calls)
	bad := maps.Clone(args)
	bad["project-id"] = "44444444-4444-4444-8444-444444444444"
	_, err = plan.PrepareSafeOutput("create-issue-in-team", bad)
	require.ErrorContains(t, err, "unknown connector argument")
}

func TestLinearWriteParityAliasBoundaries(t *testing.T) {
	imports := strings.Replace(linearConnectorImport, "      teams:", "      team-references: [ENG]\n      issue-references: [ENG-123]\n      teams:", 1)
	_, data, _, err := parseLinearConnectorWorkflow(t, imports,
		"safe-outputs:\n  linear-create-issue-in-team:\n  linear-clear-issue-body:\n")
	require.NoError(t, err)
	plan := data.Connectors["linear"].SafeOutputs
	for _, operation := range []string{"create-issue-in-team", "clear-issue-body"} {
		t.Run(operation, func(t *testing.T) {
			args := map[string]any{"issue-id": "ENG-123"}
			lookup := `{"data":{"organization":{"id":"11111111-1111-4111-8111-111111111111"},"issue":{"id":"` + linearParityIssue + `","identifier":"ENG-123","team":{"id":"` + linearParityTeam + `"},"project":{"id":"44444444-4444-4444-8444-444444444444"}}}}`
			mutation := `{"data":{"issueUpdate":{"success":true,"issue":{"id":"` + linearParityIssue + `","identifier":"ENG-123"}}}}`
			if operation == "create-issue-in-team" {
				args = map[string]any{"team-id": "ENG", "title": linearParityTitle, "body": "Reviewed description"}
				lookup = `{"data":{"organization":{"id":"11111111-1111-4111-8111-111111111111"},"teams":{"nodes":[{"id":"` + linearParityTeam + `","key":"ENG","name":"Engineering"}],"pageInfo":{"hasNextPage":false}}}}`
				mutation = `{"data":{"issueCreate":{"success":true,"issue":{"id":"` + linearParityIssue + `","identifier":"ENG-123","team":{"id":"` + linearParityTeam + `"}}}}}`
			}
			intent, err := plan.PrepareSafeOutputIntent(operation, args)
			require.NoError(t, err)
			require.NotEmpty(t, intent.UnresolvedInputs)
			lookups := []string{
				lookup,
				strings.ReplaceAll(lookup, linearParityTeam, "99999999-9999-4999-8999-999999999999"),
				strings.ReplaceAll(lookup, "11111111-1111-4111-8111-111111111111", "99999999-9999-4999-8999-999999999999"),
			}
			if operation == "create-issue-in-team" {
				lookups = append(lookups,
					strings.Replace(lookup, `"hasNextPage":false`, `"hasNextPage":true`, 1),
					strings.Replace(lookup, `"key":"ENG"`, `"key":"OTHER"`, 1),
					strings.Replace(lookup, `],"pageInfo"`, `,{"id":"`+linearParityTeam+`","key":"OTHER","name":"ENG"}],"pageInfo"`, 1),
				)
			}
			for index, evidence := range lookups {
				calls := 0
				runtime := parityProviderRuntime(t, "https://api.linear.app", "LINEAR_WRITE_API_KEY", func(*http.Request) string {
					calls++
					if calls == 1 {
						return evidence
					}
					return mutation
				})
				_, err = plan.ExecuteSafeOutput(t.Context(), runtime, *intent)
				if index != 0 {
					require.Error(t, err)
					require.Equal(t, 1, calls, "unapproved lookup evidence must prevent the mutation")
				} else {
					require.NoError(t, err)
					require.Equal(t, 2, calls)
				}
			}
		})
	}
}

func TestLinearWriteParityExactResponseIdentity(t *testing.T) {
	const anotherIssue = "99999999-9999-4999-8999-999999999999"
	const anotherTeam = "88888888-8888-4888-8888-888888888888"
	imports := strings.Replace(linearConnectorImport, "issues: ["+linearParityIssue+"]", "issues: ["+linearParityIssue+", "+anotherIssue+"]", 1)
	imports = strings.Replace(imports, "teams: ["+linearParityTeam+"]", "teams: ["+linearParityTeam+", "+anotherTeam+"]", 1)
	_, data, _, err := parseLinearConnectorWorkflow(t, imports,
		"safe-outputs:\n  linear-update-issue-title:\n  linear-update-issue-body:\n  linear-clear-issue-body:\n  linear-create-issue-in-team:\n")
	require.NoError(t, err)
	plan := data.Connectors["linear"].SafeOutputs
	for _, test := range []struct {
		name, response string
		args           map[string]any
	}{
		{"update-issue-title", anotherIssue, map[string]any{"issue-id": linearParityIssue, "title": linearParityTitle}},
		{"update-issue-body", anotherIssue, map[string]any{"issue-id": linearParityIssue, "body": "Reviewed body"}},
		{"clear-issue-body", anotherIssue, map[string]any{"issue-id": linearParityIssue}},
		{"create-issue-in-team", anotherTeam, map[string]any{"team-id": linearParityTeam, "title": linearParityTitle, "body": "Reviewed body"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			intent, err := plan.PrepareSafeOutput(test.name, test.args)
			require.NoError(t, err)
			response := `{"data":{"issueUpdate":{"success":true,"issue":{"id":"` + test.response + `","identifier":"ENG-123"}}}}`
			if test.name == "create-issue-in-team" {
				response = `{"data":{"issueCreate":{"success":true,"issue":{"id":"` + linearParityIssue + `","identifier":"ENG-123","team":{"id":"` + test.response + `"}}}}}`
			}
			runtime := parityProviderRuntime(t, "https://api.linear.app", "LINEAR_WRITE_API_KEY", func(*http.Request) string { return response })
			_, err = plan.ExecuteSafeOutput(t.Context(), runtime, *intent)
			require.ErrorContains(t, err, "does not match", "membership alone cannot correlate a different approved resource")
		})
	}
}

func parityProviderRuntime(t *testing.T, origin, secret string, response func(*http.Request) string) connectors.Runtime {
	t.Helper()
	return connectors.Runtime{
		Phase: "safe-output", AllowWrites: true, AllowedOrigins: []string{origin},
		ResolveSecret: func(_ context.Context, phase, name string) (string, error) {
			require.Equal(t, "safe-output", phase)
			require.Equal(t, secret, name)
			return "fixture-writer", nil
		},
		Client: &http.Client{Transport: connectorSmokeTransport(func(request *http.Request) (*http.Response, error) {
			expected := "fixture-writer"
			if strings.Contains(origin, "atlassian.net") {
				expected = "Basic " + expected
			}
			require.Equal(t, expected, request.Header.Get("Authorization"))
			body := response(request)
			status := http.StatusOK
			if body == "" {
				status = http.StatusNoContent
			}
			return &http.Response{StatusCode: status, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body)), Request: request}, nil
		})},
	}
}
