package workflow

// Package workflow provides DIFC proxy injection for pre-agent gh CLI steps.
//
// # DIFC Proxy Injection
//
// When DIFC guards are configured (min-integrity set), the compiler injects
// a temporary proxy (awmg-proxy) that routes pre-agent gh CLI calls through
// integrity filtering. This ensures that custom steps referencing GH_TOKEN see
// DIFC-filtered API responses, matching the integrity guarantees the agent
// itself operates under.
//
// Note: repo-memory clone steps use a direct "git clone https://x-access-token:${GH_TOKEN}@..."
// URL derived from GITHUB_SERVER_URL, not GH_HOST, so they bypass the proxy even when it
// is running. Only gh CLI calls that honour GH_HOST are actually filtered.
//
// The proxy uses the same container image as the MCP gateway (gh-aw-mcpg)
// but runs in "proxy" mode with --guards-mode filter (graceful degradation)
// and --tls (required by the gh CLI HTTPS-only constraint).
//
// Injection conditions:
//
//	Main job:     GitHub tool has explicit guard policies (min-integrity set) AND
//	              custom steps set GH_TOKEN
//	Indexing job: GitHub tool has explicit guard policies (min-integrity set)
//
// Proxy lifecycle within the main job:
//  1. Start proxy — after "Configure gh CLI" step, before custom steps
//  2. Custom steps run with step-level env blocks containing GH_HOST, GH_REPO,
//     GITHUB_API_URL, GITHUB_GRAPHQL_URL, and NODE_EXTRA_CA_CERTS. GH_HOST is
//     set to the identity host from configure_gh_for_ghe.sh (github.com on
//     public GitHub, the real GHES/GHEC hostname on enterprise deployments) so
//     the gh CLI skips spurious version checks against the proxy.  API traffic
//     always routes through the proxy via GITHUB_API_URL / GITHUB_GRAPHQL_URL.
//  3. Stop proxy — before MCP gateway starts (generateMCPSetup); always runs
//     even if earlier steps failed (if: always(), continue-on-error: true)
//
// Proxy lifecycle within the indexing job:
//  1. Start proxy — before index-building steps
//  2. Steps run with all proxy env vars set (GH_HOST, GITHUB_API_URL, GITHUB_GRAPHQL_URL,
//     NODE_EXTRA_CA_CERTS); Octokit calls in actions/github-script are intercepted
//  3. Stop proxy — after steps; always runs (if: always(), continue-on-error: true)
//
// Guard policy note:
//
// The proxy policy uses only the static fields from the workflow's frontmatter
// (min-integrity and repos). The dynamic blocked-users and approval-labels fields
// (which reference outputs from the parse-guard-vars step) are NOT included,
// because that step runs after the proxy starts. Basic integrity filtering is
// still enforced through min-integrity and repos.
//
// Log directories:
//
// The proxy and gateway share /tmp/gh-aw/mcp-logs/ for JSONL output (both append
// to rpc-messages.jsonl in chronological order). The proxy also writes TLS certs
// and container stderr to /tmp/gh-aw/proxy-logs/.

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/logger"
	"github.com/goccy/go-yaml"
)

var difcProxyLog = logger.New("workflow:difc_proxy")

// hasDIFCGuardsConfigured returns true if the GitHub tool has explicit guard policies configured
// (min-integrity is set) AND the DIFC proxy has not been explicitly disabled via
// tools.github.integrity-proxy: false.
// This is the base condition for DIFC proxy injection.
func hasDIFCGuardsConfigured(data *WorkflowData) bool {
	if data == nil {
		return false
	}
	if !isIntegrityProxyEnabled(data) {
		difcProxyLog.Print("integrity-proxy disabled via tools.github.integrity-proxy: false, skipping DIFC proxy injection")
		return false
	}
	githubTool, hasGitHub := data.Tools["github"]
	if !hasGitHub || githubTool == false {
		return false
	}
	toolConfig, _ := githubTool.(map[string]any)
	return len(getGitHubGuardPolicies(toolConfig)) > 0
}

// isIntegrityProxyEnabled returns true unless the user has explicitly disabled the DIFC proxy
// by setting tools.github.integrity-proxy: false.
// The proxy is enabled by default (opt-out model): absent or true → enabled; false → disabled.
func isIntegrityProxyEnabled(data *WorkflowData) bool {
	if data == nil {
		return true
	}
	githubTool, hasGitHub := data.Tools["github"]
	if !hasGitHub {
		return true
	}
	toolConfig, ok := githubTool.(map[string]any)
	if !ok {
		return true
	}
	val, hasField := toolConfig["integrity-proxy"]
	if !hasField {
		return true // default: enabled
	}
	if enabled, ok := val.(bool); ok {
		return enabled
	}
	return true
}

// hasDIFCProxyNeeded returns true if the DIFC proxy should be injected in the main job.
//
// The proxy is only needed when:
//  1. The GitHub tool has explicit guard policies (min-integrity is set), and
//  2. There are pre-agent steps that may call the gh CLI (identified by GH_TOKEN use
//     in custom steps, or by the presence of repo-memory configuration whose clone
//     steps always set GH_TOKEN).
func hasDIFCProxyNeeded(data *WorkflowData) bool {
	if !hasDIFCGuardsConfigured(data) {
		difcProxyLog.Print("No explicit guard policies configured, skipping DIFC proxy injection")
		return false
	}

	// Check if there are pre-agent steps that set GH_TOKEN
	if !hasPreAgentStepsWithGHToken(data) {
		difcProxyLog.Print("No pre-agent steps with GH_TOKEN, skipping DIFC proxy injection")
		return false
	}

	difcProxyLog.Print("DIFC proxy needed: guard policies configured and pre-agent steps have GH_TOKEN")
	return true
}

// hasPreAgentStepsWithGHToken returns true if there are pre-agent steps that set GH_TOKEN.
//
// The heuristic checks whether custom steps (from data.CustomSteps) reference GH_TOKEN.
//
// Note: repo-memory clone steps use a direct "git clone https://x-access-token:${GH_TOKEN}@..."
// URL derived from GITHUB_SERVER_URL, not GH_HOST, so they are not intercepted by the proxy
// and are therefore not counted here.
func hasPreAgentStepsWithGHToken(data *WorkflowData) bool {
	if data == nil {
		return false
	}

	// Check if custom steps reference GH_TOKEN
	if strings.Contains(data.CustomSteps, "GH_TOKEN") {
		difcProxyLog.Print("Custom steps contain GH_TOKEN, proxy needed")
		return true
	}

	return false
}

// getDIFCProxyPolicyJSON returns a JSON-encoded guard policy for the DIFC proxy.
//
// Unlike the gateway policy (which includes dynamic blocked-users and approval-labels
// from step outputs), the proxy policy only includes the static fields available at
// compile time: min-integrity and repos. This is because the proxy starts before the
// parse-guard-vars step that produces those dynamic outputs.
//
// When the integrity-reactions feature flag is enabled and the MCPG version supports it,
// reaction fields (endorsement-reactions, disapproval-reactions, disapproval-integrity,
// endorser-min-integrity) are also included in the proxy policy.
//
// Returns an empty string if no guard policy fields are found.
func getDIFCProxyPolicyJSON(githubTool map[string]any, data *WorkflowData, gatewayConfig *MCPGatewayRuntimeConfig) string {
	policy := make(map[string]any)

	// Support both 'allowed-repos' (preferred) and deprecated 'repos'
	repos, hasRepos := githubTool["allowed-repos"]
	if !hasRepos {
		repos, hasRepos = githubTool["repos"]
	}
	integrity, hasIntegrity := githubTool["min-integrity"]

	if !hasRepos && !hasIntegrity {
		return ""
	}

	if hasRepos {
		policy["repos"] = normalizeGitHubRepositoryInReposScope(repos)
	} else {
		// Default repos to "all" when min-integrity is specified without repos
		policy["repos"] = "all"
	}

	if hasIntegrity {
		policy["min-integrity"] = integrity
	}

	// Inject reaction fields when the feature flag is enabled and MCPG supports it.
	injectIntegrityReactionFields(policy, githubTool, data, gatewayConfig)

	guardPolicy := map[string]any{
		"allow-only": policy,
	}

	jsonBytes, err := json.Marshal(guardPolicy)
	if err != nil {
		difcProxyLog.Printf("Failed to marshal DIFC proxy policy: %v", err)
		return ""
	}

	return string(jsonBytes)
}

// resolveProxyContainerImage returns the full container image reference (container:version)
// for the DIFC/CLI proxy, falling back to the default MCP gateway version if none is configured.
func resolveProxyContainerImage(gatewayConfig *MCPGatewayRuntimeConfig) string {
	version := gatewayConfig.Version
	if version == "" {
		version = string(constants.DefaultMCPGatewayVersion)
	}
	return gatewayConfig.Container + ":" + version
}

// writeProxyUpstreamEnv writes the curated upstream GitHub env passed to DIFC/CLI
// proxy containers. These containers do not inherit the job env wholesale, so we
// must explicitly forward the enterprise host context they need for github.com,
// *.ghe.com data residency, and GHES routing.
func writeProxyUpstreamEnv(sb *strings.Builder) {
	sb.WriteString("          GITHUB_SERVER_URL: ${{ github.server_url }}\n")
	sb.WriteString("          GITHUB_API_URL: ${{ github.api_url }}\n")
	sb.WriteString("          GH_HOST: ${{ env.GH_HOST }}\n")
	sb.WriteString("          GITHUB_HOST: ${{ env.GITHUB_HOST }}\n")
	sb.WriteString("          GITHUB_ENTERPRISE_HOST: ${{ env.GITHUB_ENTERPRISE_HOST }}\n")
	sb.WriteString("          GITHUB_GRAPHQL_URL: ${{ env.GITHUB_GRAPHQL_URL }}\n")
	sb.WriteString("          GITHUB_COPILOT_BASE_URL: ${{ env.GITHUB_COPILOT_BASE_URL }}\n")
}

// buildStartDIFCProxyStepYAML returns the YAML for the "Start DIFC proxy" step,
// or an empty string if proxy injection is not needed or the policy cannot be built.
// This is the shared implementation used by both the main job and the indexing job.
func (c *Compiler) buildStartDIFCProxyStepYAML(data *WorkflowData) string {
	difcProxyLog.Print("Building Start DIFC proxy step YAML")

	githubToolConfig, _ := data.Tools["github"].(map[string]any)

	// Get MCP server token (same token the gateway uses for the GitHub MCP server)
	customGitHubToken := getGitHubToken(githubToolConfig)
	resolvedToken := resolveGitHubToken(customGitHubToken)

	// Build the simplified guard policy JSON (static fields only)
	// (plus reaction fields when integrity-reactions feature flag is enabled)
	ensureDefaultMCPGatewayConfig(data)
	policyJSON := getDIFCProxyPolicyJSON(githubToolConfig, data, data.SandboxConfig.MCP)
	if policyJSON == "" {
		difcProxyLog.Print("Could not build DIFC proxy policy JSON, skipping proxy start")
		return ""
	}

	// Resolve the container image from the MCP gateway configuration
	// (proxy uses the same image as the gateway, just in "proxy" mode)
	containerImage := resolveProxyContainerImage(data.SandboxConfig.MCP)

	var sb strings.Builder
	sb.WriteString("      - name: Start DIFC Proxy\n")
	sb.WriteString("        env:\n")
	fmt.Fprintf(&sb, "          GH_TOKEN: %s\n", resolvedToken)
	writeProxyUpstreamEnv(&sb)
	if isAWFNetworkIsolationEnabled(data) {
		sb.WriteString("          GH_AW_NETWORK_ISOLATION: 'true'\n")
	}
	// Store policy and image in env vars to avoid shell-quoting issues with
	// inline JSON arguments and to keep the run: command clean.
	fmt.Fprintf(&sb, "          DIFC_PROXY_POLICY: '%s'\n", policyJSON)
	fmt.Fprintf(&sb, "          DIFC_PROXY_IMAGE: '%s'\n", containerImage)
	sb.WriteString("        run: |\n")
	sb.WriteString("          bash \"${RUNNER_TEMP}/gh-aw/actions/start_difc_proxy.sh\"\n")
	return sb.String()
}

// generateStartDIFCProxyStep generates a step that starts the DIFC proxy container
// before pre-agent gh CLI steps. The proxy routes gh API calls through integrity filtering.
//
// The step is only emitted when hasDIFCProxyNeeded returns true.
// The generated step calls start_difc_proxy.sh with the guard policy JSON and container image.
func (c *Compiler) generateStartDIFCProxyStep(yaml *strings.Builder, data *WorkflowData) {
	if !hasDIFCProxyNeeded(data) {
		return
	}

	step := c.buildStartDIFCProxyStepYAML(data)
	if step != "" {
		yaml.WriteString(step)
	}
}

// proxyEnvVars returns the env vars to inject as step-level env on each custom step
// when the DIFC proxy is running.
//
// # GH_HOST value rationale
//
// GH_HOST must NOT be set to the proxy address (localhost:18443) because the gh
// CLI treats any host that is not github.com or *.ghe.com as GitHub Enterprise
// Server (GHES) and performs a version check by calling GET /api/v3/meta before
// every API request made with --repo. The DIFC proxy does not return the
// installed_version field that GHES instances include in /meta; the upstream
// github.com /meta response omits it, so gh rejects the response as
// "malformed version: " and aborts — crashing every gh --repo call in
// user-defined steps.
//
// The correct value for GH_HOST depends on the GitHub deployment type:
//
//   - github.com (public GitHub): configure_gh_for_ghe.sh either leaves GH_HOST
//     unset or sets it to "github.com".  gh treats "github.com" as public GitHub
//     and skips the GHES version check entirely.  All API traffic is still routed
//     through the proxy via GITHUB_API_URL.
//
//   - GHEC (*.ghe.com): configure_gh_for_ghe.sh sets GH_HOST to the tenant
//     hostname (e.g. myorg.ghe.com).  gh treats *.ghe.com the same as github.com
//     (no GHES version check), so the same "no broken version check" property
//     holds.
//
//   - GHES (any other hostname): configure_gh_for_ghe.sh sets GH_HOST to the real
//     GHES hostname (e.g. ghes.example.com).  gh performs the GHES version check
//     using GITHUB_API_URL (the proxy), which forwards GET /meta to the real GHES
//     upstream.  The real GHES returns installed_version, so the check passes.
//
// Using `${{ env.GH_HOST || 'github.com' }}` therefore selects the correct
// identity host for every deployment type while keeping all API traffic flowing
// through the proxy via GITHUB_API_URL / GITHUB_GRAPHQL_URL.
func proxyEnvVars() map[string]string {
	return map[string]string{
		// Identity host from configure_gh_for_ghe.sh, not the proxy address.
		// See function-level comment for full rationale.
		"GH_HOST":             "${{ env.GH_HOST || 'github.com' }}",
		"GH_REPO":             "${{ github.repository }}",
		"GITHUB_API_URL":      "https://localhost:18443/api/v3",
		"GITHUB_GRAPHQL_URL":  "https://localhost:18443/api/graphql",
		"NODE_EXTRA_CA_CERTS": constants.TmpProxyTLSCACert,
	}
}

// injectProxyEnvIntoCustomSteps adds the DIFC proxy routing env vars to each step
// in the custom steps YAML string as step-level env. Step-level env takes precedence
// over $GITHUB_ENV values but does not mutate them, so GHE host values set by
// configure_gh_for_ghe.sh are preserved for steps that do not need the proxy.
//
// The proxy env vars injected are:
//   - GH_HOST=${{ env.GH_HOST || 'github.com' }}  (correct identity host, not proxy addr)
//   - GH_REPO=${{ github.repository }}
//   - GITHUB_API_URL=https://localhost:18443/api/v3
//   - GITHUB_GRAPHQL_URL=https://localhost:18443/api/graphql
//   - NODE_EXTRA_CA_CERTS=/tmp/gh-aw/proxy-logs/proxy-tls/ca.crt
//
// GH_HOST is intentionally NOT set to the proxy address; see proxyEnvVars() for
// the full rationale.
//
// If a step already has an env: block, the proxy vars are merged into it (existing
// vars like GH_TOKEN are preserved). If parsing or serialization fails, the original
// customSteps string is returned unchanged.
//
// Version comments on uses lines (e.g. "uses: actions/foo@sha # v4") are preserved
// and re-applied after re-serialization. Step fields are ordered using
// constants.PriorityStepFields so name/uses stay ahead of env for stable diffs.
func injectProxyEnvIntoCustomSteps(customSteps string) string {
	if customSteps == "" {
		return customSteps
	}

	// Extract version comments from uses lines before unmarshaling.
	// YAML treats "# comment" as a comment and strips it during Unmarshal, so we
	// must capture them here and re-apply after processing to preserve annotations
	// like "uses: actions/upload-artifact@sha # v7" in the compiled lock file.
	// Without this, gh-aw-manifest falls back to recording the SHA as the version.
	versionComments := make(map[string]string) // key: action@sha, value: " # vX"
	for line := range strings.SplitSeq(customSteps, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "uses:") && strings.Contains(trimmed, " # ") {
			parts := strings.SplitN(trimmed, " # ", 2)
			if len(parts) == 2 {
				usesValue := strings.TrimSpace(strings.TrimPrefix(parts[0], "uses:"))
				versionComments[usesValue] = " # " + parts[1]
			}
		}
	}

	var parsed struct {
		Steps []map[string]any `yaml:"steps"`
	}
	if err := yaml.Unmarshal([]byte(customSteps), &parsed); err != nil || len(parsed.Steps) == 0 {
		difcProxyLog.Printf("injectProxyEnvIntoCustomSteps: could not parse custom steps, returning as-is: %v", err)
		return customSteps
	}

	proxyEnv := proxyEnvVars()

	// Convert each step to an ordered MapSlice with priority fields first so that
	// name/uses stay ahead of env for stable diffs, then merge proxy env vars.
	orderedSteps := make([]yaml.MapSlice, len(parsed.Steps))
	for i, step := range parsed.Steps {
		envMap, ok := step["env"].(map[string]any)
		if !ok {
			envMap = make(map[string]any)
		}
		for k, v := range proxyEnv {
			envMap[k] = v
		}
		step["env"] = envMap

		// Re-apply version comment to uses value so the comment survives re-serialization.
		if usesVal, hasUses := step["uses"]; hasUses {
			if usesStr, ok := usesVal.(string); ok {
				if comment, hasComment := versionComments[usesStr]; hasComment {
					step["uses"] = usesStr + comment
				}
			}
		}

		orderedSteps[i] = OrderMapFields(step, constants.PriorityStepFields)
	}

	resultBytes, err := yaml.MarshalWithOptions(
		map[string]any{"steps": orderedSteps},
		yaml.Indent(2),
		yaml.UseLiteralStyleIfMultiline(true),
	)
	if err != nil {
		difcProxyLog.Printf("injectProxyEnvIntoCustomSteps: failed to re-serialize, returning as-is: %v", err)
		return customSteps
	}

	// The YAML marshaller quotes strings containing "#" (version comments), but
	// GitHub Actions expects unquoted uses values.
	return unquoteUsesWithComments(strings.TrimRight(string(resultBytes), "\n"))
}

// generateStopDIFCProxyStep generates a step that stops the DIFC proxy container
// before the MCP gateway starts. The proxy must be stopped first to avoid
// double-filtering: the gateway uses the same guard policy for the agent phase.
//
// The step runs even if earlier steps failed (if: always(), continue-on-error: true)
// to ensure the proxy container and CA cert are always cleaned up.
//
// The step is only emitted when hasDIFCProxyNeeded returns true.
func (c *Compiler) generateStopDIFCProxyStep(yaml *strings.Builder, data *WorkflowData) {
	if !hasDIFCProxyNeeded(data) {
		return
	}

	difcProxyLog.Print("Generating Stop DIFC Proxy step")

	yaml.WriteString("      - name: Stop DIFC Proxy\n")
	yaml.WriteString("        if: always()\n")
	yaml.WriteString("        continue-on-error: true\n")
	yaml.WriteString("        run: bash \"${RUNNER_TEMP}/gh-aw/actions/stop_difc_proxy.sh\"\n")
}

// isCliProxyNeeded returns true if the CLI proxy should be started on the host.
//
// The CLI proxy is needed when:
//  1. tools.github.mode is set to gh-proxy (or legacy cli-proxy behavior is enabled), and
//  2. The AWF sandbox (firewall) is enabled, and
//  3. The AWF version supports CLI proxy flags
//
// The cli-proxy feature is implicitly enabled when integrity-reactions is enabled,
// because reaction-based integrity decisions require the proxy to identify reaction authors.
func isCliProxyNeeded(data *WorkflowData) bool {
	cliProxyEnabled := isGitHubCLIModeEnabled(data)
	integrityReactionsEnabled := isFeatureEnabled(constants.IntegrityReactionsFeatureFlag, data)

	if !cliProxyEnabled && !integrityReactionsEnabled {
		return false
	}
	if integrityReactionsEnabled && !cliProxyEnabled {
		difcProxyLog.Print("integrity-reactions enabled: implicitly enabling CLI proxy")
	}
	if !isFirewallEnabled(data) {
		return false
	}
	firewallConfig := getFirewallConfig(data)
	if !awfSupportsCliProxy(firewallConfig) {
		difcProxyLog.Printf("Skipping CLI proxy: AWF version too old")
		return false
	}
	return true
}

// generateStartCliProxyStep generates a step that starts the difc-proxy container
// on the host before the AWF execution step. AWF's cli-proxy sidecar connects
// to this host proxy via host.docker.internal:18443.
//
// The step is only emitted when isCliProxyNeeded returns true.
func (c *Compiler) generateStartCliProxyStep(yaml *strings.Builder, data *WorkflowData) {
	if !isCliProxyNeeded(data) {
		return
	}

	step := c.buildStartCliProxyStepYAML(data)
	if step != "" {
		yaml.WriteString(step)
	}
}

// defaultCliProxyPolicyJSONTemplate is the fallback guard policy template for the CLI proxy
// when no guard policy is explicitly configured in the workflow frontmatter.
// The DIFC proxy requires a --policy flag to forward requests; without it, all API
// calls return HTTP 503 with body "proxy enforcement not configured".
// This template references the determine-automatic-lockdown step outputs so that the
// repos restriction matches the repository visibility at runtime:
//   - Public repos → repos="public" (blocks access to private repos)
//   - Private repos → repos="all" (no cross-visibility restriction)
//
// This mirrors how the MCP Gateway references $GITHUB_MCP_GUARD_REPOS.
const defaultCliProxyPolicyJSONTemplate = `{"allow-only":{"repos":"${{ steps.determine-automatic-lockdown.outputs.repos }}","min-integrity":"${{ steps.determine-automatic-lockdown.outputs.min_integrity }}"}}`

const defaultReposStepOutputTemplate = "${{ steps.determine-automatic-lockdown.outputs.repos }}"
const defaultMinIntegrityStepOutputTemplate = "${{ steps.determine-automatic-lockdown.outputs.min_integrity }}"

// buildCLIProxyPolicyJSON applies runtime defaults for any guard-policy fields omitted
// in workflow frontmatter while preserving explicitly configured fields.
func buildCLIProxyPolicyJSON(githubToolConfig map[string]any, data *WorkflowData) string {
	policyJSON := getDIFCProxyPolicyJSON(githubToolConfig, data, data.SandboxConfig.MCP)
	if policyJSON == "" {
		return defaultCliProxyPolicyJSONTemplate
	}

	repos, hasRepos := githubToolConfig["allowed-repos"]
	if !hasRepos {
		repos, hasRepos = githubToolConfig["repos"]
	}
	_, hasIntegrity := githubToolConfig["min-integrity"]

	var policy map[string]any
	if err := json.Unmarshal([]byte(policyJSON), &policy); err != nil {
		difcProxyLog.Printf("Failed to unmarshal CLI proxy policy JSON for runtime defaults: %v", err)
		return policyJSON
	}
	allowOnly, ok := policy["allow-only"].(map[string]any)
	if !ok {
		return policyJSON
	}

	if !hasRepos {
		allowOnly["repos"] = defaultReposStepOutputTemplate
	} else {
		allowOnly["repos"] = normalizeGitHubRepositoryInReposScope(repos)
	}
	if !hasIntegrity {
		allowOnly["min-integrity"] = defaultMinIntegrityStepOutputTemplate
	}

	jsonBytes, err := json.Marshal(policy)
	if err != nil {
		difcProxyLog.Printf("Failed to marshal CLI proxy policy JSON with runtime defaults: %v", err)
		return policyJSON
	}
	return string(jsonBytes)
}

// buildStartCliProxyStepYAML returns the YAML for the "Start CLI proxy" step,
// or an empty string if the proxy cannot be configured.
func (c *Compiler) buildStartCliProxyStepYAML(data *WorkflowData) string {
	difcProxyLog.Print("Building Start CLI proxy step YAML")

	githubToolConfig, _ := data.Tools["github"].(map[string]any)

	// Get token for the proxy
	customGitHubToken := getGitHubToken(githubToolConfig)
	resolvedToken := resolveGitHubToken(customGitHubToken)

	// Build the guard policy JSON (static fields only, plus reaction fields when enabled).
	// The CLI proxy requires a policy to forward requests — without one, all API
	// calls return HTTP 503 ("proxy enforcement not configured"). When no guard policy
	// is explicitly configured, reference the determine-automatic-lockdown step outputs
	// so the repos restriction reflects repo visibility at runtime, matching the MCP
	// Gateway's behavior.
	ensureDefaultMCPGatewayConfig(data)
	policyJSON := buildCLIProxyPolicyJSON(githubToolConfig, data)

	// Resolve the container image from the MCP gateway configuration
	containerImage := resolveProxyContainerImage(data.SandboxConfig.MCP)

	var sb strings.Builder
	sb.WriteString("      - name: Start CLI Proxy\n")
	sb.WriteString("        env:\n")
	fmt.Fprintf(&sb, "          GH_TOKEN: %s\n", resolvedToken)
	writeProxyUpstreamEnv(&sb)
	if isAWFNetworkIsolationEnabled(data) {
		sb.WriteString("          GH_AW_NETWORK_ISOLATION: 'true'\n")
	}
	fmt.Fprintf(&sb, "          CLI_PROXY_POLICY: %s\n", quoteYAMLEnvValue(policyJSON))
	fmt.Fprintf(&sb, "          CLI_PROXY_IMAGE: '%s'\n", containerImage)
	sb.WriteString("        run: |\n")
	sb.WriteString("          bash \"${RUNNER_TEMP}/gh-aw/actions/start_cli_proxy.sh\"\n")
	return sb.String()
}

// generateStopCliProxyStep generates a step that stops the CLI proxy container
// after the AWF execution step.
//
// The step runs even if earlier steps failed (if: always(), continue-on-error: true)
// to ensure the proxy container is always cleaned up.
//
// The step is only emitted when isCliProxyNeeded returns true.
func (c *Compiler) generateStopCliProxyStep(yaml *strings.Builder, data *WorkflowData) {
	if !isCliProxyNeeded(data) {
		return
	}

	difcProxyLog.Print("Generating Stop CLI Proxy step")

	yaml.WriteString("      - name: Stop CLI Proxy\n")
	yaml.WriteString("        if: always()\n")
	yaml.WriteString("        continue-on-error: true\n")
	yaml.WriteString("        run: bash \"${RUNNER_TEMP}/gh-aw/actions/stop_cli_proxy.sh\"\n")
}

// difcProxyLogPaths returns the artifact paths for DIFC proxy logs.
// Returns an empty slice when no DIFC proxy is needed or configured.
func difcProxyLogPaths(data *WorkflowData) []string {
	// Return proxy-logs path if proxy is needed in either the main job or the indexing job.
	// hasDIFCGuardsConfigured covers the indexing job case (guard policies alone are sufficient).
	if !hasDIFCGuardsConfigured(data) {
		return nil
	}
	// proxy-logs/ contains TLS certs and container stderr from the proxy.
	// Exclude proxy-tls/ to avoid uploading TLS material (mcp-logs/ is already
	// collected as part of standard MCP logging).
	return []string{
		constants.TmpProxyLogsDir,
		"!" + constants.TmpProxyTLSDir,
	}
}
