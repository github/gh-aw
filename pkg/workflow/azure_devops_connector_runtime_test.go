//go:build !integration

package workflow

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/github/gh-aw/pkg/connectors"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const azureDevOpsWriteSelection = `safe-outputs:
  azure-devops-create-work-item:
  azure-devops-update-work-item:
  azure-devops-comment-on-work-item:
  azure-devops-assign-work-item:
  azure-devops-link-work-items:
`

func azureDevOpsConsumerArgs() map[string]any {
	return map[string]any{"organization": "contoso", "project": "33333333-3333-4333-8333-333333333333", "work-item": "9876"}
}

func TestAzureDevOpsConnectorDeclaredMutations(t *testing.T) {
	t.Parallel()
	_, data, _ := parseAzureDevOpsConsumer(t, azureDevOpsWriteSelection)
	plan := data.Connectors["azure-devops"].SafeOutputs
	require.Len(t, plan.SafeOutputNames(), 5)
	for _, operation := range plan.SafeOutputNames() {
		t.Run(operation, func(t *testing.T) {
			args := azureDevOpsConsumerArgs()
			body := "literal ${{ not.an.expression }} $(not-code)"
			switch operation {
			case "create-work-item":
				delete(args, "work-item")
				args["title"] = body
			case "update-work-item":
				args["title"] = body
			case "comment-on-work-item":
				args["text"] = body
			case "assign-work-item":
				args["assignee"] = "approved@example.invalid"
			}
			calls, secrets := 0, 0
			runtime := connectors.Runtime{
				Phase: "safe-output", AllowWrites: true, AllowedOrigins: []string{"https://dev.azure.com"},
				ResolveSecret: func(_ context.Context, phase, name string) (string, error) {
					secrets++
					assert.Equal(t, "safe-output", phase)
					assert.Equal(t, "APPROVED_AZURE_WRITER", name)
					return "fixture-write-token", nil
				},
				Client: &http.Client{Transport: connectorSmokeTransport(func(request *http.Request) (*http.Response, error) {
					calls++
					assert.Equal(t, "dev.azure.com", request.URL.Host)
					assert.Equal(t, "Bearer fixture-write-token", request.Header.Get("Authorization"))
					assert.True(t, strings.HasPrefix(request.URL.Path, "/contoso/33333333-3333-4333-8333-333333333333/_apis/wit/"))
					content, err := io.ReadAll(request.Body)
					require.NoError(t, err)
					response := `{"id":9876,"fields":{"System.TeamProject":"Approved Project"}}`
					if operation == "comment-on-work-item" {
						assert.Equal(t, http.MethodPost, request.Method)
						assert.Equal(t, "application/json", request.Header.Get("Content-Type"))
						assert.Equal(t, "7.1-preview.4", request.URL.Query().Get("api-version"))
						assert.Equal(t, "Markdown", request.URL.Query().Get("format"))
						assert.JSONEq(t, `{"text":"`+body+`"}`, string(content))
						response = `{"workItemId":9876,"text":"not projected"}`
					} else {
						assert.Equal(t, "application/json-patch+json", request.Header.Get("Content-Type"))
						assert.Equal(t, "7.1", request.URL.Query().Get("api-version"))
						var patch []map[string]any
						require.NoError(t, json.Unmarshal(content, &patch))
						require.Len(t, patch, 1)
						assert.Equal(t, "add", patch[0]["op"])
						switch operation {
						case "create-work-item", "update-work-item":
							assert.Equal(t, "/fields/System.Title", patch[0]["path"])
							assert.Equal(t, body, patch[0]["value"])
							if operation == "create-work-item" {
								assert.Equal(t, http.MethodPost, request.Method)
								assert.True(t, strings.HasSuffix(request.URL.Path, "/workitems/$Task"))
							} else {
								assert.Equal(t, http.MethodPatch, request.Method)
							}
						case "assign-work-item":
							assert.Equal(t, http.MethodPatch, request.Method)
							assert.Equal(t, "/fields/System.AssignedTo", patch[0]["path"])
							assert.Equal(t, "approved@example.invalid", patch[0]["value"])
						case "link-work-items":
							assert.Equal(t, http.MethodPatch, request.Method)
							assert.Equal(t, "/relations/-", patch[0]["path"])
							assert.Equal(t, map[string]any{
								"rel": "System.LinkTypes.Related",
								"url": "https://dev.azure.com/contoso/33333333-3333-4333-8333-333333333333/_apis/wit/workItems/9877",
							}, patch[0]["value"])
						}
					}
					return &http.Response{StatusCode: http.StatusOK, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(response)), Request: request}, nil
				})},
			}
			request, err := plan.PrepareSafeOutput(operation, args)
			require.NoError(t, err)
			require.Zero(t, calls)
			require.Zero(t, secrets, "preparation is not execution or credential access")
			result, err := plan.ExecuteSafeOutput(t.Context(), runtime, *request)
			require.NoError(t, err)
			assert.True(t, result.Untrusted)
			assert.Equal(t, 1, calls)
			assert.Equal(t, 1, secrets)
		})
	}
}

func TestAzureDevOpsConnectorRejectsAuthorityBeforeCredentials(t *testing.T) {
	t.Parallel()
	_, data, _ := parseAzureDevOpsConsumer(t, azureDevOpsWriteSelection)
	plan := data.Connectors["azure-devops"].SafeOutputs
	secrets, calls := 0, 0
	runtime := connectors.Runtime{
		Phase: "safe-output", AllowWrites: true, AllowedOrigins: []string{"https://dev.azure.com"},
		ResolveSecret: func(context.Context, string, string) (string, error) {
			secrets++
			return "", errors.New("must not resolve")
		},
		Client: &http.Client{Transport: connectorSmokeTransport(func(*http.Request) (*http.Response, error) {
			calls++
			return nil, errors.New("must not send")
		})},
	}
	for field, value := range map[string]any{
		"organization": "other", "project": "44444444-4444-4444-8444-444444444444",
		"work-item": "9999", "temporary_id": "#aw_new", "url": "https://other.invalid",
	} {
		args := azureDevOpsConsumerArgs()
		args["text"] = "approved comment"
		args[field] = value
		_, err := plan.Execute(t.Context(), runtime, "safe-output", "comment-on-work-item", args)
		require.Error(t, err)
	}
	for _, assignee := range []string{"other@example.invalid", "Agency", "GitHub Copilot"} {
		args := azureDevOpsConsumerArgs()
		args["assignee"] = assignee
		_, err := plan.Execute(t.Context(), runtime, "safe-output", "assign-work-item", args)
		require.Error(t, err)
	}
	args := azureDevOpsConsumerArgs()
	args["text"] = "approved comment"
	wrongPhase := runtime
	wrongPhase.Phase = "agent"
	_, err := plan.Execute(t.Context(), wrongPhase, "safe-output", "comment-on-work-item", args)
	require.Error(t, err)
	unapproved := runtime
	unapproved.AllowedOrigins = []string{"https://other.invalid"}
	_, err = plan.Execute(t.Context(), unapproved, "safe-output", "comment-on-work-item", args)
	require.Error(t, err)
	unapproved = runtime
	unapproved.AllowWrites = false
	_, err = plan.Execute(t.Context(), unapproved, "safe-output", "comment-on-work-item", args)
	require.Error(t, err)
	assert.Zero(t, secrets)
	assert.Zero(t, calls)
}

func TestAzureDevOpsConnectorMetadataReaderAndNoPATFallback(t *testing.T) {
	t.Setenv("AZURE_DEVOPS_EXT_PAT", "must-not-use")
	t.Setenv("SYSTEM_ACCESSTOKEN", "must-not-use")
	_, data, _ := parseAzureDevOpsConsumer(t, "tools:\n  azure-devops: [azure-devops]\n")
	plan := data.Connectors["azure-devops"].Agent
	secrets := 0
	runtime := connectors.Runtime{
		Phase: "agent", AllowedOrigins: []string{"https://mcp.dev.azure.com"},
		ResolveSecret: func(_ context.Context, phase, name string) (string, error) {
			secrets++
			assert.Equal(t, "agent", phase)
			assert.Equal(t, "APPROVED_AZURE_READER", name)
			return "", errors.New("Entra token unavailable")
		},
	}
	for _, tool := range []string{"*", "wit_get_work_item", "wit_create_work_item", "core_list_projects_write"} {
		_, err := plan.CallMCP(t.Context(), runtime, "azure-devops", tool, map[string]any{})
		require.Error(t, err)
	}
	assert.Zero(t, secrets)
	_, err := plan.CallMCP(t.Context(), runtime, "azure-devops", "core_list_projects", map[string]any{})
	require.Error(t, err)
	assert.Positive(t, secrets, "only the explicitly named Entra reader may be resolved")
	_, err = plan.Execute(t.Context(), runtime, "safe-output", "comment-on-work-item", azureDevOpsConsumerArgs())
	require.Error(t, err)
}

func TestAzureDevOpsConnectorHostedProjectMetadata(t *testing.T) {
	t.Parallel()
	_, data, _ := parseAzureDevOpsConsumer(t, "tools:\n  azure-devops: [azure-devops]\n")
	plan := data.Connectors["azure-devops"].Agent
	var calls atomic.Int32
	upstream := mcp.NewServer(&mcp.Implementation{Name: "azure-metadata-fixture", Version: "1"}, nil)
	upstream.AddTool(&mcp.Tool{Name: "core_list_projects", InputSchema: map[string]any{"type": "object"}}, func(context.Context, *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		calls.Add(1)
		return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: "project metadata only"}}}, nil
	})
	handler := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return upstream }, nil)
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/contoso", r.URL.Path)
		assert.Equal(t, "Bearer fixture-entra-token", r.Header.Get("Authorization"))
		assert.Equal(t, "true", r.Header.Get("X-MCP-Readonly"))
		assert.Equal(t, "core_list_projects", r.Header.Get("X-MCP-Tools"))
		handler.ServeHTTP(w, r)
	}))
	defer server.Close()
	destination, err := url.Parse(server.URL)
	require.NoError(t, err)
	client := server.Client()
	transport := client.Transport
	client.Transport = connectorSmokeTransport(func(request *http.Request) (*http.Response, error) {
		assert.Equal(t, "https://mcp.dev.azure.com/contoso", request.URL.String())
		local := request.Clone(request.Context())
		local.URL.Scheme, local.URL.Host = destination.Scheme, destination.Host
		local.Host = destination.Host
		return transport.RoundTrip(local)
	})
	runtime := connectors.Runtime{
		Phase: "agent", AllowedOrigins: []string{"https://mcp.dev.azure.com"}, Client: client,
		ResolveSecret: func(_ context.Context, phase, name string) (string, error) {
			assert.Equal(t, "agent", phase)
			assert.Equal(t, "APPROVED_AZURE_READER", name)
			return "fixture-entra-token", nil
		},
	}
	result, err := plan.CallMCP(t.Context(), runtime, "azure-devops", "core_list_projects", map[string]any{})
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.EqualValues(t, 1, calls.Load())
	_, err = plan.CallMCP(t.Context(), runtime, "azure-devops", "core_list_projects", map[string]any{"project": "unapproved"})
	require.Error(t, err)
	assert.EqualValues(t, 1, calls.Load())
}

func TestAzureDevOpsConnectorMutationFailuresAreNotRetried(t *testing.T) {
	t.Parallel()
	_, data, _ := parseAzureDevOpsConsumer(t, azureDevOpsWriteSelection)
	plan := data.Connectors["azure-devops"].SafeOutputs
	for name, fixture := range map[string]struct {
		status int
		body   string
	}{
		"denied":        {http.StatusForbidden, `{"message":"denied"}`},
		"deleted":       {http.StatusNotFound, `{"message":"deleted"}`},
		"redirect":      {http.StatusFound, `{}`},
		"oversized":     {http.StatusOK, strings.Repeat("x", 65537)},
		"missing-id":    {http.StatusOK, `{"fields":{"System.TeamProject":"Approved Project"}}`},
		"wrong-project": {http.StatusOK, `{"id":9876,"fields":{"System.TeamProject":"Other Project"}}`},
	} {
		t.Run(name, func(t *testing.T) {
			calls := 0
			runtime := connectors.Runtime{
				Phase: "safe-output", AllowWrites: true, AllowedOrigins: []string{"https://dev.azure.com"},
				ResolveSecret: func(context.Context, string, string) (string, error) { return "fixture-token", nil },
				Client: &http.Client{Transport: connectorSmokeTransport(func(request *http.Request) (*http.Response, error) {
					calls++
					return &http.Response{
						StatusCode: fixture.status, Header: http.Header{"Location": []string{"https://other.invalid"}},
						Body: io.NopCloser(strings.NewReader(fixture.body)), Request: request,
					}, nil
				})},
			}
			args := azureDevOpsConsumerArgs()
			args["title"] = "approved update"
			_, err := plan.Execute(t.Context(), runtime, "safe-output", "update-work-item", args)
			require.Error(t, err)
			assert.Equal(t, 1, calls, "a rejected or ambiguous write must not retry or follow redirects")
		})
	}
}
