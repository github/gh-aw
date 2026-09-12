package compilerenv

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/github/gh-aw/pkg/logger"
)

var managerLog = logger.New("compilerenv:manager")

// EnvGetter is a function type for looking up environment variables.
// It mirrors the signature of os.Getenv and is used to decouple library
// logic from direct process-environment access.
type EnvGetter func(string) string

// Manager resolves enterprise env-var overrides through an injected EnvGetter.
// Construct one with New for explicit env control (e.g., in tests); package-level
// Resolve* convenience functions delegate to a default Manager backed by os.Getenv.
type Manager struct {
	getenv EnvGetter
}

// New creates a Manager using the provided EnvGetter for environment lookups.
func New(getenv EnvGetter) *Manager {
	if getenv == nil {
		getenv = os.Getenv
	}
	return &Manager{getenv: getenv}
}

// defaultManager is the package-level Manager backed by the process environment.
// os.Getenv is passed as a function reference and called lazily on each Resolve*
// invocation, so values reflect the environment at call time.
var defaultManager = New(os.Getenv)

const (
	// DefaultMaxDailyAICredits is the enterprise override for the top-level
	// max-daily-ai-credits guardrail when it is not explicitly configured in
	// workflow frontmatter.
	DefaultMaxDailyAICredits = "GH_AW_DEFAULT_MAX_DAILY_AI_CREDITS"
	// DefaultMaxAICredits is the enterprise override for AWF apiProxy.maxAiCredits
	// when max-ai-credits is not explicitly configured in workflow frontmatter.
	DefaultMaxAICredits = "GH_AW_DEFAULT_MAX_AI_CREDITS"
	// DefaultMaxTurnCacheMisses is the enterprise override for AWF
	// apiProxy.maxCacheMisses when max-turn-cache-misses is not explicitly configured
	// in workflow frontmatter.
	DefaultMaxTurnCacheMisses = "GH_AW_DEFAULT_MAX_TURN_CACHE_MISSES"
	// DefaultDetectionMaxAICredits is the enterprise override for the
	// threat-detection AWF apiProxy.maxAiCredits budget when
	// safe-outputs.threat-detection.max-ai-credits is not explicitly configured.
	DefaultDetectionMaxAICredits = "GH_AW_DEFAULT_DETECTION_MAX_AI_CREDITS"
	// DefaultEvalsMaxAICredits is the enterprise override for the evals AWF
	// apiProxy.maxAiCredits budget when evals.max-ai-credits is not explicitly
	// configured.
	DefaultEvalsMaxAICredits = "GH_AW_DEFAULT_EVALS_MAX_AI_CREDITS"
	// DefaultMaxTurns is the enterprise override for max-turns when it is not
	// explicitly configured in workflow frontmatter.
	DefaultMaxTurns = "GH_AW_DEFAULT_MAX_TURNS"
	// DefaultTimeoutMinutes is the GitHub Actions variable used for the default
	// agentic execution step timeout when top-level timeout-minutes is not set.
	DefaultTimeoutMinutes = "GH_AW_DEFAULT_TIMEOUT_MINUTES"
	// DefaultAgentJobTimeoutMinutes is the GitHub Actions variable used for the
	// default generated agent job timeout when jobs.agent.timeout-minutes is not set.
	DefaultAgentJobTimeoutMinutes = "GH_AW_DEFAULT_AGENT_JOB_TIMEOUT_MINUTES"
	// DefaultDetectionJobTimeoutMinutes is the GitHub Actions variable used for the
	// default generated detection job timeout when jobs.detection.timeout-minutes
	// is not set.
	DefaultDetectionJobTimeoutMinutes = "GH_AW_DEFAULT_DETECTION_JOB_TIMEOUT_MINUTES"
	// DefaultDetectionModel is the enterprise override for selecting the detection
	// job model when threat-detection.engine.model is not set.
	DefaultDetectionModel = "GH_AW_DEFAULT_DETECTION_MODEL"
	// DefaultEvalsModel is the enterprise override for selecting the evals
	// job model when evals.model is not set.
	DefaultEvalsModel = "GH_AW_DEFAULT_EVALS_MODEL"

	// DefaultUTC is the enterprise override for the project home timezone used
	// when rendering local times in CLI output.
	DefaultUTC = "GH_AW_DEFAULT_UTC"

	// DefaultOTLPEndpoint is the GitHub Actions secret or variable used as the
	// fallback OTLP endpoint when observability.otlp.endpoint is not configured
	// in workflow frontmatter (or any imported workflow).
	DefaultOTLPEndpoint = "GH_AW_DEFAULT_OTLP_ENDPOINT"
	// DefaultOTLPHeaders is the GitHub Actions secret used as the fallback OTLP
	// exporter headers (comma-separated key=value pairs) that accompany
	// DefaultOTLPEndpoint.
	DefaultOTLPHeaders = "GH_AW_DEFAULT_OTLP_HEADERS"

	// DefaultModelCopilot is the enterprise override for Copilot fallback model selection.
	DefaultModelCopilot = "GH_AW_DEFAULT_MODEL_COPILOT"
	// DefaultModelClaude is the enterprise override for Claude fallback model selection.
	DefaultModelClaude = "GH_AW_DEFAULT_MODEL_CLAUDE"
	// DefaultModelCodex is the enterprise override for Codex fallback model selection.
	DefaultModelCodex = "GH_AW_DEFAULT_MODEL_CODEX"
	// PolicyStrict enables runtime enforcement that workflows must be compiled in strict mode
	// when GH_AW_POLICY_STRICT is set to the string value "true".
	PolicyStrict = "GH_AW_POLICY_STRICT"
	// PolicyModelsAllowed applies experimental models.allowed policy from env.
	// It intersects with workflow models.allowed and does not change
	// models.blocked unless PolicyModelsBlocked is also set.
	PolicyModelsAllowed = "GHAW_POLICY_MODELS_ALLOWED"
	// PolicyModelsBlocked applies experimental models.blocked policy from env.
	// It unions with workflow models.blocked and does not change models.allowed
	// unless PolicyModelsAllowed is also set.
	PolicyModelsBlocked = "GHAW_POLICY_MODELS_BLOCKED"
	// PolicyAllowCreatePullRequest controls whether create-pull-request safe-outputs
	// remain runtime-compliant. Set to the string value "false" to disable the
	// create_pull_request safe-output tool at runtime.
	PolicyAllowCreatePullRequest = "GH_AW_POLICY_ALLOW_CREATE_PULL_REQUEST"
)

// ResolveDefaultMaxTurns returns fallback when the env var is unset/invalid,
// otherwise returns the parsed override as a string.
func (m *Manager) ResolveDefaultMaxTurns(fallback string) string {
	if parsed, ok := m.parsePositiveIntEnvVar(DefaultMaxTurns); ok {
		return strconv.Itoa(parsed)
	}
	return fallback
}

// ResolveDefaultMaxTurns is a convenience wrapper that delegates to the
// default process-environment Manager.
func ResolveDefaultMaxTurns(fallback string) string {
	return defaultManager.ResolveDefaultMaxTurns(fallback)
}

// ResolveDefaultTimeoutMinutes returns fallback when the env var is unset/invalid,
// otherwise returns the parsed override.
func (m *Manager) ResolveDefaultTimeoutMinutes(fallback int) int {
	if parsed, ok := m.parsePositiveIntEnvVar(DefaultTimeoutMinutes); ok {
		return parsed
	}
	return fallback
}

// ResolveDefaultTimeoutMinutes is a convenience wrapper that delegates to the
// default process-environment Manager.
func ResolveDefaultTimeoutMinutes(fallback int) int {
	return defaultManager.ResolveDefaultTimeoutMinutes(fallback)
}

// BuildTimeoutMinutesExpression builds a GitHub Actions expression that resolves
// a default timeout from an Actions variable at runtime, falling back to the
// compiled-in default when the variable is unset.
func BuildTimeoutMinutesExpression(varName string, builtinDefault int) string {
	return fmt.Sprintf("${{ fromJSON(vars.%s || '%d') }}", varName, builtinDefault)
}

// ResolveDefaultMaxTurnCacheMisses returns fallback when the env var is unset/invalid,
// otherwise returns the parsed override.
func (m *Manager) ResolveDefaultMaxTurnCacheMisses(fallback int) int {
	if parsed, ok := m.parsePositiveIntEnvVar(DefaultMaxTurnCacheMisses); ok {
		return parsed
	}
	return fallback
}

// ResolveDefaultMaxTurnCacheMisses is a convenience wrapper that delegates to
// the default process-environment Manager.
func ResolveDefaultMaxTurnCacheMisses(fallback int) int {
	return defaultManager.ResolveDefaultMaxTurnCacheMisses(fallback)
}

// ResolveDefaultDetectionModel returns fallback when the env var is unset,
// otherwise returns the trimmed override value.
func (m *Manager) ResolveDefaultDetectionModel(fallback string) string {
	raw := strings.TrimSpace(m.getenv(DefaultDetectionModel))
	if raw == "" {
		return fallback
	}
	managerLog.Printf("Applying enterprise detection model override %s=%q (fallback was %q)", DefaultDetectionModel, raw, fallback)
	return raw
}

// ResolveDefaultDetectionModel is a convenience wrapper that delegates to the
// default process-environment Manager.
func ResolveDefaultDetectionModel(fallback string) string {
	return defaultManager.ResolveDefaultDetectionModel(fallback)
}

// ResolveDefaultEvalsModel returns fallback when the env var is unset,
// otherwise returns the trimmed override value.
func (m *Manager) ResolveDefaultEvalsModel(fallback string) string {
	raw := strings.TrimSpace(m.getenv(DefaultEvalsModel))
	if raw == "" {
		return fallback
	}
	managerLog.Printf("Applying enterprise evals model override %s=%q (fallback was %q)", DefaultEvalsModel, raw, fallback)
	return raw
}

// ResolveDefaultEvalsModel is a convenience wrapper that delegates to the
// default process-environment Manager.
func ResolveDefaultEvalsModel(fallback string) string {
	return defaultManager.ResolveDefaultEvalsModel(fallback)
}

// ResolveDefaultUTC returns fallback when the env var is unset, otherwise
// returns the trimmed override value.
func (m *Manager) ResolveDefaultUTC(fallback string) string {
	raw := strings.TrimSpace(m.getenv(DefaultUTC))
	if raw == "" {
		return fallback
	}
	managerLog.Printf("Applying enterprise timezone override %s=%q (fallback was %q)", DefaultUTC, raw, fallback)
	return raw
}

// ResolveDefaultUTC is a convenience wrapper that delegates to the default
// process-environment Manager.
func ResolveDefaultUTC(fallback string) string {
	return defaultManager.ResolveDefaultUTC(fallback)
}

// parsePositiveIntEnvVar parses an environment variable as a base-10 positive int.
// It returns (value, true) when the variable is set to a valid value > 0.
// For unset, empty, non-numeric, or non-positive values, it returns (0, false).
func (m *Manager) parsePositiveIntEnvVar(name string) (int, bool) {
	raw := strings.TrimSpace(m.getenv(name))
	if raw == "" {
		return 0, false
	}
	parsed, err := strconv.Atoi(raw)
	if err != nil || parsed <= 0 {
		return 0, false
	}
	return parsed, true
}

// BuildDefaultMaxDailyAICreditsExpression builds a vars expression that resolves
// max-daily-ai-credits at runtime from the GH_AW_DEFAULT_MAX_DAILY_AI_CREDITS
// GitHub variable, falling back to builtinDefault when the variable is unset.
func BuildDefaultMaxDailyAICreditsExpression(builtinDefault string) string {
	escaped := strings.ReplaceAll(builtinDefault, "'", "''")
	return fmt.Sprintf("${{ vars.%s || '%s' }}", DefaultMaxDailyAICredits, escaped)
}

// BuildDefaultMaxAICreditsExpression builds a vars expression that resolves
// max-ai-credits at runtime from the GH_AW_DEFAULT_MAX_AI_CREDITS GitHub variable,
// falling back to builtinDefault when the variable is unset. The expression is
// embedded in the compiled workflow and evaluated by the GitHub Actions runner.
func BuildDefaultMaxAICreditsExpression(builtinDefault string) string {
	escaped := strings.ReplaceAll(builtinDefault, "'", "''")
	return fmt.Sprintf("${{ vars.%s || '%s' }}", DefaultMaxAICredits, escaped)
}

// BuildDefaultDetectionMaxAICreditsExpression builds a vars expression that resolves
// the threat-detection max-ai-credits default at runtime from the
// GH_AW_DEFAULT_DETECTION_MAX_AI_CREDITS GitHub variable, falling back to
// builtinDefault when the variable is unset.
func BuildDefaultDetectionMaxAICreditsExpression(builtinDefault string) string {
	escaped := strings.ReplaceAll(builtinDefault, "'", "''")
	return fmt.Sprintf("${{ vars.%s || '%s' }}", DefaultDetectionMaxAICredits, escaped)
}

// BuildDefaultEvalsMaxAICreditsExpression builds a vars expression that resolves
// the evals max-ai-credits default at runtime from the
// GH_AW_DEFAULT_EVALS_MAX_AI_CREDITS GitHub variable, falling back to
// builtinDefault when the variable is unset.
func BuildDefaultEvalsMaxAICreditsExpression(builtinDefault string) string {
	escaped := strings.ReplaceAll(builtinDefault, "'", "''")
	return fmt.Sprintf("${{ vars.%s || '%s' }}", DefaultEvalsMaxAICredits, escaped)
}

// BuildDefaultMaxTurnsExpression builds a vars expression that resolves max-turns
// at runtime from the GH_AW_DEFAULT_MAX_TURNS GitHub variable. An empty string is
// returned as the fallback so that an unset variable is treated as "no limit".
func BuildDefaultMaxTurnsExpression() string {
	return fmt.Sprintf("${{ vars.%s || '' }}", DefaultMaxTurns)
}

// BuildDefaultOTLPEndpointExpression builds an expression that resolves the OTLP
// endpoint at runtime from the GH_AW_DEFAULT_OTLP_ENDPOINT GitHub secret, falling
// back to the variable for compatibility. The expression evaluates to an empty
// string when both are unset, which downstream runtime code treats as
// "observability disabled".
func BuildDefaultOTLPEndpointExpression() string {
	return fmt.Sprintf("${{ secrets.%s || vars.%s }}", DefaultOTLPEndpoint, DefaultOTLPEndpoint)
}

// BuildDefaultOTLPHeadersExpression builds a secrets expression that resolves the
// OTLP exporter headers at runtime from the GH_AW_DEFAULT_OTLP_HEADERS GitHub
// secret. The expression evaluates to an empty string when the secret is unset.
func BuildDefaultOTLPHeadersExpression() string {
	return fmt.Sprintf("${{ secrets.%s }}", DefaultOTLPHeaders)
}

// BuildModelOverrideExpression builds a vars expression with primary model var, enterprise
// default model var, and built-in fallback model.
func BuildModelOverrideExpression(primaryVar, enterpriseDefaultVar, builtinFallback string) string {
	escaped := strings.ReplaceAll(builtinFallback, "'", "''")
	return fmt.Sprintf("${{ vars.%s || vars.%s || '%s' }}", primaryVar, enterpriseDefaultVar, escaped)
}

// BuildModelOverrideExpressionEmptyFallback builds a vars expression with primary model var,
// enterprise default model var, and empty string fallback.
func BuildModelOverrideExpressionEmptyFallback(primaryVar, enterpriseDefaultVar string) string {
	return fmt.Sprintf("${{ vars.%s || vars.%s || '' }}", primaryVar, enterpriseDefaultVar)
}

// ResolvePolicyModelsAllowed returns configured allowed model policy entries.
// When the env var is unset/empty, ok=false and callers should use frontmatter policy.
func (m *Manager) ResolvePolicyModelsAllowed() ([]string, bool) {
	return m.resolveModelListEnv(PolicyModelsAllowed)
}

// ResolvePolicyModelsAllowed is a convenience wrapper that delegates to the
// default process-environment Manager.
func ResolvePolicyModelsAllowed() ([]string, bool) {
	return defaultManager.ResolvePolicyModelsAllowed()
}

// ResolvePolicyModelsBlocked returns configured blocked model policy entries.
// When the env var is unset/empty, ok=false and callers should use frontmatter policy.
func (m *Manager) ResolvePolicyModelsBlocked() ([]string, bool) {
	return m.resolveModelListEnv(PolicyModelsBlocked)
}

// ResolvePolicyModelsBlocked is a convenience wrapper that delegates to the
// default process-environment Manager.
func ResolvePolicyModelsBlocked() ([]string, bool) {
	return defaultManager.ResolvePolicyModelsBlocked()
}

func (m *Manager) resolveModelListEnv(name string) ([]string, bool) {
	raw := strings.TrimSpace(m.getenv(name))
	if raw == "" {
		return nil, false
	}
	parts := strings.FieldsFunc(raw, func(r rune) bool {
		return r == ',' || r == '\n' || r == '\r'
	})
	if len(parts) == 0 {
		return nil, false
	}
	result := make([]string, 0, len(parts))
	seen := map[string]struct{}{}
	for _, part := range parts {
		model := strings.TrimSpace(part)
		if model == "" {
			continue
		}
		if strings.ContainsAny(model, " \t") {
			managerLog.Printf("Skipping invalid model policy entry in %s: %q (use comma/newline separators)", name, model)
			continue
		}
		if _, exists := seen[model]; exists {
			continue
		}
		seen[model] = struct{}{}
		result = append(result, model)
	}
	if len(result) == 0 {
		return nil, false
	}
	managerLog.Printf("Applying model policy override %s with %d model(s)", name, len(result))
	return result, true
}
