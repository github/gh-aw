package workflow

import (
	"encoding/json"
	"fmt"

	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/parser"
)

var workflowGitHubAppLog = logger.New("workflow:workflow_github_app")

// extractTopLevelGitHubApp extracts the 'github-app' field from the top-level frontmatter.
// This provides a single GitHub App configuration that serves as a fallback for all nested
// github-app token minting operations (on, safe-outputs, checkout, tools.github, dependencies).
func extractTopLevelGitHubApp(frontmatter map[string]any) *GitHubAppConfig {
	appAny, ok := frontmatter["github-app"]
	if !ok {
		return nil
	}
	appMap, ok := appAny.(map[string]any)
	if !ok {
		return nil
	}
	app := parseAppConfig(appMap)
	if app.AppID == "" || app.PrivateKey == "" {
		return nil
	}
	return app
}

func extractTypedTopLevelGitHubApp(frontmatter map[string]any) (*GitHubAppConfig, error) {
	appRaw, exists := frontmatter["github-app"]
	if !exists || appRaw == nil {
		return nil, nil
	}
	if appMap, ok := appRaw.(map[string]any); ok {
		return extractTopLevelGitHubApp(map[string]any{"github-app": appMap}), nil
	}
	if appMap, ok := appRaw.(map[string]string); ok {
		converted := make(map[string]any, len(appMap))
		for key, value := range appMap {
			converted[key] = value
		}
		return extractTopLevelGitHubApp(map[string]any{"github-app": converted}), nil
	}
	return nil, fmt.Errorf("github-app has an unsupported type, expected an object: %T", appRaw)
}

// resolveTopLevelGitHubApp resolves the top-level github-app for token minting fallback.
// Precedence:
//  1. Current workflow's top-level github-app (explicit override wins)
//  2. First top-level github-app found across imported shared workflows
//  3. Nil (no fallback configured)
func resolveTopLevelGitHubApp(frontmatter map[string]any, importsResult *parser.ImportsResult) *GitHubAppConfig {
	if app := extractTopLevelGitHubApp(frontmatter); app != nil {
		return app
	}
	if importsResult != nil && importsResult.MergedTopLevelGitHubApp != "" {
		var appMap map[string]any
		if err := json.Unmarshal([]byte(importsResult.MergedTopLevelGitHubApp), &appMap); err == nil {
			app := parseAppConfig(appMap)
			if app.AppID != "" && app.PrivateKey != "" {
				workflowGitHubAppLog.Print("Using top-level github-app from imported shared workflow")
				return app
			}
		}
	}
	return nil
}

// topLevelFallbackNeeded reports whether the top-level github-app should be applied as a
// fallback for a given section. It returns true when the section has neither an explicit
// github-app nor an explicit github-token already configured.
//
// Rules (consistent across all sections):
//   - If a section-specific github-app is set → keep it, no fallback needed.
//   - If a section-specific github-token is set → keep it, no fallback needed (a token
//     already provides the auth; injecting a github-app would silently change precedence).
//   - Otherwise → apply the top-level fallback.
func topLevelFallbackNeeded(app *GitHubAppConfig, token string) bool {
	return app == nil && token == ""
}

// applyTopLevelGitHubAppFallbacks applies the top-level github-app as a fallback for all
// nested github-app token minting operations when no section-specific github-app is configured.
// Precedence: section-specific github-app > section-specific github-token > top-level github-app.
//
// Every section uses topLevelFallbackNeeded to decide whether the fallback is required,
// ensuring consistent behaviour across all token-minting sites.
func applyTopLevelGitHubAppFallbacks(data *WorkflowData) {
	fallback := data.TopLevelGitHubApp
	if fallback == nil {
		return
	}

	// Fallback for activation (on.github-app / on.github-token)
	if topLevelFallbackNeeded(data.ActivationGitHubApp, data.ActivationGitHubToken) {
		workflowGitHubAppLog.Print("Applying top-level github-app fallback for activation")
		data.ActivationGitHubApp = fallback
	}

	// Fallback for safe-outputs (safe-outputs.github-app / safe-outputs.github-token)
	if data.SafeOutputs != nil && topLevelFallbackNeeded(data.SafeOutputs.GitHubApp, data.SafeOutputs.GitHubToken) {
		workflowGitHubAppLog.Print("Applying top-level github-app fallback for safe-outputs")
		data.SafeOutputs.GitHubApp = fallback
	}

	// Fallback for checkout configs (checkout.github-app / checkout.github-token per entry)
	for _, cfg := range data.CheckoutConfigs {
		if topLevelFallbackNeeded(cfg.GitHubApp, cfg.GitHubToken) {
			workflowGitHubAppLog.Print("Applying top-level github-app fallback for checkout")
			cfg.GitHubApp = fallback
		}
	}

	// Fallback for tools.github (tools.github.github-app / tools.github.github-token).
	// Also skipped when tools.github is explicitly disabled (github: false) — do not re-enable it.
	if data.ParsedTools != nil && data.ParsedTools.GitHub != nil &&
		topLevelFallbackNeeded(data.ParsedTools.GitHub.GitHubApp, data.ParsedTools.GitHub.GitHubToken) &&
		data.Tools["github"] != false {
		applyTopLevelGitHubAppToolFallback(data, fallback)
	}
}

func applyTopLevelGitHubAppToolFallback(data *WorkflowData, fallback *GitHubAppConfig) {
	workflowGitHubAppLog.Print("Applying top-level github-app fallback for tools.github")
	data.ParsedTools.GitHub.GitHubApp = fallback
	// Also update the raw tools map so applyDefaultTools (called from applyDefaults in
	// processOnSectionAndFilters) does not lose the fallback when it rebuilds ParsedTools
	// from the map.
	appMap := map[string]any{
		"client-id":   fallback.AppID,
		"private-key": fallback.PrivateKey,
	}
	if fallback.IgnoreIfMissing {
		appMap["ignore-if-missing"] = true
	}
	if fallback.Owner != "" {
		appMap["owner"] = fallback.Owner
	}
	if len(fallback.Repositories) > 0 {
		repos := make([]any, len(fallback.Repositories))
		for i, r := range fallback.Repositories {
			repos[i] = r
		}
		appMap["repositories"] = repos
	}
	// Normalize data.Tools["github"] to a map so the github-app survives re-parsing.
	// Configurations like `github: true` are normalized here rather than losing the fallback.
	if github, ok := data.Tools["github"].(map[string]any); ok {
		// Already a map; inject into existing settings.
		github["github-app"] = appMap
	} else {
		// Non-map value (e.g. true) — create a fresh map.
		data.Tools["github"] = map[string]any{"github-app": appMap}
	}
}
