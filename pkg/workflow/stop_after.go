package workflow

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/github/gh-aw/pkg/stringutil"

	"github.com/github/gh-aw/pkg/console"
	"github.com/github/gh-aw/pkg/logger"
)

var stopAfterLog = logger.New("workflow:stop_after")

// extractStopAfterFromOn extracts the stop-after value from the on: section
func (c *Compiler) extractStopAfterFromOn(frontmatter map[string]any, workflowData ...*WorkflowData) (string, error) {
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
		return "", nil
	}

	// Handle different formats of the on: section
	switch on := onSection.(type) {
	case string:
		// Simple string format like "on: push" - no stop-after possible
		return "", nil
	case map[string]any:
		// Complex object format - look for stop-after
		if stopAfter, exists := on["stop-after"]; exists {
			if str, ok := stopAfter.(string); ok {
				return str, nil
			}
			return "", fmt.Errorf("stop-after value must be a string, got %T. Example: stop-after: \"+1d\"", stopAfter)
		}
		return "", nil
	default:
		return "", fmt.Errorf("invalid on: section format")
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

	// Resolve relative stop-after to absolute time if needed
	if workflowData.StopTime != "" {
		stopAfterLog.Printf("Stop-after value specified: %s", workflowData.StopTime)
		// Check if there's already a lock file with a stop time (recompilation case)
		lockFile := stringutil.MarkdownToLockFile(markdownPath)
		existingStopTime := ExtractStopTimeFromLockFile(lockFile)

		// If refresh flag is set, always regenerate the stop time
		if c.refreshStopTime {
			stopAfterLog.Print("Refresh flag set, regenerating stop time")
			resolvedStopTime, err := resolveStopTime(workflowData.StopTime, time.Now().UTC())
			if err != nil {
				return fmt.Errorf("invalid stop-after format: %w", err)
			}
			originalStopTime := stopAfter
			workflowData.StopTime = resolvedStopTime
			stopAfterLog.Printf("Resolved stop time from %s to %s", originalStopTime, resolvedStopTime)

			if c.verbose && isRelativeStopTime(originalStopTime) {
				fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("Refreshed relative stop-after to: %s", resolvedStopTime)))
			} else if c.verbose && originalStopTime != resolvedStopTime {
				fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("Refreshed absolute stop-after from '%s' to: %s", originalStopTime, resolvedStopTime)))
			}
		} else if existingStopTime != "" {
			// Preserve existing stop time during recompilation (default behavior)
			stopAfterLog.Printf("Preserving existing stop time from lock file: %s", existingStopTime)
			workflowData.StopTime = existingStopTime
			if c.verbose {
				fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("Preserving existing stop time from lock file: %s", existingStopTime)))
			}
		} else {
			// First compilation or no existing stop time, generate new one
			stopAfterLog.Print("First compilation, generating new stop time")
			resolvedStopTime, err := resolveStopTime(workflowData.StopTime, time.Now().UTC())
			if err != nil {
				return fmt.Errorf("invalid stop-after format: %w", err)
			}
			originalStopTime := stopAfter
			workflowData.StopTime = resolvedStopTime

			if c.verbose && isRelativeStopTime(originalStopTime) {
				fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("Resolved relative stop-after to: %s", resolvedStopTime)))
			} else if c.verbose && originalStopTime != resolvedStopTime {
				fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("Parsed absolute stop-after from '%s' to: %s", originalStopTime, resolvedStopTime)))
			}
		}
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

	lines := strings.Split(string(content), "\n")
	for _, line := range lines {
		// Look for GH_AW_STOP_TIME: YYYY-MM-DD HH:MM:SS
		// This is in the env section of the stop time check job
		if strings.Contains(line, "GH_AW_STOP_TIME:") {
			prefix := "GH_AW_STOP_TIME:"
			if idx := strings.Index(line, prefix); idx != -1 {
				return strings.TrimSpace(line[idx+len(prefix):])
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
					return nil, fmt.Errorf("skip-if-match object must have a 'query' field. Example:\n  skip-if-match:\n    query: \"is:issue is:open\"\n    max: 3")
				}

				queryStr, ok := queryVal.(string)
				if !ok {
					return nil, fmt.Errorf("skip-if-match 'query' field must be a string, got %T", queryVal)
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
						return nil, fmt.Errorf("skip-if-match 'max' field must be at least 1, got %d", maxVal)
					}
				}

				return &SkipIfMatchConfig{
					Query: queryStr,
					Max:   maxVal,
				}, nil
			default:
				return nil, fmt.Errorf("skip-if-match value must be a string or object, got %T. Examples:\n  skip-if-match: \"is:issue is:open\"\n  skip-if-match:\n    query: \"is:pr is:open\"\n    max: 3", skipIfMatch)
			}
		}
		return nil, nil
	default:
		return nil, fmt.Errorf("invalid on: section format")
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
					return nil, fmt.Errorf("skip-if-no-match object must have a 'query' field. Example:\n  skip-if-no-match:\n    query: \"is:pr is:open\"\n    min: 3")
				}

				queryStr, ok := queryVal.(string)
				if !ok {
					return nil, fmt.Errorf("skip-if-no-match 'query' field must be a string, got %T", queryVal)
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
						return nil, fmt.Errorf("skip-if-no-match 'min' field must be at least 1, got %d", minVal)
					}
				}

				return &SkipIfNoMatchConfig{
					Query: queryStr,
					Min:   minVal,
				}, nil
			default:
				return nil, fmt.Errorf("skip-if-no-match value must be a string or object, got %T. Examples:\n  skip-if-no-match: \"is:pr is:open\"\n  skip-if-no-match:\n    query: \"is:pr is:open\"\n    min: 3", skipIfNoMatch)
			}
		}
		return nil, nil
	default:
		return nil, fmt.Errorf("invalid on: section format")
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

	if c.verbose && workflowData.SkipIfMatch != nil {
		if workflowData.SkipIfMatch.Max == 1 {
			fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("Skip-if-match query configured: %s (max: 1 match)", workflowData.SkipIfMatch.Query)))
		} else {
			fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("Skip-if-match query configured: %s (max: %d matches)", workflowData.SkipIfMatch.Query, workflowData.SkipIfMatch.Max)))
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

	if c.verbose && workflowData.SkipIfNoMatch != nil {
		if workflowData.SkipIfNoMatch.Min == 1 {
			fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("Skip-if-no-match query configured: %s (min: 1 match)", workflowData.SkipIfNoMatch.Query)))
		} else {
			fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("Skip-if-no-match query configured: %s (min: %d matches)", workflowData.SkipIfNoMatch.Query, workflowData.SkipIfNoMatch.Min)))
		}
	}

	return nil
}
