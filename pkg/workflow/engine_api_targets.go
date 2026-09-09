package workflow

import (
	"strings"

	"github.com/github/gh-aw/pkg/constants"
)

// extractAPITargetHost extracts the hostname from a custom API base URL in engine.env.
// This supports custom OpenAI-compatible or Anthropic-compatible endpoints (e.g., internal
// LLM routers, Azure OpenAI) while preserving AWF's credential isolation and firewall features.
//
// The function:
// 1. Checks if the specified env var (e.g., "OPENAI_BASE_URL") exists in engine.env
// 2. Extracts the hostname from the URL (e.g., "https://llm-router.internal.example.com/v1" → "llm-router.internal.example.com")
// 3. Returns empty string if no custom URL is configured or if the URL is invalid
//
// Parameters:
//   - workflowData: The workflow data containing engine configuration
//   - envVar: The environment variable name (e.g., "OPENAI_BASE_URL", "ANTHROPIC_BASE_URL")
//
// Returns:
//   - string: The hostname to use as --openai-api-target or --anthropic-api-target, or empty string if not configured
//
// Example:
//
//	engine:
//	  id: codex
//	  env:
//	    OPENAI_BASE_URL: "https://llm-router.internal.example.com/v1"
//	    OPENAI_API_KEY: ${{ secrets.LLM_ROUTER_KEY }}
//
//	extractAPITargetHost(workflowData, "OPENAI_BASE_URL")
//	// Returns: "llm-router.internal.example.com"
func extractAPITargetHost(workflowData *WorkflowData, envVar string) string {
	// Check if engine config and env are available
	if workflowData == nil || workflowData.EngineConfig == nil || workflowData.EngineConfig.Env == nil {
		return ""
	}

	// Get the custom base URL from engine.env
	baseURL, exists := workflowData.EngineConfig.Env[envVar]
	if !exists || baseURL == "" {
		return ""
	}

	// Extract hostname from URL
	// URLs can be:
	// - "https://llm-router.internal.example.com/v1" → "llm-router.internal.example.com"
	// - "http://localhost:8080/v1" → "localhost:8080"
	// - "api.openai.com" → "api.openai.com" (treated as hostname)

	// Remove protocol prefix if present
	host := baseURL
	if idx := strings.Index(host, "://"); idx != -1 {
		host = host[idx+3:]
	}

	// Remove path suffix if present (everything after first /)
	if idx := strings.Index(host, "/"); idx != -1 {
		host = host[:idx]
	}

	// Validate that we have a non-empty hostname
	if host == "" {
		awfHelpersLog.Printf("Invalid %s URL (no hostname): %s", envVar, baseURL)
		return ""
	}

	awfHelpersLog.Printf("Extracted API target host from %s: %s", envVar, host)
	return host
}

// extractAPIProxyTargetHost returns the target host format expected by the
// effective AWF version, preserving an explicit http:// scheme when supported.
func extractAPIProxyTargetHost(workflowData *WorkflowData, envVar string, firewallConfig *FirewallConfig) string {
	host := extractAPITargetHost(workflowData, envVar)
	if host == "" || !awfSupportsHTTPAPITargets(firewallConfig) {
		return host
	}

	if strings.HasPrefix(workflowData.EngineConfig.Env[envVar], "http://") {
		return "http://" + host
	}
	return host
}

// extractAPIBasePath extracts the path component from a custom API base URL in engine.env.
// Returns the path prefix (e.g., "/serving-endpoints") or empty string if no path is present.
// Root-only paths ("/") and empty paths return empty string.
//
// This is used to pass --openai-api-base-path and --anthropic-api-base-path to AWF when
// the configured base URL contains a path (e.g., Databricks serving endpoints, Azure OpenAI
// deployments, or corporate LLM routers with path-based routing).
func extractAPIBasePath(workflowData *WorkflowData, envVar string) string {
	if workflowData == nil || workflowData.EngineConfig == nil || workflowData.EngineConfig.Env == nil {
		return ""
	}

	baseURL, exists := workflowData.EngineConfig.Env[envVar]
	if !exists || baseURL == "" {
		return ""
	}

	// Remove protocol prefix if present
	host := baseURL
	if idx := strings.Index(host, "://"); idx != -1 {
		host = host[idx+3:]
	}

	// Extract path (everything after the first /)
	if idx := strings.Index(host, "/"); idx != -1 {
		path := host[idx:] // e.g., "/serving-endpoints"
		// Strip query string or fragment if present
		if qi := strings.IndexAny(path, "?#"); qi != -1 {
			path = path[:qi]
		}
		// Remove trailing slashes; a root-only path "/" becomes "" and returns empty
		path = strings.TrimRight(path, "/")
		if path != "" {
			awfHelpersLog.Printf("Extracted API base path from %s: %s", envVar, path)
			return path
		}
	}

	return ""
}

// extractAPITargetAuthHeader extracts the authHeader value from the sandbox.agent.targets
// frontmatter section for a given provider (e.g. "openai" or "anthropic"). It reads:
//
//	sandbox.agent.targets.<provider>.authHeader
//
// Returns the header name string (e.g. "api-key") or empty string if not configured.
func extractAPITargetAuthHeader(workflowData *WorkflowData, provider string) string {
	if workflowData == nil || workflowData.SandboxConfig == nil || workflowData.SandboxConfig.Agent == nil {
		return ""
	}
	targets := workflowData.SandboxConfig.Agent.Targets
	if targets == nil {
		return ""
	}
	target, ok := targets[provider]
	if !ok || target == nil {
		return ""
	}
	return target.AuthHeader
}

// extractCopilotTargetConfig returns the AgentAPIProxyTargetConfig for the "copilot" provider
// from the sandbox.agent.targets frontmatter section. Returns nil if not configured.
func extractCopilotTargetConfig(workflowData *WorkflowData) *AgentAPIProxyTargetConfig {
	if workflowData == nil || workflowData.SandboxConfig == nil || workflowData.SandboxConfig.Agent == nil {
		return nil
	}
	targets := workflowData.SandboxConfig.Agent.Targets
	if targets == nil {
		return nil
	}
	return targets["copilot"]
}

// GetCopilotAPITarget returns the effective Copilot API target hostname, checking in order:
//  1. engine.api-target (explicit, takes precedence)
//  2. GITHUB_COPILOT_BASE_URL in engine.env (implicit, derived from the configured Copilot base URL)
//  3. COPILOT_PROVIDER_BASE_URL in engine.env when it is a literal URL (BYOK self-hosted target)
//
// This mirrors the pattern used by other engines:
//   - Codex:    OPENAI_BASE_URL     → --openai-api-target
//   - Claude:   ANTHROPIC_BASE_URL  → --anthropic-api-target
//   - Copilot:  GITHUB_COPILOT_BASE_URL → --copilot-api-target (fallback when api-target not set)
//
// Returns empty string if no usable source is configured.
func GetCopilotAPITarget(workflowData *WorkflowData) string {
	awfHelpersLog.Print("Getting Copilot API target")
	// Explicit engine.api-target takes precedence.
	if workflowData != nil && workflowData.EngineConfig != nil && workflowData.EngineConfig.APITarget != "" {
		awfHelpersLog.Printf("Using explicit Copilot api-target: %s", workflowData.EngineConfig.APITarget)
		return workflowData.EngineConfig.APITarget
	}

	// Fallback: derive from the well-known GITHUB_COPILOT_BASE_URL env var.
	awfHelpersLog.Print("No explicit api-target, deriving Copilot API target from GITHUB_COPILOT_BASE_URL")
	if target := extractAPITargetHost(workflowData, "GITHUB_COPILOT_BASE_URL"); target != "" {
		return target
	}

	// Final fallback: derive from a literal BYOK provider URL so AWF's api-proxy preserves
	// non-default hosts and ports for self-hosted OpenAI-compatible backends such as Ollama.
	awfHelpersLog.Print("No GITHUB_COPILOT_BASE_URL, deriving Copilot API target from literal COPILOT_PROVIDER_BASE_URL")
	return extractLiteralEngineEnvHost(workflowData, constants.CopilotProviderBaseURL)
}

// isCopilotBYOKMode returns true when Copilot execution is configured to use BYOK routing.
// BYOK mode is active when either:
//   - a non-GitHub model-provider gateway is in use (only when sandbox/firewall is enabled), or
//   - COPILOT_PROVIDER_BASE_URL is present in engine.env with a non-empty value.
//
// Note that this intentionally checks whether BYOK routing is configured, not whether
// a literal hostname can be extracted. Literal host extraction is handled separately by
// GetCopilotAPITarget().
func isCopilotBYOKMode(workflowData *WorkflowData, sandboxEnabled bool) bool {
	providerOverrideBYOK := resolveEngineLLMProvider(workflowData, LLMProviderGitHub) != LLMProviderGitHub && sandboxEnabled
	return providerOverrideBYOK || engineEnvHasNonEmptyValue(workflowData, constants.CopilotProviderBaseURL)
}

// isCopilotCustomConfig returns true when Copilot is configured away from the default
// GitHub-hosted setup via BYOK routing and/or explicit Copilot API target overrides.
func isCopilotCustomConfig(workflowData *WorkflowData) bool {
	return isCopilotBYOKMode(workflowData, isFirewallEnabled(workflowData)) || GetCopilotAPITarget(workflowData) != ""
}

func extractLiteralEngineEnvHost(workflowData *WorkflowData, envVar string) string {
	env := getEngineEnvOverrides(workflowData)
	if env == nil {
		return ""
	}
	rawValue, ok := env[envVar]
	if !ok || rawValue == "" {
		return ""
	}
	if strings.Contains(rawValue, "${{") {
		awfHelpersLog.Printf("Skipping %s host extraction from GitHub expression value", envVar)
		return ""
	}
	return extractAPITargetHost(workflowData, envVar)
}

// GetCopilotAllowlistTargets returns the Copilot-specific hosts that must be present in the
// firewall allow-list for execution to succeed.
//
// This includes:
//  1. The BYOK provider host from COPILOT_PROVIDER_BASE_URL in engine.env, when configured.
//  2. The Copilot API target from engine.api-target or GITHUB_COPILOT_BASE_URL.
//
// The BYOK provider host is added first because it is the actual outbound destination for
// Copilot CLI requests in BYOK mode. Duplicate hosts are removed.
func GetCopilotAllowlistTargets(workflowData *WorkflowData) []string {
	var targets []string
	seen := make(map[string]struct{})

	addTarget := func(target string) {
		if target == "" {
			return
		}
		if _, exists := seen[target]; exists {
			return
		}
		seen[target] = struct{}{}
		targets = append(targets, target)
	}

	addTarget(extractLiteralEngineEnvHost(workflowData, constants.CopilotProviderBaseURL))
	addTarget(GetCopilotAPITarget(workflowData))

	return targets
}

// DefaultGeminiAPITarget is the default Gemini API endpoint hostname.
const DefaultGeminiAPITarget = "generativelanguage.googleapis.com"

// GetGeminiAPITarget returns the effective Gemini API target hostname for the LLM gateway proxy.
//
// Resolution order:
//  1. GEMINI_API_BASE_URL in engine.env (custom endpoint)
//  2. Default: generativelanguage.googleapis.com (when engine is "gemini")
//
// Returns empty string if the engine is not Gemini and no custom GEMINI_API_BASE_URL is configured.
func GetGeminiAPITarget(workflowData *WorkflowData, engineName string) string {
	awfHelpersLog.Printf("Getting Gemini API target for engine: %s", engineName)
	// Check for custom GEMINI_API_BASE_URL in engine.env
	if customTarget := extractAPITargetHost(workflowData, "GEMINI_API_BASE_URL"); customTarget != "" {
		awfHelpersLog.Printf("Using custom Gemini API target from GEMINI_API_BASE_URL: %s", customTarget)
		return customTarget
	}

	// Default to the standard Gemini API endpoint when engine is Gemini
	if engineName == "gemini" {
		awfHelpersLog.Printf("Using default Gemini API target: %s", DefaultGeminiAPITarget)
		return DefaultGeminiAPITarget
	}

	awfHelpersLog.Print("No Gemini API target configured (engine is not gemini and no custom URL)")
	return ""
}

// getEngineAPIHosts returns the primary AI inference API hostnames for the given engine and
// workflow data. These are the hosts that appear in the firewall audit log when the engine
// makes authenticated API calls. The returned slice is used to populate GH_AW_ENGINE_API_HOSTS
// so the failure handler can detect credential authentication rejections without relying solely
// on hardcoded host patterns.
//
// Resolution order (per engine):
//   - engine.api-target (explicit GHES / enterprise override) takes precedence
//   - Default public API hostname(s) for the engine
func getEngineAPIHosts(data *WorkflowData, engine CodingAgentEngine) []string {
	if engine == nil {
		return nil
	}

	// Explicit api-target overrides the engine-specific default for all engine types.
	if data != nil && data.EngineConfig != nil && data.EngineConfig.APITarget != "" {
		return []string{data.EngineConfig.APITarget}
	}

	switch engine.(type) {
	case *CopilotEngine:
		// Return the full set of known Copilot inference endpoints so that any variant
		// (enterprise, business, individual, or the routing hub) is covered.
		return []string{
			"api.enterprise.githubcopilot.com",
			"api.githubcopilot.com",
			"api.business.githubcopilot.com",
			"api.individual.githubcopilot.com",
		}
	case *ClaudeEngine:
		return []string{"api.anthropic.com"}
	case *CodexEngine:
		if resolveEngineLLMProvider(data, LLMProviderOpenAI) == LLMProviderGitHub {
			return []string{
				"api.enterprise.githubcopilot.com",
				"api.githubcopilot.com",
				"api.business.githubcopilot.com",
				"api.individual.githubcopilot.com",
			}
		}
		return []string{"api.openai.com"}
	case *GeminiEngine:
		return []string{DefaultGeminiAPITarget}
	default:
		// Custom or unknown engine — no known API hosts without explicit api-target.
		return nil
	}
}
