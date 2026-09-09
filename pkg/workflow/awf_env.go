// This file contains AWF environment filtering and max-ai-credits helpers.

package workflow

import (
	"strconv"
	"strings"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/setutil"
	"github.com/github/gh-aw/pkg/workflow/compilerenv"
)

// applyDefaultMaxAICreditsEnvToMap adds the runtime max-ai-credits GitHub Actions expression
// to env when no compile-time max-ai-credits is configured.
//
// This keeps the organization/repository variable override behavior while allowing AWF run:
// scripts to read GH_AW_MAX_AI_CREDITS from step env instead of embedding ${{ vars.* }}
// directly in run blocks.
func applyDefaultMaxAICreditsEnvToMap(env map[string]string, workflowData *WorkflowData) {
	if env == nil {
		return
	}
	if workflowData != nil && workflowData.EngineConfig != nil && workflowData.EngineConfig.MaxAICredits != 0 {
		return
	}
	if workflowData != nil && workflowData.IsEvalsRun {
		env[awfMaxAICreditsVarName] = compilerenv.BuildDefaultEvalsMaxAICreditsExpression(strconv.FormatInt(constants.DefaultDetectionMaxAICredits, 10))
		return
	}
	if workflowData != nil && workflowData.IsDetectionRun {
		env[awfMaxAICreditsVarName] = compilerenv.BuildDefaultDetectionMaxAICreditsExpression(strconv.FormatInt(constants.DefaultDetectionMaxAICredits, 10))
		return
	}
	env[awfMaxAICreditsVarName] = compilerenv.BuildDefaultMaxAICreditsExpression(strconv.FormatInt(constants.DefaultMaxAICredits, 10))
}

// injectMaxAICreditsExpression inserts "maxAiCredits":expr into the apiProxy
// JSON object of awfConfigJSON directly after the "maxRuns" field value.
//
// expr is a shell variable reference such as "${GH_AW_MAX_AI_CREDITS}". The
// caller emits a local export line before the printf command that assigns the
// GitHub Actions runtime expression to that variable, so the ${{ }} expression
// lives on one clean, dedicated line rather than being embedded inside the JSON.
//
// shellEscapeArgWithVarsPreserved is then used to double-quote the JSON arg while
// preserving the ${varName} reference for bash expansion and escaping bare $ signs
// (e.g. "$schema" → "\$schema").
func injectMaxAICreditsExpression(awfConfigJSON string, expr string) string {
	const maxRunsKey = `"maxRuns":`
	idx := strings.Index(awfConfigJSON, maxRunsKey)
	if idx == -1 {
		awfHelpersLog.Print("Warning: could not find maxRuns in AWF config JSON; maxAiCredits expression not injected")
		return awfConfigJSON
	}
	// Scan past the integer value of maxRuns.
	valueEnd := idx + len(maxRunsKey)
	for valueEnd < len(awfConfigJSON) && awfConfigJSON[valueEnd] >= '0' && awfConfigJSON[valueEnd] <= '9' {
		valueEnd++
	}
	return awfConfigJSON[:valueEnd] + `,"maxAiCredits":` + expr + awfConfigJSON[valueEnd:]
}

// ComputeAWFExcludeEnvVarNames returns the list of environment variable names that must be
// excluded from the agent container's visible environment via AWF's --exclude-env flag.
//
// Env var names are included when their step-env values contain a ${{ secrets.* }} reference
// OR a ${{ needs.JOB.outputs.OUTPUT }} job-output expression (which commonly carries
// ephemeral tokens such as GitHub App installation tokens).  Non-secret static vars
// (e.g. GH_DEBUG: "1" in mcp-scripts) are never excluded.
//
// Parameters:
//   - workflowData: the workflow being compiled
//   - coreSecretVarNames: engine-specific fixed secret env var names (e.g. ["COPILOT_GITHUB_TOKEN"])
//
// The function augments coreSecretVarNames with:
//   - MCP_GATEWAY_AGENT_ID when MCP servers are present
//   - GITHUB_MCP_SERVER_TOKEN when the GitHub tool is present
//   - HTTP MCP header secret var names (values always contain ${{ secrets.* }})
//   - mcp-scripts env var names whose values contain ${{ secrets.* }} or a job-output expression
//   - engine.env var names whose values contain ${{ secrets.* }} or a job-output expression
//   - agent.env var names whose values contain ${{ secrets.* }} or a job-output expression
//   - names listed in the frontmatter excluded-env field (unconditionally)
func ComputeAWFExcludeEnvVarNames(workflowData *WorkflowData, coreSecretVarNames []string) []string { //nolint:largefunc // Existing environment classification logic is intentionally kept together.
	seen := make(map[string]struct {
	})
	var names []string

	addUnique := func(name string) {
		if !setutil.Contains(seen, name) {
			seen[name] = struct {
			}{}
			names = append(names, name)
		}
	}

	// Core secret vars for this engine (always contain secret references).
	for _, name := range coreSecretVarNames {
		addUnique(name)
	}

	// MCP gateway agent ID is always a secret when MCP servers are present.
	if HasMCPServers(workflowData) {
		addUnique("MCP_GATEWAY_AGENT_ID")
	}
	if enclavesEnabled(workflowData) {
		addUnique("MCP_GATEWAY_API_KEY")
		addUnique(enclaveGitHubDelegationEnv)
	}

	// GitHub MCP server token is always a secret when the GitHub tool is present.
	if hasGitHubTool(workflowData.ParsedTools) {
		addUnique("GITHUB_MCP_SERVER_TOKEN")
	}

	// HTTP MCP header secrets: values are always ${{ secrets.* }} references.
	for varName := range collectHTTPMCPHeaderSecrets(workflowData.Tools) {
		addUnique(varName)
	}

	// mcp-scripts env vars: only add those whose configured values contain a secret reference
	// or a job-output expression (e.g. ${{ needs.fetch_token.outputs.token }}).
	// (Non-secret vars like GH_DEBUG: "1" must NOT be excluded.)
	if workflowData.MCPScripts != nil {
		for _, toolConfig := range workflowData.MCPScripts.Tools {
			for envName, envValue := range toolConfig.Env {
				if strings.Contains(envValue, "${{ secrets.") || ContainsJobOutputExpr(envValue) {
					addUnique(envName)
				}
			}
		}
	}

	// engine.env vars that contain a secret reference or a job-output expression.
	if workflowData.EngineConfig != nil {
		for varName, varValue := range workflowData.EngineConfig.Env {
			if strings.Contains(varValue, "${{ secrets.") || ContainsJobOutputExpr(varValue) {
				addUnique(varName)
			}
		}
	}

	// agent.env vars that contain a secret reference or a job-output expression.
	agentConfig := getAgentConfig(workflowData)
	if agentConfig != nil {
		for varName, varValue := range agentConfig.Env {
			if strings.Contains(varValue, "${{ secrets.") || ContainsJobOutputExpr(varValue) {
				addUnique(varName)
			}
		}
	}

	// GH_TOKEN when GitHub mode is gh-proxy: the token is passed in the AWF step env for the
	// host difc-proxy but must be excluded from the agent container.
	if isGitHubCLIModeEnabled(workflowData) {
		addUnique("GH_TOKEN")
	}

	// Actions OIDC request credentials must never be visible to the sandboxed AWF agent.
	// The runner-owned gateway forwards them only for HTTP MCP github-oidc authentication.
	addUnique("ACTIONS_ID_TOKEN_REQUEST_URL")
	addUnique("ACTIONS_ID_TOKEN_REQUEST_TOKEN")
	if enclavesEnabled(workflowData) {
		addUnique(enclaveMCPCapabilityEnv)
		addUnique(enclaveMCPGatewayContainerEnv)
		addUnique(enclaveMCPGatewayEndpointEnv)
		addUnique(enclaveMCPGatewayIdentityEnv)
		addUnique(enclaveMCPReadinessTimeoutEnv)
	}
	if enclaveGitHubIssuesEnabled(workflowData) {
		addUnique(enclaveGitHubMCPAgentIDEnv)
	}
	if enclaveDynamicRepositoryPolicyEnabled(workflowData) {
		addUnique(enclaveGitHubDelegationControlEndpointEnv)
	}

	// Explicitly excluded env vars from the frontmatter excluded-env field.
	// These are always excluded regardless of their value content.
	for _, name := range workflowData.ExcludedEnv {
		addUnique(name)
	}

	awfHelpersLog.Printf("Computed %d AWF env vars to exclude", len(names))
	return names
}

// addCliProxyGHTokenToEnv adds GH_TOKEN to the AWF step environment when GitHub
// mode is gh-proxy. The token is NOT used by AWF or its cli-proxy
// sidecar directly — the host difc-proxy (started by start_cli_proxy.sh) already
// has it. However, --env-all passes all step env vars into the agent container,
// so we explicitly set GH_TOKEN here to ensure --exclude-env GH_TOKEN can
// reliably strip it regardless of how the token enters the environment.
// The token is excluded from the agent container via --exclude-env GH_TOKEN, so only
// inject it when the effective AWF version supports both cli-proxy flags and
// --exclude-env.
//
// #nosec G101 -- This is NOT a hardcoded credential. It is a GitHub Actions expression
// template that is resolved at runtime by the GitHub Actions runner.
func addCliProxyGHTokenToEnv(env map[string]string, workflowData *WorkflowData) {
	firewallConfig := getFirewallConfig(workflowData)
	if isGitHubCLIModeEnabled(workflowData) &&
		isFirewallEnabled(workflowData) &&
		awfSupportsCliProxy(firewallConfig) &&
		awfSupportsExcludeEnv(firewallConfig) {
		env["GH_TOKEN"] = "${{ secrets.GH_AW_GITHUB_TOKEN || github.token }}"
		awfHelpersLog.Print("Added GH_TOKEN to env for CLI proxy (excluded from agent container)")
	}
}
