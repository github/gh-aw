package workflow

import (
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/logger"
)

var programLog = logger.New("workflow:program")

// programPathPattern accepts a relative path of plain segments below the workflow
// directory. Segments cannot start with "." so ".." and hidden directories are rejected.
var programPathPattern = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9_.-]*(?:/[A-Za-z0-9_][A-Za-z0-9_.-]*)*\.(?:ts|mts|cts|js|mjs|cjs)$`)

const maxProgramPathLength = 256

// programAgentOnlyKeys are top-level frontmatter keys that configure the agent,
// its sandbox or its model. A program has none of these, so they are rejected
// instead of being silently ignored.
var programAgentOnlyKeys = []string{
	"adaptive-routing", "ambient-folders", "connectors", "enclaves", "evals", "experiments",
	"graders", "imports", "inlined-imports", "lsp", "max-ai-credits", "max-daily-ai-credits",
	"max-runs", "max-tool-calls", "max-tool-denials", "max-turn-cache-misses", "max-turns",
	"mcp-scripts", "mcp-servers", "model", "model-routing", "models", "network", "plugins",
	"pre-agent-steps", "sandbox", "skills", "skip-if-decision", "token-broker",
}

// extractProgram returns the trimmed `program:` frontmatter value, or "" when absent.
func extractProgram(frontmatter map[string]any) string {
	value, _ := frontmatter["program"].(string)
	return strings.TrimSpace(value)
}

// validateProgramConfiguration enforces the `engine: none` + `program:` contract:
// each requires the other, the program is a TypeScript or JavaScript file below the
// workflow directory, and agent-only configuration is rejected.
func validateProgramConfiguration(frontmatter map[string]any, engineSetting, markdownDir string) error {
	raw, declared := frontmatter["program"]
	isNone := isNoneEngineID(engineSetting)
	if !declared && !isNone {
		return nil
	}
	programLog.Printf("Validating program configuration: declared=%v engine=%q", declared, engineSetting)
	if !isNone {
		return errors.New("program: requires engine: none. A program replaces the agent; remove program: to run an agentic engine.\n\nExample:\nengine: none\nprogram: dispatcher.ts")
	}
	if _, ok := frontmatter["engine"].(string); !ok {
		return errors.New("engine: none must be the string form; engine objects configure an agentic engine.\n\nExample:\nengine: none\nprogram: dispatcher.ts")
	}
	if !declared {
		return errors.New("engine: none requires program: naming the deterministic program to run.\n\nExample:\nengine: none\nprogram: dispatcher.ts")
	}
	program, ok := raw.(string)
	if !ok || len(program) > maxProgramPathLength || !programPathPattern.MatchString(program) {
		return fmt.Errorf("program: %v is invalid. Expected a relative path of at most %d bytes below the workflow directory, with plain segments (no '..' or hidden directories), ending in .ts, .mts, .cts, .js, .mjs or .cjs.\n\nExample:\nprogram: programs/dispatcher.ts", raw, maxProgramPathLength)
	}
	if markdownDir != "" {
		info, err := os.Stat(filepath.Join(markdownDir, filepath.FromSlash(program)))
		if err != nil || !info.Mode().IsRegular() {
			return fmt.Errorf("program: %s was not found next to the workflow. Create %s as a regular file relative to the workflow's directory", program, filepath.ToSlash(filepath.Join(filepath.Base(markdownDir), program)))
		}
	}
	var rejected []string
	for _, key := range programAgentOnlyKeys {
		if _, present := frontmatter[key]; present {
			rejected = append(rejected, key)
		}
	}
	if tools, ok := frontmatter["tools"].(map[string]any); ok {
		for name := range tools {
			if name != "work-queue" {
				rejected = append(rejected, "tools."+name)
			}
		}
	} else if _, present := frontmatter["tools"]; present {
		rejected = append(rejected, "tools")
	}
	if len(rejected) > 0 {
		slices.Sort(rejected)
		return fmt.Errorf("engine: %s does not support agent configuration: %s. A program runs without an agent, sandbox, MCP gateway or model; remove these fields (tools accepts only work-queue)", constants.NoneEngine, strings.Join(rejected, ", "))
	}
	if safeOutputs, ok := frontmatter["safe-outputs"].(map[string]any); ok {
		if detection, present := safeOutputs["threat-detection"]; present && detection != false {
			return errors.New("engine: none does not support safe-outputs.threat-detection: a program's outputs are not model-generated. Remove threat-detection or set it to false")
		}
	}
	return nil
}

// isProgramPublisher reports whether a program workflow commits queue controls itself.
// Programs with tools.work-queue publish as the dispatcher role from the program job
// (submit always; dispatch_next with approved dispatch-workflow workers); worker programs
// keep the agent-equivalent path and hand their finish intent to the safe_outputs job.
func isProgramPublisher(data *WorkflowData) bool {
	return data != nil && data.Program != "" && isWorkQueueEnabled(data) && !workQueueRequiresAssignment(data)
}

// programRepositoryPath returns the program's path relative to the repository root:
// the program is resolved next to the workflow markdown, normally .github/workflows/.
func (c *Compiler) programRepositoryPath(data *WorkflowData) string {
	normalized := filepath.ToSlash(c.markdownPath)
	dir := ".github/workflows"
	if index := strings.LastIndex(normalized, "/.github/"); index != -1 {
		dir = path.Dir(normalized[index+1:])
	} else if strings.HasPrefix(normalized, constants.GithubDir) {
		dir = path.Dir(normalized)
	}
	return path.Join(dir, data.Program)
}

// generateProgramRunSteps replaces engine installation, MCP setup and agent execution
// for engine: none. It writes the safe-outputs configuration the collector validates
// against, runs the program through the SDK runner, then collects queue intents,
// redacts secrets and ingests safe outputs exactly like the agent job.
func (c *Compiler) generateProgramRunSteps(yaml *strings.Builder, data *WorkflowData) ([]string, string, error) {
	programLog.Printf("Generating program run steps: program=%s", data.Program)
	populateDispatchWorkflowFiles(data, c.markdownPath)
	// Publishers commit queue controls directly and cannot emit safe outputs, so they
	// need neither the safe-outputs configuration nor intent collection and ingestion.
	collectsOutputs := data.SafeOutputs != nil && !isProgramPublisher(data)
	if collectsOutputs {
		safeOutputConfig, err := generateSafeOutputsConfigIfEnabled(data)
		if err != nil {
			return nil, "", fmt.Errorf("safe outputs setup preparation failed: %w", err)
		}
		generateSafeOutputsSetup(c, yaml, safeOutputConfig, data)
	}
	step, err := c.buildProgramStep(data)
	if err != nil {
		return nil, "", err
	}
	for _, line := range step {
		yaml.WriteString(line)
	}
	c.stepOrderTracker.MarkAgentExecutionComplete()
	if !isProgramPublisher(data) {
		c.generateCollectWorkQueueIntentsStep(yaml, data)
	}
	c.generateTrackedSecretRedactionStep(yaml, yaml.String(), data, true)
	if collectsOutputs {
		if err := c.generateOutputCollectionStep(yaml, data, false); err != nil {
			return nil, "", err
		}
	}
	return nil, constants.AgentStdioLogPath, nil
}

// buildProgramStep runs the program with actions/github-script so it receives the
// authenticated Octokit client, core, exec and io. Publisher programs also receive
// the queue publisher token and protected dispatch configuration of the controls step.
func (c *Compiler) buildProgramStep(data *WorkflowData) ([]string, error) {
	steps := c.programPublisherTokenSteps(data)
	steps = append(steps,
		"      - name: Run program\n",
		"        id: program\n",
		fmt.Sprintf("        uses: %s\n", c.getActionPin("actions/github-script")),
	)
	var env []string
	if isProgramPublisher(data) {
		controls, configLine, err := c.workQueueControlEnvironment(data)
		if err != nil {
			return nil, err
		}
		env = append(controls, configLine)
	} else if isWorkQueueEnabled(data) {
		env = workQueuePolicyEnvironment(data)
	}
	if len(env) == 0 || env[0] != "        env:\n" {
		env = append([]string{"        env:\n"}, env...)
	}
	env = append(env, fmt.Sprintf("          GH_AW_PROGRAM: %q\n", c.programRepositoryPath(data)))
	steps = append(steps, env...)
	steps = append(steps, "        with:\n")
	tokenSource := ""
	if isProgramPublisher(data) {
		c.addSafeOutputGitHubTokenForConfig(&steps, data, "")
		tokenSource = ", process.env.GH_AW_GITHUB_TOKEN_SOURCE"
	}
	return append(steps,
		"          script: |\n",
		"            const { setupGlobals } = require('"+SetupActionDestination+"/setup_globals.cjs');\n",
		"            setupGlobals(core, github, context, exec, io, getOctokit"+tokenSource+");\n",
		"            const { main } = require('"+SetupActionDestination+"/run_program.cjs');\n",
		"            await main({ core, github, context, exec, io, getOctokit });\n",
	), nil
}

// programPublisherPermissions returns the permissions a publisher program's credentials
// need: contents write for the queue branch, actions write for worker launches, and
// issues write when the Issue projection runs after committed controls.
func programPublisherPermissions(data *WorkflowData) *Permissions {
	permissions := NewPermissions()
	permissions.Set(PermissionContents, PermissionWrite)
	permissions.Set(PermissionActions, PermissionWrite)
	if workQueueIssuesConfig(data) != nil {
		permissions.Set(PermissionIssues, PermissionWrite)
	}
	return permissions
}

// programPublisherTokenSteps mints the safe-outputs credential inside the program job
// when it comes from safe-outputs.github-app or the token broker; the queue publisher
// token resolves to steps.safe-outputs-app-token exactly as in the safe_outputs job.
func (c *Compiler) programPublisherTokenSteps(data *WorkflowData) []string {
	if !isProgramPublisher(data) || data.SafeOutputs == nil {
		return nil
	}
	if usesTokenBrokerSafeOutputs(data) {
		return c.buildGitHubBrokerTokenStep(data, "safe-outputs-app-token")
	}
	if data.SafeOutputs.GitHubApp != nil {
		return c.buildGitHubAppTokenMintStepForRepository(string(constants.AgentJobName), data.SafeOutputs.GitHubApp, programPublisherPermissions(data), "", "")
	}
	return nil
}

// applyProgramPublisherPermissions grants the program job's GITHUB_TOKEN the publisher
// permissions, plus id-token write when the token broker mints the credential.
func applyProgramPublisherPermissions(data *WorkflowData, permissions string) string {
	if !isProgramPublisher(data) {
		return permissions
	}
	perms := NewPermissionsParser(permissions).ToPermissions()
	perms.Merge(programPublisherPermissions(data))
	if usesTokenBrokerSafeOutputs(data) {
		perms.Set(PermissionIdToken, PermissionWrite)
	}
	return filterJobLevelPermissions(perms.RenderToYAML())
}

// programOrDefaultSandbox leaves programs without a sandbox configuration: no agent
// runs, so AWF, its firewall and their log steps have nothing to contain. This is not
// the sandbox.agent: false opt-out, which disables protection for a running agent.
// Other workflows keep the AWF default.
func programOrDefaultSandbox(frontmatter map[string]any, engineSetup *engineSetupResult) *SandboxConfig {
	if extractProgram(frontmatter) != "" && isNoneEngineID(engineSetup.engineSetting) {
		return nil
	}
	return applySandboxDefaults(engineSetup.sandboxConfig, engineSetup.engineConfig)
}
