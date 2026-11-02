//go:build !integration

package workflow

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"maps"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/connectors"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const azureWorkItemParityGrants = `      rest-reader-secret: APPROVED_AZURE_REST_READER
      work-item-type: User Story
      work-item-types: [User Story, Bug]
      states: [Active, Closed]
      tag-sets: ["triaged; backend"]
      area-paths: ['Approved Project\Backend']
      iteration-paths: ['Approved Project\Sprint 1']
      link-types: [System.LinkTypes.Hierarchy-Reverse, System.LinkTypes.Dependency-Forward]
      link-target-urls:
        - https://dev.azure.com/contoso/33333333-3333-4333-8333-333333333333/_apis/wit/workItems/9877
        - https://dev.azure.com/contoso/33333333-3333-4333-8333-333333333333/_apis/wit/workItems/9878
`

const azureWorkItemParitySelection = `safe-outputs:
  azure-devops-create-work-item-with-description:
  azure-devops-create-bug-with-repro-steps:
  azure-devops-set-work-item-description:
  azure-devops-set-work-item-repro-steps:
  azure-devops-set-work-item-state:
  azure-devops-replace-work-item-tags:
  azure-devops-set-work-item-area-path:
  azure-devops-set-work-item-iteration-path:
  azure-devops-link-approved-work-items:
`

func parseAzureWorkItemParity(t *testing.T, grants, selection string) (*Compiler, *WorkflowData, string, error) {
	t.Helper()
	root := t.TempDir()
	path := filepath.Join(root, ".github", "workflows", "azure-rich.md")
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0700))
	content := awConnectorHeader + azureDevOpsConsumerImport + grants + selection + "---\nUse only explicitly granted work items.\n"
	require.NoError(t, os.WriteFile(path, []byte(content), 0600))
	compiler := NewCompiler(WithNoEmit(true), WithSkipValidation(true))
	compiler.gitRoot = root
	installConnectorTestSources(t, root)
	compiler.SetActionMode(ActionModeDev)
	compiler.version = "v0.0.0-1-g683ddf9617"
	data, err := compiler.ParseWorkflowFile(path)
	return compiler, data, path, err
}

func azureWorkItemParityPlan(t *testing.T) *connectors.Plan {
	t.Helper()
	_, data, _, err := parseAzureWorkItemParity(t, azureWorkItemParityGrants, azureWorkItemParitySelection)
	require.NoError(t, err)
	plan := data.Connectors["azure-devops"].SafeOutputs
	encoded, err := plan.Encode()
	require.NoError(t, err)
	frozen, err := connectors.DecodePlan(encoded)
	require.NoError(t, err)
	require.Equal(t, plan.Digest, frozen.Digest)
	return frozen
}

func TestAzureWorkItemParitySparseRequests(t *testing.T) {
	plan := azureWorkItemParityPlan(t)
	tests := []struct {
		operation, input, value, field, patchOp string
		markdown                                bool
	}{
		{"set-work-item-description", "body", "Only the description changes: ${{ data }} $(not-code)", "System.Description", "add", true},
		{"set-work-item-repro-steps", "body", "Only Bug repro steps change: **untrusted**", "Microsoft.VSTS.TCM.ReproSteps", "add", true},
		{"set-work-item-state", "state", "Closed", "System.State", "add", false},
		{"replace-work-item-tags", "tags", "triaged; backend", "System.Tags", "replace", false},
		{"set-work-item-area-path", "area-path", `Approved Project\Backend`, "System.AreaPath", "add", false},
		{"set-work-item-iteration-path", "iteration-path", `Approved Project\Sprint 1`, "System.IterationPath", "add", false},
	}
	for _, test := range tests {
		t.Run(test.operation, func(t *testing.T) {
			args := azureDevOpsConsumerArgs()
			args[test.input] = test.value
			calls, secrets := 0, 0
			runtime := azureWorkItemParityRuntime(t, &secrets, func(request *http.Request) string {
				calls++
				assert.Equal(t, http.MethodPatch, request.Method)
				assert.Equal(t, "/contoso/33333333-3333-4333-8333-333333333333/_apis/wit/workitems/9876", request.URL.Path)
				patch := azureWorkItemParityPatch(t, request)
				expected := []map[string]any{{"op": test.patchOp, "path": "/fields/" + test.field, "value": test.value}}
				if test.markdown {
					expected = append(expected, map[string]any{"op": "add", "path": "/multilineFieldsFormat/" + test.field, "value": "Markdown"})
				}
				assert.Equal(t, expected, patch, "sparse writes must not read and rewrite unrelated fields")
				return `{"id":9876,"fields":{"System.TeamProject":"Approved Project"},"private":"not projected"}`
			})
			intent, err := plan.PrepareSafeOutput(test.operation, args)
			require.NoError(t, err)
			assert.Zero(t, calls)
			assert.Zero(t, secrets)
			result, err := plan.ExecuteSafeOutput(t.Context(), runtime, *intent)
			require.NoError(t, err)
			assert.JSONEq(t, `{"work-item":9876}`, string(result.Data))
			assert.True(t, result.Untrusted)
			assert.Equal(t, 1, calls, "there is no read-rewrite or automatic retry")
			assert.Equal(t, 1, secrets)
		})
	}
}

func TestAzureWorkItemParityRichCreation(t *testing.T) {
	plan := azureWorkItemParityPlan(t)
	for _, test := range []struct{ operation, kind, field string }{
		{"create-work-item-with-description", "User Story", "System.Description"},
		{"create-bug-with-repro-steps", "Bug", "Microsoft.VSTS.TCM.ReproSteps"},
	} {
		t.Run(test.operation, func(t *testing.T) {
			args := azureDevOpsConsumerArgs()
			delete(args, "work-item")
			args["work-item-type"] = test.kind
			args["title"] = "Create a carefully scoped Azure work item"
			args["body"] = "Markdown **description** with literal ${{ untrusted }} content."
			secrets, calls := 0, 0
			runtime := azureWorkItemParityRuntime(t, &secrets, func(request *http.Request) string {
				calls++
				assert.Equal(t, http.MethodPost, request.Method)
				assert.Equal(t, "/contoso/33333333-3333-4333-8333-333333333333/_apis/wit/workitems/$"+test.kind, request.URL.Path)
				assert.Equal(t, []map[string]any{
					{"op": "add", "path": "/fields/System.Title", "value": args["title"]},
					{"op": "add", "path": "/fields/" + test.field, "value": args["body"]},
					{"op": "add", "path": "/multilineFieldsFormat/" + test.field, "value": "Markdown"},
				}, azureWorkItemParityPatch(t, request))
				response, err := json.Marshal(map[string]any{"id": 9001, "fields": map[string]any{"System.TeamProject": "Approved Project", "System.WorkItemType": test.kind}})
				require.NoError(t, err)
				return string(response)
			})
			result, err := plan.Execute(t.Context(), runtime, "safe-output", test.operation, args)
			require.NoError(t, err)
			assert.JSONEq(t, `{"work-item":9001}`, string(result.Data))
			assert.Empty(t, result.CreatedResource, "numeric create results are not fabricated temporary-ID authority")
			assert.Equal(t, 1, calls)
			assert.Equal(t, 1, secrets)
		})
	}
}

func TestAzureWorkItemParityApprovedLinks(t *testing.T) {
	plan := azureWorkItemParityPlan(t)
	for _, id := range []string{"9877", "9878"} {
		for _, kind := range []string{"System.LinkTypes.Hierarchy-Reverse", "System.LinkTypes.Dependency-Forward"} {
			t.Run(id+"/"+kind, func(t *testing.T) {
				args := azureDevOpsConsumerArgs()
				target := "https://dev.azure.com/contoso/33333333-3333-4333-8333-333333333333/_apis/wit/workItems/" + id
				args["target-url"], args["link-type"] = target, kind
				secrets, calls := 0, 0
				runtime := azureWorkItemParityRuntime(t, &secrets, func(request *http.Request) string {
					calls++
					assert.Equal(t, http.MethodPatch, request.Method)
					assert.True(t, strings.HasSuffix(request.URL.Path, "/workitems/9876"))
					assert.Equal(t, []map[string]any{{
						"op": "add", "path": "/relations/-", "value": map[string]any{"rel": kind, "url": target},
					}}, azureWorkItemParityPatch(t, request))
					return `{"id":9876,"fields":{"System.TeamProject":"Approved Project"}}`
				})
				_, err := plan.Execute(t.Context(), runtime, "safe-output", "link-approved-work-items", args)
				require.NoError(t, err)
				assert.Equal(t, 1, calls, "the submitted target URL is link data, never a request destination")
			})
		}
	}
}

func TestAzureWorkItemParityRejectsBeforeCredentialAccess(t *testing.T) {
	plan := azureWorkItemParityPlan(t)
	for _, test := range []struct {
		operation string
		values    map[string]any
	}{
		{"set-work-item-state", map[string]any{"state": "Unknown"}},
		{"set-work-item-state", map[string]any{"state": "Closed", "work-item": "9999"}},
		{"set-work-item-description", map[string]any{"body": "", "state": "Closed"}},
		{"set-work-item-description", map[string]any{"body": "No arbitrary fields", "fields": map[string]any{"Custom.X": "y"}}},
		{"set-work-item-description", map[string]any{"body": "No temporary IDs", "work-item": "#aw_new"}},
		{"set-work-item-description", map[string]any{"body": "No numeric inputs", "work-item": 9876}},
		{"set-work-item-area-path", map[string]any{"area-path": `Approved Project\Backend\UnapprovedChild`}},
		{"set-work-item-iteration-path", map[string]any{"iteration-path": `Other\Sprint 1`}},
		{"replace-work-item-tags", map[string]any{"tags": []string{"triaged", "backend"}}},
		{"replace-work-item-tags", map[string]any{"tags": "triaged; not-approved"}},
		{"replace-work-item-tags", map[string]any{"tags": "triaged;; backend"}},
		{"link-approved-work-items", map[string]any{"target-url": "https://evil.invalid/9877", "link-type": "System.LinkTypes.Hierarchy-Reverse"}},
		{"link-approved-work-items", map[string]any{"target-url": "https://dev.azure.com/contoso/33333333-3333-4333-8333-333333333333/_apis/wit/workItems/9999", "link-type": "System.LinkTypes.Hierarchy-Reverse"}},
		{"link-approved-work-items", map[string]any{"target-url": "https://dev.azure.com/contoso/33333333-3333-4333-8333-333333333333/_apis/wit/workItems/9877", "link-type": "ArtifactLink"}},
		{"link-approved-work-items", map[string]any{"target-url": "https://dev.azure.com/contoso/33333333-3333-4333-8333-333333333333/_apis/wit/workItems/9877", "link-type": "System.LinkTypes.Duplicate-Forward"}},
		{"link-approved-work-items", map[string]any{"work-item": "9877", "target-url": "https://dev.azure.com/contoso/33333333-3333-4333-8333-333333333333/_apis/wit/workItems/9878", "link-type": "System.LinkTypes.Hierarchy-Reverse"}},
	} {
		t.Run(test.operation+"/"+strings.TrimSpace(test.operation), func(t *testing.T) {
			args := azureDevOpsConsumerArgs()
			maps.Copy(args, test.values)
			secrets, calls := 0, 0
			runtime := azureWorkItemParityRuntime(t, &secrets, func(*http.Request) string {
				calls++
				return "{}"
			})
			_, err := plan.Execute(t.Context(), runtime, "safe-output", test.operation, args)
			require.Error(t, err)
			assert.Zero(t, secrets)
			assert.Zero(t, calls)
		})
	}
	for _, kind := range []string{"Task", "../Bug", "Bug/../../other", "${{ input }}"} {
		args := azureDevOpsConsumerArgs()
		delete(args, "work-item")
		args["work-item-type"], args["title"], args["body"] = kind, "Create only an approved Azure type", "Approved body."
		_, err := plan.PrepareSafeOutput("create-work-item-with-description", args)
		require.Error(t, err)
	}
}

func TestAzureWorkItemParityEmptyGrantsDoNotAuthorizeEffects(t *testing.T) {
	for _, operation := range []string{
		"create-work-item-with-description", "create-bug-with-repro-steps",
		"set-work-item-state", "replace-work-item-tags", "set-work-item-area-path",
		"set-work-item-iteration-path", "link-approved-work-items",
	} {
		t.Run(operation, func(t *testing.T) {
			_, _, _, err := parseAzureWorkItemParity(t, "", "safe-outputs:\n  azure-devops-"+operation+":\n")
			require.ErrorContains(t, err, "resource allowlist")
		})
	}
}

func TestAzureWorkItemParityResponseCorrelation(t *testing.T) {
	plan := azureWorkItemParityPlan(t)
	for name, body := range map[string]string{
		"other-approved-id": `{"id":9877,"fields":{"System.TeamProject":"Approved Project"}}`,
		"wrong-project":     `{"id":9876,"fields":{"System.TeamProject":"Other Project"}}`,
		"missing-project":   `{"id":9876}`,
		"string-id":         `{"id":"9876","fields":{"System.TeamProject":"Approved Project"}}`,
	} {
		t.Run(name, func(t *testing.T) {
			args := azureDevOpsConsumerArgs()
			args["state"] = "Closed"
			secrets := 0
			runtime := azureWorkItemParityRuntime(t, &secrets, func(*http.Request) string { return body })
			_, err := plan.Execute(t.Context(), runtime, "safe-output", "set-work-item-state", args)
			require.Error(t, err)
		})
	}
}

func TestAzureWorkItemParityBoundedBodyReaders(t *testing.T) {
	for _, test := range []struct{ operation, tool, field string }{
		{"get-work-item-description", "get_work_item_description", "System.Description"},
		{"get-bug-repro-steps", "get_bug_repro_steps", "Microsoft.VSTS.TCM.ReproSteps"},
	} {
		t.Run(test.operation, func(t *testing.T) {
			selection := "tools:\n  azure-devops: [" + test.operation + "]\n"
			compiler, data, path, err := parseAzureWorkItemParity(t, azureWorkItemParityGrants, selection)
			require.NoError(t, err)
			plan := data.Connectors["azure-devops"].Agent
			assert.Equal(t, []string{"APPROVED_AZURE_REST_READER"}, plan.Secrets())
			assert.Equal(t, []string{test.tool}, plan.ToolNames())
			output, err := compiler.CompileToYAML(data, path)
			require.NoError(t, err)
			assert.Contains(t, output, "${{ secrets.APPROVED_AZURE_REST_READER }}")
			assert.NotContains(t, output, "${{ secrets.APPROVED_AZURE_WRITER }}")
			assert.NotContains(t, output, "${{ secrets.APPROVED_AZURE_READER }}")
			for _, fixture := range []struct {
				name string
				id   int
				body any
				fail bool
			}{
				{"body", 9876, "Untrusted **body** ${{ data }}", false},
				{"empty-body", 9876, "", false},
				{"other-approved-id", 9877, "Wrong identity", true},
				{"invalid-body", 9876, map[string]any{"text": "not a primitive"}, true},
			} {
				t.Run(fixture.name, func(t *testing.T) {
					calls := 0
					runtime := connectors.Runtime{
						Phase: "agent", AllowedOrigins: []string{"https://dev.azure.com"},
						ResolveSecret: func(_ context.Context, phase, name string) (string, error) {
							assert.Equal(t, "agent", phase)
							assert.Equal(t, "APPROVED_AZURE_REST_READER", name)
							return "fixture-read-token", nil
						},
						Client: &http.Client{Transport: connectorSmokeTransport(func(request *http.Request) (*http.Response, error) {
							calls++
							assert.Equal(t, http.MethodGet, request.Method)
							assert.True(t, strings.HasSuffix(request.URL.Path, "/workitems/9876"))
							assert.Equal(t, "System.TeamProject,System.Title,System.State,"+test.field, request.URL.Query().Get("fields"))
							body, err := json.Marshal(map[string]any{
								"id":     fixture.id,
								"fields": map[string]any{"System.TeamProject": "Approved Project", "System.Title": "Untrusted title", "System.State": "Active", test.field: fixture.body, "Custom.Private": "must not project"},
								"url":    "https://evil.invalid/must-not-follow",
							})
							require.NoError(t, err)
							return azureWorkItemParityResponse(request, string(body)), nil
						})},
					}
					result, err := plan.Execute(t.Context(), runtime, "mcp", test.operation, azureDevOpsConsumerArgs())
					if fixture.fail {
						require.Error(t, err)
					} else {
						require.NoError(t, err)
						assert.True(t, result.Untrusted)
						assert.NotContains(t, string(result.Data), "must not project")
						assert.NotContains(t, string(result.Data), "evil.invalid")
						var projected map[string]any
						require.NoError(t, json.Unmarshal(result.Data, &projected))
						assert.Equal(t, fixture.body, projected["body"])
						assert.Len(t, projected, 4)
					}
					assert.Equal(t, 1, calls)
				})
			}
		})
	}
}

func azureWorkItemParityPatch(t *testing.T, request *http.Request) []map[string]any {
	t.Helper()
	assert.Equal(t, "application/json-patch+json", request.Header.Get("Content-Type"))
	assert.Equal(t, "7.1", request.URL.Query().Get("api-version"))
	body, err := io.ReadAll(request.Body)
	require.NoError(t, err)
	var patch []map[string]any
	require.NoError(t, json.Unmarshal(body, &patch))
	return patch
}

func azureWorkItemParityRuntime(t *testing.T, secrets *int, respond func(*http.Request) string) connectors.Runtime {
	t.Helper()
	return connectors.Runtime{
		Phase: "safe-output", AllowWrites: true, AllowedOrigins: []string{"https://dev.azure.com"},
		ResolveSecret: func(_ context.Context, phase, name string) (string, error) {
			*secrets++
			if phase != "safe-output" || name != "APPROVED_AZURE_WRITER" {
				return "", errors.New("wrong Azure credential boundary")
			}
			return "fixture-write-token", nil
		},
		Client: &http.Client{Transport: connectorSmokeTransport(func(request *http.Request) (*http.Response, error) {
			return azureWorkItemParityResponse(request, respond(request)), nil
		})},
	}
}

func azureWorkItemParityResponse(request *http.Request, body string) *http.Response {
	return &http.Response{StatusCode: http.StatusOK, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body)), Request: request}
}
