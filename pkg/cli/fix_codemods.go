package cli

import (
	"fmt"
	"slices"
	"strings"

	"github.com/github/gh-aw/pkg/logger"
)

var fixCodemodsLog = logger.New("cli:fix_codemods")

// Codemod represents a single code transformation that can be applied to workflow files
type Codemod struct {
	ID           string // Unique identifier for the codemod
	Name         string // Human-readable name
	Description  string // Description of what the codemod does
	IntroducedIn string // Version where this codemod was introduced
	Guided       bool   // If true, errors from Apply are guided/manual-fix errors (not auto-correctable)
	Apply        func(content string, frontmatter map[string]any) (string, bool, error)
	// ApplyWithContext is an optional extension of Apply that also receives the absolute path of the
	// workflow file being processed. Codemods that need to resolve imported tools or included files
	// to derive the effective configuration should set this field; fix_command.go will call it in
	// preference to Apply when a file path is available. When ApplyWithContext is nil, Apply is
	// used as the sole handler.
	ApplyWithContext func(content string, frontmatter map[string]any, filePath string) (string, bool, error)
}

// GuidedError is returned when a codemod with Guided: true emits an error.
// Unlike regular processing errors, a guided error signals that the file was
// read successfully but requires a human to manually address an issue that
// cannot be auto-corrected by any codemod.
type GuidedError struct {
	Cause error
}

func (e *GuidedError) Error() string {
	return e.Cause.Error()
}

func (e *GuidedError) Unwrap() error {
	return e.Cause
}

// CodemodResult represents the result of applying a codemod
type CodemodResult struct {
	Applied bool   // Whether the codemod was applied
	Message string // Description of what changed
}

// GetAllCodemods returns all available codemods in the registry
func GetAllCodemods() []Codemod {
	codemods := append(getEarlyCodemods(), getLaterCodemods()...)
	fixCodemodsLog.Printf("Loaded codemod registry: %d codemods available", len(codemods))
	return codemods
}

// getEarlyCodemods returns the first half of the codemod registry, in application order.
func getEarlyCodemods() []Codemod {
	return []Codemod{
		getTimeoutMinutesCodemod(),
		getNetworkFirewallCodemod(),
		getCommandToSlashCommandCodemod(),
		getWorkflowDispatchRequiredFalseCodemod(), // Set required: false for slash/label command triggers
		getMCPScriptsModeCodemod(),
		getUploadAssetsCodemod(),
		getMigrateWritePermissionsToReadCodemod(),
		getExpandPermissionsShorthandCodemod(), // Fix permissions: read -> permissions: read-all
		getAgentTaskToAgentSessionCodemod(),
		getSandboxFalseToAgentFalseCodemod(), // Convert sandbox: false to sandbox.agent: false
		getScheduleAtToAroundCodemod(),
		getDeleteSchemaFileCodemod(),
		getGrepToolRemovalCodemod(),
		getMCPNetworkMigrationCodemod(),
		getDiscussionFlagRemovalCodemod(),
		getDiscussionTriggerCategoriesLowercaseCodemod(),
		getMCPModeToTypeCodemod(),
		getInstallScriptURLCodemod(),
		getBashAnonymousRemovalCodemod(),              // Replace bash: with bash: false
		getBashSingleQuotedArgsCodemod(),              // Rewrite single-quoted bash args to double-quoted form
		getBashAllowlistUnsupportedEngineCodemod(),    // Detect restricted tools.bash on engines that ignore it and emit guided error
		getActivationOutputsCodemod(),                 // Transform needs.activation.outputs.* to steps.sanitized.outputs.*
		getRolesToOnRolesCodemod(),                    // Move top-level roles to on.roles
		getBotsToOnBotsCodemod(),                      // Move top-level bots to on.bots
		getEngineStepsToTopLevelCodemod(),             // Move engine.steps to top-level steps
		getEngineMaxRunsToTopLevelCodemod(),           // Move engine.max-runs to top-level max-turns
		getMaxRunsToMaxTurnsCodemod(),                 // Rename top-level max-runs to max-turns
		getEngineMaxTurnsToTopLevelCodemod(),          // Move engine.max-turns to top-level max-turns
		getStepsRunSecretsToEnvCodemod(),              // Move all ${{ ... }} expressions in step run fields to step env bindings
		getEngineEnvSecretsCodemod(),                  // Remove unsafe secret-bearing engine.env entries
		getTopLevelEnvSecretsGuidedErrorCodemod(),     // Detect secrets in top-level env: and emit guided error
		getAssignToAgentDefaultAgentCodemod(),         // Rename deprecated default-agent to name in assign-to-agent
		getPlaywrightDomainsToNetworkAllowedCodemod(), // Migrate tools.playwright.allowed_domains to network.allowed
		getPlaywrightCLIModeRemovalCodemod(),          // Remove redundant tools.playwright.mode: cli (now the default)
		getExpiresIntegerToDayStringCodemod(),         // Convert expires integer (days) to string with 'd' suffix
		getGitHubAppCodemod(),                         // Rename deprecated 'app' to 'github-app'
		getGitHubAppClientIDCodemod(),                 // Rename deprecated github-app.app-id to github-app.client-id
	}
}

// getLaterCodemods returns the second half of the codemod registry, in application order.
func getLaterCodemods() []Codemod {
	return []Codemod{
		getSafeOutputRequireTitlePrefixCodemod(),                   // Rename deprecated safe-outputs title-prefix constraint fields
		getSafeOutputMergePRConstraintsCodemod(),                   // Rename deprecated merge-pull-request allowed-labels/allowed-branches
		getSafeOutputAddReviewerAllowlistsCodemod(),                // Rename deprecated add-reviewer reviewers/team-reviewers
		getSafeOutputDispatchRepositoryKeyCodemod(),                // Rename deprecated safe-outputs.dispatch_repository key
		getSafeJobRunnerCodemod(),                                  // Rename deprecated safe-outputs.jobs runner fields
		getSafeInputsToMCPScriptsCodemod(),                         // Rename safe-inputs to mcp-scripts
		getRateLimitToUserRateLimitCodemod(),                       // Rename rate-limit to user-rate-limit with max key migration
		getEffectiveTokensToAICreditsCodemod(),                     // Migrate obsolete effective-token budget keys to AI credits keys
		getMessagesEffectiveTokensSuffixToAICreditsSuffixCodemod(), // Migrate safe-outputs.messages ET suffix placeholders to AI credits suffix placeholders
		getSerenaMCPContainerLocationCodemod(),                     // Update legacy Serena MCP image and entrypoint to the project-maintained location
		getSerenaToSharedImportCodemod(),                           // Migrate removed tools.serena to shared/mcp/serena.md import
		getWorkflowRunBranchesCodemod(),                            // Add default branches to bare on.workflow_run trigger
		getCheckoutPersistCredentialsFalseCodemod(),                // Add with.persist-credentials: false to actions/checkout steps
		getPullRequestTargetCheckoutFalseCodemod(),                 // Add checkout: false for pull_request_target workflows when safe
		getDependabotPermissionsCodemod(),                          // Add vulnerability-alerts: read when dependabot toolset is used
		getGitHubReposToAllowedReposCodemod(),                      // Rename deprecated tools.github.repos to tools.github.allowed-repos
		getToolsetSingularToToolsetsCodemod(),                      // Rename mistyped tools.github.toolset to tools.github.toolsets
		getAllowedReposCurrentToGitHubRepositoryCodemod(),          // Migrate legacy tools.github.allowed-repos: current to ${{ github.repository }}
		getCopilotRequestsFeatureToPermissionsCodemod(),            // Migrate features.copilot-requests to permissions.copilot-requests
		getByokCopilotFeatureRemovalCodemod(),                      // Remove deprecated features.byok-copilot (Copilot BYOK is default)
		getInlineAgentsFeatureRemovalCodemod(),                     // Remove deprecated features.inline-agents (inline sub-agents now default)
		getCliProxyFeatureToGitHubModeCodemod(),                    // Migrate features.cli-proxy: true to tools.github.mode: gh-proxy
		getDIFCProxyToIntegrityProxyCodemod(),                      // Migrate deprecated features.difc-proxy to tools.github.integrity-proxy
		getMountAsCLIsToCLIProxyCodemod(),                          // Rename tools.mount-as-clis to tools.cli-proxy and remove features.mcp-cli
		getMinIntegrityNoneRequiresBashCodemod(),                   // Add tools.bash: false when tools.github.min-integrity is 'none'
		getCLIProxyBashDisabledCodemod(),                           // Set tools.cli-proxy: false when tools.bash is disabled
		getSandboxMCPContainerRemovalCodemod(),                     // Remove deprecated sandbox.mcp.container (now managed internally)
		getSandboxMCPVersionRemovalCodemod(),                       // Remove deprecated sandbox.mcp.version (now managed internally)
		getSandboxRuntimeProfileCodemod(),                          // Migrate sandbox.agent.sudo / legacy-security to sandbox.agent.runtime profiles
		getInferToDisableModelInvocationCodemod(),                  // Migrate deprecated 'infer' to 'disable-model-invocation'
		getRunInstallScriptsToRuntimesNodeCodemod(),                // Move top-level run-install-scripts under runtimes.node
		getMentionsAllowTeamMembersCodemod(),                       // Rename allow-team-members to allowed-collaborators in safe-outputs.mentions
		getEngineCopilotSDKDriverToDriverCodemod(),                 // Rename deprecated engine.copilot-sdk-driver to engine.driver
	}
}

// GetCodemods returns all codemods except any explicitly disabled by ID.
func GetCodemods(disabledIDs []string) ([]Codemod, error) {
	codemods := GetAllCodemods()
	if len(disabledIDs) == 0 {
		return codemods, nil
	}

	disabledSet := make(map[string]struct{}, len(disabledIDs))
	for _, id := range disabledIDs {
		if id == "" {
			continue
		}
		disabledSet[id] = struct{}{}
	}

	if len(disabledSet) == 0 {
		return codemods, nil
	}

	knownIDs := make([]string, 0, len(codemods))
	filtered := make([]Codemod, 0, len(codemods))
	for _, codemod := range codemods {
		knownIDs = append(knownIDs, codemod.ID)
		if _, disabled := disabledSet[codemod.ID]; disabled {
			continue
		}
		filtered = append(filtered, codemod)
	}

	var unknown []string
	for id := range disabledSet {
		if !slices.Contains(knownIDs, id) {
			unknown = append(unknown, id)
		}
	}
	if len(unknown) > 0 {
		slices.Sort(unknown)
		return nil, fmt.Errorf("unknown codemod ID(s): %s", strings.Join(unknown, ", "))
	}

	return filtered, nil
}
