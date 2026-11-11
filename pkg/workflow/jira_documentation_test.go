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
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/connectors"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type jiraDocumentationExample struct {
	ID     string `xml:"id,attr"`
	Path   string `xml:"path,attr"`
	YAML   string `xml:"frontmatter"`
	Prompt string `xml:"body"`
}

func jiraDocumentationExamples(t *testing.T) []jiraDocumentationExample {
	t.Helper()
	source, err := os.ReadFile("../../.github/workflows/connectors/jira.md")
	require.NoError(t, err)
	document := commentedConnectorDocumentation(t, string(source))
	require.Contains(t, document, "<frontmatter><![CDATA[", "consumer YAML must be preserved as literal CDATA")
	require.Contains(t, document, "<body><![CDATA[", "consumer prompt must be preserved as literal CDATA")
	var parsed struct {
		XMLName  xml.Name                   `xml:"connector-documentation"`
		ID       string                     `xml:"id,attr"`
		Version  string                     `xml:"version,attr"`
		Examples []jiraDocumentationExample `xml:"examples>example"`
		Sections []struct {
			XMLName xml.Name
			Content string `xml:",innerxml"`
		} `xml:",any"`
	}
	require.NoError(t, xml.Unmarshal([]byte(document), &parsed))
	require.Equal(t, "jira", parsed.ID)
	require.Equal(t, "1", parsed.Version)
	sections := map[string]string{}
	for _, section := range parsed.Sections {
		require.NotContains(t, sections, section.XMLName.Local)
		sections[section.XMLName.Local] = strings.TrimSpace(section.Content)
	}
	for _, name := range []string{"purpose", "credentials", "configuration", "triggers", "reads", "safe-outputs", "context", "limitations", "references"} {
		require.NotEmpty(t, sections[name], "self-contained documentation requires %s", name)
	}
	require.NotEmpty(t, parsed.Examples)
	return parsed.Examples
}

func parseJiraDocumentationConsumer(t *testing.T, example jiraDocumentationExample, local bool) (*Compiler, *WorkflowData, string) {
	t.Helper()
	root := t.TempDir()
	path := filepath.Join(root, filepath.FromSlash(example.Path))
	require.Equal(t, filepath.Join(root, ".github", "workflows"), filepath.Dir(path))
	source := "---\n" + example.YAML + "---\n" + example.Prompt
	if local {
		declaration, err := os.ReadFile("../../.github/workflows/connectors/jira.md")
		require.NoError(t, err)
		target := filepath.Join(root, ".github", "workflows", "connectors", "jira.md")
		require.NoError(t, os.MkdirAll(filepath.Dir(target), 0700))
		require.NoError(t, os.WriteFile(target, declaration, 0600))
		source = strings.Replace(source, ".github/workflows/connectors/jira.md", "connectors/jira.md", 1)
	}
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0700))
	require.NoError(t, os.WriteFile(path, []byte(source), 0600))
	c := NewCompiler(WithNoEmit(true), WithSkipValidation(false))
	c.gitRoot = root
	installConnectorTestSources(t, root)
	c.SetActionMode(ActionModeDev)
	c.version = "v0.0.0-1-g683ddf9617"
	data, err := c.ParseWorkflowFile(path)
	require.NoError(t, err, "documented consumer must pass schema validation at a conventional workflow path")
	return c, data, path
}

func TestJiraDocumentationConsumers(t *testing.T) {
	t.Parallel()
	for _, example := range jiraDocumentationExamples(t) {
		for _, local := range []bool{false, true} {
			name := map[bool]string{false: "embedded", true: "local"}[local]
			t.Run(example.ID+"/"+name, func(t *testing.T) {
				require.NotEmpty(t, example.Prompt)
				c, data, path := parseJiraDocumentationConsumer(t, example, local)
				require.True(t, templatableBoolIsTrue(data.SafeOutputs.Staged))
				config := data.Connectors["jira"]
				require.NotNil(t, config)
				require.NotNil(t, config.Agent)
				require.NotNil(t, config.SafeOutputs)
				assert.Nil(t, config.Context)
				assert.Equal(t, []string{"JIRA_MCP_PERSONAL_BASIC_AUTH"}, config.Agent.Secrets())
				assert.Equal(t, []string{"JIRA_REST_BASIC_AUTH"}, config.SafeOutputs.Secrets())
				assert.Equal(t, []string{"personal__getJiraIssue", "personal__searchJiraIssuesUsingJql"}, config.Agent.ToolNames())
				assert.Equal(t, []string{"add-comment", "add-label", "create-issue", "update-issue"}, config.SafeOutputs.SafeOutputNames())
				output, err := c.CompileToYAML(data, path)
				require.NoError(t, err)
				assert.NotContains(t, output, "${{ secrets.JIRA_REST_BASIC_AUTH }}", "staging must not inject writer credentials")
				assert.NotContains(t, output, "connector_4_jira_")
				for _, operation := range config.SafeOutputs.SafeOutputNames() {
					assert.Contains(t, output, "jira_"+strings.ReplaceAll(operation, "-", "_"))
				}
				if !local {
					_, err := os.Stat(filepath.Join(c.gitRoot, ".github", "aw"))
					assert.True(t, os.IsNotExist(err), "embedded consumers must not require connector checkout files")
				}
			})
		}
	}
}

func TestJiraDocumentationStagedChainAndExactGrants(t *testing.T) {
	for _, example := range jiraDocumentationExamples(t) {
		_, data, _ := parseJiraDocumentationConsumer(t, example, false)
		plan := data.Connectors["jira"].SafeOutputs
		for _, step := range []struct {
			operation string
			args      map[string]any
		}{
			{"create-issue", map[string]any{"project": "APP", "issue-type": "Task", "summary": "Review follow-up for approved issue", "description": "Review approved evidence.", "temporary_id": "aw_created"}},
			{"update-issue", map[string]any{"issue": "#aw_created", "summary": "Reviewed follow-up for approved issue", "description": "Evidence reviewed; proposed changes only."}},
			{"add-comment", map[string]any{"issue": "#aw_created", "text": "Review prepared."}},
			{"add-label", map[string]any{"issue": "#aw_created", "label": "reviewed"}},
		} {
			_, err := plan.PrepareSafeOutputIntent(step.operation, step.args)
			require.NoError(t, err, "documented staged %s must validate without credentials or network", step.operation)
		}
		for _, issue := range []string{"APP-1", "APP-2", "OTHER-1", "#aw_created"} {
			_, err := plan.PrepareSafeOutput("add-comment", map[string]any{"issue": issue, "text": "Approved evidence"})
			if issue == "APP-1" {
				require.NoError(t, err)
			} else {
				require.Error(t, err, "neither project approval nor a staged intent grants %s live authority", issue)
			}
		}

	}
}

func TestJiraDocumentationRejectsBroadenedReads(t *testing.T) {
	for _, example := range jiraDocumentationExamples(t) {
		_, data, _ := parseJiraDocumentationConsumer(t, example, false)
		plan := data.Connectors["jira"].Agent
		resolutions := 0
		runtime := connectors.Runtime{Phase: "agent", AllowedOrigins: []string{"https://mcp.atlassian.com"},
			ResolveSecret: func(context.Context, string, string) (string, error) {
				resolutions++
				return "", errors.New("unexpected credential access")
			}}
		for _, test := range []struct {
			tool string
			args map[string]any
		}{
			{"getJiraIssue", map[string]any{"cloudId": "other-cloud", "issueIdOrKey": "APP-1"}},
			{"getJiraIssue", map[string]any{"cloudId": "00000000-0000-4000-8000-000000000001", "issueIdOrKey": "APP-2"}},
			{"searchJiraIssuesUsingJql", map[string]any{"cloudId": "00000000-0000-4000-8000-000000000001", "jql": "project = APP", "maxResults": 1}},
			{"searchJiraIssuesUsingJql", map[string]any{"cloudId": "00000000-0000-4000-8000-000000000001", "jql": "issue = APP-1", "maxResults": 26}},
			{"searchJiraIssuesUsingJql", map[string]any{"cloudId": "00000000-0000-4000-8000-000000000001", "jql": "issue = APP-1", "maxResults": 1, "nextPageToken": "unapproved"}},
		} {
			_, err := plan.CallMCP(t.Context(), runtime, "personal", test.tool, test.args)
			require.Error(t, err)
			assert.Zero(t, resolutions, "broadened documented read must fail before credentials or network")
		}
	}
}

func TestJiraDocumentationImportDoesNotGrantCapabilities(t *testing.T) {
	for _, example := range jiraDocumentationExamples(t) {
		var found bool
		example.YAML, _, found = strings.Cut(example.YAML, "\ntools:\n")
		require.True(t, found)
		example.YAML += "\n"
		_, data, _ := parseJiraDocumentationConsumer(t, example, false)
		assert.Empty(t, data.Connectors, "resource inputs and import alone must not select capabilities")
	}
}

func TestJiraCommentEchoedPublicationResponse(t *testing.T) {
	_, data, _, err := parseJiraMigrationWorkflow(t, jiraEmbeddedImport+"safe-outputs:\n  jira-add-comment:\n")
	require.NoError(t, err)
	plan := data.Connectors["jira"].SafeOutputs
	encoded, err := plan.Encode()
	require.NoError(t, err)
	assets, err := filepath.Abs("../../actions/setup/js")
	require.NoError(t, err)
	payload, err := json.Marshal(map[string]string{"plan": encoded, "assets": assets})
	require.NoError(t, err)
	command := exec.CommandContext(t.Context(), "node", "-e", `
const assert = require("node:assert/strict");
const input = JSON.parse(require("node:fs").readFileSync(0, "utf8"));
const { decodePlan, executeConnector } = require(input.assets + "/connector_runtime.cjs");
const { formatConnectorArguments } = require(input.assets + "/connector_content.cjs");
process.env = { GH_AW_WORKFLOW_NAME: "Offline Jira review" };
const env = { GH_AW_CONNECTOR_SECRET_JIRA_REST_BASIC_AUTH: "fixture-writer" };
const plan = decodePlan(input.plan);
const prepared = [];
(async () => {
  for (const text of ["x".repeat(20000), "\u{1f4ac}".repeat(32767), '"\\'.repeat(16000)]) {
    const args = formatConnectorArguments(plan, "add-comment", { issue: "APP-1", text });
    assert.ok(Array.from(args.text).length <= 32767);
    const staged = await executeConnector(input.plan, "safe-output", "add-comment", args, {
      staged: true,
      env: new Proxy({}, {get() {throw Error("Staging accessed credentials");}}),
      fetchImpl: async () => {throw Error("Staging made a request");},
    });
    assert.equal(staged.staged, true);
    let writes = 0;
    const result = await executeConnector(input.plan, "safe-output", "add-comment", args, {
      env, fetchImpl: async (url, init) => {
        writes++;
        assert.equal(init.method, "POST");
        const request = JSON.parse(init.body);
        const response = JSON.stringify({id:"20001", body:request.body,
          author:{accountId:"fixture-author"}, self:String(url)+"/20001"})
          .replace(/[\u007f-\uffff]/g, char => "\\u"+char.charCodeAt(0).toString(16).padStart(4,"0"));
        assert.ok(Buffer.byteLength(response) > 16384);
        return new Response(response, {status:201});
      },
    });
    assert.equal(writes, 1, "successful appends must not retry");
    assert.deepEqual({...result.data}, {"comment-id":"20001"});
    prepared.push(args);
  }
  let writes = 0;
  await assert.rejects(() => executeConnector(input.plan, "safe-output", "add-comment", prepared[0], {
    env, fetchImpl: async () => {
      writes++;
      return new Response(JSON.stringify({id:"20001", padding:"x".repeat(1048576)}), {status:201});
    },
  }), /byte bound/);
  assert.equal(writes, 1, "over-budget responses must not retry a possibly accepted append");
  process.stdout.write(JSON.stringify(prepared));
})().catch(error => {console.error(error);process.exitCode=1;});
`)
	command.Stdin = strings.NewReader(string(payload))
	output, err := command.CombinedOutput()
	require.NoError(t, err, "%s", output)
	var prepared []map[string]any
	require.NoError(t, json.Unmarshal(output, &prepared))
	for _, args := range prepared {
		calls := 0
		runtime := connectors.Runtime{
			Phase: "safe-output", AllowWrites: true, AllowedOrigins: plan.Origins(),
			ResolveSecret: func(context.Context, string, string) (string, error) { return "fixture-writer", nil },
			Client: &http.Client{Transport: connectorSmokeTransport(func(request *http.Request) (*http.Response, error) {
				calls++
				var document map[string]any
				require.NoError(t, json.NewDecoder(request.Body).Decode(&document))
				response, err := json.Marshal(map[string]any{"id": "20001", "body": document["body"]})
				require.NoError(t, err)
				require.Greater(t, len(response), 16384)
				return &http.Response{StatusCode: http.StatusCreated, Body: io.NopCloser(strings.NewReader(string(response)))}, nil
			})},
		}
		intent, err := plan.PrepareSafeOutput("add-comment", args)
		require.NoError(t, err)
		result, err := plan.ExecuteSafeOutput(t.Context(), runtime, *intent)
		require.NoError(t, err)
		require.Equal(t, 201, result.StatusCode)
		require.JSONEq(t, `{"comment-id":"20001"}`, string(result.Data))
		require.Equal(t, 1, calls)
	}
}

func TestJiraMCPHTTPStatusDiagnostics(t *testing.T) {
	_, data, _, err := parseJiraMigrationWorkflow(t, jiraEmbeddedImport+"tools:\n  jira:\n    personal: [getJiraIssue]\n")
	require.NoError(t, err)
	plan := data.Connectors["jira"].Agent
	encoded, err := plan.Encode()
	require.NoError(t, err)
	assets, err := filepath.Abs("../../actions/setup/js")
	require.NoError(t, err)
	payload, err := json.Marshal(map[string]string{"plan": encoded, "assets": assets})
	require.NoError(t, err)
	command := exec.CommandContext(t.Context(), "node", "-e", `
const assert = require("node:assert/strict");
const {Readable, Writable} = require("node:stream");
const input = JSON.parse(require("node:fs").readFileSync(0, "utf8"));
const {serve} = require(input.assets + "/connector_runtime.cjs");
const env = {GH_AW_CONNECTOR_SECRET_JIRA_MCP_PERSONAL_BASIC_AUTH:"fixture-secret"};
const json = (body, status=200) => new Response(JSON.stringify(body), {status,
  headers:{"content-type":"application/json"}});
(async () => {
  for (const phase of ["initialize", "tools/call"])
    for (const status of [401, 403, 429, 503]) {
      let output="", failures=0;
      await serve(input.plan, Readable.from([JSON.stringify({jsonrpc:"2.0",id:1,
        method:"tools/call",params:{name:"personal__getJiraIssue",
          arguments:{cloudId:"00000000-0000-4000-8000-000000000001",issueIdOrKey:"APP-1"}}})+"\n"]),
        new Writable({write(chunk, encoding, next) {output+=chunk;next();}}),
        {env, fetchImpl:async(url, init) => {
          const rpc=JSON.parse(init.body);
          if (rpc.method===phase) {
            failures++;
            return json({diagnostic:"private provider diagnostic",token:"fixture-secret"},status);
          }
          if (rpc.method==="initialize") return json({jsonrpc:"2.0",id:rpc.id,
            result:{protocolVersion:"2025-03-26",capabilities:{},serverInfo:{name:"fixture",version:"1"}}});
          return new Response(null,{status:202});
        }});
      const result=JSON.parse(output).result;
      assert.equal(result.isError,true);
      assert.deepEqual(result.structuredContent,{httpStatus:status});
      assert.ok(result.content[0].text.includes("HTTP status "+status));
      assert.ok(!output.includes("private provider diagnostic") && !output.includes("fixture-secret"));
      assert.equal(failures,1,"HTTP failures must not retry or fall back");
    }
  let output="";
  await serve(input.plan, Readable.from([JSON.stringify({jsonrpc:"2.0",id:1,
    method:"tools/call",params:{name:"personal__getJiraIssue",
      arguments:{cloudId:"00000000-0000-4000-8000-000000000001",issueIdOrKey:"APP-1"}}})+"\n"]),
    new Writable({write(chunk, encoding, next) {output+=chunk;next();}}),
    {env, fetchImpl:async(url, init) => {
      const rpc=JSON.parse(init.body);
      if (rpc.method==="initialize") return json({jsonrpc:"2.0",id:rpc.id,
        result:{protocolVersion:"2025-03-26",capabilities:{},serverInfo:{name:"fixture",version:"1"}}});
      if (!rpc.id) return new Response(null,{status:202});
      return json({jsonrpc:"2.0",id:rpc.id,result:{isError:true,
        content:[{type:"text",text:"private provider diagnostic: scope claim missing"}],
        structuredContent:{httpStatus:401}}});
    }});
  const result=JSON.parse(output).result;
  assert.equal(result.isError,true);
  assert.equal(result.structuredContent,undefined,"tool-authored statuses are not HTTP evidence");
  assert.ok(!output.includes("private provider diagnostic") && !output.includes("401"));
})().catch(error => {console.error(error);process.exitCode=1;});
`)
	command.Stdin = strings.NewReader(string(payload))
	output, err := command.CombinedOutput()
	require.NoError(t, err, "%s", output)
}
