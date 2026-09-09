package constants

import "github.com/github/gh-aw/pkg/setutil"

// EngineName represents an AI engine name identifier (copilot, claude, codex, custom).
// This semantic type distinguishes engine names from arbitrary strings,
// making engine selection explicit and type-safe.
//
// Example usage:
//
//	const CopilotEngine EngineName = "copilot"
//	func SetEngine(engine EngineName) error { ... }
type EngineName string

// Agentic engine name constants using EngineName type for type safety
const (
	// CopilotEngine is the GitHub Copilot engine identifier
	CopilotEngine EngineName = "copilot"
	// ClaudeEngine is the Anthropic Claude engine identifier
	ClaudeEngine EngineName = "claude"
	// CodexEngine is the OpenAI Codex engine identifier
	CodexEngine EngineName = "codex"
	// GeminiEngine is the Google Gemini engine identifier
	GeminiEngine EngineName = "gemini"
	// PiEngine is the Pi engine identifier
	PiEngine EngineName = "pi"

	// DefaultEngine is the default agentic engine used when no engine is explicitly specified.
	// Currently defaults to CopilotEngine.
	DefaultEngine EngineName = CopilotEngine
)

// AgenticEngines lists all supported agentic engine names.
// Deprecated: Use workflow.NewEngineCatalog(workflow.NewEngineRegistry()).IDs() for a
// catalog-derived list. This slice is maintained for backward compatibility and must
// stay in sync with the built-in engines registered in NewEngineCatalog.
var AgenticEngines = []string{string(ClaudeEngine), string(CodexEngine), string(CopilotEngine), string(GeminiEngine), string(PiEngine)}

// EngineOption represents a selectable AI engine with its display metadata and secret configuration
type EngineOption struct {
	Value              string
	Label              string
	Description        string
	SecretName         string   // The name of the secret required for this engine (e.g., "COPILOT_GITHUB_TOKEN")
	AlternativeSecrets []string // Alternative secret names that can also be used for this engine
	EnvVarName         string   // Alternative environment variable name if different from SecretName (optional)
	KeyURL             string   // URL where users can obtain their API key/token (may be empty if not applicable)
	WhenNeeded         string   // Human-readable description of when this secret is needed
}

// EngineOptions provides the list of available AI engines for user selection.
// Each entry includes secret metadata used by the interactive add wizard.
// Must stay in sync with the built-in engines registered in workflow.NewEngineCatalog;
// the TestEngineCatalogMatchesSchema test in pkg/workflow catches catalog/schema drift.
var EngineOptions = []EngineOption{
	{
		Value:       string(CopilotEngine),
		Label:       "GitHub Copilot",
		Description: "GitHub Copilot CLI with agent support",
		SecretName:  CopilotGitHubToken,
		KeyURL:      "https://github.com/settings/personal-access-tokens/new",
		WhenNeeded:  "Copilot workflows (CLI, engine, agent tasks, etc.)",
	},
	{
		Value:              string(ClaudeEngine),
		Label:              "Claude",
		Description:        "Anthropic Claude Code coding agent",
		SecretName:         AnthropicAPIKey,
		AlternativeSecrets: []string{},
		KeyURL:             "https://console.anthropic.com/settings/keys",
		WhenNeeded:         "Claude engine workflows",
	},
	{
		Value:              string(CodexEngine),
		Label:              "Codex",
		Description:        "OpenAI Codex/GPT engine",
		SecretName:         OpenAIAPIKey,
		AlternativeSecrets: []string{CodexAPIKey},
		KeyURL:             "https://platform.openai.com/api-keys",
		WhenNeeded:         "Codex/OpenAI engine workflows",
	},
	{
		Value:       string(GeminiEngine),
		Label:       "Gemini",
		Description: "Google Gemini CLI coding agent",
		SecretName:  GeminiAPIKey,
		KeyURL:      "https://aistudio.google.com/app/apikey",
		WhenNeeded:  "Gemini engine workflows",
	},
	{
		Value:              string(PiEngine),
		Label:              "Pi",
		Description:        "Pi AI coding agent",
		SecretName:         CopilotGitHubToken,
		AlternativeSecrets: []string{AnthropicAPIKey, OpenAIAPIKey, CodexAPIKey},
		KeyURL:             "https://github.com/settings/personal-access-tokens/new",
		WhenNeeded:         "Pi engine workflows",
	},
}

// SystemSecretSpec describes a system-level secret that is not engine-specific
type SystemSecretSpec struct {
	Name        string
	WhenNeeded  string
	Description string
	Optional    bool
}

// SystemSecrets defines system-level secrets that are not tied to a specific engine
var SystemSecrets = []SystemSecretSpec{
	{
		Name:        "GH_AW_GITHUB_TOKEN",
		WhenNeeded:  "Enables the use of a user identity for GitHub write operations (instead of github-actions identity); may enable cross-repo project ops; may permit use of remote GitHub MCP tools",
		Description: "Fine-grained or classic PAT with contents/issues/pull-requests read+write and other necessary permissions on the repos gh-aw will read or write.",
		Optional:    true,
	},
	{
		Name:        "GH_AW_AGENT_TOKEN",
		WhenNeeded:  "Assigning agents/bots to issues or pull requests",
		Description: "PAT for agent assignment with issues and pull-requests write on the repos where agents act.",
		Optional:    true,
	},
	{
		Name:        "GH_AW_GITHUB_MCP_SERVER_TOKEN",
		WhenNeeded:  "Isolating MCP server permissions (advanced, optional)",
		Description: "Optional read-mostly token for the GitHub MCP server when you want different scopes than GH_AW_GITHUB_TOKEN.",
		Optional:    true,
	},
}

// GetEngineOption returns the EngineOption for the given engine value, or nil if not found
func GetEngineOption(engineValue string) *EngineOption {
	for i := range EngineOptions {
		if EngineOptions[i].Value == engineValue {
			return &EngineOptions[i]
		}
	}
	return nil
}

// GetAllEngineSecretNames returns all unique secret names across all configured engines.
// This includes primary secrets, alternative secrets, and system-level secrets.
// The returned slice contains no duplicates.
func GetAllEngineSecretNames() []string {
	seen := make(map[string]struct {
	})
	var secrets []string

	// Add primary and alternative secrets from all engines
	for _, opt := range EngineOptions {
		if opt.SecretName != "" && !setutil.Contains(seen, opt.SecretName) {
			seen[opt.SecretName] = struct {
			}{}
			secrets = append(secrets, opt.SecretName)
		}
		for _, alt := range opt.AlternativeSecrets {
			if alt != "" && !setutil.Contains(seen, alt) {
				seen[alt] = struct {
				}{}
				secrets = append(secrets, alt)
			}
		}
	}

	// Add system-level secrets from SystemSecrets
	for _, s := range SystemSecrets {
		if s.Name != "" && !setutil.Contains(seen, s.Name) {
			seen[s.Name] = struct {
			}{}
			secrets = append(secrets, s.Name)
		}
	}

	return secrets
}

// Primary secret names for each engine — the GitHub secret names that users configure
// when setting up an engine workflow.
const (
	// CopilotGitHubToken is the GitHub token secret name required by the Copilot engine.
	CopilotGitHubToken = "COPILOT_GITHUB_TOKEN"
	// AnthropicAPIKey is the API key secret name required by the Claude engine.
	AnthropicAPIKey = "ANTHROPIC_API_KEY"
	// CodexAPIKey is the API key secret name used by the Codex engine.
	CodexAPIKey = "CODEX_API_KEY"
	// OpenAIAPIKey is the OpenAI API key secret name used by the Codex engine as an alternative.
	OpenAIAPIKey = "OPENAI_API_KEY"
	// GeminiAPIKey is the API key secret name required by the Gemini engine.
	GeminiAPIKey = "GEMINI_API_KEY"
)

// Environment variable names for model configuration
const (
	// EnvVarModelAgentCopilot configures the default Copilot model for agent execution
	EnvVarModelAgentCopilot = "GH_AW_MODEL_AGENT_COPILOT"
	// EnvVarModelAgentClaude configures the default Claude model for agent execution
	EnvVarModelAgentClaude = "GH_AW_MODEL_AGENT_CLAUDE"
	// EnvVarModelAgentCodex configures the default Codex model for agent execution
	EnvVarModelAgentCodex = "GH_AW_MODEL_AGENT_CODEX"
	// EnvVarModelAgentCustom configures the default Custom model for agent execution
	EnvVarModelAgentCustom = "GH_AW_MODEL_AGENT_CUSTOM"
	// EnvVarModelAgentGemini configures the default Gemini model for agent execution
	EnvVarModelAgentGemini = "GH_AW_MODEL_AGENT_GEMINI"
	// EnvVarModelDetectionCopilot configures the default Copilot model for detection
	EnvVarModelDetectionCopilot = "GH_AW_MODEL_DETECTION_COPILOT"
	// EnvVarModelDetectionClaude configures the default Claude model for detection
	EnvVarModelDetectionClaude = "GH_AW_MODEL_DETECTION_CLAUDE"
	// EnvVarModelDetectionCodex configures the default Codex model for detection
	EnvVarModelDetectionCodex = "GH_AW_MODEL_DETECTION_CODEX"
	// EnvVarModelDetectionGemini configures the default Gemini model for detection
	EnvVarModelDetectionGemini = "GH_AW_MODEL_DETECTION_GEMINI"
	// EnvVarModelEvalsCopilot configures the default Copilot model for evals execution
	EnvVarModelEvalsCopilot = "GH_AW_MODEL_EVALS_COPILOT"
	// EnvVarModelEvalsClaude configures the default Claude model for evals execution
	EnvVarModelEvalsClaude = "GH_AW_MODEL_EVALS_CLAUDE"
	// EnvVarModelEvalsCodex configures the default Codex model for evals execution
	EnvVarModelEvalsCodex = "GH_AW_MODEL_EVALS_CODEX"
	// EnvVarModelEvalsGemini configures the default Gemini model for evals execution
	EnvVarModelEvalsGemini = "GH_AW_MODEL_EVALS_GEMINI"
	// EnvVarModelAgentPi configures the default Pi model for agent execution
	EnvVarModelAgentPi = "GH_AW_MODEL_AGENT_PI"
	// EnvVarModelEvalsPi configures the default Pi model for evals execution
	EnvVarModelEvalsPi = "GH_AW_MODEL_EVALS_PI"

	// EnvVarModelFallback carries the standard org/enterprise/built-in model fallback
	// expression for the current engine step. Runtime JavaScript harnesses can use this
	// when a configured model expression resolves to an empty string.
	EnvVarModelFallback = "GH_AW_MODEL_FALLBACK"

	// CopilotCLIModelEnvVar is the native environment variable name supported by the Copilot CLI
	// for selecting the model. Setting this env var is equivalent to passing --model to the CLI.
	CopilotCLIModelEnvVar = "COPILOT_MODEL"

	// COPILOT_PROVIDER_* environment variables activate Bring Your Own Key (BYOK) mode,
	// routing Copilot CLI requests to an external LLM provider instead of GitHub's routing.
	//
	// CopilotProviderBaseURL (REQUIRED for BYOK) is the base URL of the external provider.
	// Setting this variable activates BYOK mode. Example: https://api.openai.com/v1
	CopilotProviderBaseURL = "COPILOT_PROVIDER_BASE_URL"

	// CopilotProviderAPIKey (OPTIONAL) is the API key for authenticating to the external provider.
	// Required for cloud APIs (e.g., OpenAI, Anthropic). Not needed for local providers (Ollama, vLLM).
	CopilotProviderAPIKey = "COPILOT_PROVIDER_API_KEY"

	// CopilotProviderBearerToken (OPTIONAL) is an alternative to CopilotProviderAPIKey.
	// Sent as a Bearer token in the Authorization header. Takes precedence over COPILOT_PROVIDER_API_KEY.
	CopilotProviderBearerToken = "COPILOT_PROVIDER_BEARER_TOKEN"

	// CopilotProviderWireAPI (OPTIONAL) selects the HTTP wire API used when talking to a BYOK provider.
	// Accepted values: "responses" (OpenAI Responses API), "completions" (legacy Chat Completions API).
	// The Copilot CLI defaults to "completions" for custom providers; set to "responses" for Azure
	// o-series models (e.g. o4-mini) that reject the legacy endpoint with HTTP 400.
	CopilotProviderWireAPI = "COPILOT_PROVIDER_WIRE_API"

	// CopilotCLIIntegrationIDEnvVar is the native environment variable name supported by the Copilot CLI
	// for identifying the calling integration. This tells the Copilot CLI that it is being invoked
	// by agentic workflows.
	CopilotCLIIntegrationIDEnvVar = "GITHUB_COPILOT_INTEGRATION_ID"

	// CopilotCLIIntegrationIDValue is the value of the integration ID for agentic workflows.
	CopilotCLIIntegrationIDValue = "agentic-workflows"

	// CopilotSDKURIEnvVar is the environment variable name for the Copilot SDK URI.
	// When copilot-sdk: true is set, this env var holds the HTTP endpoint URI
	// where the harness-managed Copilot CLI sidecar listens for SDK connections
	// (e.g. "http://127.0.0.1:3002"). It is set on every child process so that the @github/copilot-sdk
	// library can locate the running Copilot HTTP server.
	CopilotSDKURIEnvVar = "COPILOT_SDK_URI"

	// CopilotSDKServerArgsEnvVar is the environment variable that holds the JSON-encoded
	// CLI argument array for the headless Copilot CLI sidecar started by copilot_harness.cjs
	// in GH_AW_COPILOT_SDK_DRIVER mode.
	// The array includes all server control and configuration flags
	// (--headless, --no-auto-update, --port, --add-dir, --log-level, etc.)
	// that the engine computes at compile time. The harness reads this variable at
	// runtime to start the sidecar without any argument parsing.
	CopilotSDKServerArgsEnvVar = "GH_AW_COPILOT_SDK_SERVER_ARGS"

	// CopilotSDKToolConfigEnvVar is the environment variable that holds the
	// compiler-owned SDK tool contract. The built-in SDK driver uses this
	// contract for model-visible tool filtering, permission enforcement, and
	// deterministic custom-tool registration.
	CopilotSDKToolConfigEnvVar = "GH_AW_COPILOT_SDK_TOOL_CONFIG"

	// CopilotSDKDriverEnvVar is set to "1" when the copilot_sdk_driver.cjs program
	// is used as the execution command instead of inline SDK handling inside the harness.
	// The harness checks this flag to run the driver as a regular subprocess via runProcess
	// while still managing sidecar start/stop itself.
	CopilotSDKDriverEnvVar = "GH_AW_COPILOT_SDK_DRIVER"

	// CopilotBYOKDummyAPIKey is the placeholder API key used to trigger AWF's
	// runtime BYOK detection for Copilot offline mode. The real credential remains
	// isolated in the AWF API proxy sidecar.
	CopilotBYOKDummyAPIKey = "dummy-byok-key-for-offline-mode"

	// CopilotBYOKDummyAPIKeyEnvVar is the environment variable that holds the
	// CopilotBYOKDummyAPIKey sentinel value in generated lock files. Using a
	// non-*_API_KEY-shaped name for the literal value prevents secret scanners from
	// flagging the generated artifact. COPILOT_API_KEY is then exported in the run:
	// shell script via shell variable expansion so the value is never written
	// inline next to a *_API_KEY key in the YAML env: block.
	CopilotBYOKDummyAPIKeyEnvVar = "COPILOT_DUMMY_BYOK"

	// SonnetDefaultModel is the shared Sonnet-tier fallback model used when workflows
	// need an explicit default and no engine-specific override is configured.
	SonnetDefaultModel = "claude-sonnet-5"

	// CopilotBYOKDefaultModel is the explicit fallback model used when no Copilot model
	// is configured and the corresponding GH_AW_MODEL_*_COPILOT variable is unset.
	//
	// "auto" lets the Copilot API select the best available model automatically.
	CopilotBYOKDefaultModel = "auto"

	// CodexDefaultModel is the default model for the Codex agentic engine.
	// Used as the fallback when no explicit model is configured and the
	// GH_AW_MODEL_AGENT_CODEX / GH_AW_MODEL_DETECTION_CODEX variable is unset.
	CodexDefaultModel = "gpt-5.4"

	// AgentDefaultModel is the model display string returned for engines whose model is
	// dynamically determined by the AI provider (e.g. Claude, Gemini, Pi).
	// It is used as the GH_AW_INFO_MODEL value when no explicit model is configured.
	AgentDefaultModel = "agent"

	// ClaudeCLIModelEnvVar is the native environment variable name supported by the Claude Code CLI
	// for selecting the model. Setting this env var is equivalent to passing --model to the CLI.
	ClaudeCLIModelEnvVar = "ANTHROPIC_MODEL"

	// GeminiCLIModelEnvVar is the native environment variable name supported by the Gemini CLI
	// for selecting the model. Setting this env var is equivalent to passing --model to the CLI.
	GeminiCLIModelEnvVar = "GEMINI_MODEL"

	// PiCLIModelEnvVar is the native environment variable name for Pi model selection.
	// Setting PI_MODEL is equivalent to passing --model to the Pi CLI.
	PiCLIModelEnvVar = "PI_MODEL"

	// Common environment variable names used across all engines

	// EnvVarPrompt is the path to the workflow prompt file
	EnvVarPrompt = "GH_AW_PROMPT"

	// EnvVarMCPConfig is the path to the MCP configuration file
	EnvVarMCPConfig = "GH_AW_MCP_CONFIG"

	// EnvVarSafeOutputs is the safe-outputs configuration JSON
	EnvVarSafeOutputs = "GH_AW_SAFE_OUTPUTS"

	// EnvVarMaxTurns is the maximum number of turns for agent execution
	EnvVarMaxTurns = "GH_AW_MAX_TURNS"

	// EnvVarMaxToolDenials is the maximum number of repeated tool denials
	// allowed in Copilot SDK driver mode before inference is stopped.
	EnvVarMaxToolDenials = "GH_AW_MAX_TOOL_DENIALS"

	// EnvVarStartupTimeout is the tool startup timeout in seconds
	EnvVarStartupTimeout = "GH_AW_STARTUP_TIMEOUT"

	// EnvVarToolTimeout is the tool execution timeout in seconds
	EnvVarToolTimeout = "GH_AW_TOOL_TIMEOUT"

	// EnvVarGitHubToken is the GitHub token for repository access
	EnvVarGitHubToken = "GH_AW_GITHUB_TOKEN"

	// EnvVarGitHubMCPServerToken is the optional token for the GitHub MCP server.
	// When set, it takes precedence over GH_AW_GITHUB_TOKEN for GitHub MCP server requests.
	EnvVarGitHubMCPServerToken = "GH_AW_GITHUB_MCP_SERVER_TOKEN"

	// EnvVarGitHubBlockedUsers is the fallback variable for the tools.github.blocked-users guard policy field.
	// When blocked-users is not explicitly set in the workflow frontmatter, this variable is used as
	// a comma- or newline-separated list of GitHub usernames to block. Set as an org or repo variable
	// to apply a consistent block list across all workflows.
	EnvVarGitHubBlockedUsers = "GH_AW_GITHUB_BLOCKED_USERS"

	// EnvVarGitHubApprovalLabels is the fallback variable for the tools.github.approval-labels guard policy field.
	// When approval-labels is not explicitly set in the workflow frontmatter, this variable is used as
	// a comma- or newline-separated list of GitHub label names that promote content to "approved" integrity.
	// Set as an org or repo variable to apply a consistent approval label list across all workflows.
	EnvVarGitHubApprovalLabels = "GH_AW_GITHUB_APPROVAL_LABELS"

	// EnvVarGitHubTrustedUsers is the fallback variable for the tools.github.trusted-users guard policy field.
	// When trusted-users is not explicitly set in the workflow frontmatter, this variable is used as
	// a comma- or newline-separated list of GitHub usernames elevated to "approved" integrity.
	// Set as an org or repo variable to apply a consistent trusted user list across all workflows.
	EnvVarGitHubTrustedUsers = "GH_AW_GITHUB_TRUSTED_USERS"
)

const (
	// DefaultMaxToolDenials is the default maximum number of repeated tool
	// denials allowed in Copilot SDK mode before stopping inference.
	DefaultMaxToolDenials = 5
)

// CopilotStemCommands defines commands that Copilot CLI treats as "stem" commands
// (commands with subcommand matching via tree-sitter). When these appear in bash
// tool lists without an explicit `:*` wildcard, the compiler automatically appends
// `:*` to ensure proper permission matching for all subcommands.
//
// For example, `bash: ["dotnet"]` compiles to `--allow-tool shell(dotnet:*)` so that
// "dotnet build", "dotnet test", etc. are all permitted.
//
// This list must be kept in sync with the stem configurations in Copilot CLI's
// shell command parser (tree-sitter based).
//
// NOTE: Network tools like curl and wget are also included here because the Copilot CLI
// treats them as stem commands — shell(curl) only matches a bare "curl" invocation with
// no arguments. Using shell(curl:*) ensures that "curl -s ...", "curl --max-time 30 ...",
// etc. are all permitted when bash: ["curl"] is specified.
var CopilotStemCommands = map[string]bool{
	"git": true, "gh": true, "glab": true,
	"npm": true, "npx": true, "yarn": true, "pnpm": true,
	"cargo": true, "go": true,
	"composer": true, "pip": true, "pipenv": true, "poetry": true, "conda": true,
	"mvn": true, "gradle": true, "gradlew": true,
	"dotnet": true,
	"bundle": true, "swift": true, "sbt": true, "flutter": true,
	"mix": true, "cabal": true, "stack": true,
	"curl": true, "wget": true,
}
