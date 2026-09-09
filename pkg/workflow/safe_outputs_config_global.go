package workflow

import (
	"math"
	"strconv"
	"strings"

	"github.com/github/gh-aw/pkg/logger"
)

// extractGlobalConfigFields parses safe-outputs fields that apply across handlers,
// keeping extractSafeOutputsConfig focused on routing handler-specific configuration.
//
//nolint:largefunc // Legacy global configuration extraction is intentionally centralized.
func (c *Compiler) extractGlobalConfigFields(outputMap map[string]any, config *SafeOutputsConfig) {
	// Handle steering issue mode.
	if steer, exists := outputMap["steer"]; exists {
		if steerBool, ok := steer.(bool); ok {
			config.Steer = steerBool
		}
	}

	// Parse allowed-domains configuration (additional domains, unioned with network.allowed; supports ecosystem identifiers)
	if allowedDomains, exists := outputMap["allowed-domains"]; exists {
		if domainsArray, ok := allowedDomains.([]any); ok {
			var domainStrings []string
			for _, domain := range domainsArray {
				if domainStr, ok := domain.(string); ok {
					domainStrings = append(domainStrings, domainStr)
				}
			}
			config.AllowedDomains = domainStrings
			safeOutputsConfigLog.Printf("Configured allowed-domains with %d domain(s)", len(domainStrings))
		}
	}

	// Parse URL sanitization policy
	if urls, exists := outputMap["urls"]; exists {
		if urlsStr, ok := urls.(string); ok {
			config.URLs = SafeOutputsURLsPolicy(urlsStr)
		}
	}

	// Parse safe-outputs.data configuration (false, true, inline schema object, or expression).
	if data, exists := outputMap["data"]; exists {
		config.Data = data
	}

	// Parse allowed-github-references configuration
	if allowGitHubRefs, exists := outputMap["allowed-github-references"]; exists {
		if refsArray, ok := allowGitHubRefs.([]any); ok {
			refStrings := []string{} // Initialize as empty slice, not nil
			for _, ref := range refsArray {
				if refStr, ok := ref.(string); ok {
					refStrings = append(refStrings, refStr)
				}
			}
			config.AllowGitHubReferences = refStrings
		}
	}

	// Handle staged flag
	if err := preprocessBoolFieldAsString(outputMap, "staged", safeOutputsConfigLog); err != nil {
		safeOutputsConfigLog.Printf("staged: %v", err)
	} else if staged, exists := outputMap["staged"]; exists {
		if stagedStr, ok := staged.(string); ok && stagedStr != "" {
			value := TemplatableBool(stagedStr)
			config.Staged = &value
		}
	}
	if c.forceStaged {
		value := TemplatableBool("true")
		config.Staged = &value
	}

	// Handle env configuration
	if env, exists := outputMap["env"]; exists {
		if envMap, ok := env.(map[string]any); ok {
			config.Env = make(map[string]string)
			for key, value := range envMap {
				if valueStr, ok := value.(string); ok {
					config.Env[key] = valueStr
				}
			}
		}
	}

	// Handle github-token configuration
	if githubToken, exists := outputMap["github-token"]; exists {
		if githubTokenStr, ok := githubToken.(string); ok {
			config.GitHubToken = githubTokenStr
		}
	}

	if linearToken, exists := outputMap["linear-token"]; exists {
		if linearTokenStr, ok := linearToken.(string); ok {
			config.LinearToken = linearTokenStr
		}
	}

	// Handle max-patch-size configuration
	config.MaximumPatchSize = parseBoundedIntFieldOrDefault(outputMap, "max-patch-size", 4096, safeOutputsConfigLog)

	// Handle max-patch-files configuration (maximum unique files allowed in
	// a create-pull-request patch). parseBoundedIntField centralizes the
	// overflow checks, clamping, and float truncation behavior shared with
	// the other global bounded integer fields.
	config.MaximumPatchFiles = parseBoundedIntFieldOrDefault(outputMap, "max-patch-files", 100, safeOutputsConfigLog)

	// Handle threat-detection
	threatDetectionConfig := c.parseThreatDetectionConfig(outputMap)
	if threatDetectionConfig != nil {
		config.ThreatDetection = threatDetectionConfig
	}

	// Handle runs-on configuration
	if runsOn, exists := outputMap["runs-on"]; exists {
		config.RunsOn = renderRunsOnSnippet(runsOn)
	}

	// Handle timeout-minutes configuration
	if timeoutMinutes, ok := parseBoundedIntField(outputMap, "timeout-minutes", safeOutputsConfigLog); ok {
		config.TimeoutMinutes = timeoutMinutes
	}
	// The safe-outputs job applies its 45-minute default at render time in
	// compiler_safe_outputs_job.go, so extraction only preserves explicit overrides.

	// Handle messages configuration
	if messages, exists := outputMap["messages"]; exists {
		if messagesMap, ok := messages.(map[string]any); ok {
			config.Messages = parseMessagesConfig(messagesMap)
		}
	}

	// Handle activation-comments at safe-outputs top level (templatable boolean)
	if err := preprocessBoolFieldAsString(outputMap, "activation-comments", safeOutputsConfigLog); err != nil {
		safeOutputsConfigLog.Printf("activation-comments: %v", err)
	}
	if activationComments, exists := outputMap["activation-comments"]; exists {
		if activationCommentsStr, ok := activationComments.(string); ok && activationCommentsStr != "" {
			if config.Messages == nil {
				config.Messages = &SafeOutputMessagesConfig{}
			}
			config.Messages.ActivationComments = activationCommentsStr
		}
	}

	// Handle mentions configuration
	if mentions, exists := outputMap["mentions"]; exists {
		config.Mentions = parseMentionsConfig(mentions)
	}

	// Handle global footer flag
	if footer, exists := outputMap["footer"]; exists {
		if footerBool, ok := footer.(bool); ok {
			config.Footer = &footerBool
			safeOutputsConfigLog.Printf("Global footer control: %t", footerBool)
		}
	}

	// Handle group-reports flag
	if groupReports, exists := outputMap["group-reports"]; exists {
		if groupReportsBool, ok := groupReports.(bool); ok {
			config.GroupReports = groupReportsBool
			safeOutputsConfigLog.Printf("Group reports control: %t", groupReportsBool)
		}
	}

	// Handle report-failure-as-issue as templatable bool or array of categories.
	if reportFailureAsIssue, exists := outputMap["report-failure-as-issue"]; exists {
		// Support []any category filters.
		if categoriesList, ok := reportFailureAsIssue.([]any); ok {
			// Parse as array of category strings, separating included (no prefix) and excluded (! prefix)
			includedCategories := make([]string, 0, len(categoriesList))
			excludedCategories := make([]string, 0, len(categoriesList))
			for _, cat := range categoriesList {
				if catStr, ok := cat.(string); ok {
					if category, isExcluded := strings.CutPrefix(catStr, "!"); isExcluded {
						// Excluded category: "!" prefix was found and removed
						excludedCategories = append(excludedCategories, category)
					} else {
						// Included category: no prefix
						includedCategories = append(includedCategories, catStr)
					}
				}
			}
			reportAsIssue := TemplatableBool("true")
			config.ReportFailureAsIssue = &reportAsIssue
			config.ReportFailureAsIssueCategories = includedCategories
			config.ReportFailureAsIssueExcludedCategories = excludedCategories
			if len(includedCategories) > 0 && len(excludedCategories) > 0 {
				safeOutputsConfigLog.Printf("Report failure as issue with include filter: %v, exclude filter: %v", includedCategories, excludedCategories)
			} else if len(includedCategories) > 0 {
				safeOutputsConfigLog.Printf("Report failure as issue with include filter: %v", includedCategories)
			} else if len(excludedCategories) > 0 {
				safeOutputsConfigLog.Printf("Report failure as issue with exclude filter: %v", excludedCategories)
			}
		} else {
			// Support bool and templatable string values.
			if err := preprocessBoolFieldAsString(outputMap, "report-failure-as-issue", safeOutputsConfigLog); err != nil {
				safeOutputsConfigLog.Printf("Failed to preprocess report-failure-as-issue field: %v (ignoring invalid value and leaving field unset)", err)
			} else {
				if reportFailureAsIssueStr, ok := outputMap["report-failure-as-issue"].(string); ok {
					reportAsIssue := TemplatableBool(reportFailureAsIssueStr)
					config.ReportFailureAsIssue = &reportAsIssue
					safeOutputsConfigLog.Printf("Report failure as issue: %s", reportAsIssue.String())
				} else if reportFailureAsIssueBool, ok := outputMap["report-failure-as-issue"].(bool); ok {
					reportAsIssue := TemplatableBool(strconv.FormatBool(reportFailureAsIssueBool))
					config.ReportFailureAsIssue = &reportAsIssue
					safeOutputsConfigLog.Printf("Report failure as issue: %t", reportFailureAsIssueBool)
				}
			}
		}
	}

	// Handle failure-issue-repo (repository for failure issues, format: "owner/repo")
	if failureIssueRepo, exists := outputMap["failure-issue-repo"]; exists {
		if failureIssueRepoStr, ok := failureIssueRepo.(string); ok && failureIssueRepoStr != "" {
			config.FailureIssueRepo = failureIssueRepoStr
			safeOutputsConfigLog.Printf("Failure issue repo: %s", failureIssueRepoStr)
		}
	}

	// Handle report-failed-jobs flag (bool, default true)
	if reportFailedJobs, exists := outputMap["report-failed-jobs"]; exists {
		if reportFailedJobsBool, ok := reportFailedJobs.(bool); ok {
			config.ReportFailedJobs = &reportFailedJobsBool
			safeOutputsConfigLog.Printf("Report failed jobs: %t", reportFailedJobsBool)
		}
	}

	// Handle max-bot-mentions (templatable integer)
	if err := preprocessIntFieldAsString(outputMap, "max-bot-mentions", safeOutputsConfigLog); err != nil {
		safeOutputsConfigLog.Printf("max-bot-mentions: %v", err)
	} else if maxBotMentions, exists := outputMap["max-bot-mentions"]; exists {
		if maxBotMentionsStr, ok := maxBotMentions.(string); ok {
			config.MaxBotMentions = &maxBotMentionsStr
		}
	}

	// Handle steps (user-provided steps injected after checkout/setup, before safe-output code)
	if steps, exists := outputMap["steps"]; exists {
		if stepsList, ok := steps.([]any); ok {
			config.Steps = stepsList
			safeOutputsConfigLog.Printf("Configured %d user-provided steps for safe-outputs", len(stepsList))
		}
	}

	// Handle id-token permission override ("write" to force-add, "none" to disable auto-detection)
	if idToken, exists := outputMap["id-token"]; exists {
		if idTokenStr, ok := idToken.(string); ok {
			if idTokenStr == "write" || idTokenStr == "none" {
				config.IDToken = &idTokenStr
				safeOutputsConfigLog.Printf("Configured id-token permission override: %s", idTokenStr)
			} else {
				safeOutputsConfigLog.Printf("Warning: unrecognized safe-outputs id-token value %q (expected \"write\" or \"none\"); ignoring", idTokenStr)
			}
		}
	}

	// Handle concurrency-group configuration
	if concurrencyGroup, exists := outputMap["concurrency-group"]; exists {
		if concurrencyGroupStr, ok := concurrencyGroup.(string); ok && concurrencyGroupStr != "" {
			config.ConcurrencyGroup = concurrencyGroupStr
			safeOutputsConfigLog.Printf("Configured concurrency-group for safe-outputs job: %s", concurrencyGroupStr)
		}
	}

	// Handle needs configuration
	if needsValue, exists := outputMap["needs"]; exists {
		if needsArray, ok := needsValue.([]any); ok {
			for _, need := range needsArray {
				if needStr, ok := need.(string); ok && needStr != "" {
					config.Needs = append(config.Needs, needStr)
				}
			}
			if len(config.Needs) > 0 {
				safeOutputsConfigLog.Printf("Configured %d explicit safe-outputs needs dependency(ies)", len(config.Needs))
			}
		}
	}

	// Handle environment configuration (override for safe-outputs job; falls back to top-level environment)
	config.Environment = c.extractTopLevelYAMLSection(outputMap, "environment")
	if config.Environment != "" {
		safeOutputsConfigLog.Printf("Configured environment override for safe-outputs job: %s", config.Environment)
	}

	// Handle jobs (safe-jobs must be under safe-outputs)
	if jobs, exists := outputMap["jobs"]; exists {
		if jobsMap, ok := jobs.(map[string]any); ok {
			config.Jobs = c.parseSafeJobsConfig(jobsMap)
		}
	}

	// Handle scripts (inline handlers that run in the safe-output handler loop)
	if scripts, exists := outputMap["scripts"]; exists {
		if scriptsMap, ok := scripts.(map[string]any); ok {
			config.Scripts = parseSafeScriptsConfig(scriptsMap)
			safeOutputsConfigLog.Printf("Configured %d custom safe-output script(s)", len(config.Scripts))
		}
	}

	// Handle actions (custom GitHub Actions mounted as safe output tools)
	if actions, exists := outputMap["actions"]; exists {
		if actionsMap, ok := actions.(map[string]any); ok {
			config.Actions = parseActionsConfig(actionsMap)
			safeOutputsConfigLog.Printf("Configured %d custom safe-output action(s)", len(config.Actions))
		}
	}

	// Handle app configuration for GitHub App token minting
	if app, exists := outputMap["github-app"]; exists {
		if appMap, ok := app.(map[string]any); ok {
			config.GitHubApp = parseAppConfig(appMap)
		}
	}
}

// parseBoundedIntField parses a positive integer field from a heterogeneous YAML map.
// It accepts int, int64, uint64, and float64 values, clamps integer overflow to math.MaxInt,
// logs float truncation, and rejects non-positive, NaN, infinite, or otherwise invalid values.
func parseBoundedIntField(configMap map[string]any, key string, debugLog *logger.Logger) (int, bool) {
	raw, exists := configMap[key]
	if !exists {
		return 0, false
	}

	switch v := raw.(type) {
	case int:
		if v >= 1 {
			return v, true
		}
	case int64:
		if v < 1 {
			return 0, false
		}
		if v > int64(math.MaxInt) {
			debugLog.Printf("%s: int64 value %d exceeds platform int range, clamping to %d", key, v, math.MaxInt)
			return math.MaxInt, true
		}
		return int(v), true
	case uint64:
		if v < 1 {
			return 0, false
		}
		if v > uint64(math.MaxInt) {
			debugLog.Printf("%s: uint64 value %d exceeds platform int range, clamping to %d", key, v, math.MaxInt)
			return math.MaxInt, true
		}
		return int(v), true
	case float64:
		// float64 loses integer precision near MaxInt on 64-bit platforms, so treat
		// values at or above the rounded float boundary conservatively as out of range.
		maxIntFloat := float64(math.MaxInt)
		if v < 1 || v >= maxIntFloat || math.IsNaN(v) || math.IsInf(v, 0) {
			debugLog.Printf("%s: float value %.2f is out of range, ignoring", key, v)
			return 0, false
		}
		intVal := int(v)
		if v != float64(intVal) {
			debugLog.Printf("%s: float value %.2f truncated to integer %d", key, v, intVal)
		}
		if intVal >= 1 {
			return intVal, true
		}
	default:
		debugLog.Printf("%s: unsupported type %T, ignoring", key, raw)
	}

	return 0, false
}

func parseBoundedIntFieldOrDefault(configMap map[string]any, key string, defaultValue int, debugLog *logger.Logger) int {
	if value, ok := parseBoundedIntField(configMap, key, debugLog); ok {
		return value
	}
	return defaultValue
}
