package workflow

import (
	"errors"
	"fmt"
	"maps"
	"regexp"
	"slices"
	"strings"

	"github.com/github/gh-aw/pkg/constants"
)

const copilotGoRepositoryToolProfile = "go-repository"
const copilotGoRepositoryToolName = "go_repository"

var goRepositoryLiteralRepoPattern = regexp.MustCompile(`^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$`)

func engineToolProfile(data *WorkflowData) string {
	if data == nil || data.EngineConfig == nil {
		return ""
	}
	return data.EngineConfig.ToolProfile
}

func validateCopilotToolProfile(data *WorkflowData) error {
	if data == nil {
		return nil
	}
	if err := validateSecondaryEngineToolProfiles(data); err != nil {
		return err
	}
	profile := engineToolProfile(data)
	if profile == "" {
		return nil
	}
	if profile != copilotGoRepositoryToolProfile {
		return fmt.Errorf("unsupported engine.tool-profile %q; supported value: go-repository", profile)
	}
	if data.EngineConfig.ID != "copilot" || (data.AI != "" && data.AI != "copilot") || !isCopilotSDKMode(data) {
		return errors.New("engine.tool-profile: go-repository requires the effective engine to be copilot with copilot-sdk: true")
	}
	if data.IsDetectionRun || data.IsEvalsRun || data.UseSamples {
		return errors.New("engine.tool-profile: go-repository is only supported for the main agent execution")
	}
	if err := validateGoRepositoryExecution(data); err != nil {
		return err
	}
	if err := validateGoRepositoryCheckout(data); err != nil {
		return err
	}
	// Extraction enables omitted noop by default, so nil means it is unavailable.
	if data.SafeOutputs.NoOp == nil {
		return errors.New("engine.tool-profile: go-repository requires safe-outputs.noop for native preflight; noop: false is not supported")
	}
	if err := validateGoRepositoryTools(data); err != nil {
		return err
	}
	for _, exclusion := range data.SafeOutputs.CreatePullRequests.ProtectedFilesExclude {
		if strings.Contains(exclusion, "${") {
			return errors.New("engine.tool-profile: go-repository requires literal protected-files.exclude entries because protection exclusions are normalized at compile time")
		}
	}
	policy, err := projectGoRepositoryPolicy(data)
	if err != nil {
		return err
	}
	if err := validateGoRepositoryPolicyExpressions(policy); err != nil {
		return err
	}
	toolArgs := NewCopilotEngine().computeCopilotToolArguments(data.Tools, data.SafeOutputs, data.MCPScripts, data)
	_, bindings, err := buildGoRepositoryToolConfigRuntimeData(buildCopilotSDKToolConfigJSON(data, toolArgs))
	if err != nil {
		return err
	}
	excluded := ComputeAWFExcludeEnvVarNames(data, nil)
	for _, name := range append(slices.Sorted(maps.Keys(bindings)), constants.CopilotSDKToolConfigEnvVar) {
		if slices.Contains(excluded, name) {
			return fmt.Errorf("engine.tool-profile: go-repository requires runtime binding %s; it must not be excluded from the agent environment", name)
		}
	}
	return nil
}

func validateSecondaryEngineToolProfiles(data *WorkflowData) error {
	if data.SafeOutputs != nil && data.SafeOutputs.ThreatDetection != nil {
		engine := data.SafeOutputs.ThreatDetection.EngineConfig
		if engine != nil && engine.ToolProfile != "" {
			return errors.New("safe-outputs.threat-detection.engine.tool-profile is not supported; tool profiles are main-agent only")
		}
	}
	evals, _ := data.RawFrontmatter["evals"].(map[string]any)
	outputs, _ := data.RawFrontmatter["safe-outputs"].(map[string]any)
	detection, _ := outputs["threat-detection"].(map[string]any)
	for _, nested := range []struct {
		name   string
		config map[string]any
	}{{"evals", evals}, {"safe-outputs.threat-detection", detection}} {
		engine, _ := nested.config["engine"].(map[string]any)
		if _, exists := engine["tool-profile"]; exists {
			return fmt.Errorf("%s.engine.tool-profile is not supported; tool profiles are main-agent only", nested.name)
		}
	}
	return nil
}

func validateGoRepositoryExecution(data *WorkflowData) error {
	engine := data.EngineConfig
	if engine.Command != "" || engine.Driver != "" || engine.InlineDriver != nil || engine.HarnessScript != "" || engine.IsInlineDefinition {
		return errors.New("engine.tool-profile: go-repository requires the bundled Copilot command, SDK driver, and harness; harness retry policy settings are supported")
	}
	if engine.Cwd != "" || len(engine.Args) != 0 || engine.Agent != "" || len(engine.Extensions) != 0 {
		return errors.New("engine.tool-profile: go-repository does not support engine.cwd, args, agent, or extensions overrides")
	}
	if !isFirewallEnabled(data) {
		return errors.New("engine.tool-profile: go-repository requires sandbox.agent: awf")
	}
	if agent := getAgentConfig(data); agent != nil {
		if agent.Command != "" || len(agent.Args) != 0 || (agent.ID != "" && agent.ID != "awf") ||
			(agent.Type != "" && agent.Type != SandboxTypeAWF && agent.Type != SandboxTypeDefault) {
			return errors.New("engine.tool-profile: go-repository requires the standard AWF execution path")
		}
	}
	return validateGoRepositoryEnvironment(data)
}

func validateGoRepositoryCheckout(data *WorkflowData) error {
	if data.SafeOutputs == nil || data.SafeOutputs.CreatePullRequests == nil {
		return errors.New("engine.tool-profile: go-repository requires safe-outputs.create-pull-request")
	}
	pr := data.SafeOutputs.CreatePullRequests
	if !isGoRepositorySinglePRTarget(pr) {
		return errors.New("engine.tool-profile: go-repository requires one current-repository create-pull-request target: target-repo must be omitted, ${{ github.repository }}, or a literal owner/repo; allowed-repos must be empty or contain exactly the matching repository")
	}
	if pr.HeadRepoSlug != "" || data.SafeOutputs.PushToPullRequestBranch != nil || data.TrialLogicalRepo != "" {
		return errors.New("engine.tool-profile: go-repository requires one current-repository create-pull-request target; forks, push-to-pull-request-branch, and trial repository overrides are not supported")
	}
	if data.CheckoutDisabled || data.CheckoutExplicitlyDisabled || data.CheckoutSkipDefault || len(data.CheckoutConfigs) > 1 {
		return errors.New("engine.tool-profile: go-repository requires one enabled current-repository checkout at the workspace root")
	}
	for _, checkout := range data.CheckoutConfigs {
		if checkout == nil || checkout.Repository != "" || checkout.Wiki ||
			(checkout.Path != "" && checkout.Path != ".") || checkout.SparseCheckout != "" ||
			(checkout.Submodules != "" && checkout.Submodules != "false") {
			return errors.New("engine.tool-profile: go-repository requires a full current-repository checkout at the workspace root; alternate paths, wiki, sparse checkout, and submodules are not supported")
		}
	}
	return validateGoRepositoryCheckoutSteps(data)
}

func isGoRepositoryCurrentRepo(repository string) bool {
	return repository == "" || repository == "${{ github.repository }}"
}

// Literal identity is checked against GITHUB_REPOSITORY by the runtime before
// session creation. Compilation checks scope without rewriting publication policy.
func isGoRepositorySinglePRTarget(pr *CreatePullRequestsConfig) bool {
	if isGoRepositoryCurrentRepo(pr.TargetRepoSlug) {
		return len(pr.AllowedRepos) == 0 ||
			(len(pr.AllowedRepos) == 1 && pr.AllowedRepos[0] == "${{ github.repository }}")
	}
	if !goRepositoryLiteralRepoPattern.MatchString(pr.TargetRepoSlug) {
		return false
	}
	owner, repository, _ := strings.Cut(pr.TargetRepoSlug, "/")
	if owner == "." || owner == ".." || repository == "." || repository == ".." {
		return false
	}
	return len(pr.AllowedRepos) == 0 ||
		(len(pr.AllowedRepos) == 1 && goRepositoryLiteralRepoPattern.MatchString(pr.AllowedRepos[0]) &&
			strings.EqualFold(pr.TargetRepoSlug, pr.AllowedRepos[0]))
}

func validateGoRepositoryTools(data *WorkflowData) error {
	if !isBashExplicitlyRefused(data.Tools) {
		_, hasBash := data.Tools["bash"]
		_, disabled := data.ExplicitlyDisabledTools["bash"]
		if hasBash || !disabled {
			return errors.New("engine.tool-profile: go-repository requires explicit tools.bash: false or []")
		}
	}
	if !isToolExplicitlyFalse(data.Tools["cli-proxy"]) || (data.ParsedTools != nil && data.ParsedTools.CLIProxy) {
		return errors.New("engine.tool-profile: go-repository requires explicit tools.cli-proxy: false")
	}
	if _, enabled := IsGitHubCLIProxyMode(data.Tools); enabled {
		return errors.New("engine.tool-profile: go-repository requires an MCP-backed GitHub mode, not gh-proxy or cli")
	}
	if !isCopilotEditToolEnabled(data.Tools, data) {
		return errors.New("engine.tool-profile: go-repository requires editing to be enabled")
	}
	if _, exists := data.Tools[copilotGoRepositoryToolName]; exists {
		return errors.New("engine.tool-profile: go-repository reserves the MCP server name go_repository")
	}
	if _, exists := data.ResolvedMCPServers[copilotGoRepositoryToolName]; exists {
		return errors.New("engine.tool-profile: go-repository reserves the MCP server name go_repository")
	}
	args := NewCopilotEngine().computeCopilotToolArguments(data.Tools, data.SafeOutputs, data.MCPScripts, data)
	for _, permission := range extractCopilotAllowedTools(args) {
		if permission == "*" || permission == "shell" || strings.HasPrefix(permission, "shell(") {
			return errors.New("engine.tool-profile: go-repository cannot grant shell or unrestricted tool permissions")
		}
		if strings.Contains(permission, "${") {
			return errors.New("engine.tool-profile: go-repository requires static SDK tool permission names")
		}
	}
	for name := range data.ExplicitlyDisabledTools {
		if strings.Contains(name, "${") {
			return errors.New("engine.tool-profile: go-repository requires static tool names")
		}
	}
	return nil
}
