//go:build !integration

package workflow

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/pelletier/go-toml/v2"
)

func TestRenderJSONMCPConfig_GatewayAgentIDValidatesAgainstSchema(t *testing.T) {
	gatewayConfig := &MCPGatewayRuntimeConfig{
		Domain:  "${MCP_GATEWAY_DOMAIN}",
		AgentID: "${MCP_GATEWAY_AGENT_ID}",
	}

	var output strings.Builder
	err := RenderJSONMCPConfig(
		&output,
		map[string]any{},
		[]string{},
		&WorkflowData{Name: "test-workflow", FrontmatterHash: "abc123"},
		JSONMCPConfigOptions{
			ConfigPath:    "/tmp/test/mcp-servers.json",
			GatewayConfig: gatewayConfig,
			Renderers:     MCPToolRenderers{},
		},
	)
	if err != nil {
		t.Fatalf("RenderJSONMCPConfig returned error: %v", err)
	}

	result := output.String()
	if strings.Contains(result, `"apiKey"`) {
		t.Fatalf("rendered gateway config must not contain apiKey:\n%s", result)
	}
	if !strings.Contains(result, `"agentId": "${MCP_GATEWAY_AGENT_ID}"`) {
		t.Fatalf("rendered gateway config must contain agentId:\n%s", result)
	}

	start := strings.Index(result, "          {\n")
	end := strings.LastIndex(result, "\n          GH_AW_MCP_CONFIG")
	if start < 0 || end < 0 || end <= start {
		t.Fatalf("failed to locate rendered gateway JSON in output:\n%s", result)
	}
	renderedJSON := strings.ReplaceAll(result[start:end], "$MCP_GATEWAY_PORT", "8080")
	renderedJSON = strings.ReplaceAll(renderedJSON, "${MCP_GATEWAY_DOMAIN}", "localhost")
	renderedJSON = strings.ReplaceAll(renderedJSON, "${MCP_GATEWAY_AGENT_ID}", "test-agent")
	var renderedConfig map[string]any
	if err := json.Unmarshal([]byte(renderedJSON), &renderedConfig); err != nil {
		t.Fatalf("rendered gateway config should be valid JSON: %v\njson:\n%s", err, renderedJSON)
	}

	schemaJSON, err := os.ReadFile("schemas/mcp-gateway-config.schema.json")
	if err != nil {
		t.Fatalf("failed to read gateway schema: %v", err)
	}
	schema, err := compileSchema(string(schemaJSON), "https://docs.github.com/gh-aw/schemas/mcp-gateway-config.schema.json")
	if err != nil {
		t.Fatalf("failed to compile gateway schema: %v", err)
	}
	if err := schema.Validate(renderedConfig); err != nil {
		t.Fatalf("rendered gateway config should validate against schema: %v\njson:\n%s", err, renderedJSON)
	}
}

func TestRenderJSONMCPConfig_TrustedBots(t *testing.T) {
	for _, test := range []struct {
		name    string
		bot     string
		wantErr bool
	}{
		{"GitHub Actions bot", "github-actions[bot]", false},
		{"Copilot bot", "copilot-swe-agent[bot]", false},
		{"login without suffix", "My-Bot_123", false},
		{"empty identity", "", true},
		{"command substitution", "$(id)", true},
		{"backtick substitution", "`id`", true},
		{"variable expansion", "${HOME}", true},
		{"workflow expression", "${{ github.actor }}", true},
		{"whitespace", "my bot", true},
		{"trailing newline", "github-actions[bot]\n", true},
		{"quote", `my"bot`, true},
		{"backslash", `my\bot`, true},
		{"non-ASCII identity", "böt", true},
		{"invalid suffix", "my-bot[other]", true},
	} {
		t.Run(test.name, func(t *testing.T) {
			var output strings.Builder
			err := RenderJSONMCPConfig(&output, nil, nil, &WorkflowData{}, JSONMCPConfigOptions{
				GatewayConfig: &MCPGatewayRuntimeConfig{
					Domain:      "localhost",
					AgentID:     "test-agent",
					TrustedBots: []string{"existing-bot[bot]", test.bot},
				},
			})
			if test.wantErr {
				if err == nil || !strings.Contains(err.Error(), "sandbox.mcp.trusted-bots") ||
					!strings.Contains(err.Error(), "use only ASCII letters") {
					t.Fatalf("expected actionable identity validation error, got %v", err)
				}
				if output.Len() != 0 {
					t.Fatal("invalid identities must not emit a shell script")
				}
			} else {
				if err != nil {
					t.Fatalf("RenderJSONMCPConfig returned error: %v", err)
				}
				expected := fmt.Sprintf(`"trustedBots": ["existing-bot[bot]", %q]`, test.bot)
				if !strings.Contains(output.String(), expected) {
					t.Fatalf("missing trusted bots in generated configuration: %s", output.String())
				}
			}
		})
	}
}

func TestMCPRenderersPreserveMappedContainerDigests(t *testing.T) {
	for _, image := range []string{
		"registry.example.com/server:v1@sha256:" + strings.Repeat("a", 64),
		"registry.example.com/server@sha256:" + strings.Repeat("b", 64),
	} {
		for _, format := range []string{"json", "toml"} {
			t.Run(format+"/"+image, func(t *testing.T) {
				mappings := map[string]string{
					constants.DefaultGhAwNodeImage:            image,
					"ghcr.io/github/github-mcp-server:v1.0.0": image,
					"example.com/custom:v1":                   image,
				}
				data := &WorkflowData{ContainerPinMappings: mappings}
				renderer := NewMCPConfigRenderer(MCPRendererOptions{
					Format: format, IsLast: true, ContainerPinMappings: mappings,
				})
				renderers := map[string]func(*strings.Builder){
					"safeoutputs": func(output *strings.Builder) { renderer.RenderSafeOutputsMCP(output, data) },
					"github": func(output *strings.Builder) {
						renderer.RenderGitHubMCP(output, map[string]any{
							"type": "local", "version": "v1.0.0",
						}, data)
					},
					"custom": func(output *strings.Builder) {
						err := renderSharedMCPConfig(output, "custom", map[string]any{
							"container": "example.com/custom:v1",
						}, MCPConfigRenderer{Format: format, ContainerPinMappings: mappings})
						if err != nil {
							t.Fatalf("render custom MCP config: %v", err)
						}
					},
				}
				for name, render := range renderers {
					t.Run(name, func(t *testing.T) {
						var output strings.Builder
						render(&output)
						field := `"container": "` + image + `"`
						if format == "toml" {
							field = `container = "` + image + `"`
						}
						if !strings.Contains(output.String(), field) {
							t.Fatalf("missing pinned container %s in config:\n%s", field, output.String())
						}
					})
				}
			})
		}
	}
}

func TestNewMCPConfigRenderer(t *testing.T) {
	tests := []struct {
		name    string
		options MCPRendererOptions
	}{
		{
			name: "copilot options",
			options: MCPRendererOptions{
				IncludeCopilotFields: true,
				InlineArgs:           true,
				Format:               "json",
				IsLast:               false,
			},
		},
		{
			name: "claude options",
			options: MCPRendererOptions{
				IncludeCopilotFields: false,
				InlineArgs:           false,
				Format:               "json",
				IsLast:               true,
			},
		},
		{
			name: "codex options",
			options: MCPRendererOptions{
				IncludeCopilotFields: false,
				InlineArgs:           false,
				Format:               "toml",
				IsLast:               false,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			renderer := NewMCPConfigRenderer(tt.options)
			if renderer == nil {
				t.Fatal("Expected non-nil renderer")
			}
			if renderer.options.Format != tt.options.Format {
				t.Errorf("Expected format %s, got %s", tt.options.Format, renderer.options.Format)
			}
			if renderer.options.IncludeCopilotFields != tt.options.IncludeCopilotFields {
				t.Errorf("Expected IncludeCopilotFields %t, got %t", tt.options.IncludeCopilotFields, renderer.options.IncludeCopilotFields)
			}
			if renderer.options.InlineArgs != tt.options.InlineArgs {
				t.Errorf("Expected InlineArgs %t, got %t", tt.options.InlineArgs, renderer.options.InlineArgs)
			}
			if renderer.options.IsLast != tt.options.IsLast {
				t.Errorf("Expected IsLast %t, got %t", tt.options.IsLast, renderer.options.IsLast)
			}
		})
	}
}

func TestRenderWorkQueueMCPUsesSnapshotAndFinishIntentMounts(t *testing.T) {
	worker := &WorkflowData{Tools: map[string]any{"work-queue": map[string]any{"worker": true}}}
	t.Run("JSON Copilot", func(t *testing.T) {
		var output strings.Builder
		renderer := NewMCPConfigRenderer(MCPRendererOptions{Format: "json", IncludeCopilotFields: true, IsLast: true})
		renderer.RenderWorkQueueMCP(&output, worker)
		rendered := output.String()
		if !strings.Contains(rendered, `"work-queue": {`) {
			t.Fatalf("expected work-queue MCP server: %s", rendered)
		}
		if !strings.Contains(rendered, `"work_queue_read"`) {
			t.Fatalf("expected the read tool to be exposed: %s", rendered)
		}
		if !strings.Contains(rendered, constants.WorkQueueSnapshotMount) {
			t.Fatalf("expected a read-only snapshot mount: %s", rendered)
		}
		if !strings.Contains(rendered, constants.WorkQueueFinishIntentMount) || !strings.Contains(rendered, `"work_queue_claim_finish"`) {
			t.Fatalf("expected the finish tool to use the safe-output intent mount: %s", rendered)
		}
		if !strings.Contains(rendered, `"GITHUB_SERVER_URL": "\${GITHUB_SERVER_URL}"`) {
			t.Fatalf("expected the configured GitHub server URL in the queue MCP environment: %s", rendered)
		}
		if strings.Contains(rendered, "GITHUB_TOKEN") || strings.Contains(rendered, constants.DefaultWorkspaceMount) || strings.Contains(rendered, constants.DefaultTmpGhAwMount) {
			t.Fatalf("queue MCP must not receive Git credentials or workspace mounts: %s", rendered)
		}
	})

	t.Run("Codex TOML", func(t *testing.T) {
		var output strings.Builder
		renderer := NewMCPConfigRenderer(MCPRendererOptions{Format: "toml"})
		renderer.RenderWorkQueueMCP(&output, worker)
		rendered := output.String()
		if !strings.Contains(rendered, "[mcp_servers.work-queue]") {
			t.Fatalf("expected work-queue MCP server: %s", rendered)
		}
		if !strings.Contains(rendered, constants.WorkQueueSnapshotMount) {
			t.Fatalf("expected a read-only snapshot mount: %s", rendered)
		}
		if !strings.Contains(rendered, constants.WorkQueueFinishIntentMount) {
			t.Fatalf("expected the safe-output intent mount: %s", rendered)
		}
		if !strings.Contains(rendered, `env_vars = ["GITHUB_SERVER_URL", "RUNNER_TEMP"]`) {
			t.Fatalf("expected the configured GitHub server URL in the queue MCP environment: %s", rendered)
		}
		if strings.Contains(rendered, "GITHUB_TOKEN") || strings.Contains(rendered, constants.DefaultWorkspaceMount) || strings.Contains(rendered, constants.DefaultTmpGhAwMount) {
			t.Fatalf("queue MCP must not receive Git credentials or workspace mounts: %s", rendered)
		}
	})
}

func TestRenderWorkQueueObserverMCPIsReadOnly(t *testing.T) {
	observer := &WorkflowData{Tools: map[string]any{"work-queue": true}}
	for _, format := range []string{"json", "toml"} {
		t.Run(format, func(t *testing.T) {
			var output strings.Builder
			renderer := NewMCPConfigRenderer(MCPRendererOptions{Format: format, IncludeCopilotFields: true})
			renderer.RenderWorkQueueMCP(&output, observer)
			rendered := output.String()
			if !strings.Contains(rendered, constants.WorkQueueSnapshotMount) || !strings.Contains(rendered, `"observer"`) {
				t.Fatalf("observer requires its read-only snapshot and protected role: %s", rendered)
			}
			for _, denied := range []string{constants.WorkQueueFinishIntentMount, "work_queue_submit", "work_queue_dispatch_next", "work_queue_claim_finish", "GITHUB_TOKEN", constants.DefaultWorkspaceMount, constants.DefaultTmpGhAwMount} {
				if strings.Contains(rendered, denied) {
					t.Fatalf("observer MCP must not expose %s: %s", denied, rendered)
				}
			}
			if format == "json" && !strings.Contains(rendered, `"tools": ["work_queue_read", "work_queue_explain"]`) {
				t.Fatalf("observer must advertise only read/explain: %s", rendered)
			}
		})
	}
}

func TestRenderWorkQueueMCPEnvironmentLoadsEnterpriseSnapshot(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("Node required for work-queue MCP snapshot validation")
	}
	for _, format := range []string{"json", "toml"} {
		t.Run(format, func(t *testing.T) {
			var output strings.Builder
			renderer := NewMCPConfigRenderer(MCPRendererOptions{Format: format, IsLast: true})
			renderer.RenderWorkQueueMCP(&output, &WorkflowData{Tools: map[string]any{"work-queue": true}})
			source := map[string]string{
				"GITHUB_SERVER_URL": "https://contoso.example",
				"RUNNER_TEMP":       ".",
			}
			var environment map[string]string
			if format == "json" {
				var configuration map[string]struct {
					Env map[string]string `json:"env"`
				}
				// The generated heredoc escapes expansion until the runner writes the config.
				rendered := strings.ReplaceAll(output.String(), `\${`, `${`)
				if err := json.Unmarshal([]byte("{"+rendered+"}"), &configuration); err != nil {
					t.Fatal(err)
				}
				environment = configuration["work-queue"].Env
				for key, value := range environment {
					environment[key] = os.Expand(value, func(name string) string { return source[name] })
				}
			} else {
				var configuration struct {
					Servers map[string]struct {
						EnvVars []string          `toml:"env_vars"`
						Env     map[string]string `toml:"env"`
					} `toml:"mcp_servers"`
				}
				if err := toml.Unmarshal([]byte(output.String()), &configuration); err != nil {
					t.Fatal(err)
				}
				server := configuration.Servers["work-queue"]
				environment = server.Env
				for _, name := range server.EnvVars {
					environment[name] = source[name]
				}
			}
			directory, err := os.MkdirTemp(".", ".work-queue-enterprise-snapshot-")
			if err != nil {
				t.Fatal(err)
			}
			defer os.RemoveAll(directory)
			script := `
const assert = require("node:assert/strict");
const fs = require("node:fs");
const helpers = require("../../actions/setup/js/work_queue_test_helpers.cjs");
const queue = require("../../actions/setup/js/work_queue_replay.cjs");
const { defaultPolicy } = require("../../actions/setup/js/work_queue_policy.cjs");
const { newWork } = require("../../actions/setup/js/work_queue_graph.cjs");
const { loadWorkQueueSnapshot, createWorkQueueTools } = require("../../actions/setup/js/work_queue_mcp_server.cjs");
const resource = { kind: "issue", host: "contoso.example", repository: "owner/repo", repository_id: "1", resource_id: "2", number: "3" };
const ref = "0".repeat(40);
const policy = defaultPolicy({ repository: "owner/repo", principal: "1001" });
policy.projectors = [{ principal: "1001", workflow: helpers.dispatcher.workflow, ref, pools: ["default"], repositories: ["owner/repo"] }];
const work = { ...newWork({}, "enterprise", "subject", "default", policy, 1), subject: resource, depends_on: [{ kind: "issue", resource, condition: "closed" }] };
const transactions = [helpers.genesis(policy)];
transactions.push(helpers.commit("genesis", "enterprise-submit", "submit", helpers.dispatcher, { nodes: [work] }, [work], 1));
transactions.push(helpers.operationCommit(transactions, "enterprise-link", "issue_link", [{ kind: "IssueLink", work_id: work.work_id, resource, projector_ref: ref }], { ...helpers.dispatcher, role: "projector" }));
fs.writeFileSync(process.argv[1], JSON.stringify({ version: 3, sha: "activation-head", captured_at: 10, origin: helpers.dispatcher, worker: null, role: "observer", transactionLog: queue.serializeTransactionLog(transactions) }));
const snapshot = loadWorkQueueSnapshot(process.argv[1]);
const actual = snapshot.projection.works.get(work.work_id);
assert.deepEqual({ ...actual.subject }, resource);
assert.deepEqual({ ...actual.depends_on[0].resource }, resource);
assert.deepEqual({ ...actual.issue_link }, resource);
assert.deepEqual(createWorkQueueTools(snapshot).map(tool => tool.name), ["work_queue_read", "work_queue_explain"]);
process.env.GITHUB_SERVER_URL = "https://another.example";
assert.throws(() => loadWorkQueueSnapshot(process.argv[1]), /resource_unauthorized/);
`
			command := exec.Command(node, "-e", script, filepath.Join(directory, "snapshot.json"))
			command.Env = []string{}
			for name, value := range environment {
				command.Env = append(command.Env, name+"="+value)
			}
			if result, err := command.CombinedOutput(); err != nil {
				t.Fatalf("enterprise snapshot failed with generated %s environment: %v\n%s", format, err, result)
			}
		})
	}
}

func TestWorkQueueMCPIsRegisteredInManifest(t *testing.T) {
	data := &WorkflowData{Tools: map[string]any{"work-queue": true}}
	if !strings.Contains(strings.Join(collectMCPTools(data), ","), "work-queue") {
		t.Fatal("expected the enabled queue tool to register an MCP server")
	}
	servers := collectMCPServersForManifest(data)
	if len(servers) != 1 || servers[0].Name != "work-queue" {
		t.Fatalf("expected queue server in manifest, got %#v", servers)
	}
	if len(servers[0].Tools) != 2 || strings.Join(servers[0].Tools, ",") != "work_queue_explain,work_queue_read" {
		t.Fatalf("expected only observer read/explain tools, got %#v", servers[0].Tools)
	}
	for _, participant := range []*WorkflowData{
		{Tools: map[string]any{"work-queue": map[string]any{"worker": true}}},
		{Tools: map[string]any{"work-queue": true}, SafeOutputs: &SafeOutputsConfig{DispatchWorkflow: &DispatchWorkflowConfig{Workflows: []string{"worker"}}}},
	} {
		servers := collectMCPServersForManifest(participant)
		found := false
		for _, server := range servers {
			if server.Name == "work-queue" && len(server.Tools) == 5 {
				found = true
			}
		}
		if !found {
			t.Fatalf("expected participant queue control tools, got %#v", servers)
		}
	}
}

func TestRenderSafeOutputsMCP_JSON_Copilot(t *testing.T) {
	pinnedGhAwNodeImage := resolveMCPGatewayContainerImage(constants.DefaultGhAwNodeImage, nil)
	renderer := NewMCPConfigRenderer(MCPRendererOptions{
		IncludeCopilotFields: true,
		InlineArgs:           true,
		Format:               "json",
		IsLast:               false,
	})

	var yaml strings.Builder
	renderer.RenderSafeOutputsMCP(&yaml, nil)

	output := yaml.String()

	// Verify Safe Outputs now uses containerized stdio transport
	if !strings.Contains(output, `"type": "stdio"`) {
		t.Error("Expected 'type': 'stdio' field for Copilot safe outputs server")
	}
	if !strings.Contains(output, `"safeoutputs": {`) {
		t.Error("Expected safeoutputs server ID")
	}
	if !strings.Contains(output, `"container": "`+pinnedGhAwNodeImage+`"`) {
		t.Error("Expected gh-aw node container image")
	}
	if !strings.Contains(output, `"mounts": ["\${GITHUB_WORKSPACE}:\${GITHUB_WORKSPACE}:rw", "${RUNNER_TEMP}/gh-aw/safeoutputs:${RUNNER_TEMP}/gh-aw/safeoutputs:rw", "/tmp/gh-aw:/tmp/gh-aw:rw"]`) {
		t.Error("Expected workspace, safe-outputs, log, and tmp mounts")
	}
	if !strings.Contains(output, `"args": ["-w", "\${GITHUB_WORKSPACE}"]`) {
		t.Error("Expected working directory args")
	}
	if !strings.Contains(output, `"entrypoint": "sh"`) {
		t.Error("Expected entrypoint override to sh")
	}
	if !strings.Contains(output, `"entrypointArgs": ["-c", "sh ${RUNNER_TEMP}/gh-aw/safeoutputs/start_safe_outputs_mcp.sh"]`) {
		t.Error("Expected entrypointArgs to run the stdio MCP server script")
	}
	if !strings.Contains(output, `"GH_AW_SAFE_OUTPUTS_CONFIG_PATH": "\${GH_AW_SAFE_OUTPUTS_CONFIG_PATH}"`) {
		t.Error("Expected safe-outputs config path env var")
	}
	if !strings.Contains(output, `"GH_AW_TARGET_REPO_SLUG": "\${GH_AW_TARGET_REPO_SLUG}"`) {
		t.Error("Expected target repository env var")
	}
	if !strings.Contains(output, `"GITHUB_EVENT_NAME": "\${GITHUB_EVENT_NAME}"`) {
		t.Error("Expected GitHub event name env var")
	}
	if !strings.Contains(output, `"GITHUB_EVENT_PATH": "\${GITHUB_EVENT_PATH}"`) {
		t.Error("Expected GitHub event path env var")
	}
	for _, name := range []string{
		"GH_AW_PR_HEAD_BASE_BRANCH",
		"GH_AW_PR_HEAD_BASE_SHA",
		"GH_AW_PR_HEAD_BASE_REPO",
		"GH_AW_PR_HEAD_BASE_PR_NUMBER",
		"GH_AW_PR_HEAD_BASE_REF",
		"GH_AW_PR_HEAD_REPO",
	} {
		if !strings.Contains(output, fmt.Sprintf(`"%s": "\${%s}"`, name, name)) {
			t.Errorf("Expected PR head baseline env var %s", name)
		}
	}
	if strings.Contains(output, `"url": "http://`) {
		t.Error("Did not expect HTTP URL field")
	}
	if strings.Contains(output, `"Authorization":`) {
		t.Error("Did not expect Authorization header")
	}
}

func TestRenderSafeOutputsMCP_JSON_Claude(t *testing.T) {
	pinnedGhAwNodeImage := resolveMCPGatewayContainerImage(constants.DefaultGhAwNodeImage, nil)
	renderer := NewMCPConfigRenderer(MCPRendererOptions{
		IncludeCopilotFields: false,
		InlineArgs:           false,
		Format:               "json",
		IsLast:               true,
	})

	var yaml strings.Builder
	renderer.RenderSafeOutputsMCP(&yaml, nil)

	output := yaml.String()

	if !strings.Contains(output, `"safeoutputs": {`) {
		t.Error("Expected safeoutputs server ID")
	}
	if !strings.Contains(output, `"container": "`+pinnedGhAwNodeImage+`"`) {
		t.Error("Expected gh-aw node container image")
	}
	if !strings.Contains(output, `"entrypoint": "sh"`) {
		t.Error("Expected entrypoint override to sh")
	}
	if !strings.Contains(output, `"entrypointArgs": ["-c", "sh ${RUNNER_TEMP}/gh-aw/safeoutputs/start_safe_outputs_mcp.sh"]`) {
		t.Error("Expected entrypointArgs to run the stdio MCP server script")
	}
	if !strings.Contains(output, `"GH_AW_SAFE_OUTPUTS": "\${GH_AW_SAFE_OUTPUTS}"`) {
		t.Error("Expected backslash-escaped shell variable reference for safe outputs path")
	}
	if !strings.Contains(output, `"RUNNER_TEMP": "\${RUNNER_TEMP}"`) {
		t.Error("Expected backslash-escaped shell variable reference for RUNNER_TEMP")
	}
	if strings.Contains(output, `"tools"`) {
		t.Error("Should not contain 'tools' field")
	}
	if strings.Contains(output, `"type"`) {
		t.Error("Should not contain 'type' field for Claude stdio server config")
	}
	if strings.Contains(output, `"url": "http://`) {
		t.Error("Did not expect HTTP URL field")
	}
}

func TestRenderSafeOutputsMCP_TOML(t *testing.T) {
	pinnedGhAwNodeImage := resolveMCPGatewayContainerImage(constants.DefaultGhAwNodeImage, nil)
	renderer := NewMCPConfigRenderer(MCPRendererOptions{
		IncludeCopilotFields: false,
		InlineArgs:           false,
		Format:               "toml",
		IsLast:               false,
	})

	var yaml strings.Builder
	renderer.RenderSafeOutputsMCP(&yaml, nil)

	output := yaml.String()

	// Verify TOML format with containerized stdio transport
	if !strings.Contains(output, "[mcp_servers.safeoutputs]") {
		t.Error("Expected TOML section header")
	}
	if !strings.Contains(output, `container = "`+pinnedGhAwNodeImage+`"`) {
		t.Error("Expected gh-aw node container image")
	}
	if !strings.Contains(output, `mounts = ["\${GITHUB_WORKSPACE}:\${GITHUB_WORKSPACE}:rw", "${RUNNER_TEMP}/gh-aw/safeoutputs:${RUNNER_TEMP}/gh-aw/safeoutputs:rw", "/tmp/gh-aw:/tmp/gh-aw:rw"]`) {
		t.Error("Expected TOML mounts")
	}
	if !strings.Contains(output, `args = ["-w", "$GITHUB_WORKSPACE"]`) {
		t.Error("Expected TOML args")
	}
	if !strings.Contains(output, `entrypoint = "sh"`) {
		t.Error("Expected TOML entrypoint override to sh")
	}
	if !strings.Contains(output, `entrypointArgs = ["-c", "sh ${RUNNER_TEMP}/gh-aw/safeoutputs/start_safe_outputs_mcp.sh"]`) {
		t.Error("Expected TOML entrypointArgs to run the stdio MCP server script")
	}
	if !strings.Contains(output, `env_vars = ["DEBUG", "DEFAULT_BRANCH", "GH_AW_ASSETS_ALLOWED_EXTS", "GH_AW_ASSETS_BRANCH", "GH_AW_ASSETS_MAX_SIZE_KB", "GH_AW_MCP_LOG_DIR", "GH_AW_SAFE_OUTPUTS", "GH_AW_SAFE_OUTPUTS_CONFIG_PATH", "GH_AW_SAFE_OUTPUTS_TOOLS_PATH", "GH_AW_TARGET_REPO_SLUG", "GH_AW_POLICY_ALLOW_CREATE_PULL_REQUEST", "GH_AW_PR_HEAD_BASE_BRANCH", "GH_AW_PR_HEAD_BASE_SHA", "GH_AW_PR_HEAD_BASE_REPO", "GH_AW_PR_HEAD_BASE_PR_NUMBER", "GH_AW_PR_HEAD_BASE_REF", "GH_AW_PR_HEAD_REPO", "GITHUB_EVENT_NAME", "GITHUB_EVENT_PATH", "GITHUB_REPOSITORY", "GITHUB_SHA", "GITHUB_TOKEN", "GITHUB_WORKSPACE", "RUNNER_TEMP", "`+strings.Join(objectPackLimitEnvNames, `", "`)+`"]`) {
		t.Error("Expected TOML env vars")
	}
	if strings.Contains(output, `type = "http"`) {
		t.Error("Did not expect TOML HTTP type field")
	}
	if strings.Contains(output, `url = "http://`) {
		t.Error("Did not expect TOML HTTP URL")
	}
}

func TestRenderAgenticWorkflowsMCP_JSON_Copilot(t *testing.T) {
	renderer := NewMCPConfigRenderer(MCPRendererOptions{
		IncludeCopilotFields: true,
		InlineArgs:           true,
		Format:               "json",
		IsLast:               true,
		ActionMode:           ActionModeDev, // Default action mode is dev
	})

	var yaml strings.Builder
	renderer.RenderAgenticWorkflowsMCP(&yaml)

	output := yaml.String()

	// Verify MCP Gateway Specification v1.0.0 fields
	if !strings.Contains(output, `"type": "stdio"`) {
		t.Error("Expected 'type': 'stdio' field per MCP Gateway Specification")
	}
	if !strings.Contains(output, `"`+constants.AgenticWorkflowsMCPServerID.String()+`": {`) {
		t.Error("Expected agenticworkflows server ID")
	}
	// Per MCP Gateway Specification v1.0.0, stdio servers MUST use container format
	// In dev mode, should use locally built image
	if !strings.Contains(output, `"container": "localhost/gh-aw:dev"`) {
		t.Error("Expected dev mode container image for containerized server")
	}
	// In dev mode, should NOT have entrypoint (uses container's default ENTRYPOINT)
	if strings.Contains(output, `"entrypoint"`) {
		t.Error("Did not expect entrypoint field in dev mode (uses container's ENTRYPOINT)")
	}
	// In dev mode, should NOT have entrypointArgs (uses container's default CMD)
	if strings.Contains(output, `"entrypointArgs"`) {
		t.Error("Did not expect entrypointArgs field in dev mode (uses container's CMD)")
	}
	// In dev mode, should NOT have binary mounts
	if strings.Contains(output, `${RUNNER_TEMP}/gh-aw:${RUNNER_TEMP}/gh-aw:ro`) {
		t.Error("Did not expect ${RUNNER_TEMP}/gh-aw mount in dev mode (binary is in image)")
	}
	if strings.Contains(output, `/usr/bin/gh:/usr/bin/gh:ro`) {
		t.Error("Did not expect /usr/bin/gh mount in dev mode (gh CLI is in image)")
	}
	// Should have DEBUG and GITHUB_TOKEN
	if !strings.Contains(output, `"DEBUG": "\${DEBUG}"`) {
		t.Error("Expected DEBUG forwarded from the environment")
	}
	if !strings.Contains(output, `"GITHUB_TOKEN"`) {
		t.Error("Expected GITHUB_TOKEN in env vars")
	}
	// Should have network access and working directory args
	if !strings.Contains(output, `"args": ["--network", "host", "-w", "\${GITHUB_WORKSPACE}"]`) {
		t.Error("Expected args with network access and working directory set to workspace")
	}
}

func TestRenderAgenticWorkflowsMCP_JSON_Claude(t *testing.T) {
	renderer := NewMCPConfigRenderer(MCPRendererOptions{
		IncludeCopilotFields: false,
		InlineArgs:           false,
		Format:               "json",
		IsLast:               false,
		ActionMode:           ActionModeDev, // Default action mode is dev
	})

	var yaml strings.Builder
	renderer.RenderAgenticWorkflowsMCP(&yaml)

	output := yaml.String()

	// Verify Claude format (no Copilot-specific fields)
	if strings.Contains(output, `"type"`) {
		t.Error("Should not contain 'type' field for Claude")
	}
	if strings.Contains(output, `"tools"`) {
		t.Error("Should not contain 'tools' field for Claude")
	}
}

func TestRenderAgenticWorkflowsMCP_TOML(t *testing.T) {
	renderer := NewMCPConfigRenderer(MCPRendererOptions{
		IncludeCopilotFields: false,
		InlineArgs:           false,
		Format:               "toml",
		IsLast:               false,
		ActionMode:           ActionModeDev, // Default action mode is dev
	})

	var yaml strings.Builder
	renderer.RenderAgenticWorkflowsMCP(&yaml)

	output := yaml.String()

	// Verify TOML format (per MCP Gateway Specification v1.0.0)
	if !strings.Contains(output, "[mcp_servers."+constants.AgenticWorkflowsMCPServerID.String()+"]") {
		t.Error("Expected TOML section header")
	}
	// Per MCP Gateway Specification v1.0.0, stdio servers MUST use container format
	// In dev mode, should use locally built image
	if !strings.Contains(output, `container = "localhost/gh-aw:dev"`) {
		t.Error("Expected dev mode container image for containerized server")
	}
	// In dev mode, should NOT have entrypoint (uses container's default ENTRYPOINT)
	if strings.Contains(output, `entrypoint =`) {
		t.Error("Did not expect entrypoint field in dev mode (uses container's ENTRYPOINT)")
	}
	// In dev mode, should NOT have entrypointArgs (uses container's default CMD)
	if strings.Contains(output, `entrypointArgs =`) {
		t.Error("Did not expect entrypointArgs field in dev mode (uses container's CMD)")
	}
	// In dev mode, should NOT have binary mounts
	if strings.Contains(output, `${RUNNER_TEMP}/gh-aw:${RUNNER_TEMP}/gh-aw:ro`) {
		t.Error("Did not expect ${RUNNER_TEMP}/gh-aw mount in dev mode (binary is in image)")
	}
	if strings.Contains(output, `/usr/bin/gh:/usr/bin/gh:ro`) {
		t.Error("Did not expect /usr/bin/gh mount in dev mode (gh CLI is in image)")
	}
	// Should have DEBUG, GH_TOKEN and GITHUB_TOKEN
	if !strings.Contains(output, `"DEBUG"`) {
		t.Error("Expected DEBUG in env_vars")
	}
	if !strings.Contains(output, `"GH_TOKEN"`) {
		t.Error("Expected GH_TOKEN in env_vars")
	}
	if !strings.Contains(output, `"GITHUB_TOKEN"`) {
		t.Error("Expected GITHUB_TOKEN in env_vars")
	}
}

func TestRenderGitHubMCP_JSON_Copilot_Local(t *testing.T) {
	renderer := NewMCPConfigRenderer(MCPRendererOptions{
		IncludeCopilotFields: true,
		InlineArgs:           true,
		Format:               "json",
		IsLast:               false,
	})

	githubTool := map[string]any{
		"mode":     "local",
		"toolsets": "default",
	}

	workflowData := &WorkflowData{
		Name: "test-workflow",
	}

	var yaml strings.Builder
	renderer.RenderGitHubMCP(&yaml, githubTool, workflowData)

	output := yaml.String()

	// Verify GitHub MCP config
	if !strings.Contains(output, `"github": {`) {
		t.Error("Expected github server ID")
	}
	if !strings.Contains(output, `"type": "stdio"`) {
		t.Error("Expected 'type': 'stdio' field for Copilot")
	}
	if !strings.Contains(output, `"container":`) {
		t.Error("Expected container field for local mode")
	}
}

func TestRenderGitHubMCP_JSON_Claude_Local(t *testing.T) {
	renderer := NewMCPConfigRenderer(MCPRendererOptions{
		IncludeCopilotFields: false,
		InlineArgs:           false,
		Format:               "json",
		IsLast:               true,
	})

	githubTool := map[string]any{
		"mode":     "local",
		"toolsets": "default",
	}

	workflowData := &WorkflowData{
		Name: "test-workflow",
	}

	var yaml strings.Builder
	renderer.RenderGitHubMCP(&yaml, githubTool, workflowData)

	output := yaml.String()

	// Verify GitHub MCP config for Claude (no type field)
	if !strings.Contains(output, `"github": {`) {
		t.Error("Expected github server ID")
	}
	if !strings.Contains(output, `"container":`) {
		t.Error("Expected container field for local mode")
	}
	// Claude format does NOT include 'type' field (added only for Copilot)
	if strings.Contains(output, `"type"`) {
		t.Error("Should not contain 'type' field for Claude")
	}
}

func TestRenderGitHubMCP_JSON_Copilot_Remote(t *testing.T) {
	renderer := NewMCPConfigRenderer(MCPRendererOptions{
		IncludeCopilotFields: true,
		InlineArgs:           true,
		Format:               "json",
		IsLast:               false,
	})

	githubTool := map[string]any{
		"mode":     "remote",
		"toolsets": "default",
	}

	workflowData := &WorkflowData{
		Name: "test-workflow",
	}

	var yaml strings.Builder
	renderer.RenderGitHubMCP(&yaml, githubTool, workflowData)

	output := yaml.String()

	// Verify remote GitHub MCP config
	if !strings.Contains(output, `"github": {`) {
		t.Error("Expected github server ID")
	}
	if !strings.Contains(output, `"type": "http"`) {
		t.Error("Expected 'type': 'http' field for remote mode")
	}
	if !strings.Contains(output, `"url"`) {
		t.Error("Expected url field for remote mode")
	}
}

func TestRenderGitHubMCP_TOML(t *testing.T) {
	renderer := NewMCPConfigRenderer(MCPRendererOptions{
		IncludeCopilotFields: false,
		InlineArgs:           false,
		Format:               "toml",
		IsLast:               false,
	})

	githubTool := map[string]any{
		"mode":     "local",
		"toolsets": "default",
	}

	workflowData := &WorkflowData{
		Name: "test-workflow",
	}

	var yaml strings.Builder
	renderer.RenderGitHubMCP(&yaml, githubTool, workflowData)

	output := yaml.String()

	// TOML format should now be supported and generate valid output
	if output == "" {
		t.Error("Expected non-empty output for TOML format")
	}

	// Verify key TOML elements are present
	expectedElements := []string{
		"[mcp_servers.github]",
		"user_agent =",
		"startup_timeout_sec =",
		"tool_timeout_sec =",
	}

	for _, expected := range expectedElements {
		if !strings.Contains(output, expected) {
			t.Errorf("Expected output to contain %q, but it didn't.\nOutput:\n%s", expected, output)
		}
	}
}

func TestOptionCombinations(t *testing.T) {
	tests := []struct {
		name    string
		options MCPRendererOptions
	}{
		{
			name: "all true",
			options: MCPRendererOptions{
				IncludeCopilotFields: true,
				InlineArgs:           true,
				Format:               "json",
				IsLast:               true,
			},
		},
		{
			name: "all false",
			options: MCPRendererOptions{
				IncludeCopilotFields: false,
				InlineArgs:           false,
				Format:               "json",
				IsLast:               false,
			},
		},
		{
			name: "mixed copilot inline",
			options: MCPRendererOptions{
				IncludeCopilotFields: true,
				InlineArgs:           false,
				Format:               "json",
				IsLast:               false,
			},
		},
		{
			name: "mixed claude inline",
			options: MCPRendererOptions{
				IncludeCopilotFields: false,
				InlineArgs:           true,
				Format:               "json",
				IsLast:               false,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			renderer := NewMCPConfigRenderer(tt.options)

			// Test each render method doesn't panic
			var yaml strings.Builder

			renderer.RenderSafeOutputsMCP(&yaml, nil)

			yaml.Reset()
			renderer.RenderAgenticWorkflowsMCP(&yaml)

			yaml.Reset()
			githubTool := map[string]any{
				"mode":     "local",
				"toolsets": "default",
			}
			workflowData := &WorkflowData{Name: "test"}
			renderer.RenderGitHubMCP(&yaml, githubTool, workflowData)
		})
	}
}

func TestRenderJSONMCPConfig_OTLPGateway(t *testing.T) {
	tests := []struct {
		name         string
		otlpEndpoint string
		otlpHeaders  string
		wantEndpoint bool
	}{
		{
			name:         "OTLP endpoint only (no headers)",
			otlpEndpoint: "https://otel.example.com:4318",
			otlpHeaders:  "",
			wantEndpoint: true,
		},
		{
			name:         "OTLP endpoint and headers",
			otlpEndpoint: "https://otel.example.com:4318",
			otlpHeaders:  "Authorization=Bearer token123",
			wantEndpoint: true,
		},
		{
			name:         "no OTLP config",
			otlpEndpoint: "",
			otlpHeaders:  "",
			wantEndpoint: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gatewayConfig := &MCPGatewayRuntimeConfig{
				Domain:       "localhost",
				AgentID:      "test-api-key",
				OTLPEndpoint: tt.otlpEndpoint,
				OTLPHeaders:  tt.otlpHeaders,
			}

			workflowData := &WorkflowData{
				Name:            "test-workflow",
				FrontmatterHash: "abc123",
			}

			var output strings.Builder
			err := RenderJSONMCPConfig(
				&output,
				map[string]any{},
				[]string{},
				workflowData,
				JSONMCPConfigOptions{
					ConfigPath:    "/tmp/test/mcp-servers.json",
					GatewayConfig: gatewayConfig,
					Renderers:     MCPToolRenderers{},
				},
			)

			if err != nil {
				t.Fatalf("RenderJSONMCPConfig returned error: %v", err)
			}

			result := output.String()

			// Verify no node/bash preamble for header conversion is emitted
			if strings.Contains(result, "_GH_AW_OTLP_HEADERS_JSON") {
				t.Error("output must not contain old _GH_AW_OTLP_HEADERS_JSON preamble")
			}
			if strings.Contains(result, "_GH_AW_OTLP_HEADERS_ESC") {
				t.Error("output must not contain _GH_AW_OTLP_HEADERS_ESC preamble")
			}

			// Verify headers field is never emitted in JSON config; headers are now
			// passed exclusively via the OTEL_EXPORTER_OTLP_HEADERS container env var.
			if strings.Contains(result, `"headers"`) {
				t.Errorf("headers field must not appear in gateway JSON config (use OTEL_EXPORTER_OTLP_HEADERS env var instead)\noutput:\n%s", result)
			}

			// Verify endpoint is present iff configured
			if tt.wantEndpoint && !strings.Contains(result, `"endpoint": "${OTEL_EXPORTER_OTLP_ENDPOINT}"`) {
				t.Errorf("expected endpoint in output\noutput:\n%s", result)
			}
			if !tt.wantEndpoint && strings.Contains(result, `"opentelemetry"`) {
				t.Errorf("expected no opentelemetry section when no endpoint configured\noutput:\n%s", result)
			}
		})
	}
}

// TestRenderJSONMCPConfig_SessionTimeout verifies that sessionTimeout is emitted
// in the gateway JSON section when set on the MCPGatewayRuntimeConfig.
func TestRenderJSONMCPConfig_SessionTimeout(t *testing.T) {
	tests := []struct {
		name           string
		sessionTimeout string
		wantField      bool
	}{
		{
			name:           "includes sessionTimeout when set",
			sessionTimeout: "4h",
			wantField:      true,
		},
		{
			name:           "omits sessionTimeout when empty",
			sessionTimeout: "",
			wantField:      false,
		},
		{
			name:           "includes sessionTimeout 30m",
			sessionTimeout: "30m",
			wantField:      true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gatewayConfig := &MCPGatewayRuntimeConfig{
				Domain:         "localhost",
				AgentID:        "test-api-key",
				SessionTimeout: tt.sessionTimeout,
			}

			workflowData := &WorkflowData{
				Name:            "test-workflow",
				FrontmatterHash: "abc123",
			}

			var output strings.Builder
			err := RenderJSONMCPConfig(
				&output,
				map[string]any{},
				[]string{},
				workflowData,
				JSONMCPConfigOptions{
					ConfigPath:    "/tmp/test/mcp-servers.json",
					GatewayConfig: gatewayConfig,
					Renderers:     MCPToolRenderers{},
				},
			)

			if err != nil {
				t.Fatalf("RenderJSONMCPConfig returned error: %v", err)
			}

			result := output.String()
			hasField := strings.Contains(result, `"sessionTimeout":`)
			if hasField != tt.wantField {
				t.Errorf("sessionTimeout field presence = %v, want %v\noutput:\n%s", hasField, tt.wantField, result)
			}

			if tt.wantField && tt.sessionTimeout != "" {
				expected := `"sessionTimeout": "` + tt.sessionTimeout + `"`
				if !strings.Contains(result, expected) {
					t.Errorf("expected %q in output\noutput:\n%s", expected, result)
				}
			}
		})
	}
}

// TestRenderJSONMCPConfig_ToolTimeout verifies that toolTimeout is emitted
// in the gateway JSON section when set on the MCPGatewayRuntimeConfig.
func TestRenderJSONMCPConfig_ToolTimeout(t *testing.T) {
	tests := []struct {
		name        string
		toolTimeout string
		expected    int
		wantField   bool
	}{
		{
			name:        "includes toolTimeout when set",
			toolTimeout: "2m",
			expected:    120,
			wantField:   true,
		},
		{
			name:        "omits toolTimeout when empty",
			toolTimeout: "",
			wantField:   false,
		},
		{
			name:        "includes toolTimeout 30s",
			toolTimeout: "30s",
			expected:    30,
			wantField:   true,
		},
		{
			name:        "rounds fractional toolTimeout to nearest second",
			toolTimeout: "90500ms",
			expected:    91,
			wantField:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gatewayConfig := &MCPGatewayRuntimeConfig{
				Domain:      "localhost",
				AgentID:     "test-api-key",
				ToolTimeout: tt.toolTimeout,
			}

			workflowData := &WorkflowData{
				Name:            "test-workflow",
				FrontmatterHash: "abc123",
			}

			var output strings.Builder
			err := RenderJSONMCPConfig(
				&output,
				map[string]any{},
				[]string{},
				workflowData,
				JSONMCPConfigOptions{
					ConfigPath:    "/tmp/test/mcp-servers.json",
					GatewayConfig: gatewayConfig,
					Renderers:     MCPToolRenderers{},
				},
			)

			if err != nil {
				t.Fatalf("RenderJSONMCPConfig returned error: %v", err)
			}

			result := output.String()
			hasField := strings.Contains(result, `"toolTimeout":`)
			if hasField != tt.wantField {
				t.Errorf("toolTimeout field presence = %v, want %v\noutput:\n%s", hasField, tt.wantField, result)
			}

			if tt.wantField && tt.toolTimeout != "" {
				expected := fmt.Sprintf(`"toolTimeout": %d`, tt.expected)
				if !strings.Contains(result, expected) {
					t.Errorf("expected %q in output\noutput:\n%s", expected, result)
				}
			}
		})
	}
}

// TestRenderJSONMCPConfig_StartupTimeout verifies that startupTimeout is emitted
// in the gateway JSON section when set on the MCPGatewayRuntimeConfig.
func TestRenderJSONMCPConfig_StartupTimeout(t *testing.T) {
	tests := []struct {
		name           string
		startupTimeout int
		wantField      bool
		wantValue      int
	}{
		{
			name:           "emits default startupTimeout of 120",
			startupTimeout: 120,
			wantField:      true,
			wantValue:      120,
		},
		{
			name:           "emits custom startupTimeout of 180",
			startupTimeout: 180,
			wantField:      true,
			wantValue:      180,
		},
		{
			name:           "omits startupTimeout when zero",
			startupTimeout: 0,
			wantField:      false,
		},
		{
			name:           "emits minimum startupTimeout of 1",
			startupTimeout: 1,
			wantField:      true,
			wantValue:      1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gatewayConfig := &MCPGatewayRuntimeConfig{
				Domain:         "localhost",
				AgentID:        "test-api-key",
				StartupTimeout: tt.startupTimeout,
			}

			workflowData := &WorkflowData{
				Name:            "test-workflow",
				FrontmatterHash: "abc123",
			}

			var output strings.Builder
			err := RenderJSONMCPConfig(
				&output,
				map[string]any{},
				[]string{},
				workflowData,
				JSONMCPConfigOptions{
					ConfigPath:    "/tmp/test/mcp-servers.json",
					GatewayConfig: gatewayConfig,
					Renderers:     MCPToolRenderers{},
				},
			)

			if err != nil {
				t.Fatalf("RenderJSONMCPConfig returned error: %v", err)
			}

			result := output.String()
			hasField := strings.Contains(result, `"startupTimeout":`)
			if hasField != tt.wantField {
				t.Errorf("startupTimeout field presence = %v, want %v\noutput:\n%s", hasField, tt.wantField, result)
			}

			if tt.wantField {
				expected := fmt.Sprintf(`"startupTimeout": %d`, tt.wantValue)
				if !strings.Contains(result, expected) {
					t.Errorf("expected %q in output\noutput:\n%s", expected, result)
				}
			}
		})
	}
}
