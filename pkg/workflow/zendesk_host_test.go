//go:build !integration

package workflow

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/github/gh-aw/pkg/connectors"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"
)

func TestZendeskConnectorMCPReadOnlyBridge(t *testing.T) {
	plan := zendeskPlan(t, "agent")
	var calls atomic.Int32
	runtime := zendeskRuntime(t, "agent", func(request *http.Request) (int, string) {
		calls.Add(1)
		require.Equal(t, http.MethodGet, request.Method)
		return 200, zendeskFixture(t, "ticket.json")
	})
	adapter, err := plan.MCPServer(runtime)
	require.NoError(t, err)
	clientTransport, serverTransport := mcp.NewInMemoryTransports()
	server, err := adapter.Connect(t.Context(), serverTransport, nil)
	require.NoError(t, err)
	defer func() { require.NoError(t, server.Close()) }()
	client := mcp.NewClient(&mcp.Implementation{Name: "zendesk-fixture", Version: "1"}, nil)
	session, err := client.Connect(t.Context(), clientTransport, nil)
	require.NoError(t, err)
	defer func() { require.NoError(t, session.Close()) }()
	tools, err := session.ListTools(t.Context(), nil)
	require.NoError(t, err)
	require.Len(t, tools.Tools, 1)
	require.Equal(t, "get_ticket", tools.Tools[0].Name)
	result, err := session.CallTool(t.Context(), &mcp.CallToolParams{Name: "get_ticket", Arguments: zendeskReadArgs()})
	require.NoError(t, err)
	require.False(t, result.IsError)
	require.Len(t, result.Content, 1)
	content, ok := result.Content[0].(*mcp.TextContent)
	require.True(t, ok)
	require.Contains(t, content.Text, `"untrusted":true`)
	require.Contains(t, content.Text, `"ticket-id":35436`)
	require.NotContains(t, content.Text, "Unprojected customer content")
	for _, args := range []map[string]any{
		{"subdomain": "other", "ticket": "35436"},
		{"subdomain": "example", "ticket": "35437"},
		{"subdomain": "example", "ticket": "35436", "fields": "requester"},
	} {
		result, err := session.CallTool(t.Context(), &mcp.CallToolParams{Name: "get_ticket", Arguments: args})
		require.NoError(t, err)
		require.True(t, result.IsError)
	}
	result, err = session.CallTool(t.Context(), &mcp.CallToolParams{Name: "add_internal_note", Arguments: zendeskNoteArgs()})
	require.True(t, err != nil || result.IsError)
	require.EqualValues(t, 1, calls.Load())
}

func TestZendeskConnectorFrozenPlanIgnoresWorkspaceChanges(t *testing.T) {
	_, data, path, err := parseZendeskWorkflow(t, zendeskFixture(t, "workflow.md"))
	require.NoError(t, err)
	encoded, err := data.Connectors["zendesk"].Agent.Encode()
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(path, []byte("untrusted replacement without connector authority"), 0600))
	plan, err := connectors.DecodePlan(encoded)
	require.NoError(t, err)
	require.Equal(t, data.Connectors["zendesk"].Agent.Digest, plan.Digest)
	require.NotEqual(t, plan.Digest, data.Connectors["zendesk"].SafeOutputs.Digest)
	result, err := plan.Execute(t.Context(), zendeskRuntime(t, "agent", func(*http.Request) (int, string) {
		return 200, zendeskFixture(t, "ticket.json")
	}), "mcp", "get-ticket", zendeskReadArgs())
	require.NoError(t, err)
	require.Contains(t, string(result.Data), `"ticket-id":35436`)
}

func TestZendeskConnectorGrantBoundaries(t *testing.T) {
	for _, test := range []struct {
		name, subdomain, ticket string
		valid                   bool
	}{
		{"minimum", "a", "1", true},
		{"maximum", strings.Repeat("a", 63), strings.Repeat("9", 15), true},
		{"internal hyphens", "approved-support", "35436", true},
		{"oversized subdomain", strings.Repeat("a", 64), "35436", false},
		{"leading hyphen", "-example", "35436", false},
		{"trailing hyphen", "example-", "35436", false},
		{"subdomain dot", "example.other", "35436", false},
		{"uppercase", "EXAMPLE", "35436", false},
		{"oversized ticket", "example", strings.Repeat("9", 16), false},
		{"zero ticket", "example", "0", false},
		{"leading zero", "example", "035436", false},
	} {
		t.Run(test.name, func(t *testing.T) {
			source := strings.Replace(zendeskFixture(t, "workflow.md"), "subdomain: example", "subdomain: "+test.subdomain, 1)
			source = strings.Replace(source, `ticket: "35436"`, `ticket: "`+test.ticket+`"`, 1)
			_, data, _, err := parseZendeskWorkflow(t, source)
			require.NoError(t, err)
			_, err = data.Connectors["zendesk"].SafeOutputs.PrepareSafeOutput("add-internal-note", map[string]any{
				"subdomain": test.subdomain, "ticket": test.ticket, "text": "Bounded triage.",
			})
			if test.valid {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
			}
		})
	}
}

func TestZendeskConnectorStagedHostDoesNotReadSecrets(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("Node.js is required for the staged safe-output host")
	}
	runnerTemp := t.TempDir()
	actions := filepath.Join(runnerTemp, "gh-aw", "actions")
	require.NoError(t, os.MkdirAll(actions, 0700))
	for _, name := range []string{"connector_runtime.cjs", "connector_regex.cjs", "error_codes.cjs"} {
		asset, err := os.ReadFile(filepath.Join("../../actions/setup/js", name))
		require.NoError(t, err)
		require.NoError(t, os.WriteFile(filepath.Join(actions, name), asset, 0600))
	}
	_, data, _, err := parseZendeskWorkflow(t, zendeskFixture(t, "workflow.md"))
	require.NoError(t, err)
	require.Len(t, data.SafeOutputs.Scripts, 1)
	effect := data.SafeOutputs.Scripts["zendesk-add-internal-note"]
	require.NotNil(t, effect)
	script := effect.Script
	source, err := json.Marshal(script)
	require.NoError(t, err)
	plan := data.Connectors["zendesk"].SafeOutputs
	encoded, err := plan.Encode()
	require.NoError(t, err)
	encodedJSON, err := json.Marshal(encoded)
	require.NoError(t, err)
	proposal, err := plan.PrepareSafeOutput("add-internal-note", zendeskNoteArgs())
	require.NoError(t, err)
	proposalJSON, err := json.Marshal(proposal)
	require.NoError(t, err)
	setup, err := filepath.Abs("../../actions/setup/js")
	require.NoError(t, err)
	driver := fmt.Sprintf(`
const assert = require('node:assert/strict');
const path = require('node:path');
let calls = 0;
const proposal = %s;
const runnerTemp = %q;
const setup = %q;
const encoded = %s;
const { decodePlan } = require(path.join(setup, 'connector_runtime.cjs'));
const { formatConnectorArguments } = require(path.join(setup, 'connector_content.cjs'));
const originalArguments = proposal.arguments;
proposal.arguments = formatConnectorArguments(decodePlan(encoded), 'add-internal-note', originalArguments);
const adaptedRequire = name => {
  if (name === 'node:child_process') return {spawnSync: (binary, args, options) => {
    calls++;
    assert.equal(binary, process.execPath);
    assert.deepEqual(args, [require('node:path').join(runnerTemp,'gh-aw','actions','connector_runtime.cjs'),'invoke','safe-output','add-internal-note','--stage']);
    assert.deepEqual(options.env, {GH_AW_CONNECTOR_PLAN: encoded});
    assert.deepEqual(JSON.parse(options.input), proposal.arguments);
    assert.equal(options.shell, undefined);
    assert.equal(options.timeout, 25000);
    assert.equal(options.maxBuffer, 2097152);
    return require('node:child_process').spawnSync(binary, args, options);
  }};
  if (name === './safe_output_helpers.cjs') return {isStagedMode: () => true};
  if (name === './connector_effect_bindings.cjs') return {connectorEffectBindings: () => ({
    get env() { throw new Error('staging must not read effect bindings'); },
    record() { throw new Error('staging must not record a delivered write'); }
  })};
  return name.startsWith('./') ? require(path.join(setup, name)) : require(name);
};
const env = new Proxy({}, {get: (_, key) => {
  if (key === 'RUNNER_TEMP') return runnerTemp;
  throw new Error('staging must not read provider credentials: ' + key);
}});
const result = new Function('item','require','process','temporaryIdMap', %s)(
  {type:'zendesk_add_internal_note', ...originalArguments},
  adaptedRequire, {execPath: process.execPath, env}, new Map()
);
assert.deepEqual(result, {staged:true,request:proposal});
assert.equal(calls, 1);
`, proposalJSON, runnerTemp, setup, encodedJSON, source)
	command := exec.CommandContext(t.Context(), node, "-e", driver)
	command.Env = []string{}
	output, err := command.CombinedOutput()
	require.NoError(t, err, "%s", output)
}
