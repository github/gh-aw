//go:build !integration

package workflow

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/connectors"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"
)

type gitLabTransport func(*http.Request) (*http.Response, error)

func (transport gitLabTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	return transport(request)
}

func gitLabRuntimeFixture(t *testing.T, phase string, transport gitLabTransport) connectors.Runtime {
	t.Helper()
	return connectors.Runtime{
		Phase: phase, AllowWrites: phase == "safe-output", AllowedOrigins: []string{"https://gitlab.com"},
		ResolveSecret: func(_ context.Context, actualPhase, name string) (string, error) {
			require.Equal(t, phase, actualPhase)
			expected := "APPROVED_GITLAB_READER"
			if phase == "safe-output" {
				expected = "APPROVED_GITLAB_WRITER"
			}
			require.Equal(t, expected, name)
			return "fixture-" + phase, nil
		},
		Client: &http.Client{Transport: transport},
	}
}

func gitLabResponse(request *http.Request, status int, body string) *http.Response {
	return &http.Response{StatusCode: status, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body)), Request: request}
}

func gitLabResponseFixture(t *testing.T, name string) string {
	t.Helper()
	body, err := os.ReadFile(gitLabFixture + name + ".json")
	require.NoError(t, err)
	return string(body)
}

func TestGitLabConnectorRESTReadsAndStagedNote(t *testing.T) {
	_, data, _, err := parseGitLabConsumer(t, gitLabConsumerSource(t,
		"tools:\n  gitlab: [get-merge-request, get-pipeline]\nsafe-outputs:\n  gitlab-add-merge-request-note:\n"))
	require.NoError(t, err)
	for _, test := range []struct {
		name, phase, capability, fixture, path, method string
		args                                           map[string]any
		fields                                         []string
	}{
		{"get-merge-request", "agent", "mcp", "merge-request", "/api/v4/projects/42/merge_requests/7", "GET",
			map[string]any{"project": "42", "merge-request": "7"}, []string{"id", "project", "merge-request", "title", "state", "draft"}},
		{"get-pipeline", "agent", "mcp", "pipeline", "/api/v4/projects/42/pipelines/900", "GET",
			map[string]any{"project": "42", "pipeline": "900"}, []string{"pipeline", "project", "status", "sha", "ref", "source"}},
		{"add-merge-request-note", "safe-output", "safe-output", "note", "/api/v4/projects/42/merge_requests/7/notes", "POST",
			map[string]any{"project": "42", "merge-request": "7", "body": "Coordination review requested."}, []string{"note", "project", "merge-request"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			plan := data.Connectors["gitlab"].Agent
			if test.phase == "safe-output" {
				plan = data.Connectors["gitlab"].SafeOutputs
			}
			calls, secrets := 0, 0
			runtime := gitLabRuntimeFixture(t, test.phase, func(request *http.Request) (*http.Response, error) {
				calls++
				require.Equal(t, test.method, request.Method)
				require.Equal(t, "https://gitlab.com"+test.path, request.URL.String())
				require.Equal(t, "fixture-"+test.phase, request.Header.Get("PRIVATE-TOKEN"))
				require.Empty(t, request.Header.Get("Authorization"))
				require.Empty(t, request.Header.Get("JOB-TOKEN"))
				require.Empty(t, request.URL.RawQuery)
				if test.phase == "safe-output" {
					require.Equal(t, "application/json", request.Header.Get("Content-Type"))
					body, err := io.ReadAll(request.Body)
					require.NoError(t, err)
					require.JSONEq(t, `{"body":"Coordination review requested."}`, string(body))
				}
				return gitLabResponse(request, http.StatusOK, gitLabResponseFixture(t, test.fixture)), nil
			})
			resolve := runtime.ResolveSecret
			runtime.ResolveSecret = func(ctx context.Context, phase, name string) (string, error) {
				secrets++
				return resolve(ctx, phase, name)
			}
			var result *connectors.Result
			if test.phase == "safe-output" {
				request, err := plan.PrepareSafeOutput(test.name, test.args)
				require.NoError(t, err)
				require.Zero(t, calls)
				require.Zero(t, secrets)
				result, err = plan.ExecuteSafeOutput(t.Context(), runtime, *request)
				require.NoError(t, err)
			} else {
				result, err = plan.Execute(t.Context(), runtime, test.capability, test.name, test.args)
				require.NoError(t, err)
			}
			require.True(t, result.Untrusted)
			var projected map[string]any
			require.NoError(t, json.Unmarshal(result.Data, &projected))
			require.Len(t, projected, len(test.fields))
			for _, field := range test.fields {
				require.Contains(t, projected, field)
			}
			for _, private := range []string{"description", "author", "web_url", "head_pipeline", "user", "variables", "detailed_status", "body", "internal"} {
				require.NotContains(t, projected, private)
			}
			require.Equal(t, 1, calls)
			require.Equal(t, 1, secrets)
		})
	}
}

func TestGitLabConnectorRejectsArgumentsAndAuthorityBeforeCredentials(t *testing.T) {
	_, data, _, err := parseGitLabConsumer(t, gitLabConsumerSource(t,
		"tools:\n  gitlab: [get-merge-request, get-pipeline]\nsafe-outputs:\n  gitlab-add-merge-request-note:\n"))
	require.NoError(t, err)
	read, write := data.Connectors["gitlab"].Agent, data.Connectors["gitlab"].SafeOutputs
	calls, secrets := 0, 0
	runtime := gitLabRuntimeFixture(t, "safe-output", func(*http.Request) (*http.Response, error) {
		calls++
		return nil, errors.New("unexpected network")
	})
	runtime.ResolveSecret = func(context.Context, string, string) (string, error) {
		secrets++
		return "", errors.New("unexpected secret")
	}
	for index, change := range []struct {
		field string
		value any
	}{
		{"project", "43"}, {"project", "example/approved"}, {"project", "../42"},
		{"merge-request", "8"}, {"merge-request", "123"}, {"merge-request", 7},
		{"merge-request", "0"}, {"merge-request", "07"}, {"merge-request", "7%2fnotes"},
		{"merge-request", strings.Repeat("7", 17)},
		{"body", ""}, {"body", strings.Repeat("a", 2001)}, {"body", "/merge"},
		{"body", "Text\n/close"}, {"body", "Text\r/close"}, {"body", "Text\r\n/close"},
		{"body", "Text\n /close"}, {"body", "Text\n\t/merge"}, {"body", "Text\n   /label"},
		{"body", "Text\r\n /close"}, {"body", "Text\r\n\t/merge"}, {"body", "Text\r\n   /label"},
		{"internal", true}, {"created_at", "2020-01-01"}, {"merge_request_diff_head_sha", "a"},
		{"url", "https://other.invalid"}, {"host", "other.invalid"}, {"temporary_id", "#aw_new"},
	} {
		t.Run(fmt.Sprintf("%02d-%s", index, change.field), func(t *testing.T) {
			args := map[string]any{"project": "42", "merge-request": "7", "body": "Coordination note."}
			args[change.field] = change.value
			_, err := write.PrepareSafeOutput("add-merge-request-note", args)
			require.Error(t, err)
			_, err = write.Execute(t.Context(), runtime, "safe-output", "add-merge-request-note", args)
			require.Error(t, err)
		})
	}
	valid := map[string]any{"project": "42", "merge-request": "7", "body": "A" + strings.Repeat("x", 1999)}
	_, err = write.PrepareSafeOutput("add-merge-request-note", valid)
	require.NoError(t, err, "exact 2000-character limit must be accepted")
	_, err = write.PrepareSafeOutput("add-merge-request-note", map[string]any{
		"project": "42", "merge-request": "7", "body": "Coordination note.\n\n> Generated by [Workflow](https://github.com/example/repo/actions/runs/42)\n\n<!-- gh-aw-agentic-workflow: Workflow -->",
	})
	require.NoError(t, err, "safe multiline generated attribution must fit the declared pattern")
	_, err = write.Execute(t.Context(), runtime, "safe-output", "merge-request", valid)
	require.Error(t, err)
	for _, altered := range []connectors.Runtime{
		{Phase: "agent", AllowWrites: true, AllowedOrigins: runtime.AllowedOrigins, ResolveSecret: runtime.ResolveSecret, Client: runtime.Client},
		{Phase: "safe-output", AllowWrites: false, AllowedOrigins: runtime.AllowedOrigins, ResolveSecret: runtime.ResolveSecret, Client: runtime.Client},
		{Phase: "safe-output", AllowWrites: true, AllowedOrigins: []string{"https://other.invalid"}, ResolveSecret: runtime.ResolveSecret, Client: runtime.Client},
	} {
		_, err := write.Execute(t.Context(), altered, "safe-output", "add-merge-request-note", valid)
		require.Error(t, err)
	}
	runtime.Phase = "agent"
	for _, args := range []map[string]any{
		{"project": "43", "pipeline": "900"}, {"project": "42", "pipeline": "901"},
		{"project": "42", "pipeline": "15"}, {"project": "42", "pipeline": "latest"},
		{"project": "42", "pipeline": "900", "variables": true},
	} {
		_, err := read.Execute(t.Context(), runtime, "mcp", "get-pipeline", args)
		require.Error(t, err)
	}
	_, err = read.Execute(t.Context(), runtime, "safe-output", "add-merge-request-note", valid)
	require.Error(t, err)
	require.Zero(t, calls)
	require.Zero(t, secrets)
}

func TestGitLabConnectorProviderErrorsAreNotRetried(t *testing.T) {
	source := strings.NewReplacer(
		`projects: ["42"]`, `projects: ["42", "43"]`,
		`merge-requests: ["7"]`, `merge-requests: ["7", "8"]`,
		`pipelines: ["900"]`, `pipelines: ["900", "901"]`,
	).Replace(gitLabConsumerSource(t,
		"tools:\n  gitlab: [get-merge-request, get-pipeline]\nsafe-outputs:\n  gitlab-add-merge-request-note:\n"))
	_, data, _, err := parseGitLabConsumer(t, source)
	require.NoError(t, err)
	for _, operation := range []struct {
		name, fixture, capability, phase string
		plan                             *connectors.Plan
		args                             map[string]any
	}{
		{"get-merge-request", "merge-request", "mcp", "agent", data.Connectors["gitlab"].Agent, map[string]any{"project": "42", "merge-request": "7"}},
		{"get-pipeline", "pipeline", "mcp", "agent", data.Connectors["gitlab"].Agent, map[string]any{"project": "42", "pipeline": "900"}},
		{"add-merge-request-note", "note", "safe-output", "safe-output", data.Connectors["gitlab"].SafeOutputs, map[string]any{"project": "42", "merge-request": "7", "body": "Coordination note."}},
	} {
		body := gitLabResponseFixture(t, operation.fixture)
		for _, failure := range []struct {
			name   string
			status int
			body   string
		}{
			{"expired", 401, `{"message":"Unauthorized"}`},
			{"denied", 403, `{"message":"Forbidden"}`},
			{"missing", 404, `{"message":"Not Found"}`},
			{"rate-limit", 429, `{"message":"Rate limited"}`},
			{"server", 503, `{"message":"Unavailable"}`},
			{"redirect", 302, `{}`},
			{"empty", 204, ""},
			{"malformed", 200, "not JSON"},
			{"oversized", 200, strings.Repeat("x", 65537)},
			{"missing-evidence", 200, `{}`},
			{"wrong-project", 200, strings.Replace(body, `"project_id": 42`, `"project_id": 43`, 1)},
			{"wrong-project-type", 200, strings.Replace(body, `"project_id": 42`, `"project_id": "42"`, 1)},
			{"wrong-child", 200, strings.ReplaceAll(strings.ReplaceAll(strings.ReplaceAll(body,
				`"iid": 7`, `"iid": 8`), `"id": 900`, `"id": 901`), `"noteable_iid": 7`, `"noteable_iid": 8`)}, //nolint:misspell // GitLab's wire field is noteable_iid.
		} {
			t.Run(operation.name+"/"+failure.name, func(t *testing.T) {
				calls := 0
				runtime := gitLabRuntimeFixture(t, operation.phase, func(request *http.Request) (*http.Response, error) {
					calls++
					response := gitLabResponse(request, failure.status, failure.body)
					response.Header.Set("Location", "https://other.invalid/must-not-follow")
					response.Header.Set("Retry-After", "1")
					return response, nil
				})
				_, err := operation.plan.Execute(t.Context(), runtime, operation.capability, operation.name, operation.args)
				require.Error(t, err)
				require.Equal(t, 1, calls)
			})
		}
	}
}

func TestGitLabConnectorNoteResponseAndTransportFailures(t *testing.T) {
	_, data, _, err := parseGitLabConsumer(t, gitLabConsumerSource(t, "safe-outputs:\n  gitlab-add-merge-request-note:\n"))
	require.NoError(t, err)
	plan := data.Connectors["gitlab"].SafeOutputs
	body := gitLabResponseFixture(t, "note")
	for _, responseBody := range []string{
		strings.Replace(body, `"noteable_type": "MergeRequest"`, `"noteable_type": "Issue"`, 1), //nolint:misspell // GitLab's wire field is noteable_type.
		strings.Replace(body, `"system": false`, `"system": true`, 1),
		strings.Replace(body, `"id": 301`, `"id": "301"`, 1),
	} {
		calls := 0
		runtime := gitLabRuntimeFixture(t, "safe-output", func(request *http.Request) (*http.Response, error) {
			calls++
			return gitLabResponse(request, 200, responseBody), nil
		})
		_, err := plan.Execute(t.Context(), runtime, "safe-output", "add-merge-request-note",
			map[string]any{"project": "42", "merge-request": "7", "body": "Coordination note."})
		require.Error(t, err)
		require.Equal(t, 1, calls, "a write may already have occurred; validation failures must never retry")
	}
	calls := 0
	runtime := gitLabRuntimeFixture(t, "safe-output", func(*http.Request) (*http.Response, error) {
		calls++
		return nil, context.DeadlineExceeded
	})
	_, err = plan.Execute(t.Context(), runtime, "safe-output", "add-merge-request-note",
		map[string]any{"project": "42", "merge-request": "7", "body": "Coordination note."})
	require.ErrorContains(t, err, "outcome may be unknown, no retry performed")
	require.Equal(t, 1, calls)
}

func TestGitLabConnectorMCPNamesAndNoCredentialFallback(t *testing.T) {
	_, data, _, err := parseGitLabConsumer(t, gitLabConsumerSource(t, "tools:\n  gitlab: [get-merge-request, get-pipeline]\n"))
	require.NoError(t, err)
	plan := data.Connectors["gitlab"].Agent
	require.Equal(t, []string{"gitlab_get_merge_request", "gitlab_get_pipeline"}, plan.ToolNames())
	calls := 0
	runtime := gitLabRuntimeFixture(t, "agent", func(request *http.Request) (*http.Response, error) {
		calls++
		require.Equal(t, "/api/v4/projects/42/pipelines/900", request.URL.Path)
		return gitLabResponse(request, 200, gitLabResponseFixture(t, "pipeline")), nil
	})
	server, err := plan.MCPServer(runtime)
	require.NoError(t, err)
	clientTransport, serverTransport := mcp.NewInMemoryTransports()
	serverSession, err := server.Connect(t.Context(), serverTransport, nil)
	require.NoError(t, err)
	defer serverSession.Close()
	client := mcp.NewClient(&mcp.Implementation{Name: "gitlab-fixture", Version: "1"}, nil)
	session, err := client.Connect(t.Context(), clientTransport, nil)
	require.NoError(t, err)
	defer session.Close()
	listed, err := session.ListTools(t.Context(), &mcp.ListToolsParams{})
	require.NoError(t, err)
	require.Len(t, listed.Tools, 2)
	invalid, err := session.CallTool(t.Context(), &mcp.CallToolParams{
		Name: "gitlab_get_pipeline", Arguments: map[string]any{"project": "42", "pipeline": "901"},
	})
	require.NoError(t, err)
	require.True(t, invalid.IsError)
	require.Zero(t, calls)
	result, err := session.CallTool(t.Context(), &mcp.CallToolParams{
		Name: "gitlab_get_pipeline", Arguments: map[string]any{"project": "42", "pipeline": "900"},
	})
	require.NoError(t, err)
	require.False(t, result.IsError)
	require.Contains(t, result.Content[0].(*mcp.TextContent).Text, `"untrusted":true`)
	require.Equal(t, 1, calls)

	t.Setenv("GITLAB_TOKEN", "must-not-use")
	t.Setenv("CI_JOB_TOKEN", "must-not-use")
	secrets := 0
	runtime.ResolveSecret = func(context.Context, string, string) (string, error) {
		secrets++
		return "", errors.New("approved token unavailable")
	}
	_, err = plan.Execute(t.Context(), runtime, "mcp", "get-pipeline", map[string]any{"project": "42", "pipeline": "900"})
	require.Error(t, err)
	require.Equal(t, 1, secrets)
	require.Equal(t, 1, calls, "credential resolution failure must not try another token or network request")
}
