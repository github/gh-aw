//go:build !integration

package workflow

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"encoding/xml"
	"errors"
	"io"
	"maps"
	"net/http"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/connectors"
	"github.com/stretchr/testify/require"
)

const azureBoundedReaderGrants = `      rest-reader-secret: APPROVED_AZURE_REST_READER
      code-reader-secret: APPROVED_AZURE_CODE_READER
      build-reader-secret: APPROVED_AZURE_BUILD_READER
      read-work-item-ids: ["9876", "9877"]
      read-query-ids: [66666666-6666-4666-8666-666666666666]
      read-repository-ids: [44444444-4444-4444-8444-444444444444, 55555555-5555-4555-8555-555555555555]
      read-file-paths: [/README.md, /OTHER.md]
      read-commits: [aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa, bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb]
      read-target-refs: [refs/heads/main, refs/heads/other]
      read-pull-request-ids: ["42", "43"]
      read-pipeline-ids: ["12", "13"]
      read-run-ids: ["90", "91"]
`

const azureBoundedReaderSelection = `tools:
  azure-devops: [get-work-item, run-work-item-query, get-repository, get-repository-file, list-pull-requests, get-pull-request, list-pipeline-definitions, get-pipeline-definition, list-pipeline-runs, get-pipeline-run]
`

type azureBoundedReaderFixture struct {
	Operation string            `json:"operation"`
	Secret    string            `json:"secret"`
	Args      map[string]any    `json:"args"`
	Path      string            `json:"path"`
	Query     map[string]string `json:"query"`
	Response  map[string]any    `json:"response"`
}

func azureBoundedReaderPlan(t *testing.T) *connectors.Plan {
	t.Helper()
	_, data, _, err := parseAzureWorkItemParity(t, azureBoundedReaderGrants, azureBoundedReaderSelection)
	require.NoError(t, err)
	encoded, err := data.Connectors["azure-devops"].Agent.Encode()
	require.NoError(t, err)
	plan, err := connectors.DecodePlan(encoded)
	require.NoError(t, err)
	return plan
}

func azureBoundedReaderFixtures(t *testing.T) []azureBoundedReaderFixture {
	t.Helper()
	base := map[string]any{"organization": "contoso", "project": "33333333-3333-4333-8333-333333333333"}
	repo := "44444444-4444-4444-8444-444444444444"
	repoObject := map[string]any{"id": repo, "name": "Approved repository", "project": map[string]any{"id": base["project"]}}
	pr := map[string]any{
		"pullRequestId": 42, "repository": repoObject, "title": "Untrusted PR title",
		"description": strings.Repeat("Full PR description. ", 30), "status": "active",
		"sourceRefName": "refs/heads/feature", "targetRefName": "refs/heads/main",
	}
	definition := map[string]any{
		"id": 12, "name": "Approved pipeline", "project": map[string]any{"id": base["project"]},
		"revision": 3, "path": `\`, "repository": map[string]any{"id": repo},
		"process": map[string]any{"yamlFilename": "azure-pipelines.yml", "type": 2},
	}
	run := map[string]any{
		"id": 90, "definition": map[string]any{"id": 12}, "project": map[string]any{"id": base["project"]},
		"repository": map[string]any{"id": repo}, "sourceBranch": "refs/heads/main",
		"sourceVersion": strings.Repeat("a", 40), "status": "inProgress", "buildNumber": "20261102.1",
	}
	root := "/contoso/33333333-3333-4333-8333-333333333333/_apis/"
	var fixtures []azureBoundedReaderFixture
	add := func(operation, secret, path string, args map[string]any, query map[string]string, response map[string]any) {
		arguments := maps.Clone(base)
		maps.Copy(arguments, args)
		params := map[string]string{"api-version": "7.1"}
		maps.Copy(params, query)
		fixtures = append(fixtures, azureBoundedReaderFixture{operation, secret, arguments, root + path, params, response})
	}
	add("get-work-item", "APPROVED_AZURE_REST_READER", "wit/workitems/9876",
		map[string]any{"work-item": "9876"},
		map[string]string{"fields": "System.TeamProject,System.Title,System.State,System.WorkItemType,System.AssignedTo,System.AreaPath,System.IterationPath,System.Tags,System.Description,Microsoft.VSTS.TCM.ReproSteps"},
		map[string]any{"id": 9876, "rev": 4, "fields": map[string]any{
			"System.TeamProject": "Approved Project", "System.Title": "Untrusted item", "System.State": "Active",
			"System.Description": "Actual **work-item body**",
		}, "private": "not projected"})
	add("run-work-item-query", "APPROVED_AZURE_REST_READER", "wit/wiql/66666666-6666-4666-8666-666666666666",
		map[string]any{"query-id": "66666666-6666-4666-8666-666666666666", "first": 2},
		map[string]string{"$top": "2"},
		map[string]any{"queryType": "flat", "columns": []any{}, "workItems": []any{map[string]any{"id": 9876}}, "asOf": "2026-11-02T00:00:00Z"})
	add("get-repository", "APPROVED_AZURE_CODE_READER", "git/repositories/"+repo,
		map[string]any{"repository-id": repo}, nil, repoObject)
	add("get-repository-file", "APPROVED_AZURE_CODE_READER", "git/repositories/"+repo+"/items",
		map[string]any{"repository-id": repo, "path": "/README.md", "commit": strings.Repeat("a", 40)},
		map[string]string{
			"path": "/README.md", "versionDescriptor.version": strings.Repeat("a", 40),
			"versionDescriptor.versionType": "commit", "versionDescriptor.versionOptions": "none",
			"includeContent": "true", "includeContentMetadata": "true", "resolveLfs": "false", "download": "false", "$format": "json",
		},
		map[string]any{"path": "/README.md", "commitId": strings.Repeat("a", 40), "objectId": strings.Repeat("c", 40),
			"gitObjectType": "blob", "content": "Actual repository **source content**\n${{ untrusted }}",
			"contentMetadata": map[string]any{"isBinary": false}, "url": "https://evil.invalid/never-follow"})
	add("list-pull-requests", "APPROVED_AZURE_CODE_READER", "git/repositories/"+repo+"/pullrequests",
		map[string]any{"repository-id": repo, "target-ref": "refs/heads/main", "status": "active", "first": 2, "skip": 0},
		map[string]string{"searchCriteria.targetRefName": "refs/heads/main", "searchCriteria.status": "active", "$top": "2", "$skip": "0"},
		map[string]any{"count": 1, "value": []any{pr}})
	add("get-pull-request", "APPROVED_AZURE_CODE_READER", "git/repositories/"+repo+"/pullrequests/42",
		map[string]any{"repository-id": repo, "pull-request-id": "42", "target-ref": "refs/heads/main"},
		map[string]string{"includeCommits": "false", "includeWorkItemRefs": "false"}, pr)
	add("list-pipeline-definitions", "APPROVED_AZURE_BUILD_READER", "build/definitions",
		map[string]any{"first": 2},
		map[string]string{"$top": "2", "includeAllProperties": "false", "includeLatestBuilds": "false"},
		map[string]any{"count": 1, "value": []any{map[string]any{
			"id": 12, "name": "Approved pipeline", "project": map[string]any{"id": base["project"]}, "revision": 3,
		}}})
	add("get-pipeline-definition", "APPROVED_AZURE_BUILD_READER", "build/definitions/12",
		map[string]any{"repository-id": repo, "pipeline-id": "12"},
		map[string]string{"includeLatestBuilds": "false"}, definition)
	add("list-pipeline-runs", "APPROVED_AZURE_BUILD_READER", "build/builds",
		map[string]any{"repository-id": repo, "source-ref": "refs/heads/main", "first": 2},
		map[string]string{"repositoryId": repo, "repositoryType": "TfsGit", "branchName": "refs/heads/main", "$top": "2"},
		map[string]any{"count": 1, "value": []any{run}})
	add("get-pipeline-run", "APPROVED_AZURE_BUILD_READER", "build/builds/90",
		map[string]any{"repository-id": repo, "source-ref": "refs/heads/main", "pipeline-id": "12", "run-id": "90"}, nil, run)
	return fixtures
}

func azureBoundedReaderExecute(t *testing.T, plan *connectors.Plan, fixture azureBoundedReaderFixture, response any) (*connectors.Result, int, int, error) {
	t.Helper()
	calls, secrets := 0, 0
	payload, err := json.Marshal(response)
	require.NoError(t, err)
	result, executionErr := plan.Execute(t.Context(), connectors.Runtime{
		Phase: "agent", AllowedOrigins: plan.Origins(),
		ResolveSecret: func(_ context.Context, phase, secret string) (string, error) {
			secrets++
			require.Equal(t, "agent", phase)
			require.Equal(t, fixture.Secret, secret)
			return "fixture-read-token", nil
		},
		Client: &http.Client{Transport: connectorSmokeTransport(func(request *http.Request) (*http.Response, error) {
			calls++
			require.Equal(t, "https://dev.azure.com", request.URL.Scheme+"://"+request.URL.Host)
			require.Equal(t, http.MethodGet, request.Method)
			require.Equal(t, fixture.Path, request.URL.Path)
			params := map[string]string{}
			for key := range request.URL.Query() {
				params[key] = request.URL.Query().Get(key)
			}
			require.Equal(t, fixture.Query, params)
			require.Equal(t, "Bearer fixture-read-token", request.Header.Get("Authorization"))
			body, err := io.ReadAll(request.Body)
			require.NoError(t, err)
			require.Empty(t, body)
			return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(string(payload)))}, nil
		})},
	}, "mcp", fixture.Operation, fixture.Args)
	return result, calls, secrets, executionErr
}

func azureBoundedReaderClone(t *testing.T, source map[string]any) map[string]any {
	t.Helper()
	payload, err := json.Marshal(source)
	require.NoError(t, err)
	var cloned map[string]any
	require.NoError(t, json.Unmarshal(payload, &cloned))
	return cloned
}

func azureBoundedReaderBadArguments(fixture azureBoundedReaderFixture) []map[string]any {
	values := map[string]any{
		"organization": "not-approved", "project": "77777777-7777-4777-8777-777777777777",
		"repository-id": "77777777-7777-4777-8777-777777777777", "query-id": "77777777-7777-4777-8777-777777777777",
		"work-item": "9999", "pull-request-id": "9999", "pipeline-id": "9999", "run-id": "9999",
		"path": "/NOT-APPROVED.md", "commit": strings.Repeat("c", 40),
		"target-ref": "refs/heads/not-approved", "source-ref": "refs/heads/not-approved",
		"first": 101, "skip": 10001, "status": "not-a-status",
	}
	var invalid []map[string]any
	for name := range fixture.Args {
		value, exists := values[name]
		if !exists {
			continue
		}
		args := maps.Clone(fixture.Args)
		args[name] = value
		invalid = append(invalid, args)
		args = maps.Clone(fixture.Args)
		delete(args, name)
		invalid = append(invalid, args)
	}
	return invalid
}

func TestAzureBoundedReadersGoRuntime(t *testing.T) {
	plan := azureBoundedReaderPlan(t)
	require.Empty(t, plan.SafeOutputNames())
	require.ElementsMatch(t, []string{"APPROVED_AZURE_REST_READER", "APPROVED_AZURE_CODE_READER", "APPROVED_AZURE_BUILD_READER"}, plan.Secrets())
	for _, fixture := range azureBoundedReaderFixtures(t) {
		t.Run(fixture.Operation, func(t *testing.T) {
			result, calls, secrets, err := azureBoundedReaderExecute(t, plan, fixture, fixture.Response)
			require.NoError(t, err)
			require.True(t, result.Untrusted)
			require.Equal(t, 1, calls, "one request, no implicit hydration, cursor traversal or retry")
			require.Equal(t, 1, secrets)
			require.NotEmpty(t, result.Data)
			switch fixture.Operation {
			case "get-work-item":
				require.Contains(t, string(result.Data), "Actual **work-item body**")
				require.NotContains(t, string(result.Data), "private")
			case "get-repository-file":
				require.Contains(t, string(result.Data), "Actual repository **source content**")
			case "get-pull-request":
				require.Contains(t, string(result.Data), strings.Repeat("Full PR description. ", 30))
			}

			oversized := azureBoundedReaderClone(t, fixture.Response)
			oversized["padding"] = strings.Repeat("x", 262145)
			_, calls, _, err = azureBoundedReaderExecute(t, plan, fixture, oversized)
			require.Error(t, err)
			require.Equal(t, 1, calls)

			inputs, err := plan.Inputs("mcp", fixture.Operation)
			require.NoError(t, err)
			for name, input := range inputs {
				for _, bad := range []any{map[string]any{"not": "primitive"}, strings.Repeat("x", 2049), -1} {
					args := maps.Clone(fixture.Args)
					args[name] = bad
					badFixture := fixture
					badFixture.Args = args
					_, calls, secrets, err := azureBoundedReaderExecute(t, plan, badFixture, fixture.Response)
					require.Error(t, err, "%s.%s (%s)", fixture.Operation, name, input.Type)
					require.Zero(t, calls)
					require.Zero(t, secrets)
				}
			}
			args := maps.Clone(fixture.Args)
			args["url"] = "https://evil.invalid/"
			badFixture := fixture
			badFixture.Args = args
			_, calls, secrets, err = azureBoundedReaderExecute(t, plan, badFixture, fixture.Response)
			require.Error(t, err)
			require.Zero(t, calls)
			require.Zero(t, secrets)
			for _, args := range azureBoundedReaderBadArguments(fixture) {
				badFixture.Args = args
				_, calls, secrets, err = azureBoundedReaderExecute(t, plan, badFixture, fixture.Response)
				require.Error(t, err, "well-typed but unapproved resources are not authority")
				require.Zero(t, calls)
				require.Zero(t, secrets)
			}
		})
	}
}

func azureBoundedReaderInvalidResponses(t *testing.T, fixture azureBoundedReaderFixture) []map[string]any {
	t.Helper()
	invalid := []map[string]any{{}}
	add := func(change func(map[string]any)) {
		response := azureBoundedReaderClone(t, fixture.Response)
		change(response)
		invalid = append(invalid, response)
	}
	op := fixture.Operation
	switch op {
	case "get-work-item":
		add(func(r map[string]any) { r["id"] = 9877 })
		add(func(r map[string]any) { r["id"] = "9876" })
		add(func(r map[string]any) { r["fields"].(map[string]any)["System.TeamProject"] = "Other Project" })
	case "run-work-item-query":
		add(func(r map[string]any) { r["queryType"] = "tree" })
		add(func(r map[string]any) { r["workItems"] = []any{map[string]any{"id": 9876}, map[string]any{"id": 9999}} })
		add(func(r map[string]any) { r["workItems"] = slices.Repeat([]any{map[string]any{"id": 9876}}, 101) })
		add(func(r map[string]any) {
			r["columns"] = slices.Repeat([]any{map[string]any{"referenceName": "System.Id"}}, 33)
		})
	case "get-repository":
		add(func(r map[string]any) { r["id"] = "55555555-5555-4555-8555-555555555555" })
		add(func(r map[string]any) { r["project"].(map[string]any)["id"] = "other-project" })
	case "get-repository-file":
		add(func(r map[string]any) { r["path"] = "/OTHER.md" })
		add(func(r map[string]any) { r["commitId"] = strings.Repeat("b", 40) })
		add(func(r map[string]any) { r["gitObjectType"] = "tree" })
		add(func(r map[string]any) { r["contentMetadata"].(map[string]any)["isBinary"] = true })
		add(func(r map[string]any) { delete(r, "content") })
		add(func(r map[string]any) { r["content"] = map[string]any{"downloadUrl": "https://evil.invalid"} })
	case "get-pull-request":
		add(func(r map[string]any) { r["pullRequestId"] = 43 })
		add(func(r map[string]any) {
			r["repository"].(map[string]any)["id"] = "55555555-5555-4555-8555-555555555555"
		})
		add(func(r map[string]any) { r["targetRefName"] = "refs/heads/other" })
	case "get-pipeline-definition":
		add(func(r map[string]any) { r["id"] = 13 })
		add(func(r map[string]any) {
			r["repository"].(map[string]any)["id"] = "55555555-5555-4555-8555-555555555555"
		})
	case "get-pipeline-run":
		add(func(r map[string]any) { r["id"] = 91 })
		add(func(r map[string]any) { r["definition"].(map[string]any)["id"] = 13 })
		add(func(r map[string]any) { r["sourceBranch"] = "refs/heads/other" })
	}
	if strings.HasPrefix(op, "list-") {
		for _, pointer := range []string{
			"/value/*/id", "/value/*/pullRequestId", "/value/*/repository/id",
			"/value/*/repository/project/id", "/value/*/project/id", "/value/*/targetRefName",
			"/value/*/definition/id", "/value/*/sourceBranch",
		} {
			item := fixture.Response["value"].([]any)[0].(map[string]any)
			parts := strings.Split(strings.TrimPrefix(pointer, "/value/*/"), "/")
			target := item
			exists := true
			for _, name := range parts[:len(parts)-1] {
				child, ok := target[name].(map[string]any)
				if !ok {
					exists = false
					break
				}
				target = child
			}
			if _, ok := target[parts[len(parts)-1]]; !exists || !ok {
				continue
			}
			add(func(r map[string]any) {
				first := r["value"].([]any)[0].(map[string]any)
				second := azureBoundedReaderClone(t, first)
				target := second
				for _, name := range parts[:len(parts)-1] {
					target = target[name].(map[string]any)
				}
				target[parts[len(parts)-1]] = "not-approved"
				r["value"] = []any{first, second}
			})
		}
		add(func(r map[string]any) {
			item := r["value"].([]any)[0]
			r["value"] = slices.Repeat([]any{item}, 101)
		})
	}
	require.NotEmpty(t, invalid, "every new capability needs response failures: %s", op)
	return invalid
}

func TestAzureBoundedReadersGoResponseEvidence(t *testing.T) {
	plan := azureBoundedReaderPlan(t)
	for _, fixture := range azureBoundedReaderFixtures(t) {
		t.Run(fixture.Operation, func(t *testing.T) {
			for _, response := range azureBoundedReaderInvalidResponses(t, fixture) {
				_, calls, _, err := azureBoundedReaderExecute(t, plan, fixture, response)
				require.Error(t, err, "%s must fail closed", fixture.Operation)
				require.Equal(t, 1, calls)
			}
			if strings.HasPrefix(fixture.Operation, "list-") || fixture.Operation == "run-work-item-query" {
				response := azureBoundedReaderClone(t, fixture.Response)
				key := "value"
				if fixture.Operation == "run-work-item-query" {
					key = "workItems"
				}
				response[key] = []any{}
				result, calls, _, err := azureBoundedReaderExecute(t, plan, fixture, response)
				require.NoError(t, err)
				require.True(t, result.Untrusted)
				require.Equal(t, 1, calls, "an empty authorized list is not missing evidence")
			}
		})
	}
}

func TestAzureBoundedReadersIndependentSelectionAndCredentials(t *testing.T) {
	_, registered, _, err := parseAzureWorkItemParity(t, "", "")
	require.NoError(t, err)
	require.Empty(t, registered.Connectors)
	for _, fixture := range azureBoundedReaderFixtures(t) {
		t.Run(fixture.Operation, func(t *testing.T) {
			selection := "tools:\n  azure-devops: [" + fixture.Operation + "]\n"
			_, _, _, err := parseAzureWorkItemParity(t, "", selection)
			require.Error(t, err, "writer specimen grants and unrelated reader credentials cannot enable new readers")
			compiler, data, path, err := parseAzureWorkItemParity(t, azureBoundedReaderGrants, selection)
			require.NoError(t, err)
			plan := data.Connectors["azure-devops"].Agent
			require.Equal(t, []string{fixture.Secret}, plan.Secrets())
			require.Nil(t, data.Connectors["azure-devops"].SafeOutputs)
			output, err := compiler.CompileToYAML(data, path)
			require.NoError(t, err)
			require.Contains(t, output, "${{ secrets."+fixture.Secret+" }}")
			for _, secret := range []string{"APPROVED_AZURE_WRITER", "APPROVED_AZURE_READER", "APPROVED_AZURE_REST_READER", "APPROVED_AZURE_CODE_READER", "APPROVED_AZURE_BUILD_READER"} {
				if secret != fixture.Secret {
					require.NotContains(t, output, "${{ secrets."+secret+" }}")
				}
			}
			secretCalls, requests := 0, 0
			_, err = plan.Execute(t.Context(), connectors.Runtime{
				Phase: "agent", AllowedOrigins: plan.Origins(),
				ResolveSecret: func(context.Context, string, string) (string, error) {
					secretCalls++
					return "", errors.New("fixture missing selected credential")
				},
				Client: &http.Client{Transport: connectorSmokeTransport(func(*http.Request) (*http.Response, error) {
					requests++
					return nil, errors.New("must not use a fallback credential")
				})},
			}, "mcp", fixture.Operation, fixture.Args)
			require.Error(t, err)
			require.Equal(t, 1, secretCalls)
			require.Zero(t, requests)
		})
	}
}

func TestAzureBoundedReadersGoTransportGuards(t *testing.T) {
	plan := azureBoundedReaderPlan(t)
	for _, fixture := range azureBoundedReaderFixtures(t) {
		for _, status := range []int{200, 302, 401, 503} {
			calls := 0
			_, err := plan.Execute(t.Context(), connectors.Runtime{
				Phase: "agent", AllowedOrigins: plan.Origins(),
				ResolveSecret: func(_ context.Context, phase, name string) (string, error) {
					require.Equal(t, "agent", phase)
					require.Equal(t, fixture.Secret, name)
					return "fixture-read-token", nil
				},
				Client: &http.Client{Transport: connectorSmokeTransport(func(request *http.Request) (*http.Response, error) {
					calls++
					return &http.Response{
						StatusCode: status, Request: request,
						Header: http.Header{"Content-Type": {"text/plain"}, "Location": {"https://evil.invalid/redirect"}},
						Body:   io.NopCloser(strings.NewReader("raw text is not a JSON reader response")),
					}, nil
				})},
			}, "mcp", fixture.Operation, fixture.Args)
			require.Error(t, err, "%s status %d", fixture.Operation, status)
			require.Equal(t, 1, calls, "raw text and status failures never follow redirects, retry or change credential")
		}
	}
}

func TestAzureBoundedReadersEvidenceDocumentation(t *testing.T) {
	fixtureJSON, err := os.ReadFile("testdata/azure_devops_bounded_reader_evidence.json")
	require.NoError(t, err)
	var fixtureEvidence struct {
		Operations map[string]struct {
			URL    string `json:"url"`
			SHA256 string `json:"sha256"`
		} `json:"operations"`
	}
	require.NoError(t, json.Unmarshal(fixtureJSON, &fixtureEvidence))
	require.Len(t, fixtureEvidence.Operations, 10)
	source, err := os.ReadFile("../../.github/workflows/connectors/azure-devops.md")
	require.NoError(t, err)
	parts := strings.SplitN(string(source), "---", 3)
	var document struct {
		Read struct {
			Readers []struct {
				ID string `xml:"id,attr"`
			} `xml:"reader"`
			Evidence struct {
				Sources []struct {
					Operation string `xml:"operation,attr"`
					URL       string `xml:"href,attr"`
					SHA256    string `xml:"sha256,attr"`
				} `xml:"source"`
			} `xml:"evidence"`
		} `xml:"read-parity"`
	}
	body := strings.TrimSuffix(strings.TrimPrefix(strings.TrimSpace(parts[2]), "<!--\n"), "\n-->")
	require.NoError(t, xml.Unmarshal([]byte(body), &document))
	require.Len(t, document.Read.Readers, 10)
	plan := azureBoundedReaderPlan(t)
	encoded, err := plan.Encode()
	require.NoError(t, err)
	raw, err := base64.StdEncoding.DecodeString(encoded)
	require.NoError(t, err)
	var compiled struct {
		Spec connectors.ConnectorSpec `json:"spec"`
	}
	require.NoError(t, json.Unmarshal(raw, &compiled))
	for _, fixture := range azureBoundedReaderFixtures(t) {
		found := false
		for _, evidence := range document.Read.Evidence.Sources {
			if evidence.Operation == fixture.Operation {
				require.Regexp(t, `^[a-f0-9]{64}$`, evidence.SHA256)
				require.True(t, strings.HasPrefix(evidence.URL, "https://learn.microsoft.com/en-us/rest/api/azure/devops/"))
				require.Equal(t, fixtureEvidence.Operations[fixture.Operation].URL, evidence.URL)
				require.Equal(t, fixtureEvidence.Operations[fixture.Operation].SHA256, evidence.SHA256)
				found = true
			}
		}
		require.True(t, found, "fixed provider REST evidence is required for %s", fixture.Operation)
		op := compiled.Spec.MCP[fixture.Operation]
		require.Empty(t, op.Checks, "reads must not misuse safe-output checks")
		require.Nil(t, op.Request.Binary, "uploads are not reader downloads")
		require.Positive(t, op.MaxResponseBytes)
		require.LessOrEqual(t, op.MaxResponseBytes, 262144)
	}
}
