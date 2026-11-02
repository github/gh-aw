//go:build !integration

package workflow

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/github/gh-aw/pkg/connectors"
	"github.com/github/gh-aw/pkg/parser"
	"github.com/stretchr/testify/require"
	"go.yaml.in/yaml/v3"
)

type connectorSmokeTarget struct {
	provider, resource, scope, team, organization string
}

func connectorSmokePlan(t *testing.T, target connectorSmokeTarget) *connectors.Plan {
	t.Helper()
	var declaration, resources, frontmatter map[string]any
	if target.provider != "linear" {
		source, err := os.ReadFile("../../.github/aw/connectors/" + target.provider + ".md")
		require.NoError(t, err)
		document, err := parser.ExtractFrontmatterFromContent(string(source))
		require.NoError(t, err)
		frontmatter = document.Frontmatter
		declarations, ok := frontmatter["connectors"].(map[string]any)
		require.True(t, ok)
		declaration, ok = declarations[target.provider].(map[string]any)
		require.True(t, ok)
		resources, ok = declaration["resources"].(map[string]any)
		require.True(t, ok)
	}
	var imported any = ".github/aw/connectors/" + target.provider + ".md"
	var tool string
	switch target.provider {
	case "linear":
		require.NotEmpty(t, target.resource, "resource_id must be an approved Linear issue UUID")
		require.NotEmpty(t, target.scope, "scope_id must be the approved Linear organization UUID")
		require.NotEmpty(t, target.team, "LINEAR_TEAM_ID is required")
		imported = map[string]any{
			"uses": "@builtin:connectors/linear.md",
			"with": map[string]any{"issues": []string{target.resource}, "organizations": []string{target.scope}, "teams": []string{target.team}},
		}
		tool = "get-issue"
	case "jira":
		require.NotEmpty(t, target.resource, "resource_id must be an approved Jira issue key")
		require.NotEmpty(t, target.scope, "scope_id must be the approved Jira cloud ID")
		resources["issues"], resources["cloud-ids"] = []string{target.resource}, []string{target.scope}
		tool = "personal"
	case "azure-devops":
		require.Regexp(t, `^[A-Za-z0-9][A-Za-z0-9-]{0,49}$`, target.organization, "organization must be an Azure DevOps organization name, not a URL")
		servers, ok := declaration["mcp-servers"].(map[string]any)
		require.True(t, ok)
		server, ok := servers["azure-devops"].(map[string]any)
		require.True(t, ok)
		server["url"] = "https://mcp.dev.azure.com/" + target.organization
		resources["organizations"] = []string{target.organization}
		tool = "azure-devops"
	default:
		t.Fatalf("unsupported live connector %q", target.provider)
	}
	root := t.TempDir()
	if frontmatter != nil {
		dir := filepath.Join(root, ".github/aw/connectors")
		require.NoError(t, os.MkdirAll(dir, 0700))
		config, err := yaml.Marshal(frontmatter)
		require.NoError(t, err)
		require.NoError(t, os.WriteFile(filepath.Join(dir, target.provider+".md"), []byte("---\n"+string(config)+"---\n"), 0600))
	}
	content, err := yaml.Marshal(map[string]any{
		"on": "workflow_dispatch", "engine": "copilot",
		"permissions": map[string]string{"contents": "read"},
		"imports":     []any{imported},
		"tools":       map[string][]string{target.provider: {tool}},
	})
	require.NoError(t, err)
	dir := filepath.Join(root, ".github/workflows")
	require.NoError(t, os.MkdirAll(dir, 0700))
	path := filepath.Join(dir, "connector-smoke.md")
	require.NoError(t, os.WriteFile(path, []byte("---\n"+string(content)+"---\nRead approved references only.\n"), 0600))
	compiler := NewCompiler(WithNoEmit(true))
	compiler.gitRoot = root
	data, err := compiler.ParseWorkflowFile(path)
	require.NoError(t, err)
	selected := data.Connectors[target.provider]
	require.NotNil(t, selected)
	require.Nil(t, selected.Context)
	require.Nil(t, selected.SafeOutputs)
	require.NotNil(t, selected.Agent)
	return selected.Agent
}

func connectorSmokeSecret(_ context.Context, phase, name string) (string, error) {
	if phase != "agent" {
		return "", errors.New("connector smoke tests never resolve write credentials")
	}
	if name == "JIRA_MCP_PERSONAL_BASIC_AUTH" {
		email, token := os.Getenv("JIRA_USER_EMAIL"), os.Getenv("JIRA_API_TOKEN")
		if email == "" || token == "" {
			return "", errors.New("JIRA_USER_EMAIL and JIRA_API_TOKEN are required for the selected Jira check")
		}
		return base64.StdEncoding.EncodeToString([]byte(email + ":" + token)), nil
	}
	if name != "LINEAR_READ_API_KEY" && name != "AZURE_DEVOPS_MCP_ENTRA_TOKEN" {
		return "", errors.New("connector smoke test requested an unexpected credential")
	}
	value := os.Getenv(name)
	if value == "" {
		return "", errors.New("the selected connector read credential is not configured")
	}
	return value, nil
}

func TestConnectorLiveProvider(t *testing.T) {
	provider := os.Getenv("GH_AW_CONNECTOR_PROVIDER")
	if provider == "" {
		t.Skip("live provider checks require explicit GH_AW_CONNECTOR_PROVIDER opt-in")
	}
	target := connectorSmokeTarget{
		provider: provider, resource: os.Getenv("GH_AW_CONNECTOR_RESOURCE_ID"),
		scope: os.Getenv("GH_AW_CONNECTOR_SCOPE_ID"), team: os.Getenv("LINEAR_TEAM_ID"),
		organization: os.Getenv("GH_AW_CONNECTOR_ORGANIZATION"),
	}
	client := &http.Client{Timeout: 20 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	target = discoverConnectorSmokeTarget(t, target, client)
	plan := connectorSmokePlan(t, target)
	for _, name := range plan.Secrets() {
		_, err := connectorSmokeSecret(t.Context(), "agent", name)
		require.NoError(t, err, "selected live checks must fail, not skip, when credentials are missing")
	}
	runtime := connectors.Runtime{
		Phase: "agent", AllowedOrigins: []string{"https://api.linear.app", "https://mcp.atlassian.com", "https://mcp.dev.azure.com"},
		ResolveSecret: connectorSmokeSecret, Client: client,
	}
	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
	defer cancel()
	switch provider {
	case "linear":
		result, err := plan.Execute(ctx, runtime, "mcp", "get-issue", map[string]any{"issue-id": target.resource})
		require.NoError(t, err)
		require.NotNil(t, result)
		require.True(t, result.Untrusted)
	case "jira":
		result, err := plan.CallMCP(ctx, runtime, "personal", "getJiraIssue", map[string]any{"cloudId": target.scope, "issueIdOrKey": target.resource})
		require.NoError(t, err)
		require.NotNil(t, result)
	case "azure-devops":
		result, err := plan.CallMCP(ctx, runtime, "azure-devops", "core_list_projects", map[string]any{})
		require.NoError(t, err)
		require.NotNil(t, result)
	}
	t.Log("Read-only connector request completed; provider content is not logged.")
}

func connectorSmokeJSON(t *testing.T, client *http.Client, request *http.Request, target any) {
	t.Helper()
	response, err := client.Do(request)
	require.NoError(t, err, "read-only target discovery failed")
	defer func() { require.NoError(t, response.Body.Close()) }()
	require.Equal(t, http.StatusOK, response.StatusCode, "read-only target discovery failed; response content is withheld")
	data, err := io.ReadAll(io.LimitReader(response.Body, 65537))
	require.NoError(t, err)
	require.LessOrEqual(t, len(data), 65536, "target discovery exceeded its byte limit")
	require.NoError(t, json.Unmarshal(data, target))
}

func discoverConnectorSmokeTarget(t *testing.T, target connectorSmokeTarget, client *http.Client) connectorSmokeTarget {
	t.Helper()
	if target.provider == "linear" && (target.resource == "" || target.scope == "") {
		require.NotEmpty(t, target.team, "LINEAR_TEAM_ID is required for bounded discovery")
		token, err := connectorSmokeSecret(t.Context(), "agent", "LINEAR_READ_API_KEY")
		require.NoError(t, err)
		body, err := json.Marshal(map[string]any{
			"query":     `query ConnectorSmokeTarget($team: String!) { organization { id } team(id: $team) { id issues(first: 1) { nodes { id } } } }`,
			"variables": map[string]string{"team": target.team},
		})
		require.NoError(t, err)
		request, err := http.NewRequestWithContext(t.Context(), http.MethodPost, "https://api.linear.app/graphql", strings.NewReader(string(body)))
		require.NoError(t, err)
		request.Header.Set("Authorization", token)
		request.Header.Set("Content-Type", "application/json")
		var result struct {
			Data struct {
				Organization struct{ ID string }
				Team         struct {
					ID     string
					Issues struct{ Nodes []struct{ ID string } }
				}
			}
			Errors []json.RawMessage
		}
		connectorSmokeJSON(t, client, request, &result)
		require.Empty(t, result.Errors, "target discovery returned GraphQL errors; content is withheld")
		require.Equal(t, target.team, result.Data.Team.ID, "discovery must remain in LINEAR_TEAM_ID")
		require.NotEmpty(t, result.Data.Organization.ID)
		if target.scope == "" {
			target.scope = result.Data.Organization.ID
		}
		if target.resource == "" {
			require.Len(t, result.Data.Team.Issues.Nodes, 1, "the approved team must contain a readable issue")
			target.resource = result.Data.Team.Issues.Nodes[0].ID
		}
	}
	if target.provider == "jira" && (target.resource == "" || target.scope == "") {
		base := os.Getenv("JIRA_BASE_URL")
		require.Regexp(t, `^https://[a-z0-9][a-z0-9-]*\.atlassian\.net/?$`, base, "JIRA_BASE_URL must pin an approved Jira Cloud site")
		base = strings.TrimSuffix(base, "/")
		if target.scope == "" {
			request, err := http.NewRequestWithContext(t.Context(), http.MethodGet, base+"/_edge/tenant_info", nil)
			require.NoError(t, err)
			var tenant struct{ CloudID string }
			connectorSmokeJSON(t, client, request, &tenant)
			require.NotEmpty(t, tenant.CloudID)
			target.scope = tenant.CloudID
		}
		if target.resource == "" {
			project := os.Getenv("JIRA_PROJECT_KEY")
			require.Regexp(t, `^[A-Z][A-Z0-9_]*$`, project, "JIRA_PROJECT_KEY is required for bounded discovery")
			credential, err := connectorSmokeSecret(t.Context(), "agent", "JIRA_MCP_PERSONAL_BASIC_AUTH")
			require.NoError(t, err)
			query := url.Values{"jql": {"project = " + project + " ORDER BY created DESC"}, "maxResults": {"1"}, "fields": {"key"}}
			request, err := http.NewRequestWithContext(t.Context(), http.MethodGet, base+"/rest/api/3/search/jql?"+query.Encode(), nil)
			require.NoError(t, err)
			request.Header.Set("Authorization", "Basic "+credential)
			var result struct{ Issues []struct{ Key string } }
			connectorSmokeJSON(t, client, request, &result)
			require.Len(t, result.Issues, 1, "the approved project must contain a readable issue")
			require.True(t, strings.HasPrefix(result.Issues[0].Key, project+"-"), "discovery returned another project")
			target.resource = result.Issues[0].Key
		}
	}
	return target
}

func TestConnectorSmokePlans(t *testing.T) {
	for _, target := range []connectorSmokeTarget{
		{provider: "linear", resource: "22222222-2222-4222-8222-222222222222", scope: "11111111-1111-4111-8111-111111111111", team: "33333333-3333-4333-8333-333333333333"},
		{provider: "jira", resource: "APP-1", scope: "fixture-cloud"},
		{provider: "azure-devops", organization: "fabrikam"},
	} {
		t.Run(target.provider, func(t *testing.T) {
			plan := connectorSmokePlan(t, target)
			expected := map[string]string{"linear": "LINEAR_READ_API_KEY", "jira": "JIRA_MCP_PERSONAL_BASIC_AUTH", "azure-devops": "AZURE_DEVOPS_MCP_ENTRA_TOKEN"}
			require.Equal(t, []string{expected[target.provider]}, plan.Secrets())
			_, err := plan.Encode()
			require.NoError(t, err)
		})
	}
}

type connectorSmokeTransport func(*http.Request) (*http.Response, error)

func (transport connectorSmokeTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	return transport(request)
}

func TestConnectorSmokeReadAndCredentialBoundaries(t *testing.T) {
	t.Setenv("LINEAR_READ_API_KEY", "fixture-read-key")
	t.Setenv("JIRA_USER_EMAIL", "fixture@example.com")
	t.Setenv("JIRA_API_TOKEN", "fixture-jira-token")
	value, err := connectorSmokeSecret(t.Context(), "agent", "JIRA_MCP_PERSONAL_BASIC_AUTH")
	require.NoError(t, err)
	require.Equal(t, base64.StdEncoding.EncodeToString([]byte("fixture@example.com:fixture-jira-token")), value)
	_, err = connectorSmokeSecret(t.Context(), "safe-output", "LINEAR_READ_API_KEY")
	require.ErrorContains(t, err, "never resolve write credentials")
	_, err = connectorSmokeSecret(t.Context(), "agent", "LINEAR_WRITE_API_KEY")
	require.ErrorContains(t, err, "unexpected credential")
	t.Setenv("AZURE_DEVOPS_MCP_ENTRA_TOKEN", "")
	_, err = connectorSmokeSecret(t.Context(), "agent", "AZURE_DEVOPS_MCP_ENTRA_TOKEN")
	require.ErrorContains(t, err, "not configured")
	plan := connectorSmokePlan(t, connectorSmokeTarget{
		provider: "linear", resource: "22222222-2222-4222-8222-222222222222",
		scope: "11111111-1111-4111-8111-111111111111", team: "33333333-3333-4333-8333-333333333333",
	})
	calls := 0
	runtime := connectors.Runtime{
		Phase: "agent", AllowedOrigins: []string{"https://api.linear.app"}, ResolveSecret: connectorSmokeSecret,
		Client: &http.Client{Transport: connectorSmokeTransport(func(request *http.Request) (*http.Response, error) {
			calls++
			require.Equal(t, "api.linear.app", request.URL.Host)
			require.Equal(t, "fixture-read-key", request.Header.Get("Authorization"))
			body, err := io.ReadAll(request.Body)
			require.NoError(t, err)
			require.NotContains(t, string(body), "mutation")
			payload := `{"data":{"organization":{"id":"11111111-1111-4111-8111-111111111111"},"issue":{"id":"22222222-2222-4222-8222-222222222222","title":"fixture","team":{"id":"33333333-3333-4333-8333-333333333333"}}}}`
			return &http.Response{StatusCode: http.StatusOK, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(payload)), Request: request}, nil
		})},
	}

	result, err := plan.Execute(t.Context(), runtime, "mcp", "get-issue", map[string]any{"issue-id": "22222222-2222-4222-8222-222222222222"})
	require.NoError(t, err)
	require.True(t, result.Untrusted)
	_, err = plan.Execute(t.Context(), runtime, "mcp", "get-issue", map[string]any{"issue-id": "99999999-9999-4999-8999-999999999999"})
	require.Error(t, err)
	require.Equal(t, 1, calls, "an unapproved target must fail before any HTTP request")
}

func TestConnectorSmokeBoundedDiscovery(t *testing.T) {
	t.Setenv("LINEAR_READ_API_KEY", "fixture-read-key")
	t.Setenv("JIRA_USER_EMAIL", "fixture@example.com")
	t.Setenv("JIRA_API_TOKEN", "fixture-jira-token")
	t.Setenv("JIRA_BASE_URL", "https://example.atlassian.net")
	t.Setenv("JIRA_PROJECT_KEY", "APP")
	calls := 0
	client := &http.Client{Transport: connectorSmokeTransport(func(request *http.Request) (*http.Response, error) {
		calls++
		var body string
		switch request.URL.Host + request.URL.Path {
		case "api.linear.app/graphql":
			require.Equal(t, "fixture-read-key", request.Header.Get("Authorization"))
			body = `{"data":{"organization":{"id":"organization"},"team":{"id":"team","issues":{"nodes":[{"id":"issue"}]}}}}`
		case "example.atlassian.net/_edge/tenant_info":
			require.Empty(t, request.Header.Get("Authorization"), "public tenant lookup needs no credential")
			body = `{"cloudId":"cloud"}`
		case "example.atlassian.net/rest/api/3/search/jql":
			require.Equal(t, "1", request.URL.Query().Get("maxResults"))
			require.Equal(t, "project = APP ORDER BY created DESC", request.URL.Query().Get("jql"))
			require.Equal(t, "key", request.URL.Query().Get("fields"))
			require.Equal(t, "Basic "+base64.StdEncoding.EncodeToString([]byte("fixture@example.com:fixture-jira-token")), request.Header.Get("Authorization"))
			body = `{"issues":[{"key":"APP-1"}]}`
		default:
			t.Fatalf("unexpected discovery endpoint %s", request.URL.Host+request.URL.Path)
		}
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body)), Request: request}, nil
	})}
	linear := discoverConnectorSmokeTarget(t, connectorSmokeTarget{provider: "linear", team: "team"}, client)
	require.Equal(t, "issue", linear.resource)
	require.Equal(t, "organization", linear.scope)
	jira := discoverConnectorSmokeTarget(t, connectorSmokeTarget{provider: "jira"}, client)
	require.Equal(t, "APP-1", jira.resource)
	require.Equal(t, "cloud", jira.scope)
	require.Equal(t, 3, calls)
	discoverConnectorSmokeTarget(t, linear, client)
	discoverConnectorSmokeTarget(t, jira, client)
	require.Equal(t, 3, calls, "explicit targets must not trigger discovery")
}
