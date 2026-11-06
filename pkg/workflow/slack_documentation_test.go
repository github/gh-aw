//go:build !integration

package workflow

import (
	"context"
	"encoding/json"
	"encoding/xml"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/connectors"
	"github.com/stretchr/testify/require"
	"go.yaml.in/yaml/v3"
)

type slackDocumentation struct {
	XMLName xml.Name                   `xml:"connector-documentation"`
	ID      string                     `xml:"id,attr"`
	Version string                     `xml:"version,attr"`
	Reads   []slackDocumentedOperation `xml:"reads>operation"`
	Effects []slackDocumentedOperation `xml:"safe-outputs>operation"`
	Grants  struct {
		Workspace string `xml:"workspace,attr"`
		App       string `xml:"app,attr"`
		Channel   string `xml:"channel,attr"`
		Thread    string `xml:"parent-thread,attr"`
	} `xml:"configuration>grants"`
	Examples []struct {
		ID     string `xml:"id,attr"`
		Path   string `xml:"path,attr"`
		YAML   string `xml:"yaml"`
		Prompt string `xml:"prompt"`
	} `xml:"examples>example"`
}

type slackDocumentedOperation struct {
	ID               string `xml:"id,attr"`
	Credential       string `xml:"credential,attr"`
	Method           string `xml:"method,attr"`
	URL              string `xml:"url,attr"`
	MaxResponseBytes int    `xml:"max-response-bytes,attr"`
	Inputs           []struct {
		Name      string `xml:"name,attr"`
		Type      string `xml:"type,attr"`
		Required  bool   `xml:"required,attr"`
		MaxLength int    `xml:"max-length,attr"`
		Pattern   string `xml:"pattern,attr"`
		Enum      string `xml:"enum,attr"`
		Resource  string `xml:"resource,attr"`
	} `xml:"input"`
}

func slackDocumentationSource(t *testing.T) (string, slackDocumentation) {
	t.Helper()
	source, err := os.ReadFile("../../.github/workflows/connectors/slack.md")
	require.NoError(t, err)
	_, body, found := strings.Cut(strings.TrimPrefix(string(source), "---\n"), "\n---\n")
	require.True(t, found)
	body = strings.TrimSpace(body)
	require.True(t, strings.HasPrefix(body, "<!--\n"))
	require.Equal(t, 1, strings.Count(body, "<!--"))
	require.Equal(t, 1, strings.Count(body, "-->"))
	comment, _, found := strings.Cut(strings.TrimPrefix(body, "<!--\n"), "\n-->")
	require.True(t, found)
	require.NotContains(t, comment, "--", "XML comments forbid double hyphens, including inside CDATA")
	var documentation slackDocumentation
	decoder := xml.NewDecoder(strings.NewReader(comment))
	require.NoError(t, decoder.Decode(&documentation))
	_, err = decoder.Token()
	require.ErrorIs(t, err, io.EOF, "the comment must contain exactly one complete XML document")
	require.Equal(t, "slack", documentation.ID)
	require.Equal(t, "1", documentation.Version)
	require.Len(t, documentation.Examples, 1)
	return string(source), documentation
}

func TestSlackDocumentationDeclarationContract(t *testing.T) {
	source, documentation := slackDocumentationSource(t)
	frontmatter, _, found := strings.Cut(strings.TrimPrefix(source, "---\n"), "\n---\n")
	require.True(t, found)
	var declaration struct {
		Connectors   map[string]ConnectorDeclaration `json:"connectors"`
		ImportSchema any                             `json:"import-schema"`
	}
	require.NoError(t, connectors.Decode([]byte(frontmatter), &declaration))
	slack := declaration.Connectors["slack"]
	require.Empty(t, slack.MCPServers, "REST compatibility must not enable hosted Slack MCP")
	require.Len(t, slack.Tools, 1)
	require.Len(t, slack.SafeOutputs, 1)
	require.Len(t, documentation.Reads, 1)
	require.Len(t, documentation.Effects, 1)
	require.Equal(t, "read-thread", documentation.Reads[0].ID)
	require.Equal(t, "reply-in-thread", documentation.Effects[0].ID)
	requireSlackDocumentedOperation(t, documentation.Reads[0], slack.Tools["read-thread"].Operation, slack.Endpoints)
	requireSlackDocumentedOperation(t, documentation.Effects[0], slack.SafeOutputs["reply-in-thread"], slack.Endpoints)
	require.Equal(t, []string{"channels:history"}, slack.Credentials["reader"].Scopes)
	require.Equal(t, []string{"chat:write"}, slack.Credentials["writer"].Scopes)
	require.Equal(t, map[string][]string{
		"apps": {"A123ABC456"}, "channels": {"C123ABC456"},
		"threads": {"1515449522.000016"}, "workspaces": {"T123ABC456"},
	}, slack.Resources)
	require.Equal(t, map[string][]string{
		"apps": {documentation.Grants.App}, "channels": {documentation.Grants.Channel},
		"threads": {documentation.Grants.Thread}, "workspaces": {documentation.Grants.Workspace},
	}, slack.Resources)
}

func requireSlackDocumentedOperation(t *testing.T, documented slackDocumentedOperation, actual connectors.Operation, endpoints map[string]string) {
	t.Helper()
	require.Equal(t, actual.Credential, documented.Credential)
	require.Equal(t, actual.Request.Method, documented.Method)
	require.Equal(t, endpoints[actual.Request.Endpoint]+actual.Request.Path, documented.URL)
	require.Equal(t, actual.MaxResponseBytes, documented.MaxResponseBytes)
	require.Len(t, documented.Inputs, len(actual.Inputs))
	for _, input := range documented.Inputs {
		value, exists := actual.Inputs[input.Name]
		require.True(t, exists)
		require.Equal(t, value.Type, input.Type)
		require.Equal(t, value.Required, input.Required)
		require.Equal(t, value.MaxLength, input.MaxLength)
		require.Equal(t, value.Pattern, input.Pattern)
		require.Equal(t, strings.Join(value.Enum, ","), input.Enum)
		require.Equal(t, actual.Resources[input.Name], input.Resource)
	}
}

func slackDocumentationConsumer(t *testing.T) (string, string) {
	t.Helper()
	_, documentation := slackDocumentationSource(t)
	example := documentation.Examples[0]
	require.Equal(t, "manual-staged", example.ID)
	require.Equal(t, ".github/workflows/slack-thread-review.md", example.Path)
	require.NotEmpty(t, example.Prompt)
	var frontmatter map[string]any
	require.NoError(t, yaml.Unmarshal([]byte(example.YAML), &frontmatter))
	require.Equal(t, "workflow_dispatch", frontmatter["on"])
	require.Equal(t, true, frontmatter["safe-outputs"].(map[string]any)["staged"])
	return example.Path, "---\n" + example.YAML + "---\n" + example.Prompt
}

func slackDocumentationCompiler(t *testing.T, content, relativePath string) (*Compiler, *WorkflowData, string, error) {
	t.Helper()
	root := t.TempDir()
	path := filepath.Join(root, relativePath)
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0700))
	require.NoError(t, os.WriteFile(path, []byte(content), 0600))
	if strings.Contains(content, "uses: .github/workflows/connectors/slack.md") {
		source, _ := slackDocumentationSource(t)
		local := filepath.Join(root, ".github/workflows/connectors/slack.md")
		require.NoError(t, os.MkdirAll(filepath.Dir(local), 0700))
		require.NoError(t, os.WriteFile(local, []byte(source), 0600))
	}
	compiler := NewCompiler(WithNoEmit(true))
	compiler.gitRoot = root
	installConnectorTestSources(t, root)
	compiler.SetActionMode(ActionModeDev)
	compiler.version = "v0.0.0-1-gdc0f368c6c"
	data, err := compiler.ParseWorkflowFile(path)
	return compiler, data, path, err
}

func TestSlackDocumentationConsumerCompilation(t *testing.T) {
	relativePath, sample := slackDocumentationConsumer(t)
	for _, local := range []bool{false, true} {
		t.Run(map[bool]string{false: "embedded", true: "local"}[local], func(t *testing.T) {
			content := sample
			if local {
				content = strings.ReplaceAll(content, `".github/workflows/connectors/slack.md"`, ".github/workflows/connectors/slack.md")
			}
			compiler, data, path, err := slackDocumentationCompiler(t, content, relativePath)
			require.NoError(t, err)
			config := data.Connectors["slack"]
			require.NotNil(t, config)
			require.NotNil(t, config.Agent)
			require.NotNil(t, config.SafeOutputs)
			require.Nil(t, config.Context)
			require.Equal(t, []string{"read-thread"}, config.Agent.ToolNames())
			require.Contains(t, data.Tools, "slack")
			require.NotContains(t, data.Tools, "connector-slack")
			require.Equal(t, []string{"SLACK_READ_TOKEN"}, config.Agent.Secrets())
			require.Equal(t, []string{"SLACK_WRITE_TOKEN"}, config.SafeOutputs.Secrets())
			output, err := compiler.CompileToYAML(data, path)
			require.NoError(t, err)
			require.Contains(t, output, "slack_reply_in_thread")
			require.NotContains(t, output, "connector_5_slack")
			require.NotContains(t, output, "Checkout trusted connector runtime")
			require.NotContains(t, output, "Build trusted connector runtime")
			require.NotContains(t, output, "${{ secrets.SLACK_WRITE_TOKEN }}", "staged effects must not resolve writer secrets")
			require.NotContains(t, output, "SLACK_CONTEXT_TOKEN")
		})
	}
}

func TestSlackDocumentationIndependentSelections(t *testing.T) {
	relativePath, sample := slackDocumentationConsumer(t)
	for _, mode := range []string{"registration", "read", "write", "combined"} {
		t.Run(mode, func(t *testing.T) {
			content := strings.ReplaceAll(sample, "staged: true", "staged: false")
			content = strings.ReplaceAll(content, "reader-secret: SLACK_READ_TOKEN", "reader-secret: APPROVED_SLACK_READER")
			content = strings.ReplaceAll(content, "writer-secret: SLACK_WRITE_TOKEN", "writer-secret: APPROVED_SLACK_WRITER")
			if mode == "registration" || mode == "write" {
				content = strings.Replace(content, "tools:\n  slack: [read-thread]\n", "", 1)
			}
			if mode == "registration" || mode == "read" {
				content = strings.Replace(content, "  slack-reply-in-thread: {}\n", "", 1)
			}
			compiler, data, path, err := slackDocumentationCompiler(t, content, relativePath)
			require.NoError(t, err)
			if mode == "registration" {
				require.Empty(t, data.Connectors)
				return
			}
			config := data.Connectors["slack"]
			require.NotNil(t, config)
			require.Equal(t, mode == "read" || mode == "combined", config.Agent != nil)
			require.Equal(t, mode == "write" || mode == "combined", config.SafeOutputs != nil)
			output, err := compiler.CompileToYAML(data, path)
			require.NoError(t, err)
			requireSlackCredentialJobs(t, output, mode)
		})
	}
}

func requireSlackCredentialJobs(t *testing.T, output, mode string) {
	t.Helper()
	var document struct {
		Jobs map[string]any `yaml:"jobs"`
	}
	require.NoError(t, yaml.Unmarshal([]byte(output), &document))
	agent, err := json.Marshal(document.Jobs["agent"])
	require.NoError(t, err)
	safe, err := json.Marshal(document.Jobs["safe_outputs"])
	require.NoError(t, err)
	require.Equal(t, mode != "write", strings.Contains(string(agent), "${{ secrets.APPROVED_SLACK_READER }}"))
	require.Equal(t, mode != "read", strings.Contains(string(safe), "${{ secrets.APPROVED_SLACK_WRITER }}"))
	require.NotContains(t, string(agent), "${{ secrets.APPROVED_SLACK_WRITER }}")
	require.NotContains(t, string(safe), "${{ secrets.APPROVED_SLACK_READER }}")
}

func TestSlackDocumentationRejectedSelections(t *testing.T) {
	relativePath, sample := slackDocumentationConsumer(t)
	for _, test := range []struct{ name, old, replacement string }{
		{"ingress", "on: workflow_dispatch", "on:\n  slack-threaded-app-mention:"},
		{"write-as-read", "slack: [read-thread]", "slack: [reply-in-thread]"},
		{"unknown-tool", "slack: [read-thread]", "slack: [search]"},
		{"unknown-effect", "slack-reply-in-thread: {}", "slack-post-message: {}"},
		{"unknown-effect-option", "slack-reply-in-thread: {}", "slack-reply-in-thread: {unknown: false}"},
		{"resource-input", "reader-secret: SLACK_READ_TOKEN", "channels: [COTHER]"},
		{"public-context", "tools:\n", "context:\n  slack:\n    thread-context: {}\ntools:\n"},
	} {
		t.Run(test.name, func(t *testing.T) {
			content := strings.Replace(sample, test.old, test.replacement, 1)
			_, _, path, err := slackDocumentationCompiler(t, content, relativePath)
			require.Error(t, err)
			if test.name == "ingress" {
				require.ErrorContains(t, err, "gateway-dispatch")
			}
			_, err = os.Stat(strings.TrimSuffix(path, ".md") + ".lock.yml")
			require.ErrorIs(t, err, os.ErrNotExist)
		})
	}
}

type slackDocumentationTransport func(*http.Request) (*http.Response, error)

func (transport slackDocumentationTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	return transport(request)
}

func slackDocumentationPlan(t *testing.T) *WorkflowConnector {
	t.Helper()
	path, sample := slackDocumentationConsumer(t)
	_, data, _, err := slackDocumentationCompiler(t, sample, path)
	require.NoError(t, err)
	return data.Connectors["slack"]
}

func slackDocumentationRuntime(t *testing.T, phase, response string, calls *int) connectors.Runtime {
	t.Helper()
	return connectors.Runtime{
		Phase: phase, AllowedOrigins: []string{"https://slack.com"}, AllowWrites: phase == "safe-output",
		ResolveSecret: func(_ context.Context, actualPhase, name string) (string, error) {
			require.Equal(t, phase, actualPhase)
			expected := "SLACK_READ_TOKEN"
			if phase == "safe-output" {
				expected = "SLACK_WRITE_TOKEN"
			}
			require.Equal(t, expected, name)
			return "slack-documentation-fixture", nil
		},
		Client: &http.Client{Transport: slackDocumentationTransport(func(request *http.Request) (*http.Response, error) {
			*calls++
			require.Equal(t, "https", request.URL.Scheme)
			require.Equal(t, "slack.com", request.URL.Host)
			if phase == "agent" {
				require.Equal(t, http.MethodGet, request.Method)
				require.Equal(t, "/api/conversations.replies", request.URL.Path)
				require.Equal(t, "15", request.URL.Query().Get("limit"))
				require.Equal(t, "C123ABC456", request.URL.Query().Get("channel"))
				require.Equal(t, "1515449522.000016", request.URL.Query().Get("ts"))
				require.Len(t, request.URL.Query(), 3)
			} else {
				require.Equal(t, http.MethodPost, request.Method)
				require.Equal(t, "/api/chat.postMessage", request.URL.Path)
				body, err := io.ReadAll(request.Body)
				require.NoError(t, err)
				require.JSONEq(t, `{"channel":"C123ABC456","thread_ts":"1515449522.000016","text":"Reviewed.","reply_broadcast":false,"unfurl_links":false,"unfurl_media":false,"mrkdwn":false,"parse":"none"}`, string(body))
			}
			return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(response))}, nil
		})},
	}
}

func slackDocumentationArguments() map[string]any {
	return map[string]any{"channel": "C123ABC456", "thread": "1515449522.000016"}
}

func TestSlackDocumentationRESTContract(t *testing.T) {
	config := slackDocumentationPlan(t)
	calls := 0
	readBody := `{"ok":true,"has_more":false,"messages":[{"ts":"1515449522.000016","text":"Untrusted fixture"}],"response_metadata":{"next_cursor":""},"private":"excluded"}`
	runtime := slackDocumentationRuntime(t, "agent", readBody, &calls)
	result, err := config.Agent.Execute(t.Context(), runtime, "mcp", "read-thread", slackDocumentationArguments())
	require.NoError(t, err)
	require.True(t, result.Untrusted)
	require.JSONEq(t, `{"has-more":false,"messages":[{"ts":"1515449522.000016","text":"Untrusted fixture"}]}`, string(result.Data))
	args := slackDocumentationArguments()
	args["text"] = "Reviewed."
	staged, err := config.SafeOutputs.PrepareSafeOutput("reply-in-thread", args)
	require.NoError(t, err)
	require.Equal(t, 1, calls, "preparation must not resolve credentials or send HTTP")
	runtime = slackDocumentationRuntime(t, "safe-output", `{"ok":true,"channel":"C123ABC456","ts":"1515449599.000001","message":{"thread_ts":"1515449522.000016"},"private":"excluded"}`, &calls)
	result, err = config.SafeOutputs.ExecuteSafeOutput(t.Context(), runtime, *staged)
	require.NoError(t, err)
	require.JSONEq(t, `{"channel":"C123ABC456","message-id":"1515449599.000001","thread-id":"1515449522.000016"}`, string(result.Data))
	require.Equal(t, 2, calls)
}

func TestSlackDocumentationResourceAndInputDenial(t *testing.T) {
	config := slackDocumentationPlan(t)
	runtime := connectors.Runtime{Phase: "agent", AllowedOrigins: []string{"https://slack.com"}, ResolveSecret: func(context.Context, string, string) (string, error) {
		t.Fatal("invalid inputs must fail before credential resolution")
		return "", errors.New("unreachable")
	}}
	for _, args := range []map[string]any{
		{}, {"channel": "COTHER", "thread": "1515449522.000016"},
		{"channel": "C123ABC456", "thread": "1515449522.000017"},
		{"channel": "C123ABC456", "thread": 1515449522.000016},
		{"channel": "C123ABC456", "thread": "1515449522.000016", "cursor": "escape"},
	} {
		_, err := config.Agent.Execute(t.Context(), runtime, "mcp", "read-thread", args)
		require.Error(t, err)
	}
	args := slackDocumentationArguments()
	args["text"] = strings.Repeat("x", 4001)
	_, err := config.SafeOutputs.PrepareSafeOutput("reply-in-thread", args)
	require.Error(t, err)
	args["text"] = strings.Repeat("x", 4000)
	_, err = config.SafeOutputs.PrepareSafeOutput("reply-in-thread", args)
	require.NoError(t, err)
	args["text"] = strings.Repeat("\u00e9", 4000)
	_, err = config.SafeOutputs.PrepareSafeOutput("reply-in-thread", args)
	require.NoError(t, err, "length limits count Unicode characters, not UTF-8 bytes")
	args["text"] = strings.Repeat("\u00e9", 4001)
	_, err = config.SafeOutputs.PrepareSafeOutput("reply-in-thread", args)
	require.Error(t, err)
}

func TestSlackDocumentationResponseFailures(t *testing.T) {
	config := slackDocumentationPlan(t)
	for name, body := range map[string]string{
		"application-error": `{"ok":false,"error":"missing_scope"}`,
		"wrong-ok-type":     `{"ok":"true","has_more":false,"messages":[{"ts":"1515449522.000016"}]}`,
		"partial-page":      `{"ok":true,"has_more":true,"messages":[{"ts":"1515449522.000016"}]}`,
		"wrong-parent":      `{"ok":true,"has_more":false,"messages":[{"ts":"1515449522.000017"}]}`,
		"numeric-parent":    `{"ok":true,"has_more":false,"messages":[{"ts":1515449522.000016}]}`,
		"no-messages":       `{"ok":true,"has_more":false,"messages":[]}`,
		"cursor-page":       `{"ok":true,"has_more":false,"messages":[{"ts":"1515449522.000016"}],"response_metadata":{"next_cursor":"more"}}`,
		"whitespace-cursor": `{"ok":true,"has_more":false,"messages":[{"ts":"1515449522.000016"}],"response_metadata":{"next_cursor":" "}}`,
		"numeric-cursor":    `{"ok":true,"has_more":false,"messages":[{"ts":"1515449522.000016"}],"response_metadata":{"next_cursor":0}}`,
		"object-cursor":     `{"ok":true,"has_more":false,"messages":[{"ts":"1515449522.000016"}],"response_metadata":{"next_cursor":{}}}`,
		"oversized":         strings.Repeat("x", 65537),
		"invalid-json":      "not JSON",
	} {
		t.Run(name, func(t *testing.T) {
			calls := 0
			runtime := slackDocumentationRuntime(t, "agent", body, &calls)
			_, err := config.Agent.Execute(t.Context(), runtime, "mcp", "read-thread", slackDocumentationArguments())
			require.Error(t, err)
			require.Equal(t, 1, calls)
		})
	}
}

func TestSlackDocumentationCompletePage(t *testing.T) {
	config := slackDocumentationPlan(t)
	for _, metadata := range []string{"", `,"response_metadata":{}`, `,"response_metadata":{"next_cursor":null}`, `,"response_metadata":{"next_cursor":""}`} {
		calls := 0
		body := `{"ok":true,"has_more":false,"messages":[{"ts":"1515449522.000016"}]` + metadata + `}`
		body += strings.Repeat(" ", 65536-len(body))
		runtime := slackDocumentationRuntime(t, "agent", body, &calls)
		_, err := config.Agent.Execute(t.Context(), runtime, "mcp", "read-thread", slackDocumentationArguments())
		require.NoError(t, err, "an exhausted page at exactly the byte bound must be accepted")
		require.Equal(t, 1, calls)
	}
}

func TestSlackDocumentationWriteFailuresNeverRetry(t *testing.T) {
	config := slackDocumentationPlan(t)
	for name, body := range map[string]string{
		"application-error": `{"ok":false,"error":"internal_error"}`,
		"missing-id":        `{"ok":true,"channel":"C123ABC456","message":{"thread_ts":"1515449522.000016"}}`,
		"wrong-channel":     `{"ok":true,"channel":"COTHER","ts":"1515449599.000001","message":{"thread_ts":"1515449522.000016"}}`,
		"wrong-thread":      `{"ok":true,"channel":"C123ABC456","ts":"1515449599.000001","message":{"thread_ts":"1515449522.000017"}}`,
		"numeric-id":        `{"ok":true,"channel":"C123ABC456","ts":1515449599.000001,"message":{"thread_ts":"1515449522.000016"}}`,
	} {
		t.Run(name, func(t *testing.T) {
			calls := 0
			args := slackDocumentationArguments()
			args["text"] = "Reviewed."
			runtime := slackDocumentationRuntime(t, "safe-output", body, &calls)
			_, err := config.SafeOutputs.Execute(t.Context(), runtime, "safe-output", "reply-in-thread", args)
			require.Error(t, err)
			require.Equal(t, 1, calls)
		})
	}
}
