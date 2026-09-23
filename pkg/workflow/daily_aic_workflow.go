package workflow

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/typeutil"
	"github.com/github/gh-aw/pkg/workflow/compilerenv"
)

var dailyAICWorkflowLog = logger.New("workflow:daily_ai_credits")

const maxDailyAICreditsField = "max-daily-ai-credits"
const maxDailyAICreditsEnvVar = "GH_AW_MAX_DAILY_AI_CREDITS"
const maxDailyAICBackendEnvVar = "GH_AW_MAX_DAILY_AI_CREDITS_BACKEND"
const maxDailyAICreditsConfiguredIfExpr = "${{ env.GH_AW_MAX_DAILY_AI_CREDITS != '' }}"
const maxDailyAICBackendRepoMemory = "repo-memory"

// extractMaxDailyAICObjectValue normalizes the max-daily-ai-credits frontmatter
// value for scalar processing. When the value is in the object form
// (e.g. {value: 123, github-app: {...}}), the inner "value" key is extracted
// and returned. When the object form lacks a "value" key, nil is returned so
// that downstream callers treat the limit as unset and fall back to imported or
// default values. When the value is not in object form, it is returned unchanged.
func extractMaxDailyAICObjectValue(raw any) any {
	if m, ok := raw.(map[string]any); ok {
		// Object form: extract "value" if present, otherwise return nil so the
		// limit is treated as unset (the github-app key alone does not set a limit).
		return m["value"]
	}
	return raw
}

// extractMaxDailyAICGitHubApp extracts the optional github-app configuration
// from the object form of the max-daily-ai-credits frontmatter field.
//
//	max-daily-ai-credits:
//	  value: 123
//	  github-app:
//	    client-id: ${{ vars.APP_ID }}
//	    private-key: ${{ secrets.APP_PRIVATE_KEY }}
//
// Returns nil when the field is not in object form or no valid github-app is present.
func extractMaxDailyAICGitHubApp(frontmatter map[string]any) *GitHubAppConfig {
	raw, ok := frontmatter[maxDailyAICreditsField]
	if !ok {
		return nil
	}
	rawMap, ok := raw.(map[string]any)
	if !ok {
		return nil
	}
	appAny, ok := rawMap["github-app"]
	if !ok {
		return nil
	}
	appMap, ok := appAny.(map[string]any)
	if !ok {
		return nil
	}
	app := parseAppConfig(appMap)
	if (app.AppID == "" || app.PrivateKey == "") && !app.IgnoreIfMissing {
		return nil
	}
	return app
}

// extractMaxDailyAICContinueOnError reports whether failures to determine the
// daily AI Credits total should be treated as warnings, reading the
// continue-on-error key directly from an already-unwrapped max-daily-ai-credits
// object (either the main workflow's raw frontmatter value or an imported one).
func extractMaxDailyAICContinueOnError(raw any) (bool, bool) {
	rawMap, ok := raw.(map[string]any)
	if !ok {
		return false, false
	}
	continueOnError, ok := rawMap["continue-on-error"].(bool)
	if !ok {
		return false, false
	}
	return continueOnError, true
}

// extractMaxDailyAICBackend reads the optional backend key from an
// already-unwrapped max-daily-ai-credits object and reports whether it was
// present.
func extractMaxDailyAICBackend(raw any) (string, bool) {
	rawMap, ok := raw.(map[string]any)
	if !ok {
		return "", false
	}
	backend, ok := rawMap["backend"].(string)
	if !ok {
		return "", false
	}
	return strings.TrimSpace(backend), true
}

// resolveMaxDailyAICContinueOnError resolves the effective continue-on-error
// mode for the daily AI Credits guardrail. The main workflow frontmatter takes
// precedence; when it does not set continue-on-error, the first-wins imported
// max-daily-ai-credits object (MergedMaxDailyAICredits) is consulted so
// imported warning-mode configuration is not silently dropped.
func resolveMaxDailyAICContinueOnError(frontmatter map[string]any, importedJSON string) bool {
	if continueOnError, ok := extractMaxDailyAICContinueOnError(frontmatter[maxDailyAICreditsField]); ok {
		return continueOnError
	}
	if importedJSON == "" {
		return false
	}
	var imported any
	if err := json.Unmarshal([]byte(importedJSON), &imported); err != nil {
		dailyAICWorkflowLog.Printf("Failed to unmarshal imported max-daily-ai-credits JSON for continue-on-error resolution: %v", err)
		return false
	}
	continueOnError, _ := extractMaxDailyAICContinueOnError(imported)
	return continueOnError
}

func resolveMaxDailyAICBackend(frontmatter map[string]any, importedJSON string) string {
	if backend, ok := extractMaxDailyAICBackend(frontmatter[maxDailyAICreditsField]); ok {
		return backend
	}
	if importedJSON == "" {
		return ""
	}
	var imported any
	if err := json.Unmarshal([]byte(importedJSON), &imported); err != nil {
		dailyAICWorkflowLog.Printf("Failed to unmarshal imported max-daily-ai-credits JSON for backend resolution: %v", err)
		return ""
	}
	backend, _ := extractMaxDailyAICBackend(imported)
	return backend
}

func resolveMaxDailyAICBackendIntoWorkflowData(data *WorkflowData, frontmatter map[string]any, importedJSON string) {
	if data == nil {
		return
	}
	data.MaxDailyAICBackend = resolveMaxDailyAICBackend(frontmatter, importedJSON)
}

// parseMaxDailyAICValue normalizes max-daily-ai-credits
// values into a runtime-ready string.
//
// Supported inputs:
//   - positive integers
//   - positive numeric strings
//   - GitHub Actions expressions (${{
//     ... }}) preserved verbatim for runtime evaluation
//
// Returns a pointer to the normalized runtime string when valid; nil means the
// field is unset, explicitly disabled, or invalid for runtime use.
func parseMaxDailyAICValue(raw any) *string {
	if normalized, ok := normalizePositiveAICLimit(raw); ok {
		s := normalized
		return &s
	}

	rawStr, ok := raw.(string)
	if !ok {
		return nil
	}

	rawStr = strings.TrimSpace(rawStr)
	if rawStr == "" {
		return nil
	}
	if isExpression(rawStr) {
		return &rawStr
	}
	return nil
}

// isDisabledAICValue reports whether an already-extracted scalar value
// represents an explicit disable (i.e. equals -1). Call this when the value
// has already been unwrapped by extractMaxDailyAICObjectValue to avoid
// a redundant extraction pass.
func isDisabledAICValue(value any) bool {
	if val, ok := typeutil.ParseIntValue(value); ok {
		return val == -1
	}
	rawStr, ok := value.(string)
	if !ok {
		return false
	}
	return strings.TrimSpace(rawStr) == "-1"
}

func isMaxDailyAICDisabled(raw any) bool {
	return isDisabledAICValue(extractMaxDailyAICObjectValue(raw))
}

func resolveMaxDailyAICFromRaw(raw any) (*string, bool) {
	value := extractMaxDailyAICObjectValue(raw)
	if isDisabledAICValue(value) {
		return nil, true
	}
	if parsed := parseMaxDailyAICValue(value); parsed != nil {
		return parsed, true
	}
	return nil, false
}

func resolveMaxDailyAIC(frontmatter map[string]any, importedJSON string) *string {
	if value, found := resolveMaxDailyAICFromRaw(frontmatter[maxDailyAICreditsField]); found {
		dailyAICWorkflowLog.Print("Resolved max-daily-ai-credits from workflow frontmatter")
		return value
	}
	if importedJSON == "" {
		dailyAICWorkflowLog.Print("No frontmatter value and no imported config; falling back to default max-daily-ai-credits")
		expr := compilerenv.BuildDefaultMaxDailyAICreditsExpression(constants.DefaultMaxDailyAICredits)
		return parseMaxDailyAICValue(expr)
	}
	var imported any
	if err := json.Unmarshal([]byte(importedJSON), &imported); err != nil {
		dailyAICWorkflowLog.Printf("Failed to unmarshal imported max-daily-ai-credits JSON, using default: %v", err)
		expr := compilerenv.BuildDefaultMaxDailyAICreditsExpression(constants.DefaultMaxDailyAICredits)
		return parseMaxDailyAICValue(expr)
	}
	if value, found := resolveMaxDailyAICFromRaw(imported); found {
		dailyAICWorkflowLog.Print("Resolved max-daily-ai-credits from imported config")
		return value
	}
	dailyAICWorkflowLog.Print("Imported config did not provide a usable value; falling back to default max-daily-ai-credits")
	expr := compilerenv.BuildDefaultMaxDailyAICreditsExpression(constants.DefaultMaxDailyAICredits)
	return parseMaxDailyAICValue(expr)
}

// hasMaxDailyAICGuardrail reports whether compiler should emit the
// daily AI Credits guardrail wiring. The guardrail is enabled by default.
func hasMaxDailyAICGuardrail(data *WorkflowData) bool {
	return !hasWorkflowExplicitMaxDailyAICDisable(data)
}

func hasWorkflowExplicitMaxDailyAICDisable(data *WorkflowData) bool {
	if data == nil || data.RawFrontmatter == nil {
		return false
	}
	return isMaxDailyAICDisabled(data.RawFrontmatter[maxDailyAICreditsField])
}

// hasMaxDailyAICFrontmatterConfig reports whether the daily AI Credits threshold
// is configured via the max-daily-ai-credits frontmatter/import/default resolution.
// The resolved value is propagated to activation job env so runtime expressions can gate
// setup and guardrail execution consistently.
func hasMaxDailyAICFrontmatterConfig(data *WorkflowData) bool {
	return data != nil && data.MaxDailyAICredits != nil && strings.TrimSpace(*data.MaxDailyAICredits) != ""
}

// validateMaxDailyAICFrontmatter returns an error when the
// max-daily-ai-credits frontmatter field
// is set to an integer below -1. Zero, positive values, and -1 (explicit disable)
// are accepted; GitHub Actions expressions are passed through unchanged for
// runtime evaluation.
func validateMaxDailyAICFrontmatter(data *WorkflowData) error {
	if data == nil {
		return nil
	}
	raw, ok := data.RawFrontmatter[maxDailyAICreditsField]
	if ok {
		backend, hasBackend := extractMaxDailyAICBackend(raw)
		// Object form: require a "value" key and validate the value.
		if m, ok := raw.(map[string]any); ok {
			if _, hasValue := m["value"]; !hasValue {
				return fmt.Errorf("%s object form requires a 'value' field", maxDailyAICreditsField)
			}
		}
		effective := extractMaxDailyAICObjectValue(raw)
		if val, ok := typeutil.ParseIntValue(effective); ok && val < -1 {
			return fmt.Errorf("%s must be -1 (disable) or a positive integer, got %d", maxDailyAICreditsField, val)
		}
		if hasBackend {
			switch backend {
			case "", maxDailyAICBackendRepoMemory:
			default:
				return fmt.Errorf("%s backend must be %q, got %q", maxDailyAICreditsField, maxDailyAICBackendRepoMemory, backend)
			}
		}
	}
	if data.MaxDailyAICBackend == maxDailyAICBackendRepoMemory && (data.RepoMemoryConfig == nil || len(data.RepoMemoryConfig.Memories) == 0) {
		return fmt.Errorf("%s backend %q requires tools.repo-memory", maxDailyAICreditsField, maxDailyAICBackendRepoMemory)
	}
	return nil
}
