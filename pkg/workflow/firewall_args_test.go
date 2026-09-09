//go:build !integration

package workflow

import (
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/constants"
)

// TestFirewallArgsInCopilotEngine tests that custom firewall args are included in AWF command
func TestFirewallArgsInCopilotEngine(t *testing.T) {
	t.Run("no custom args uses only default flags", func(t *testing.T) {
		workflowData := &WorkflowData{
			Name: "test-workflow",
			EngineConfig: &EngineConfig{
				ID: "copilot",
			},
			NetworkPermissions: &NetworkPermissions{
				Allowed: []string{"copilot"},
				Firewall: &FirewallConfig{
					Enabled: true,
				},
			},
		}

		engine := NewCopilotEngine()
		steps := engine.GetExecutionSteps(workflowData, "test.log")

		stepContent := requireCopilotExecutionStep(t, steps)

		// Check that the command contains awf (AWF v0.15.0+ uses chroot mode by default)
		if !strings.Contains(stepContent, "awf ") {
			t.Error("Expected command to contain AWF")
		}

		// With config file support (default AWF version), domains appear in the JSON config
		// rather than as a --allow-domains CLI flag. Verify the config JSON is written.
		if !strings.Contains(stepContent, "allowDomains") {
			t.Error("Expected command to contain 'allowDomains' in the AWF config JSON")
		}

		if !strings.Contains(stepContent, "--log-level") {
			t.Error("Expected command to contain '--log-level'")
		}

		// docker-host-path-prefix is no longer emitted (removed for sysroot, gh-aw#34896)
		if strings.Contains(stepContent, "--docker-host-path-prefix") {
			t.Error("Expected command NOT to emit --docker-host-path-prefix (sysroot handles path visibility)")
		}

		// --docker-host probe must still be present
		dockerHostInitSnippet := `GH_AW_DOCKER_HOST=""`
		dockerHostConditionSnippet := `if [[ "${DOCKER_HOST:-}" =~ ^tcp:// ]]; then`
		dockerHostAssignmentSnippet := `GH_AW_DOCKER_HOST="${DOCKER_HOST}"`
		dockerHostArgsRefSnippet := `${GH_AW_DOCKER_HOST:+--docker-host "$GH_AW_DOCKER_HOST"}`

		dockerHostInitIdx := strings.Index(stepContent, dockerHostInitSnippet)
		dockerHostConditionIdx := strings.Index(stepContent, dockerHostConditionSnippet)
		dockerHostAssignmentIdx := strings.Index(stepContent, dockerHostAssignmentSnippet)
		dockerHostArgsRefIdx := strings.Index(stepContent, dockerHostArgsRefSnippet)
		if dockerHostInitIdx == -1 || dockerHostConditionIdx == -1 || dockerHostAssignmentIdx == -1 || dockerHostArgsRefIdx == -1 || dockerHostInitIdx >= dockerHostConditionIdx || dockerHostConditionIdx >= dockerHostAssignmentIdx || dockerHostAssignmentIdx >= dockerHostArgsRefIdx {
			t.Error("Expected command to initialize docker-host variable, evaluate DOCKER_HOST condition, set --docker-host source variable, then expand --docker-host args in AWF invocation")
		}

		// Verify that --log-dir is included in copilot args for log collection
		if !strings.Contains(stepContent, "--log-dir /tmp/gh-aw/sandbox/agent/logs/") {
			t.Error("Expected copilot command to contain '--log-dir /tmp/gh-aw/sandbox/agent/logs/' for log collection in firewall mode")
		}
	})

	t.Run("custom args are included in AWF command", func(t *testing.T) {
		workflowData := &WorkflowData{
			Name: "test-workflow",
			EngineConfig: &EngineConfig{
				ID: "copilot",
			},
			NetworkPermissions: &NetworkPermissions{
				Allowed: []string{"copilot"},
				Firewall: &FirewallConfig{
					Enabled: true,
					Args:    []string{"--custom-arg", "value", "--another-flag"},
				},
			},
		}

		engine := NewCopilotEngine()
		steps := engine.GetExecutionSteps(workflowData, "test.log")

		stepContent := requireCopilotExecutionStep(t, steps)

		// Check that custom args are included
		if !strings.Contains(stepContent, "--custom-arg") {
			t.Error("Expected command to contain custom arg '--custom-arg'")
		}

		if !strings.Contains(stepContent, "value") {
			t.Error("Expected command to contain custom arg value 'value'")
		}

		if !strings.Contains(stepContent, "--another-flag") {
			t.Error("Expected command to contain custom arg '--another-flag'")
		}

		// With config file support, domains appear in the JSON config (not as --allow-domains)
		if !strings.Contains(stepContent, "allowDomains") {
			t.Error("Expected command to still contain 'allowDomains' in the AWF config JSON")
		}
	})

	t.Run("custom args with spaces are properly escaped", func(t *testing.T) {
		workflowData := &WorkflowData{
			Name: "test-workflow",
			EngineConfig: &EngineConfig{
				ID: "copilot",
			},
			NetworkPermissions: &NetworkPermissions{
				Firewall: &FirewallConfig{
					Enabled: true,
					Args:    []string{"--message", "hello world", "--path", "/some/path with spaces"},
				},
			},
		}

		engine := NewCopilotEngine()
		steps := engine.GetExecutionSteps(workflowData, "test.log")

		stepContent := requireCopilotExecutionStep(t, steps)

		// Check that args with spaces are present (they should be escaped)
		if !strings.Contains(stepContent, "--message") {
			t.Error("Expected command to contain '--message' flag")
		}

		// The value might be escaped, so just check the flag exists
		if !strings.Contains(stepContent, "--path") {
			t.Error("Expected command to contain '--path' flag")
		}
	})

	t.Run("AWF uses chroot mode instead of individual binary mounts", func(t *testing.T) {
		workflowData := &WorkflowData{
			Name: "test-workflow",
			EngineConfig: &EngineConfig{
				ID: "copilot",
			},
			NetworkPermissions: &NetworkPermissions{
				Firewall: &FirewallConfig{
					Enabled: true,
				},
			},
		}

		engine := NewCopilotEngine()
		steps := engine.GetExecutionSteps(workflowData, "test.log")

		stepContent := requireCopilotExecutionStep(t, steps)

		// Check that AWF is used for transparent host access (AWF v0.15.0+)
		// Chroot mode is now the default, so no --enable-chroot flag is needed
		if !strings.Contains(stepContent, "awf ") {
			t.Error("Expected AWF command for transparent host access")
		}

		// Verify that individual binary mounts are not used (chroot mode is default)
		if strings.Contains(stepContent, "--mount /usr/bin/gh:/usr/bin/gh:ro") {
			t.Error("Individual binary mounts should not be present with default chroot mode")
		}
	})

	t.Run("AWF command includes image-tag with default version", func(t *testing.T) {
		workflowData := &WorkflowData{
			Name: "test-workflow",
			EngineConfig: &EngineConfig{
				ID: "copilot",
			},
			NetworkPermissions: &NetworkPermissions{
				Firewall: &FirewallConfig{
					Enabled: true,
				},
			},
		}

		engine := NewCopilotEngine()
		steps := engine.GetExecutionSteps(workflowData, "test.log")

		stepContent := requireCopilotExecutionStep(t, steps)

		// With config file support (default AWF version), the image tag is expressed in the
		// JSON config file rather than as a --image-tag CLI flag.
		// Verify the image tag version appears in the config JSON.
		expectedVersion := strings.TrimPrefix(string(constants.DefaultFirewallVersion), "v")
		if !strings.Contains(stepContent, expectedVersion) {
			t.Errorf("Expected AWF config JSON to contain image tag version '%s'", expectedVersion)
		}
		// imageTag field name must be present
		if !strings.Contains(stepContent, "imageTag") {
			t.Error("Expected AWF config JSON to contain 'imageTag' field")
		}
	})

	t.Run("AWF command includes image-tag with custom version", func(t *testing.T) {
		customVersion := "v0.5.0"
		workflowData := &WorkflowData{
			Name: "test-workflow",
			EngineConfig: &EngineConfig{
				ID: "copilot",
			},
			NetworkPermissions: &NetworkPermissions{
				Firewall: &FirewallConfig{
					Enabled: true,
					Version: customVersion,
				},
			},
		}

		engine := NewCopilotEngine()
		steps := engine.GetExecutionSteps(workflowData, "test.log")

		stepContent := requireCopilotExecutionStep(t, steps)

		// Image tag is now always written to the JSON config file, never as a CLI flag.
		expectedImageTag := `\"imageTag\":\"` + strings.TrimPrefix(customVersion, "v")
		if !strings.Contains(stepContent, expectedImageTag) {
			t.Errorf("Expected AWF config JSON to contain '%s', got:\n%s", expectedImageTag, stepContent)
		}

		// --image-tag must NOT appear as a CLI flag
		if strings.Contains(stepContent, "--image-tag") {
			t.Error("--image-tag should not appear as a CLI flag; it is in the config JSON")
		}
	})

	t.Run("skips docker-host-path-prefix probe and arg ref when AWF version is too old", func(t *testing.T) {
		workflowData := &WorkflowData{
			Name: "test-workflow",
			EngineConfig: &EngineConfig{
				ID: "copilot",
			},
			NetworkPermissions: &NetworkPermissions{
				Firewall: &FirewallConfig{
					Enabled: true,
					Version: "v0.25.42",
				},
			},
		}

		engine := NewCopilotEngine()
		steps := engine.GetExecutionSteps(workflowData, "test.log")
		stepContent := requireCopilotExecutionStep(t, steps)

		if strings.Contains(stepContent, `GH_AW_DOCKER_HOST_PATH_PREFIX_ARGS=""`) {
			t.Error("Expected command to skip docker-host-path-prefix probe variable initialization for unsupported AWF versions")
		}
		if strings.Contains(stepContent, `GH_AW_DOCKER_HOST_PATH_PREFIX_ARGS="--docker-host-path-prefix ${RUNNER_TEMP}/gh-aw"`) {
			t.Error("Expected command to skip docker-host-path-prefix assignment for unsupported AWF versions")
		}
		if strings.Contains(stepContent, `${GH_AW_DOCKER_HOST_PATH_PREFIX_ARGS}`) {
			t.Error("Expected command to skip docker-host-path-prefix args variable expansion for unsupported AWF versions")
		}
	})

	t.Run("AWF command includes ssl-bump flag when enabled", func(t *testing.T) {
		workflowData := &WorkflowData{
			Name: "test-workflow",
			EngineConfig: &EngineConfig{
				ID: "copilot",
			},
			NetworkPermissions: &NetworkPermissions{
				Firewall: &FirewallConfig{
					Enabled: true,
					SSLBump: true,
				},
			},
		}

		engine := NewCopilotEngine()
		steps := engine.GetExecutionSteps(workflowData, "test.log")

		stepContent := requireCopilotExecutionStep(t, steps)

		// Check that --ssl-bump flag is included
		if !strings.Contains(stepContent, "--ssl-bump") {
			t.Error("Expected AWF command to contain '--ssl-bump' flag")
		}
	})

	t.Run("arc-dind uses daemon-visible paths and overlay mounts", func(t *testing.T) {
		workflowData := &WorkflowData{
			Name: "test-workflow",
			EngineConfig: &EngineConfig{
				ID: "copilot",
			},
			RunnerConfig: &RunnerConfig{
				Topology: RunnerTopologyArcDind,
			},
			Tools: map[string]any{"github": map[string]any{}},
			NetworkPermissions: &NetworkPermissions{
				Firewall: &FirewallConfig{
					Enabled: true,
				},
			},
		}

		engine := NewCopilotEngine()
		steps := engine.GetExecutionSteps(workflowData, "test.log")
		stepContent := requireCopilotExecutionStep(t, steps)

		// docker-host-path-prefix must NOT be emitted — sysroot makes it unnecessary and it
		// causes the workspace mount to be translated to a non-existent path (gh-aw#34896).
		if strings.Contains(stepContent, `--docker-host-path-prefix`) {
			t.Error("Expected arc-dind NOT to emit --docker-host-path-prefix (sysroot handles path visibility)")
		}
		if !strings.Contains(stepContent, `--mount "${GITHUB_WORKSPACE}:${GITHUB_WORKSPACE}:rw"`) {
			t.Error("Expected explicit workspace mount for arc-dind")
		}
		if !strings.Contains(stepContent, `--mount "${RUNNER_TEMP}/gh-aw:${RUNNER_TEMP}/gh-aw:ro"`) {
			t.Error("Expected read-only base mount for ${RUNNER_TEMP}/gh-aw")
		}
		if !strings.Contains(stepContent, `--mount "${RUNNER_TEMP}/gh-aw/home:${RUNNER_TEMP}/gh-aw/home:rw"`) {
			t.Error("Expected read-write home overlay mount for arc-dind")
		}
		if !strings.Contains(stepContent, `--mount "${RUNNER_TEMP}/gh-aw/sandbox/agent:${RUNNER_TEMP}/gh-aw/sandbox/agent:rw"`) {
			t.Error("Expected read-write sandbox/agent overlay mount for arc-dind")
		}
		if !strings.Contains(stepContent, `\"proxyLogsDir\":\"${RUNNER_TEMP}/gh-aw/sandbox/firewall/logs\"`) {
			t.Error("Expected proxyLogsDir in AWF config JSON to resolve under ${RUNNER_TEMP}/gh-aw")
		}
		if !strings.Contains(stepContent, `\"auditDir\":\"${RUNNER_TEMP}/gh-aw/sandbox/firewall/audit\"`) {
			t.Error("Expected auditDir in AWF config JSON to resolve under ${RUNNER_TEMP}/gh-aw")
		}
		if !strings.Contains(stepContent, "export HOME=${RUNNER_TEMP}/gh-aw/home") {
			t.Error("Expected command to export HOME under ${RUNNER_TEMP}/gh-aw/home for arc-dind")
		}
		if !strings.Contains(stepContent, "--prompt-file ${RUNNER_TEMP}/gh-aw/aw-prompts/prompt.txt") {
			t.Error("Expected prompt file path to be rewritten under ${RUNNER_TEMP}/gh-aw for arc-dind")
		}
		if !strings.Contains(stepContent, "--log-dir ${RUNNER_TEMP}/gh-aw/sandbox/agent/logs/") {
			t.Error("Expected copilot log-dir to be rewritten under ${RUNNER_TEMP}/gh-aw for arc-dind")
		}
		if !strings.Contains(stepContent, "--add-dir ${RUNNER_TEMP}/gh-aw/") {
			t.Error("Expected copilot --add-dir path to be rewritten under ${RUNNER_TEMP}/gh-aw for arc-dind")
		}
		if !strings.Contains(stepContent, `${RUNNER_TEMP}/gh-aw/bin/copilot`) {
			t.Error("Expected arc-dind command to use Copilot binary staged under ${RUNNER_TEMP}/gh-aw/bin/copilot")
		}
		if strings.Contains(stepContent, "/usr/local/bin/copilot") {
			t.Error("Expected arc-dind command not to reference /usr/local/bin/copilot")
		}

		homeExport := "export HOME=${RUNNER_TEMP}/gh-aw/home"
		firstHomeExportIdx := strings.Index(stepContent, homeExport)
		if firstHomeExportIdx < 0 {
			t.Fatalf("Expected path setup to export HOME for arc-dind:\n%s", stepContent)
		}
		if strings.Count(stepContent, homeExport) < 2 {
			t.Fatalf("Expected both path-setup and engine-command HOME exports for arc-dind:\n%s", stepContent)
		}

		settingsSetupIdx := strings.Index(stepContent, `mkdir -p "$HOME/.copilot"`)
		if settingsSetupIdx < 0 {
			t.Fatalf("Expected Copilot settings setup to be present:\n%s", stepContent)
		}
		xdgExportIdx := strings.Index(stepContent, `export XDG_CONFIG_HOME="$HOME"`)
		if xdgExportIdx < 0 {
			t.Fatalf("Expected XDG_CONFIG_HOME export to be present:\n%s", stepContent)
		}
		mcpConfigExportIdx := strings.Index(stepContent, `export GH_AW_MCP_CONFIG="$HOME/.copilot/mcp-config.json"`)
		if mcpConfigExportIdx < 0 {
			t.Fatalf("Expected GH_AW_MCP_CONFIG export when MCP is enabled:\n%s", stepContent)
		}

		if firstHomeExportIdx > settingsSetupIdx {
			t.Fatalf("Expected arc-dind HOME export to run before Copilot settings setup:\n%s", stepContent)
		}
		if firstHomeExportIdx > xdgExportIdx {
			t.Fatalf("Expected arc-dind HOME export to run before XDG_CONFIG_HOME export:\n%s", stepContent)
		}
		if firstHomeExportIdx > mcpConfigExportIdx {
			t.Fatalf("Expected arc-dind HOME export to run before GH_AW_MCP_CONFIG export:\n%s", stepContent)
		}
	})

	t.Run("AWF command includes allow-urls with ssl-bump enabled", func(t *testing.T) {
		workflowData := &WorkflowData{
			Name: "test-workflow",
			EngineConfig: &EngineConfig{
				ID: "copilot",
			},
			NetworkPermissions: &NetworkPermissions{
				Firewall: &FirewallConfig{
					Enabled:   true,
					SSLBump:   true,
					AllowURLs: []string{"https://github.com/githubnext/*", "https://api.github.com/repos/*"},
				},
			},
		}

		engine := NewCopilotEngine()
		steps := engine.GetExecutionSteps(workflowData, "test.log")

		stepContent := requireCopilotExecutionStep(t, steps)

		// Check that --ssl-bump flag is included
		if !strings.Contains(stepContent, "--ssl-bump") {
			t.Error("Expected AWF command to contain '--ssl-bump' flag")
		}

		// Check that --allow-urls is included with the comma-separated URLs
		if !strings.Contains(stepContent, "--allow-urls") {
			t.Error("Expected AWF command to contain '--allow-urls' flag")
		}

		if !strings.Contains(stepContent, "https://github.com/githubnext/*") {
			t.Error("Expected AWF command to contain URL pattern 'https://github.com/githubnext/*'")
		}
	})

	t.Run("AWF command does not include allow-urls without ssl-bump", func(t *testing.T) {
		workflowData := &WorkflowData{
			Name: "test-workflow",
			EngineConfig: &EngineConfig{
				ID: "copilot",
			},
			NetworkPermissions: &NetworkPermissions{
				Firewall: &FirewallConfig{
					Enabled:   true,
					SSLBump:   false, // SSL Bump disabled
					AllowURLs: []string{"https://github.com/githubnext/*"},
				},
			},
		}

		engine := NewCopilotEngine()
		steps := engine.GetExecutionSteps(workflowData, "test.log")

		stepContent := requireCopilotExecutionStep(t, steps)

		// Check that --ssl-bump flag is NOT included
		if strings.Contains(stepContent, "--ssl-bump") {
			t.Error("Expected AWF command to NOT contain '--ssl-bump' flag when SSLBump is false")
		}

		// Check that --allow-urls is NOT included when ssl-bump is disabled
		if strings.Contains(stepContent, "--allow-urls") {
			t.Error("Expected AWF command to NOT contain '--allow-urls' flag when SSLBump is false")
		}
	})
}
