package workflow

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/github/gh-aw/pkg/stringutil"

	"github.com/github/gh-aw/pkg/console"
	"github.com/github/gh-aw/pkg/logger"
)

var stopAfterLog = logger.New("workflow:stop_after")

// parseOnStopAfterValue extracts and validates the "stop-after" key from an on: map.
// This is the single source of truth for reading on.stop-after so that the typed
// FrontmatterConfig.OnStopAfter field (populated in ParseFrontmatterConfig) and
// extractStopAfterFromOn never diverge on how the raw key is interpreted.
// Accepts either a literal string (relative delta or absolute timestamp) or a
// GitHub Actions expression (e.g. "${{ inputs.stop-after }}"), which is passed
// through verbatim for evaluation at workflow runtime.
func parseOnStopAfterValue(onMap map[string]any) (string, error) {
	if onMap == nil {
		return "", nil
	}
	stopAfter, exists := onMap["stop-after"]
	if !exists {
		return "", nil
	}
	str, ok := stopAfter.(string)
	if !ok {
		return "", fmt.Errorf("stop-after value must be a string, got %T. Example: stop-after: \"+1d\"", stopAfter)
	}
	return str, nil
}

// extractStopAfterFromOn extracts the stop-after value from the on: section
func (c *Compiler) extractStopAfterFromOn(frontmatter map[string]any, workflowData ...*WorkflowData) (string, error) {
	// Prefer the typed field populated by ParseFrontmatterConfig when available, so
	// production code has a single source of truth instead of reparsing the raw map.
	// ParseFrontmatterConfig silently leaves OnStopAfter empty on a parse error (e.g. a
	// non-string value), so fall back to re-parsing the raw on: map in that edge case to
	// surface the original compile error instead of silently treating it as unset.
	if len(workflowData) > 0 && workflowData[0] != nil && workflowData[0].ParsedFrontmatter != nil {
		pf := workflowData[0].ParsedFrontmatter
		if pf.OnStopAfter != "" {
			return pf.OnStopAfter, nil
		}
		return parseOnStopAfterValue(pf.On)
	}

	// Fallback: no typed ParsedFrontmatter available, so parse the raw frontmatter map.
	onSection, exists := frontmatter["on"]

	if !exists {
		return "", nil
	}

	// Handle different formats of the on: section
	switch on := onSection.(type) {
	case string:
		// Simple string format like "on: push" - no stop-after possible
		return "", nil
	case map[string]any:
		// Complex object format - look for stop-after via the shared typed-field parser
		return parseOnStopAfterValue(on)
	default:
		return "", errors.New("invalid on: section format. Expected a string or an object of triggers. Example:\non:\n  issues:\n    types: [opened]")
	}
}

// logStopAfterExpressionPassthrough logs (and, in verbose mode, prints) that a
// GitHub Actions expression stop-after value is being passed through verbatim
// rather than resolved at compile time.
func (c *Compiler) logStopAfterExpressionPassthrough(stopTime string) {
	stopAfterLog.Printf("Stop-after value is a GitHub Actions expression, passing through verbatim: %s", stopTime)
	if c.verbose {
		fmt.Fprintln(os.Stderr, console.FormatInfoMessage("Stop-after is a GitHub Actions expression, resolved at runtime: "+stopTime))
	}
}

// processStopAfterConfiguration extracts and processes stop-after configuration from frontmatter
func (c *Compiler) processStopAfterConfiguration(frontmatter map[string]any, workflowData *WorkflowData, markdownPath string) error {
	stopAfterLog.Printf("Processing stop-after configuration for workflow: %s", markdownPath)
	// Extract stop-after from the on: section
	stopAfter, err := c.extractStopAfterFromOn(frontmatter, workflowData)
	if err != nil {
		return err
	}
	workflowData.StopTime = stopAfter

	// GitHub Actions expressions (e.g. "${{ inputs.stop-after }}") are resolved at
	// workflow runtime, not at compile time, so pass them through verbatim without
	// attempting to parse them as a relative delta or absolute timestamp.
	if isExpression(workflowData.StopTime) {
		c.logStopAfterExpressionPassthrough(workflowData.StopTime)
		return nil
	}

	if workflowData.StopTime == "" {
		return nil
	}
	return c.resolveAndApplyStopTime(workflowData, markdownPath, stopAfter)
}

// resolveAndApplyStopTime resolves a relative or absolute stop-after value to an
// absolute timestamp, preserving an existing lock file's stop time across
// recompilations unless the refresh flag is set.
func (c *Compiler) resolveAndApplyStopTime(workflowData *WorkflowData, markdownPath string, originalStopTime string) error {
	stopAfterLog.Printf("Stop-after value specified: %s", workflowData.StopTime)
	// Check if there's already a lock file with a stop time (recompilation case)
	lockFile := stringutil.MarkdownToLockFile(markdownPath)
	existingStopTime := ExtractStopTimeFromLockFile(lockFile)

	// If refresh flag is set, always regenerate the stop time
	if c.refreshStopTime {
		stopAfterLog.Print("Refresh flag set, regenerating stop time")
		resolvedStopTime, err := resolveStopTime(workflowData.StopTime, time.Now().UTC())
		if err != nil {
			return fmt.Errorf("invalid stop-after format, expected a relative delta such as \"+25h\" or an absolute timestamp such as \"2025-01-15 14:30:00\": %w", err)
		}
		workflowData.StopTime = resolvedStopTime
		stopAfterLog.Printf("Resolved stop time from %s to %s", originalStopTime, resolvedStopTime)

		if c.verbose && isRelativeStopTime(originalStopTime) {
			fmt.Fprintln(os.Stderr, console.FormatInfoMessage("Refreshed relative stop-after to: "+resolvedStopTime))
		} else if c.verbose && originalStopTime != resolvedStopTime {
			fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("Refreshed absolute stop-after from '%s' to: %s", originalStopTime, resolvedStopTime)))
		}
		return nil
	}

	if existingStopTime != "" {
		// Preserve existing stop time during recompilation (default behavior)
		stopAfterLog.Printf("Preserving existing stop time from lock file: %s", existingStopTime)
		workflowData.StopTime = existingStopTime
		if c.verbose {
			fmt.Fprintln(os.Stderr, console.FormatInfoMessage("Preserving existing stop time from lock file: "+existingStopTime))
		}
		return nil
	}

	// First compilation or no existing stop time, generate new one
	stopAfterLog.Print("First compilation, generating new stop time")
	resolvedStopTime, err := resolveStopTime(workflowData.StopTime, time.Now().UTC())
	if err != nil {
		return fmt.Errorf("invalid stop-after format, expected a relative delta such as \"+25h\" or an absolute timestamp such as \"2025-01-15 14:30:00\": %w", err)
	}
	workflowData.StopTime = resolvedStopTime

	if c.verbose && isRelativeStopTime(originalStopTime) {
		fmt.Fprintln(os.Stderr, console.FormatInfoMessage("Resolved relative stop-after to: "+resolvedStopTime))
	} else if c.verbose && originalStopTime != resolvedStopTime {
		fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("Parsed absolute stop-after from '%s' to: %s", originalStopTime, resolvedStopTime)))
	}
	return nil
}

// resolveStopTime resolves a stop-time value to an absolute timestamp
// If the stop-time is relative (starts with '+'), it calculates the absolute time
// from the compilation time. Otherwise, it parses the absolute time using various formats.
func resolveStopTime(stopTime string, compilationTime time.Time) (string, error) {
	if stopTime == "" {
		return "", nil
	}

	if isRelativeStopTime(stopTime) {
		// Parse the relative time delta (minutes not allowed for stop-after)
		delta, err := parseTimeDeltaForStopAfter(stopTime)
		if err != nil {
			return "", err
		}

		// Calculate absolute time in UTC using precise calculation
		// Always use AddDate for months, weeks, and days for maximum precision
		absoluteTime := compilationTime.UTC()
		absoluteTime = absoluteTime.AddDate(0, delta.Months, delta.Weeks*7+delta.Days)
		absoluteTime = absoluteTime.Add(time.Duration(delta.Hours)*time.Hour + time.Duration(delta.Minutes)*time.Minute)

		// Format in the expected format: "YYYY-MM-DD HH:MM:SS"
		return absoluteTime.Format("2006-01-02 15:04:05"), nil
	}

	// Parse absolute date-time with flexible format support
	return parseAbsoluteDateTime(stopTime)
}

// ExtractStopTimeFromLockFile extracts the STOP_TIME value from a compiled workflow lock file
func ExtractStopTimeFromLockFile(lockFilePath string) string {
	content, err := os.ReadFile(lockFilePath)
	if err != nil {
		return ""
	}

	contentStr := string(content)

	// Try to extract from metadata first
	metadata, isLegacy, err := ExtractMetadataFromLockFile(contentStr)

	// If metadata extraction failed with an error (malformed JSON), log warning but don't fall back
	// This is different from no metadata (which is intentional for legacy files)
	if err != nil {
		stopAfterLog.Printf("Warning: Failed to parse metadata from %s: %v. Falling back to legacy extraction.", lockFilePath, err)
		// Malformed metadata - fall through to legacy extraction as a safety measure
		// but this indicates a potential issue with the lock file
	} else if metadata != nil && metadata.StopTime != "" {
		// Successfully extracted stop time from metadata
		stopAfterLog.Printf("Extracted stop time from metadata: %s", metadata.StopTime)
		return metadata.StopTime
	}

	// Validate lock file schema compatibility before parsing
	// Non-critical operation - continue even if validation fails
	if err := ValidateLockSchemaCompatibility(contentStr, lockFilePath); err != nil {
		stopAfterLog.Printf("Warning: Lock file schema validation failed for %s: %v", lockFilePath, err)
		// Continue anyway for legacy compatibility
	}

	// Legacy fallback: Look for GH_AW_STOP_TIME in the workflow body
	// Use legacy method if: no metadata, legacy format, metadata exists but stop_time is empty, or metadata was malformed
	if err != nil || metadata == nil || isLegacy || metadata.StopTime == "" {
		lines := strings.SplitSeq(contentStr, "\n")
		for line := range lines {
			// Look for GH_AW_STOP_TIME: YYYY-MM-DD HH:MM:SS
			// This is in the env section of the stop time check job
			if strings.Contains(line, "GH_AW_STOP_TIME:") {
				prefix := "GH_AW_STOP_TIME:"
				if _, after, ok := strings.Cut(line, prefix); ok {
					extracted := strings.TrimSpace(after)
					// Strip surrounding quotes added by newer compiler versions
					extracted = strings.Trim(extracted, "\"")
					return extracted
				}
			}
		}
	}
	return ""
}

// extractSkipIfMatchFromOn extracts the skip-if-match value from the on: section
func (c *Compiler) extractSkipIfMatchFromOn(frontmatter map[string]any, workflowData ...*WorkflowData) (*SkipIfMatchConfig, error) {
	// Use cached On field from ParsedFrontmatter if available (when workflowData is provided)
	var onSection any
	var exists bool
	if len(workflowData) > 0 && workflowData[0] != nil && workflowData[0].ParsedFrontmatter != nil && workflowData[0].ParsedFrontmatter.On != nil {
		onSection = workflowData[0].ParsedFrontmatter.On
		exists = true
	} else {
		onSection, exists = frontmatter["on"]
	}

	if !exists {
		return nil, nil
	}

	// Handle different formats of the on: section
	switch on := onSection.(type) {
	case string:
		// Simple string format like "on: push" - no skip-if-match possible
		return nil, nil
	case map[string]any:
		// Complex object format - look for skip-if-match
		if skipIfMatch, exists := on["skip-if-match"]; exists {
			// Handle both string and object formats
			switch skip := skipIfMatch.(type) {
			case string:
				// Simple string format: skip-if-match: "query" (implies max=1)
				return &SkipIfMatchConfig{
					Query: skip,
					Max:   1,
				}, nil
			case map[string]any:
				// Object format: skip-if-match: { query: "...", max: 3 }
				queryVal, hasQuery := skip["query"]
				if !hasQuery {
					return nil, errors.New("skip-if-match object must have a 'query' field. Example:\n  skip-if-match:\n    query: \"is:issue is:open\"\n    max: 3")
				}

				queryStr, ok := queryVal.(string)
				if !ok {
					return nil, fmt.Errorf("skip-if-match 'query' field must be a string, got %T. Example:\n  skip-if-match:\n    query: \"is:issue is:open\"", queryVal)
				}

				// Extract max value (optional, defaults to 1)
				maxVal := 1
				if maxRaw, hasMax := skip["max"]; hasMax {
					switch m := maxRaw.(type) {
					case int:
						maxVal = m
					case int64:
						maxVal = int(m)
					case uint64:
						maxVal = int(m)
					case float64:
						maxVal = int(m)
					default:
						return nil, fmt.Errorf("skip-if-match 'max' field must be an integer, got %T. Example: max: 3", maxRaw)
					}

					if maxVal < 1 {
						return nil, fmt.Errorf("skip-if-match 'max' field must be at least 1, got %d. Example:\n  skip-if-match:\n    query: \"is:issue is:open\"\n    max: 3", maxVal)
					}
				}

				// Extract scope (auth is now configured via top-level on.github-app / on.github-token)
				scope, err := extractSkipIfScope(skip, "skip-if-match")
				if err != nil {
					return nil, err
				}

				return &SkipIfMatchConfig{
					Query: queryStr,
					Max:   maxVal,
					Scope: scope,
				}, nil
			default:
				return nil, fmt.Errorf("skip-if-match value must be a string or object, got %T. Example:\n  skip-if-match: \"is:issue is:open\"\n  skip-if-match:\n    query: \"is:pr is:open\"\n    max: 3", skipIfMatch)
			}
		}
		return nil, nil
	default:
		return nil, errors.New("invalid on: section format. Expected a string or an object of triggers. Example:\non:\n  issues:\n    types: [opened]")
	}
}

// extractSkipIfNoMatchFromOn extracts the skip-if-no-match value from the on: section
func (c *Compiler) extractSkipIfNoMatchFromOn(frontmatter map[string]any, workflowData ...*WorkflowData) (*SkipIfNoMatchConfig, error) {
	// Use cached On field from ParsedFrontmatter if available (when workflowData is provided)
	var onSection any
	var exists bool
	if len(workflowData) > 0 && workflowData[0] != nil && workflowData[0].ParsedFrontmatter != nil && workflowData[0].ParsedFrontmatter.On != nil {
		onSection = workflowData[0].ParsedFrontmatter.On
		exists = true
	} else {
		onSection, exists = frontmatter["on"]
	}

	if !exists {
		return nil, nil
	}

	// Handle different formats of the on: section
	switch on := onSection.(type) {
	case string:
		// Simple string format like "on: push" - no skip-if-no-match possible
		return nil, nil
	case map[string]any:
		// Complex object format - look for skip-if-no-match
		if skipIfNoMatch, exists := on["skip-if-no-match"]; exists {
			// Handle both string and object formats
			switch skip := skipIfNoMatch.(type) {
			case string:
				// Simple string format: skip-if-no-match: "query" (implies min=1)
				return &SkipIfNoMatchConfig{
					Query: skip,
					Min:   1,
				}, nil
			case map[string]any:
				// Object format: skip-if-no-match: { query: "...", min: 3 }
				queryVal, hasQuery := skip["query"]
				if !hasQuery {
					return nil, errors.New("skip-if-no-match object must have a 'query' field. Example:\n  skip-if-no-match:\n    query: \"is:pr is:open\"\n    min: 3")
				}

				queryStr, ok := queryVal.(string)
				if !ok {
					return nil, fmt.Errorf("skip-if-no-match 'query' field must be a string, got %T. Example:\n  skip-if-no-match:\n    query: \"is:pr is:open\"", queryVal)
				}

				// Extract min value (optional, defaults to 1)
				minVal := 1
				if minRaw, hasMin := skip["min"]; hasMin {
					switch m := minRaw.(type) {
					case int:
						minVal = m
					case int64:
						minVal = int(m)
					case uint64:
						minVal = int(m)
					case float64:
						minVal = int(m)
					default:
						return nil, fmt.Errorf("skip-if-no-match 'min' field must be an integer, got %T. Example: min: 3", minRaw)
					}

					if minVal < 1 {
						return nil, fmt.Errorf("skip-if-no-match 'min' field must be at least 1, got %d. Example:\n  skip-if-no-match:\n    query: \"is:pr is:open\"\n    min: 3", minVal)
					}
				}

				// Extract scope (auth is now configured via top-level on.github-app / on.github-token)
				scope, err := extractSkipIfScope(skip, "skip-if-no-match")
				if err != nil {
					return nil, err
				}

				return &SkipIfNoMatchConfig{
					Query: queryStr,
					Min:   minVal,
					Scope: scope,
				}, nil
			default:
				return nil, fmt.Errorf("skip-if-no-match value must be a string or object, got %T. Example:\n  skip-if-no-match: \"is:pr is:open\"\n  skip-if-no-match:\n    query: \"is:pr is:open\"\n    min: 3", skipIfNoMatch)
			}
		}
		return nil, nil
	default:
		return nil, errors.New("invalid on: section format. Expected a string or an object of triggers. Example:\non:\n  issues:\n    types: [opened]")
	}
}

// processSkipIfMatchConfiguration extracts and processes skip-if-match configuration from frontmatter
func (c *Compiler) processSkipIfMatchConfiguration(frontmatter map[string]any, workflowData *WorkflowData) error {
	// Extract skip-if-match from the on: section
	skipIfMatchConfig, err := c.extractSkipIfMatchFromOn(frontmatter, workflowData)
	if err != nil {
		return err
	}
	workflowData.SkipIfMatch = skipIfMatchConfig

	if workflowData.SkipIfMatch != nil {
		if workflowData.SkipIfMatch.Max == 1 {
			stopAfterLog.Printf("Skip-if-match query configured: %s (max: 1 match)", workflowData.SkipIfMatch.Query)
		} else {
			stopAfterLog.Printf("Skip-if-match query configured: %s (max: %d matches)", workflowData.SkipIfMatch.Query, workflowData.SkipIfMatch.Max)
		}
	}

	return nil
}

// processSkipIfNoMatchConfiguration extracts and processes skip-if-no-match configuration from frontmatter
func (c *Compiler) processSkipIfNoMatchConfiguration(frontmatter map[string]any, workflowData *WorkflowData) error {
	// Extract skip-if-no-match from the on: section
	skipIfNoMatchConfig, err := c.extractSkipIfNoMatchFromOn(frontmatter, workflowData)
	if err != nil {
		return err
	}
	workflowData.SkipIfNoMatch = skipIfNoMatchConfig

	if workflowData.SkipIfNoMatch != nil {
		if workflowData.SkipIfNoMatch.Min == 1 {
			stopAfterLog.Printf("Skip-if-no-match query configured: %s (min: 1 match)", workflowData.SkipIfNoMatch.Query)
		} else {
			stopAfterLog.Printf("Skip-if-no-match query configured: %s (min: %d matches)", workflowData.SkipIfNoMatch.Query, workflowData.SkipIfNoMatch.Min)
		}
	}

	return nil
}

// extractSkipIfCheckFailingFromOn extracts the skip-if-check-failing value from the on: section
func (c *Compiler) extractSkipIfCheckFailingFromOn(frontmatter map[string]any, workflowData ...*WorkflowData) (*SkipIfCheckFailingConfig, error) {
	// Use cached On field from ParsedFrontmatter if available (when workflowData is provided)
	var onSection any
	var exists bool
	if len(workflowData) > 0 && workflowData[0] != nil && workflowData[0].ParsedFrontmatter != nil && workflowData[0].ParsedFrontmatter.On != nil {
		onSection = workflowData[0].ParsedFrontmatter.On
		exists = true
	} else {
		onSection, exists = frontmatter["on"]
	}

	if !exists {
		return nil, nil
	}

	// Handle different formats of the on: section
	switch on := onSection.(type) {
	case string:
		// Simple string format like "on: push" - no skip-if-check-failing possible
		return nil, nil
	case map[string]any:
		// Complex object format - look for skip-if-check-failing
		if skipIfCheckFailing, exists := on["skip-if-check-failing"]; exists {
			switch skip := skipIfCheckFailing.(type) {
			case nil:
				// Null value (bare key with no value): skip-if-check-failing:
				return &SkipIfCheckFailingConfig{}, nil
			case bool:
				// Simple boolean format: skip-if-check-failing: true
				if !skip {
					return nil, errors.New("skip-if-check-failing: false is not valid; remove the field to disable the check")
				}
				return &SkipIfCheckFailingConfig{}, nil
			case map[string]any:
				// Object format: skip-if-check-failing: { include: [...], exclude: [...], branch: "...", allow-pending: true }
				config := &SkipIfCheckFailingConfig{}

				// Extract include list (optional)
				if includeRaw, hasInclude := skip["include"]; hasInclude {
					includeSlice, ok := includeRaw.([]any)
					if !ok {
						return nil, errors.New("skip-if-check-failing 'include' field must be a list of strings. Example:\n  skip-if-check-failing:\n    include:\n      - build\n      - test")
					}
					for _, item := range includeSlice {
						s, ok := item.(string)
						if !ok {
							return nil, fmt.Errorf("skip-if-check-failing 'include' list items must be strings, got %T. Example:\n  skip-if-check-failing:\n    include:\n      - build", item)
						}
						config.Include = append(config.Include, s)
					}
				}

				// Extract exclude list (optional)
				if excludeRaw, hasExclude := skip["exclude"]; hasExclude {
					excludeSlice, ok := excludeRaw.([]any)
					if !ok {
						return nil, errors.New("skip-if-check-failing 'exclude' field must be a list of strings. Example:\n  skip-if-check-failing:\n    exclude:\n      - lint")
					}
					for _, item := range excludeSlice {
						s, ok := item.(string)
						if !ok {
							return nil, fmt.Errorf("skip-if-check-failing 'exclude' list items must be strings, got %T. Example:\n  skip-if-check-failing:\n    exclude:\n      - lint", item)
						}
						config.Exclude = append(config.Exclude, s)
					}
				}

				// Extract branch (optional)
				if branchRaw, hasBranch := skip["branch"]; hasBranch {
					branchStr, ok := branchRaw.(string)
					if !ok {
						return nil, fmt.Errorf("skip-if-check-failing 'branch' field must be a string, got %T. Example: branch: main", branchRaw)
					}
					config.Branch = branchStr
				}

				// Extract allow-pending (optional, defaults to false — pending counts as failing)
				if allowPendingRaw, hasAllowPending := skip["allow-pending"]; hasAllowPending {
					allowPending, ok := allowPendingRaw.(bool)
					if !ok {
						return nil, fmt.Errorf("skip-if-check-failing 'allow-pending' field must be a boolean, got %T. Example: allow-pending: true", allowPendingRaw)
					}
					config.AllowPending = allowPending
				}

				return config, nil
			default:
				return nil, fmt.Errorf("skip-if-check-failing value must be true or an object, got %T. Example:\n  skip-if-check-failing:\n  skip-if-check-failing: true\n  skip-if-check-failing:\n    include:\n      - build\n    branch: main\n    allow-pending: true", skipIfCheckFailing)
			}
		}
		return nil, nil
	default:
		return nil, errors.New("invalid on: section format. Expected a string or an object of triggers. Example:\non:\n  issues:\n    types: [opened]")
	}
}

// processSkipIfCheckFailingConfiguration extracts and processes skip-if-check-failing configuration from frontmatter
func (c *Compiler) processSkipIfCheckFailingConfiguration(frontmatter map[string]any, workflowData *WorkflowData) error {
	skipIfCheckFailingConfig, err := c.extractSkipIfCheckFailingFromOn(frontmatter, workflowData)
	if err != nil {
		return err
	}
	workflowData.SkipIfCheckFailing = skipIfCheckFailingConfig

	if workflowData.SkipIfCheckFailing != nil {
		stopAfterLog.Printf("Skip-if-check-failing configured: include=%v, exclude=%v, branch=%q, allow-pending=%v",
			workflowData.SkipIfCheckFailing.Include,
			workflowData.SkipIfCheckFailing.Exclude,
			workflowData.SkipIfCheckFailing.Branch,
			workflowData.SkipIfCheckFailing.AllowPending,
		)
	}

	return nil
}

// object configuration. Auth fields (github-token, github-app) are configured at the top-level
// on: section and are no longer accepted inside skip-if blocks.
// conditionName is used only for error messages (e.g. "skip-if-match").
func extractSkipIfScope(skip map[string]any, conditionName string) (string, error) {
	if scopeRaw, hasScope := skip["scope"]; hasScope {
		scopeStr, ok := scopeRaw.(string)
		if !ok {
			return "", fmt.Errorf("%s 'scope' field must be a string, got %T. Example: scope: none", conditionName, scopeRaw)
		}
		if scopeStr != "none" {
			return "", fmt.Errorf("%s 'scope' field must be \"none\" or omitted, got %q. Example: scope: none", conditionName, scopeStr)
		}
		return scopeStr, nil
	}
	return "", nil
}
