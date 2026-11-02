//go:build !integration

package workflow

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/connectors"
	"github.com/stretchr/testify/require"
)

func notionResponseFixture(t *testing.T, name string) string {
	t.Helper()
	data, err := os.ReadFile("testdata/notion/" + name + ".json")
	require.NoError(t, err)
	return string(data)
}

func notionTestRuntime(t *testing.T, phase, response string, status int, calls, secrets *int) connectors.Runtime {
	t.Helper()
	return connectors.Runtime{
		Phase: phase, AllowWrites: phase == "safe-output",
		AllowedOrigins: []string{"https://api.notion.com"},
		ResolveSecret: func(_ context.Context, requestedPhase, name string) (string, error) {
			*secrets++
			require.Equal(t, phase, requestedPhase)
			expected := "NOTION_READ_TOKEN"
			if phase == "safe-output" {
				expected = "NOTION_COMMENT_WRITE_TOKEN"
			}
			require.Equal(t, expected, name)
			return "fixture-not-a-real-token", nil
		},
		Client: &http.Client{Transport: connectorSmokeTransport(func(request *http.Request) (*http.Response, error) {
			*calls++
			require.Equal(t, "api.notion.com", request.URL.Host)
			require.Equal(t, "Bearer fixture-not-a-real-token", request.Header.Get("Authorization"))
			require.Equal(t, "2026-03-11", request.Header.Get("Notion-Version"))
			require.Equal(t, "application/json", request.Header.Get("Content-Type"))
			if phase == "agent" {
				require.Equal(t, "GET", request.Method)
				require.Equal(t, "/v1/pages/"+notionPageID, request.URL.Path)
				require.Equal(t, "title", request.URL.Query().Get("filter_properties"))
			} else {
				require.Equal(t, "POST", request.Method)
				require.Equal(t, "/v1/comments", request.URL.Path)
				body, err := io.ReadAll(request.Body)
				require.NoError(t, err)
				require.JSONEq(t, `{"parent":{"page_id":"`+notionPageID+`"},"rich_text":[{"type":"text","text":{"content":"Release reference reviewed."}}]}`, string(body))
			}
			return &http.Response{StatusCode: status, Header: http.Header{"Retry-After": {"10"}},
				Body: io.NopCloser(strings.NewReader(response)), Request: request}, nil
		})},
	}
}

func TestNotionConnectorFrozenExecution(t *testing.T) {
	read, write := notionFrozenPlans(t)
	calls, secrets := 0, 0
	result, err := read.Execute(t.Context(), notionTestRuntime(t, "agent", notionResponseFixture(t, "page"), 200, &calls, &secrets),
		"mcp", "get-page", map[string]any{"page-id": notionPageID})
	require.NoError(t, err)
	require.True(t, result.Untrusted)
	require.JSONEq(t, `{"page-id":"`+notionPageID+`","url":"https://app.notion.com/p/11111111222243338444555555555555","last-edited-time":"2026-03-12T00:00:00.000Z"}`, string(result.Data))
	staged, err := write.PrepareSafeOutput("add-comment", map[string]any{"page-id": notionPageID, "text": "Release reference reviewed."})
	require.NoError(t, err)
	require.Equal(t, 1, calls, "staging cannot perform a request")
	require.Equal(t, 1, secrets, "staging cannot resolve any secret")
	result, err = write.ExecuteSafeOutput(t.Context(), notionTestRuntime(t, "safe-output", notionResponseFixture(t, "comment"), 200, &calls, &secrets), *staged)
	require.NoError(t, err)
	require.True(t, result.Untrusted)
	require.JSONEq(t, `{"page-id":"`+notionPageID+`","comment-id":"22222222-3333-4444-8555-666666666666","discussion-id":"33333333-4444-4555-8666-777777777777","created-time":"2026-03-12T00:00:00.000Z"}`, string(result.Data))
	require.Equal(t, 2, calls)
	require.Equal(t, 2, secrets)
}

func TestNotionConnectorRejectsInputsBeforeCredentials(t *testing.T) {
	read, write := notionFrozenPlans(t)
	calls, secrets := 0, 0
	readRuntime := notionTestRuntime(t, "agent", "{}", 200, &calls, &secrets)
	writeRuntime := notionTestRuntime(t, "safe-output", "{}", 200, &calls, &secrets)
	for _, page := range []any{"", "*", "11111111222243338444555555555555", strings.ToUpper(notionPageID + "a"), "11111111-2222-4333-8444-555555555556", "../comments", 42} {
		_, err := read.Execute(t.Context(), readRuntime, "mcp", "get-page", map[string]any{"page-id": page})
		require.Error(t, err)
		_, err = write.PrepareSafeOutput("add-comment", map[string]any{"page-id": page, "text": "text"})
		require.Error(t, err)
	}
	for _, args := range []map[string]any{
		{}, {"page-id": notionPageID}, {"page-id": notionPageID, "text": ""},
		{"page-id": notionPageID, "text": " \n\t"},
		{"page-id": notionPageID, "text": "\v\f\r"},
		{"page-id": notionPageID, "text": strings.Repeat("x", 2001)},
		{"page-id": notionPageID, "text": 12},
		{"page-id": notionPageID, "text": "text", "discussion-id": "arbitrary"},
	} {
		_, err := write.PrepareSafeOutput("add-comment", args)
		require.Error(t, err)
		_, err = write.Execute(t.Context(), writeRuntime, "safe-output", "add-comment", args)
		require.Error(t, err)
	}
	_, err := write.PrepareSafeOutput("add-comment", map[string]any{"page-id": notionPageID, "text": strings.Repeat("x", 2000)})
	require.NoError(t, err)
	_, err = write.PrepareSafeOutput("add-comment", map[string]any{"page-id": notionPageID, "text": strings.Repeat("\U0001f600", 2000)})
	require.NoError(t, err)
	require.Zero(t, calls)
	require.Zero(t, secrets)
}

func TestNotionConnectorRejectsResponseEvidence(t *testing.T) {
	read, write := notionFrozenPlans(t)
	for _, test := range []struct {
		name, fixture, from, to string
		read                    bool
	}{
		{"wrong-page", "page", notionPageID, "11111111-2222-4333-8444-555555555556", true},
		{"wrong-object", "page", `"object": "page"`, `"object": "block"`, true},
		{"trashed", "page", `"in_trash": false`, `"in_trash": true`, true},
		{"missing-time", "page", `"last_edited_time"`, `"missing_time"`, true},
		{"wrong-time-type", "page", `"2026-03-12T00:00:00.000Z"`, `42`, true},
		{"wrong-parent", "comment", notionPageID, "11111111-2222-4333-8444-555555555556", false},
		{"wrong-parent-type", "comment", `"type": "page_id"`, `"type": "block_id"`, false},
		{"wrong-comment-object", "comment", `"object": "comment"`, `"object": "error"`, false},
		{"missing-id", "comment", `"id":`, `"missing_id":`, false},
		{"missing-discussion", "comment", `"discussion_id":`, `"missing_discussion":`, false},
		{"wrong-text", "comment", "Release reference reviewed.", "Different comment.", false},
	} {
		t.Run(test.name, func(t *testing.T) {
			calls, secrets := 0, 0
			response := strings.Replace(notionResponseFixture(t, test.fixture), test.from, test.to, 1)
			plan, phase, capability, operation := write, "safe-output", "safe-output", "add-comment"
			args := map[string]any{"page-id": notionPageID, "text": "Release reference reviewed."}
			if test.read {
				plan, phase, capability, operation = read, "agent", "mcp", "get-page"
				delete(args, "text")
			}
			result, err := plan.Execute(t.Context(), notionTestRuntime(t, phase, response, 200, &calls, &secrets), capability, operation, args)
			require.Error(t, err)
			require.Nil(t, result)
			require.Equal(t, 1, calls)
		})
	}
}

func TestNotionConnectorFailuresNeverRetry(t *testing.T) {
	read, write := notionFrozenPlans(t)
	for _, phase := range []string{"agent", "safe-output"} {
		for _, status := range []int{302, 400, 401, 403, 404, 429, 500, 503, 529} {
			t.Run(phase+"/"+http.StatusText(status), func(t *testing.T) {
				calls, secrets := 0, 0
				runtime := notionTestRuntime(t, phase, `{"object":"error","message":"provider failure"}`, status, &calls, &secrets)
				plan, capability, operation := read, "mcp", "get-page"
				args := map[string]any{"page-id": notionPageID}
				if phase == "safe-output" {
					plan, capability, operation = write, "safe-output", "add-comment"
					args["text"] = "Release reference reviewed."
				}
				result, err := plan.Execute(t.Context(), runtime, capability, operation, args)
				require.Error(t, err)
				require.Nil(t, result)
				require.Equal(t, 1, calls)
			})
		}
	}
}

func TestNotionConnectorAuthorityBoundaries(t *testing.T) {
	read, write := notionFrozenPlans(t)
	calls, secrets := 0, 0
	args := map[string]any{"page-id": notionPageID, "text": "Release reference reviewed."}
	request, err := write.PrepareSafeOutput("add-comment", args)
	require.NoError(t, err)
	runtime := notionTestRuntime(t, "agent", "{}", 200, &calls, &secrets)
	_, err = write.ExecuteSafeOutput(t.Context(), runtime, *request)
	require.Error(t, err)
	runtime.Phase = "safe-output"
	_, err = write.ExecuteSafeOutput(t.Context(), runtime, *request)
	require.Error(t, err, "write opt-in is required")
	runtime.AllowWrites = true
	runtime.AllowedOrigins = []string{"https://example.invalid"}
	_, err = write.ExecuteSafeOutput(t.Context(), runtime, *request)
	require.Error(t, err)
	_, err = read.Execute(t.Context(), runtime, "mcp", "get-page", map[string]any{"page-id": notionPageID})
	require.Error(t, err)
	require.Zero(t, calls)
	require.Zero(t, secrets)
}

func TestNotionConnectorRejectsUnusableResponses(t *testing.T) {
	read, _ := notionFrozenPlans(t)
	for _, response := range []string{"", "not-json", "{}", strings.Repeat("x", 65537)} {
		calls, secrets := 0, 0
		result, err := read.Execute(t.Context(), notionTestRuntime(t, "agent", response, 200, &calls, &secrets),
			"mcp", "get-page", map[string]any{"page-id": notionPageID})
		require.Error(t, err)
		require.Nil(t, result)
		require.Equal(t, 1, calls)
	}
	calls, secrets := 0, 0
	runtime := notionTestRuntime(t, "agent", "{}", 200, &calls, &secrets)
	runtime.Client.Transport = connectorSmokeTransport(func(_ *http.Request) (*http.Response, error) {
		calls++
		return nil, errors.New("fixture transport failure")
	})
	_, err := read.Execute(t.Context(), runtime, "mcp", "get-page", map[string]any{"page-id": notionPageID})
	require.Error(t, err)
	require.Equal(t, 1, calls)
}

func TestNotionConnectorTextIsData(t *testing.T) {
	_, write := notionFrozenPlans(t)
	text := "<script>not executable</script> $(not-code) ${{ secrets.NOTION_READ_TOKEN }}"
	request, err := write.PrepareSafeOutput("add-comment", map[string]any{"page-id": notionPageID, "text": text})
	require.NoError(t, err)
	encoded, err := json.Marshal(request)
	require.NoError(t, err)
	require.Contains(t, string(encoded), "$(not-code)")
	calls := 0
	runtime := connectors.Runtime{
		Phase: "safe-output", AllowWrites: true, AllowedOrigins: []string{"https://api.notion.com"},
		ResolveSecret: func(context.Context, string, string) (string, error) { return "fixture-token", nil },
		Client: &http.Client{Transport: connectorSmokeTransport(func(r *http.Request) (*http.Response, error) {
			calls++
			var body struct {
				RichText []struct {
					Text struct {
						Content string `json:"content"`
					} `json:"text"`
				} `json:"rich_text"`
			}
			require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
			require.Len(t, body.RichText, 1)
			require.Equal(t, text, body.RichText[0].Text.Content)
			response := strings.ReplaceAll(notionResponseFixture(t, "comment"), "Release reference reviewed.", text)
			return &http.Response{StatusCode: http.StatusOK, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(response)), Request: r}, nil
		})},
	}
	_, err = write.ExecuteSafeOutput(t.Context(), runtime, *request)
	require.NoError(t, err)
	require.Equal(t, 1, calls)
}

func TestNotionConnectorRequiresExactResponseNotJustGrant(t *testing.T) {
	other := "11111111-2222-4333-8444-555555555556"
	source := strings.Replace(notionConsumerExample(t), "pages: ["+notionPageID+"]", "pages: ["+notionPageID+", "+other+"]", 1)
	_, data, _, err := parseNotionConsumer(t, source)
	require.NoError(t, err)
	for _, read := range []bool{true, false} {
		calls, secrets := 0, 0
		plan, phase, capability, operation, fixture := data.Connectors["notion"].Agent, "agent", "mcp", "get-page", "page"
		args := map[string]any{"page-id": notionPageID}
		if !read {
			plan, phase, capability, operation, fixture = data.Connectors["notion"].SafeOutputs, "safe-output", "safe-output", "add-comment", "comment"
			args["text"] = "Release reference reviewed."
		}
		response := strings.Replace(notionResponseFixture(t, fixture), notionPageID, other, 1)
		result, err := plan.Execute(t.Context(), notionTestRuntime(t, phase, response, 200, &calls, &secrets), capability, operation, args)
		require.ErrorContains(t, err, "does not match request input")
		require.Nil(t, result)
		require.Equal(t, 1, calls)
	}
}
