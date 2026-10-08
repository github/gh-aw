package workflow

import (
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/github/gh-aw/pkg/parser"
)

// LockedTasksToolConfig preserves the authored configuration and its expanded tasks.
type LockedTasksToolConfig struct {
	Definitions map[string]parser.TaskDefinition
	Raw         any
}

func resolvedWorkflowTasks(data *WorkflowData) map[string]parser.TaskDefinition {
	if data == nil || data.IsDetectionRun || data.IsEvalsRun {
		return nil
	}
	value, exists := data.Tools["locked-tasks"]
	if !exists {
		return nil
	}
	tasks, err := parser.ResolveTasks(value)
	if err != nil {
		panic(fmt.Sprintf("BUG: invalid validated tasks: %v", err))
	}
	return tasks
}

func hasWorkflowTasks(data *WorkflowData) bool {
	return len(resolvedWorkflowTasks(data)) > 0
}

func validateTasks(data *WorkflowData) error {
	if data == nil {
		return nil
	}
	value, exists := data.Tools["locked-tasks"]
	if !exists {
		return nil
	}
	tasks, err := parser.ResolveTasks(value)
	if err != nil {
		return err
	}
	if len(tasks) == 0 {
		return nil
	}
	encoded, err := json.Marshal(tasks)
	if err != nil {
		return fmt.Errorf("tools.locked-tasks cannot be encoded: %w", err)
	}
	if len(encoded) > 1024*1024-64 {
		return errors.New("tools.locked-tasks expanded manifest must fit within 1 MiB")
	}
	engine := data.EngineConfig
	if engine == nil || (engine.ID != "pi" && (engine.ID != "copilot" || !engine.CopilotSDK)) {
		return errors.New("tools.locked-tasks requires engine: pi or the bundled Copilot SDK engine")
	}
	if engine.Command != "" || engine.InlineDriver != nil || engine.HarnessScript != "" || engine.Cwd != "" || len(engine.Args) > 0 || len(engine.Extensions) > 0 {
		return errors.New("tools.locked-tasks requires bundled engine execution without command, cwd, args, extensions or harness overrides")
	}
	if engine.Driver != "" && (engine.ID != "pi" || (engine.Driver != "pi_agent_core_driver.cjs" && engine.Driver != "pi_rpc_driver.cjs")) {
		return errors.New("tools.locked-tasks does not support custom engine drivers")
	}
	if !isFirewallEnabled(data) || isCloudHypervisorRuntime(data) || isArcDindTopology(data) {
		return errors.New("tools.locked-tasks requires the AWF Docker runtime on a standard runner; Cloud Hypervisor and ARC/DinD are not supported")
	}
	if data.IsDetectionRun || data.IsEvalsRun || data.UseSamples {
		return errors.New("tools.locked-tasks is only supported for the main agent execution")
	}
	if !isToolExplicitlyFalse(data.Tools["cli-proxy"]) {
		return errors.New("tools.locked-tasks requires explicit tools.cli-proxy: false for native MCP access")
	}
	if _, exists := data.ResolvedMCPServers["locked-tasks"]; exists {
		return errors.New("tools.locked-tasks reserves the MCP server name locked-tasks")
	}
	var overrides []string
	for name := range parseEnvYAMLSection(data.Env) {
		overrides = append(overrides, name)
	}
	for name := range engine.Env {
		overrides = append(overrides, name)
	}
	if data.CheckoutDisabled || data.CheckoutExplicitlyDisabled || data.CheckoutSkipDefault || len(data.CheckoutConfigs) > 1 {
		return errors.New("tools.locked-tasks requires one enabled checkout at the workspace root")
	}
	for _, checkout := range data.CheckoutConfigs {
		if checkout != nil && (checkout.Repository != "" || checkout.Wiki || (checkout.Path != "" && checkout.Path != ".")) {
			return errors.New("tools.locked-tasks requires a current-repository checkout at the workspace root")
		}
	}
	if agent := getAgentConfig(data); agent != nil {
		for name := range agent.Env {
			overrides = append(overrides, name)
		}
		if agent.Command != "" || len(agent.Args) > 0 || len(agent.Mounts) > 0 {
			return errors.New("tools.locked-tasks does not support custom sandbox commands, arguments or mounts")
		}
	}
	slices.Sort(overrides)
	for _, name := range overrides {
		if strings.HasPrefix(name, "GH_AW_LOCKED_TASKS_") || name == "RUNNER_TEMP" || name == "GITHUB_WORKSPACE" || name == "GH_AW_ENGINE_CWD" || name == "GH_AW_MCP_CONFIG" {
			return fmt.Errorf("tools.locked-tasks reserves runtime environment variable %s", name)
		}
	}
	return nil
}

func buildTasksAWFSetup(data *WorkflowData) string {
	tasks := resolvedWorkflowTasks(data)
	if len(tasks) == 0 {
		return ""
	}
	manifest, err := json.Marshal(struct {
		Version int                              `json:"version"`
		Tasks   map[string]parser.TaskDefinition `json:"tasks"`
	}{Version: 1, Tasks: tasks})
	if err != nil {
		panic(fmt.Sprintf("BUG: cannot marshal tasks manifest: %v", err))
	}
	setup := "mkdir -p \"${RUNNER_TEMP}/gh-aw/locked-tasks\"\n" +
		"printf '%s' " + shellEscapeArg(string(manifest)) + " > \"${RUNNER_TEMP}/gh-aw/locked-tasks/manifest.json\"\n"
	if data.EngineConfig.CopilotSDK && HasMCPServers(data) {
		setup += "cp \"$HOME/.copilot/mcp-config.json\" \"${RUNNER_TEMP}/gh-aw/locked-tasks/copilot-mcp.json\"\n" +
			"chmod 600 \"${RUNNER_TEMP}/gh-aw/locked-tasks/copilot-mcp.json\"\n"
	}
	return setup
}

func wrapTasksEngineCommand(data *WorkflowData, command string) string {
	if !hasWorkflowTasks(data) {
		return command
	}
	// The wrapper is inside AWF; only the compiler-owned engine command reaches bash.
	return nodeRuntimeResolutionCommand + ` "` + SetupActionDestinationShell + `/tasks_runtime.cjs" --manifest "${RUNNER_TEMP}/gh-aw/locked-tasks/manifest.json" -- /bin/bash -e -o pipefail -c ` + shellEscapeArg(command)
}

func tasksSDKPermission(data *WorkflowData) []string {
	if hasWorkflowTasks(data) {
		return []string{"--allow-tool", "locked-tasks(run_task)"}
	}
	return nil
}
