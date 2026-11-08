package workflow

import (
	"encoding/json"
	"errors"
	"fmt"
	"path"
	"slices"
	"strconv"
	"strings"

	"github.com/github/gh-aw/pkg/constants"
)

func durableSessionDirectory(engine string) string {
	return path.Join(constants.TmpGhAwAgentDir, "durable-sessions", engine)
}

func sessionDurableFiles(engine string) []string {
	root := durableSessionDirectory(engine)
	var files []string
	switch engine {
	case "claude":
		files = append(files, path.Join(root, "session.json"), path.Join(root, "projects"), path.Join(root, "tasks"), path.Join(root, "todos"))
	case "codex":
		files = append(files, path.Join(root, "session.json"), path.Join(root, "sessions"), path.Join(root, "archived_sessions"))
	case "copilot":
		files = append(files, path.Join(root, "session.json"), path.Join(root, "session-state"))
	case "gemini":
		files = append(files, path.Join(root, "home/.gemini/tmp"), path.Join(root, "home/.gemini/projects.json"))
	case "agy":
		files = append(files, path.Join(root, "home/.gemini/antigravity-cli/conversations"))
	case "pi":
		files = append(files, path.Join(root, "session.jsonl"))
	}
	return files
}

func sessionDurableExecutionPlan(engine CodingAgentEngine, data *WorkflowData, logFile string) (*DurableExecutionPlan, error) {
	config := data.EngineConfig
	if config == nil || config.Durable == nil {
		return nil, fmt.Errorf("engine %q durable execution requires engine.durable", engine.GetID())
	}
	builtinPiDriver := engine.GetID() == "pi" && (config.Driver == "pi_agent_core_driver.cjs" || config.Driver == "pi_rpc_driver.cjs")
	if config.Command != "" || (config.Driver != "" && !builtinPiDriver) || config.HarnessScript != "" {
		return nil, fmt.Errorf("engine %q durable execution does not support custom commands, drivers or harnesses", engine.GetID())
	}
	if err := validateDurableSessionArgs(engine.GetID(), config.Args); err != nil {
		return nil, err
	}
	if engine.GetID() == "copilot" {
		agent := getAgentConfig(data)
		if config.Env["COPILOT_HOME"] != "" || (agent != nil && agent.Env["COPILOT_HOME"] != "") {
			return nil, errors.New("copilot durable execution cannot override COPILOT_HOME because it collects the native session directory")
		}
	}
	steps := engine.GetExecutionSteps(data, logFile)
	if engine.GetID() == "copilot" {
		steps = append(steps, durableSessionStep(data, "capture-copilot"))
	}
	return &DurableExecutionPlan{
		Files:          sessionDurableFiles(engine.GetID()),
		ExecutionSteps: steps,
	}, nil
}

func validateDurableSessionArgs(engine string, args []string) error {
	var selectors []string
	switch engine {
	case "claude":
		selectors = []string{"--resume", "-r", "--continue", "-c", "--session-id", "--fork-session", "--no-session-persistence"}
	case "copilot":
		selectors = []string{"--resume", "-r", "--continue", "--session-id"}
	case "codex":
		selectors = []string{"resume", "fork", "--last", "--ephemeral"}
	case "pi":
		selectors = []string{"--session", "--session-dir", "--session-id", "--resume", "-r", "--continue", "-c", "--fork", "--no-session"}
	}
	for _, arg := range args {
		flag, _, _ := strings.Cut(arg, "=")
		if slices.Contains(selectors, flag) {
			return fmt.Errorf("engine %q durable execution owns session selection and persistence; remove %q from engine.args", engine, arg)
		}
	}
	return nil
}

func durableSessionStep(data *WorkflowData, operation string) GitHubActionStep {
	files, err := json.Marshal(sessionDurableFiles(data.EngineConfig.ID))
	if err != nil {
		panic(fmt.Sprintf("BUG: cannot encode durable session files: %v", err))
	}
	name := "Prepare durable continuation prompt"
	condition := "success()"
	switch operation {
	case "capture-copilot":
		name = "Collect Copilot durable session files"
		condition = "always() && steps.agentic_execution.outcome != '' && steps.agentic_execution.outcome != 'skipped'"
	case "trim-copilot":
		name = "Scope Copilot session logs to current run"
		condition = "always() && steps.agentic_execution.outcome != '' && steps.agentic_execution.outcome != 'skipped'"
	}
	step := GitHubActionStep{
		"      - name: " + name,
		"        if: " + condition,
		`        run: node "${RUNNER_TEMP}/gh-aw/actions/durable_session.cjs" ` + operation,
		"        env:",
		"          GH_AW_DURABLE_ENGINE: " + data.EngineConfig.ID,
		"          GH_AW_DURABLE_FILES: " + strconv.Quote(string(files)),
		"          GH_AW_DURABLE_SESSION_DIR: " + durableSessionDirectory(data.EngineConfig.ID),
		"          GH_AW_DURABLE_RESUMED: ${{ steps.durable_checkpoint.outputs.restored == 'true' && 'true' || 'false' }}",
	}
	if operation == "capture-copilot" {
		step = append(step, "        id: capture_durable_sessions")
	}
	return step
}

func (e *ClaudeEngine) GetDurableExecutionPlan(data *WorkflowData, logFile string) (*DurableExecutionPlan, error) {
	return sessionDurableExecutionPlan(e, data, logFile)
}

func (e *CodexEngine) GetDurableExecutionPlan(data *WorkflowData, logFile string) (*DurableExecutionPlan, error) {
	return sessionDurableExecutionPlan(e, data, logFile)
}

func (e *CopilotEngine) GetDurableExecutionPlan(data *WorkflowData, logFile string) (*DurableExecutionPlan, error) {
	return sessionDurableExecutionPlan(e, data, logFile)
}

func (e *GeminiEngine) GetDurableExecutionPlan(data *WorkflowData, logFile string) (*DurableExecutionPlan, error) {
	return sessionDurableExecutionPlan(e, data, logFile)
}

func (e *AgyEngine) GetDurableExecutionPlan(data *WorkflowData, logFile string) (*DurableExecutionPlan, error) {
	return sessionDurableExecutionPlan(e, data, logFile)
}

func (e *PiEngine) GetDurableExecutionPlan(data *WorkflowData, logFile string) (*DurableExecutionPlan, error) {
	if piSessionSettings(data).Enabled {
		return nil, errors.New("pi durable execution owns session selection; remove engine.config.session")
	}
	return sessionDurableExecutionPlan(e, data, logFile)
}

func applyDurableSessionEnv(env map[string]string, data *WorkflowData) {
	engine := data.EngineConfig.ID
	root := durableSessionDirectory(engine)
	env["GH_AW_DURABLE_ENGINE"] = engine
	env["GH_AW_DURABLE_SESSION_DIR"] = root
	switch engine {
	case "claude":
		env["CLAUDE_CONFIG_DIR"] = root
	case "codex":
		env["CODEX_HOME"] = root
	case "gemini":
		env["GEMINI_CLI_HOME"] = path.Join(root, "home")
	}
}
