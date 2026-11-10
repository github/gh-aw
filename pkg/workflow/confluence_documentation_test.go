//go:build !integration

package workflow

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"encoding/xml"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/connectors"
	"github.com/stretchr/testify/require"
	"go.yaml.in/yaml/v3"
)

const confluencePublicImport = `imports:
  - uses: ".github/workflows/connectors/confluence.md"
    with:
      clouds: ["11111111-2222-4333-8444-555555555555"]
      spaces: ["123"]
      pages: ["456"]
`

const confluencePublicSelections = `tools:
  confluence: [get-page]
safe-outputs:
  confluence-create-runbook:
  confluence-add-review-comment:
`

type confluenceDocumentation struct {
	XMLName xml.Name `xml:"connector-documentation"`
	ID      string   `xml:"id,attr"`
	Version string   `xml:"version,attr"`
	Example struct {
		Frontmatter string `xml:"frontmatter"`
		Body        string `xml:"body"`
	} `xml:"example"`
}

func confluenceDocumentationSource(t *testing.T) (string, confluenceDocumentation) {
	t.Helper()
	source, err := os.ReadFile("../../.github/workflows/connectors/confluence.md")
	require.NoError(t, err)
	comment := commentedConnectorDocumentation(t, string(source))
	var documentation confluenceDocumentation
	require.NoError(t, xml.Unmarshal([]byte(comment), &documentation))
	require.Equal(t, "confluence", documentation.ID)
	require.Equal(t, "1", documentation.Version)
	for _, section := range []string{"purpose", "authentication", "endpoints", "imports", "triggers", "read-capabilities", "safe-outputs", "context", "sources"} {
		require.Contains(t, comment, "<"+section)
	}
	require.Contains(t, comment, "<frontmatter><![CDATA[")
	require.Contains(t, comment, "<body><![CDATA[")
	return strings.TrimSpace(documentation.Example.Frontmatter), documentation
}

func confluencePublicConsumer(t *testing.T, source string, local bool) (*Compiler, string) {
	t.Helper()
	root := t.TempDir()
	path := filepath.Join(root, ".github", "workflows", "confluence.md")
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0700))
	if local {
		declaration, err := os.ReadFile("../../.github/workflows/connectors/confluence.md")
		require.NoError(t, err)
		target := filepath.Join(root, ".github", "workflows", "connectors", "confluence.md")
		require.NoError(t, os.MkdirAll(filepath.Dir(target), 0700))
		require.NoError(t, os.WriteFile(target, declaration, 0600))
		source = strings.ReplaceAll(source, ".github/workflows/connectors/confluence.md", ".github/workflows/connectors/confluence.md")
	}
	require.NoError(t, os.WriteFile(path, []byte(source), 0600))
	compiler := NewCompiler(WithVersion("v0.0.0-1-gabcdef0"))
	compiler.gitRoot = root
	installConnectorTestSources(t, root)
	compiler.SetActionMode(ActionModeDev)
	compiler.SetStrictMode(true)
	return compiler, path
}

func confluencePublicWorkflow(t *testing.T, imports, selection string) (*Compiler, *WorkflowData, string, error) {
	t.Helper()
	source := awConnectorHeader + imports + selection + "---\nRead only approved untrusted page data.\n"
	compiler, path := confluencePublicConsumer(t, source, false)
	data, err := compiler.ParseWorkflowFile(path)
	return compiler, data, path, err
}

func confluencePublicSpec(t *testing.T, plan *connectors.Plan) connectors.ConnectorSpec {
	t.Helper()
	encoded, err := plan.Encode()
	require.NoError(t, err)
	raw, err := base64.StdEncoding.DecodeString(encoded)
	require.NoError(t, err)
	var snapshot struct {
		Spec connectors.ConnectorSpec `json:"spec"`
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	require.NoError(t, decoder.Decode(&snapshot))
	_, err = connectors.DecodePlan(encoded)
	require.NoError(t, err)
	return snapshot.Spec
}

func TestConfluenceDocumentationConsumer(t *testing.T) {
	frontmatter, documentation := confluenceDocumentationSource(t)
	var fields map[string]any
	require.NoError(t, yaml.Unmarshal([]byte(frontmatter), &fields))
	require.Equal(t, "workflow_dispatch", fields["on"])
	require.Equal(t, true, fields["safe-outputs"].(map[string]any)["staged"])
	require.NotEmpty(t, documentation.Example.Body)
	source := "---\n" + frontmatter + "\n---\n" + strings.TrimSpace(documentation.Example.Body) + "\n"
	for _, local := range []bool{false, true} {
		t.Run(map[bool]string{false: "embedded", true: "local"}[local], func(t *testing.T) {
			compiler, path := confluencePublicConsumer(t, source, local)
			require.NoError(t, compiler.CompileWorkflow(path))
			lock, err := os.ReadFile(strings.TrimSuffix(path, ".md") + ".lock.yml")
			require.NoError(t, err)
			var workflow struct {
				Jobs map[string]any `yaml:"jobs"`
			}
			require.NoError(t, yaml.Unmarshal(lock, &workflow))
			agent, err := json.Marshal(workflow.Jobs["agent"])
			require.NoError(t, err)
			effects, err := json.Marshal(workflow.Jobs["safe_outputs"])
			require.NoError(t, err)
			require.Contains(t, string(agent), "${{ secrets.CONFLUENCE_READ_ACCESS_TOKEN }}")
			for _, secret := range []string{"CONFLUENCE_PAGE_WRITE_ACCESS_TOKEN", "CONFLUENCE_COMMENT_WRITE_ACCESS_TOKEN"} {
				require.NotContains(t, string(agent), secret)
				require.NotContains(t, string(effects), "${{ secrets."+secret+" }}", "staging must not resolve writer secrets")
			}
			require.NotContains(t, string(effects), "${{ secrets.CONFLUENCE_READ_ACCESS_TOKEN }}")
			require.NotContains(t, string(lock), "CONFLUENCE_CONTEXT")
			if !local {
				_, err := os.Stat(filepath.Join(compiler.gitRoot, ".github", "aw"))
				require.ErrorIs(t, err, os.ErrNotExist, "embedded import must not require a consumer connector copy")
			}
		})
	}
}

func TestConfluencePublicIndependentSelections(t *testing.T) {
	for _, test := range []struct {
		name, selection string
		read, write     bool
	}{
		{"registration", "", false, false},
		{"read", "tools:\n  confluence: [get-page]\n", true, false},
		{"create", "safe-outputs:\n  confluence-create-runbook:\n", false, true},
		{"comment", "safe-outputs:\n  confluence-add-review-comment:\n", false, true},
		{"combined", confluencePublicSelections, true, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			compiler, data, path, err := confluencePublicWorkflow(t, confluencePublicImport, test.selection)
			require.NoError(t, err)
			if !test.read && !test.write {
				require.Empty(t, data.Connectors)
				return
			}
			selected := data.Connectors["confluence"]
			require.NotNil(t, selected)
			require.Equal(t, test.read, selected.Agent != nil)
			require.Equal(t, test.write, selected.SafeOutputs != nil)
			require.Nil(t, selected.Context)
			if test.read {
				require.Equal(t, []string{"CONFLUENCE_READ_ACCESS_TOKEN"}, selected.Agent.Secrets())
			}
			if test.write && !test.read {
				require.Len(t, selected.SafeOutputs.Secrets(), 1)
			}
			_, err = compiler.CompileToYAML(data, path)
			require.NoError(t, err)
		})
	}
}

func TestConfluencePublicResourceGrants(t *testing.T) {
	const read = "tools:\n  confluence: [get-page]\n"
	for _, grant := range []string{
		`      clouds: ["11111111-2222-4333-8444-555555555555"]`,
		`      spaces: ["123"]`,
		`      pages: ["456"]`,
	} {
		_, _, _, err := confluencePublicWorkflow(t, strings.Replace(confluencePublicImport, grant+"\n", "", 1), read)
		require.ErrorContains(t, err, "resource allowlist")
	}
	_, _, _, err := confluencePublicWorkflow(t, strings.Replace(confluencePublicImport, `pages: ["456"]`, `pages: ["*"]`, 1), read)
	require.ErrorContains(t, err, "exact allowlist")
	_, data, _, err := confluencePublicWorkflow(t, "imports: ['.github/workflows/connectors/confluence.md']\n", "")
	require.NoError(t, err)
	require.Empty(t, data.Connectors)
	comment := "safe-outputs:\n  confluence-add-review-comment:\n"
	imports := strings.Replace(confluencePublicImport, "      spaces: [\"123\"]\n", "", 1)
	imports += "      comment-writer-secret: APPROVED_CONFLUENCE_COMMENT_TOKEN\n"
	_, data, _, err = confluencePublicWorkflow(t, imports, comment)
	require.NoError(t, err, "comment authority requires only clouds and pages")
	require.Equal(t, []string{"APPROVED_CONFLUENCE_COMMENT_TOKEN"}, data.Connectors["confluence"].SafeOutputs.Secrets())
}

func TestConfluencePublicUnsupportedSurfaces(t *testing.T) {
	for _, selection := range []string{
		"tools:\n  confluence: [create-runbook]\n",
		"tools:\n  confluence: [hosted-mcp]\n",
		"safe-outputs:\n  confluence-update-page:\n",
		"safe-outputs:\n  confluence-add-review-comment: {unknown: true}\n",
		"context:\n  confluence: {}\n",
	} {
		_, _, _, err := confluencePublicWorkflow(t, confluencePublicImport, selection)
		require.Error(t, err)
	}
	for _, trigger := range []string{"page-created", "page-updated"} {
		source := strings.Replace(awConnectorHeader, "on: workflow_dispatch", "on: confluence-"+trigger, 1) +
			confluencePublicImport + "---\nDo not activate ingress.\n"
		compiler, path := confluencePublicConsumer(t, source, false)
		require.ErrorContains(t, compiler.CompileWorkflow(path), "gateway-dispatch")
		_, err := os.Stat(strings.TrimSuffix(path, ".md") + ".lock.yml")
		require.ErrorIs(t, err, os.ErrNotExist)
	}
}

func TestConfluencePublicProviderContract(t *testing.T) {
	_, data, _, err := confluencePublicWorkflow(t, confluencePublicImport, confluencePublicSelections)
	require.NoError(t, err)
	spec := confluencePublicSpec(t, data.Connectors["confluence"].Plan)
	require.Equal(t, "https://api.atlassian.com", spec.Endpoints["api"])
	for name, scope := range map[string]string{
		"reader": "read:page:confluence", "page-writer": "write:page:confluence", "comment-writer": "write:comment:confluence",
	} {
		credential := spec.Credentials[name]
		require.Equal(t, "bearer", credential.Scheme)
		require.Equal(t, "Authorization", credential.Header)
		require.Equal(t, []string{scope}, credential.Scopes)
	}
	require.Empty(t, spec.HTTPMCP)
	require.Empty(t, spec.Context)
	read := spec.MCP["get-page"]
	require.Equal(t, "get-page", read.Tool)
	require.Equal(t, "GET", read.Request.Method)
	require.Equal(t, "/ex/confluence/{cloud}/wiki/api/v2/pages", read.Request.Path)
	require.Equal(t, "page-id", read.Request.Query["id"].Input)
	require.Equal(t, "space-id", read.Request.Query["space-id"].Input)
	require.Equal(t, "storage", read.Request.Query["body-format"].Literal)
	require.Equal(t, json.Number("1"), read.Request.Query["limit"].Literal)
	require.Len(t, spec.SafeOutputs, 2)
	for _, operation := range spec.SafeOutputs {
		require.Equal(t, 65536, operation.MaxResponseBytes)
		require.Empty(t, operation.Request.Body["body"].Input)
		require.Equal(t, map[string]any{
			"representation": "storage", "value": "<p>Review the workflow result before taking action.</p>",
		}, operation.Request.Body["body"].Literal)
	}
}

func TestConfluencePublicIdentifierShapes(t *testing.T) {
	_, data, _, err := confluencePublicWorkflow(t, confluencePublicImport, confluencePublicSelections)
	require.NoError(t, err)
	spec := confluencePublicSpec(t, data.Connectors["confluence"].Plan)
	operations := []connectors.Operation{spec.MCP["get-page"].Operation}
	for _, operation := range spec.SafeOutputs {
		operations = append(operations, operation)
	}
	for _, operation := range operations {
		for name, input := range operation.Inputs {
			require.Equal(t, "string", input.Type)
			require.True(t, input.Required)
			pattern, err := regexp.Compile(input.Pattern)
			require.NoError(t, err)
			if name == "cloud-id" {
				require.Equal(t, 36, input.MaxLength)
				require.True(t, pattern.MatchString(confluenceFixtureCloudID))
				require.False(t, pattern.MatchString("https://example.invalid"))
				continue
			}
			require.Equal(t, 20, input.MaxLength)
			for _, valid := range []string{"1", "456", strings.Repeat("9", 20)} {
				require.True(t, pattern.MatchString(valid))
			}
			for _, invalid := range []string{"", "0", "01", "-1", "1.5", "1/2", " 456", strings.Repeat("9", 21)} {
				require.False(t, pattern.MatchString(invalid))
			}
		}
	}
}

func confluencePublicRuntime(t *testing.T, phase, response string, calls, resolutions *int) connectors.Runtime {
	t.Helper()
	return connectors.Runtime{
		Phase: phase, AllowWrites: phase == "safe-output", AllowedOrigins: []string{"https://api.atlassian.com"},
		ResolveSecret: func(_ context.Context, requestedPhase, name string) (string, error) {
			*resolutions++
			require.Equal(t, phase, requestedPhase)
			if phase == "agent" {
				require.Equal(t, "CONFLUENCE_READ_ACCESS_TOKEN", name)
			} else {
				require.Contains(t, []string{"CONFLUENCE_PAGE_WRITE_ACCESS_TOKEN", "CONFLUENCE_COMMENT_WRITE_ACCESS_TOKEN"}, name)
			}
			return "fixture-access-token", nil
		},
		Client: &http.Client{Transport: connectorSmokeTransport(func(request *http.Request) (*http.Response, error) {
			*calls++
			require.Equal(t, "api.atlassian.com", request.URL.Host)
			require.Equal(t, "Bearer fixture-access-token", request.Header.Get("Authorization"))
			require.True(t, strings.HasPrefix(request.URL.Path, "/ex/confluence/"+confluenceFixtureCloudID+"/wiki/api/v2/"))
			if phase == "agent" {
				require.Equal(t, http.MethodGet, request.Method)
				require.Equal(t, "body-format=storage&id=456&limit=1&space-id=123&status=current", request.URL.RawQuery)
			} else {
				confluencePublicWriteRequest(t, request)
			}
			return &http.Response{StatusCode: http.StatusOK, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(response)), Request: request}, nil
		})},
	}
}

func confluencePublicWriteRequest(t *testing.T, request *http.Request) {
	t.Helper()
	require.Equal(t, http.MethodPost, request.Method)
	require.Equal(t, "application/json", request.Header.Get("Content-Type"))
	var body map[string]any
	require.NoError(t, json.NewDecoder(request.Body).Decode(&body))
	require.Equal(t, map[string]any{"representation": "storage", "value": "<p>Review the workflow result before taking action.</p>"}, body["body"])
	if strings.HasSuffix(request.URL.Path, "/footer-comments") {
		require.Len(t, body, 2)
		require.Equal(t, "456", body["pageId"])
	} else {
		require.True(t, strings.HasSuffix(request.URL.Path, "/pages"))
		require.Len(t, body, 5)
		require.Equal(t, "123", body["spaceId"])
		require.Equal(t, "456", body["parentId"])
		require.Equal(t, "current", body["status"])
		require.Equal(t, "Agentic workflow runbook", body["title"])
	}
}

func TestConfluencePublicReadBoundaries(t *testing.T) {
	_, data, _, err := confluencePublicWorkflow(t, confluencePublicImport, confluencePublicSelections)
	require.NoError(t, err)
	fixture, err := os.ReadFile("../connectors/testdata/providers/confluence/pages-response.json")
	require.NoError(t, err)
	plan := data.Connectors["confluence"].Agent
	for name, response := range map[string]string{
		"valid": string(fixture), "wrong-page": strings.ReplaceAll(string(fixture), `"456"`, `"999"`),
		"wrong-space":  strings.ReplaceAll(string(fixture), `"123"`, `"999"`),
		"wrong-format": strings.ReplaceAll(string(fixture), `"storage"`, `"view"`),
		"empty":        `{"results":[]}`, "malformed": `{`,
	} {
		t.Run(name, func(t *testing.T) {
			calls, resolutions := 0, 0
			result, err := plan.Execute(t.Context(), confluencePublicRuntime(t, "agent", response, &calls, &resolutions),
				"mcp", "get-page", map[string]any{"cloud-id": confluenceFixtureCloudID, "space-id": "123", "page-id": "456"})
			if name == "valid" {
				require.NoError(t, err)
				require.True(t, result.Untrusted)
				var projected map[string]any
				require.NoError(t, json.Unmarshal(result.Data, &projected))
				require.Len(t, projected, 6)
				require.Equal(t, "456", projected["page"])
			} else {
				require.Error(t, err)
			}
			require.Equal(t, 1, calls)
			require.Equal(t, 1, resolutions)
		})
	}
}

func TestConfluencePublicLiteralWrites(t *testing.T) {
	_, data, _, err := confluencePublicWorkflow(t, confluencePublicImport, confluencePublicSelections)
	require.NoError(t, err)
	plan := data.Connectors["confluence"].SafeOutputs
	for _, operation := range []string{"create-runbook", "add-review-comment"} {
		t.Run(operation, func(t *testing.T) {
			args := map[string]any{"cloud-id": confluenceFixtureCloudID}
			response := `{"id":"789","version":{"number":1},`
			if operation == "create-runbook" {
				args["space-id"], args["parent-id"] = "123", "456"
				response += `"spaceId":"123","parentId":"456"}`
			} else {
				args["page-id"] = "456"
				response += `"pageId":"456"}`
			}
			calls, resolutions := 0, 0
			staged, err := plan.PrepareSafeOutput(operation, args)
			require.NoError(t, err)
			require.Zero(t, calls)
			require.Zero(t, resolutions)
			_, err = plan.ExecuteSafeOutput(t.Context(), confluencePublicRuntime(t, "safe-output", response, &calls, &resolutions), *staged)
			require.NoError(t, err)
			require.Equal(t, 1, calls)
			require.Equal(t, 1, resolutions)
		})
	}
}

func TestConfluencePublicReadSizeLimit(t *testing.T) {
	_, data, _, err := confluencePublicWorkflow(t, confluencePublicImport, "tools:\n  confluence: [get-page]\n")
	require.NoError(t, err)
	fixture, err := os.ReadFile("../connectors/testdata/providers/confluence/pages-response.json")
	require.NoError(t, err)
	for _, size := range []int{65535, 65536, 65537} {
		calls, resolutions := 0, 0
		response := strings.Repeat(" ", size-len(fixture)) + string(fixture)
		_, err := data.Connectors["confluence"].Agent.Execute(t.Context(),
			confluencePublicRuntime(t, "agent", response, &calls, &resolutions), "mcp", "get-page",
			map[string]any{"cloud-id": confluenceFixtureCloudID, "space-id": "123", "page-id": "456"})
		if size > 65536 {
			require.ErrorContains(t, err, "byte bound")
		} else {
			require.NoError(t, err)
		}
		require.Equal(t, 1, calls)
	}
}

func TestConfluencePublicDenyBeforeCredentials(t *testing.T) {
	_, data, _, err := confluencePublicWorkflow(t, confluencePublicImport, confluencePublicSelections)
	require.NoError(t, err)
	calls, resolutions := 0, 0
	runtime := connectors.Runtime{
		Phase: "safe-output", AllowWrites: true, AllowedOrigins: []string{"https://api.atlassian.com"},
		ResolveSecret: func(context.Context, string, string) (string, error) {
			resolutions++
			return "", errors.New("must not resolve")
		},
		Client: &http.Client{Transport: connectorSmokeTransport(func(*http.Request) (*http.Response, error) {
			calls++
			return nil, errors.New("must not send")
		})},
	}
	for key, value := range map[string]any{
		"cloud-id": "22222222-2222-4222-8222-222222222222", "page-id": "999",
		"body": "<script>not supported</script>", "title": "not supported", "version": 2,
	} {
		args := map[string]any{"cloud-id": confluenceFixtureCloudID, "page-id": "456"}
		args[key] = value
		_, err := data.Connectors["confluence"].SafeOutputs.Execute(t.Context(), runtime, "safe-output", "add-review-comment", args)
		require.Error(t, err)
	}
	require.Zero(t, calls)
	require.Zero(t, resolutions)
}
