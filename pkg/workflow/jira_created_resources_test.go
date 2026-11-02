//go:build !integration

package workflow

import (
	"context"
	"encoding/base64"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/connectors"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestJiraCreatedResourceChain(t *testing.T) {
	calls, secretCalls := 0, 0
	createResponse := `{"id":"10002","key":"APP-2","self":"https://approved.atlassian.net/rest/api/3/issue/10002"}`
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		switch {
		case r.URL.Path == "/rest/api/3/issue":
			_, err := io.WriteString(w, createResponse)
			assert.NoError(t, err)
		case strings.HasSuffix(r.URL.Path, "/comment"):
			assert.Equal(t, "/rest/api/3/issue/APP-2/comment", r.URL.Path)
			_, err := io.WriteString(w, `{"id":"20001"}`)
			assert.NoError(t, err)
		default:
			assert.Equal(t, "/rest/api/3/issue/APP-2", r.URL.Path)
			w.WriteHeader(http.StatusNoContent)
		}
	}))
	defer server.Close()
	header := strings.Replace(jiraEmbeddedImport, "      issues: [APP-1]\n", "", 1)
	header = strings.Replace(header, "https://approved.atlassian.net", server.URL, 1)
	_, data, _, err := parseJiraMigrationWorkflow(t, header+"safe-outputs:\n  jira-create-issue:\n  jira-update-issue:\n  jira-add-comment:\n  jira-add-label:\n")
	require.NoError(t, err)
	plan := data.Connectors["jira"].SafeOutputs
	runtime := connectors.Runtime{Phase: "safe-output", AllowWrites: true, Client: server.Client(), AllowedOrigins: []string{server.URL},
		ResolveSecret: func(_ context.Context, phase, name string) (string, error) {
			secretCalls++
			assert.Equal(t, "safe-output", phase)
			assert.Equal(t, "JIRA_REST_BASIC_AUTH", name)
			return base64.StdEncoding.EncodeToString([]byte("fixture:token")), nil
		}}
	host := connectors.CreatedResourceHost{Run: "trusted-execution", Key: []byte(strings.Repeat("k", 32))}
	create := map[string]any{"project": "APP", "issue-type": "Task", "summary": "Haiku", "temporary_id": "aw_created"}
	result, err := plan.ExecuteCreatedSafeOutput(t.Context(), runtime, host, "create-issue", create)
	require.NoError(t, err)
	require.NotEmpty(t, result.CreatedResource)
	host.Receipts = []string{result.CreatedResource}
	for _, step := range []struct {
		name string
		args map[string]any
	}{
		{"update-issue", map[string]any{"issue": "#aw_created", "summary": "Haiku", "description": "Revised haiku"}},
		{"add-comment", map[string]any{"issue": "aw_created", "text": "Run URL"}},
		{"add-label", map[string]any{"issue": "#aw_created", "label": "gh-aw-smoke-test"}},
	} {
		_, err := plan.ExecuteCreatedSafeOutput(t.Context(), runtime, host, step.name, step.args)
		require.NoError(t, err)
	}
	assert.Equal(t, 4, calls)
	assert.Equal(t, 4, secretCalls)
	badHosts := []connectors.CreatedResourceHost{
		{Run: "another-execution", Key: host.Key, Receipts: host.Receipts},
		{Run: host.Run, Key: []byte(strings.Repeat("x", 32)), Receipts: host.Receipts},
		{Run: host.Run, Key: host.Key, Receipts: []string{"forged"}},
		{Run: host.Run, Key: host.Key},
		{Run: host.Run, Key: host.Key, Receipts: append(host.Receipts, host.Receipts...)},
	}
	for _, bad := range badHosts {
		_, err := plan.ExecuteCreatedSafeOutput(t.Context(), runtime, bad, "add-comment", map[string]any{"issue": "#aw_created", "text": "Rejected"})
		require.Error(t, err)
	}
	for _, issue := range []string{"APP-2", "OTHER-1", "#aw_unknown"} {
		_, err := plan.ExecuteCreatedSafeOutput(t.Context(), runtime, host, "add-comment", map[string]any{"issue": issue, "text": "Rejected"})
		require.Error(t, err)
	}
	_, err = plan.ExecuteCreatedSafeOutput(t.Context(), runtime, host, "create-issue", create)
	require.ErrorContains(t, err, "already bound")
	assert.Equal(t, 4, calls, "invalid binding attempts must fail before credentials or network")
	assert.Equal(t, 4, secretCalls)

	encoded, err := plan.Encode()
	require.NoError(t, err)
	otherPlan, err := connectors.DecodePlan(encoded)
	require.NoError(t, err)
	otherPlan.Digest = strings.Repeat("0", 64)
	_, err = otherPlan.ExecuteCreatedSafeOutput(t.Context(), runtime, host, "add-comment", map[string]any{"issue": "#aw_created", "text": "Rejected"})
	require.ErrorContains(t, err, "different execution or connector")
	assert.Equal(t, 4, calls)

	staged, err := plan.PrepareSafeOutputIntent("add-comment", map[string]any{"issue": "#aw_staged", "text": "No effect"})
	require.NoError(t, err)
	_, err = plan.ExecuteSafeOutput(t.Context(), runtime, *staged)
	require.Error(t, err, "staged temporary references are not executable receipts")
	assert.Equal(t, 4, calls)

	for _, response := range []string{`{}`, `{"id":"1","key":"OTHER-1","self":"https://approved.atlassian.net/rest/api/3/issue/1"}`} {
		createResponse = response
		delete(create, "temporary_id")
		create["temporary_id"] = "aw_invalid"
		result, err := plan.ExecuteCreatedSafeOutput(t.Context(), runtime, host, "create-issue", create)
		require.Error(t, err)
		assert.Nil(t, result, "incomplete or out-of-scope creates must never issue a receipt")
	}
}
