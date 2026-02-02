package workflow

import (
	"fmt"
	"os"
	"strings"

	"github.com/github/gh-aw/pkg/console"
	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/parser"
	"github.com/goccy/go-yaml"
)

var frontmatterLog = logger.New("workflow:frontmatter_extraction")

// extractYAMLValue extracts a scalar value from the frontmatter map
func (c *Compiler) extractYAMLValue(frontmatter map[string]any, key string) string {
	if value, exists := frontmatter[key]; exists {
		if str, ok := value.(string); ok {
			return str
		}
		if num, ok := value.(int); ok {
			return fmt.Sprintf("%d", num)
		}
		if num, ok := value.(int64); ok {
			return fmt.Sprintf("%d", num)
		}
		if num, ok := value.(uint64); ok {
			return fmt.Sprintf("%d", num)
		}
		if float, ok := value.(float64); ok {
			return fmt.Sprintf("%.0f", float)
		}
	}
	return ""
}

// indentYAMLLines adds indentation to all lines of a multi-line YAML string except the first
func (c *Compiler) indentYAMLLines(yamlContent, indent string) string {
	if yamlContent == "" {
		return yamlContent
	}

	lines := strings.Split(yamlContent, "\n")
	if len(lines) <= 1 {
		return yamlContent
	}

	// First line doesn't get additional indentation
	result := lines[0]
	for i := 1; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) != "" {
			result += "\n" + indent + lines[i]
		} else {
			result += "\n" + lines[i]
		}
	}

	return result
}

// extractTopLevelYAMLSection extracts a top-level YAML section from frontmatter
func (c *Compiler) extractTopLevelYAMLSection(frontmatter map[string]any, key string) string {
	value, exists := frontmatter[key]
	if !exists {
		return ""
	}

	frontmatterLog.Printf("Extracting YAML section: %s", key)

	// Convert the value back to YAML format with field ordering
	var yamlBytes []byte
	var err error

	// Check if value is a map that we should order alphabetically
	if valueMap, ok := value.(map[string]any); ok {
		// Use OrderMapFields for alphabetical sorting (empty priority list = all alphabetical)
		orderedValue := OrderMapFields(valueMap, []string{})
		// Wrap the ordered value with the key using MapSlice
		wrappedData := yaml.MapSlice{{Key: key, Value: orderedValue}}
		yamlBytes, err = yaml.MarshalWithOptions(wrappedData, DefaultMarshalOptions...)
		if err != nil {
			return ""
		}
	} else {
		// Use standard marshaling for non-map types
		yamlBytes, err = yaml.Marshal(map[string]any{key: value})
		if err != nil {
			return ""
		}
	}

	yamlStr := string(yamlBytes)
	// Remove the trailing newline
	yamlStr = strings.TrimSuffix(yamlStr, "\n")

	// Post-process YAML to ensure cron expressions are quoted
	// The YAML library may drop quotes from cron expressions like "0 14 * * 1-5"
	// which causes validation errors since they start with numbers but contain spaces
	yamlStr = parser.QuoteCronExpressions(yamlStr)

	// Clean up null values - replace `: null` with `:` for cleaner output
	// GitHub Actions treats `workflow_dispatch:` and `workflow_dispatch: null` identically
	yamlStr = CleanYAMLNullValues(yamlStr)

	// Clean up quoted keys - replace "key": with key: at the start of a line
	// Don't unquote "on" key as it's a YAML boolean keyword and must remain quoted
	if key != "on" {
		yamlStr = UnquoteYAMLKey(yamlStr, key)
	}

	// Special handling for "on" section - comment out draft and fork fields from pull_request
	if key == "on" {
		yamlStr = c.commentOutProcessedFieldsInOnSection(yamlStr, frontmatter)
		// Add zizmor ignore comment if workflow_run trigger is present
		yamlStr = c.addZizmorIgnoreForWorkflowRun(yamlStr)
		// Add friendly format comments for schedule cron expressions
		yamlStr = c.addFriendlyScheduleComments(yamlStr, frontmatter)
	}

	return yamlStr
}

// commentOutProcessedFieldsInOnSection comments out draft, fork, forks, names, manual-approval, stop-after, skip-if-match, skip-if-no-match, reaction, and lock-for-agent fields in the on section
// These fields are processed separately and should be commented for documentation
// Exception: names fields in sections with __gh_aw_native_label_filter__ marker in frontmatter are NOT commented out
func (c *Compiler) commentOutProcessedFieldsInOnSection(yamlStr string, frontmatter map[string]any) string {
	frontmatterLog.Print("Processing 'on' section to comment out processed fields")

	// Check frontmatter for native label filter markers
	nativeLabelFilterSections := make(map[string]bool)
	if onValue, exists := frontmatter["on"]; exists {
		if onMap, ok := onValue.(map[string]any); ok {
			for _, sectionKey := range []string{"issues", "pull_request", "discussion", "issue_comment"} {
				if sectionValue, hasSec := onMap[sectionKey]; hasSec {
					if sectionMap, ok := sectionValue.(map[string]any); ok {
						if marker, hasMarker := sectionMap["__gh_aw_native_label_filter__"]; hasMarker {
							if useNative, ok := marker.(bool); ok && useNative {
								nativeLabelFilterSections[sectionKey] = true
								frontmatterLog.Printf("Section %s uses native label filtering", sectionKey)
							}
						}
					}
				}
			}
		}
	}

	lines := strings.Split(yamlStr, "\n")
	var result []string
	inPullRequest := false
	inIssues := false
	inDiscussion := false
	inIssueComment := false
	inForksArray := false
	inSkipIfMatch := false
	inSkipIfNoMatch := false
	currentSection := "" // Track which section we're in ("issues", "pull_request", "discussion", or "issue_comment")

	for _, line := range lines {
		// Check if we're entering a pull_request, issues, discussion, or issue_comment section
		if strings.Contains(line, "pull_request:") {
			inPullRequest = true
			inIssues = false
			inDiscussion = false
			inIssueComment = false
			currentSection = "pull_request"
			result = append(result, line)
			continue
		}
		if strings.Contains(line, "issues:") {
			inIssues = true
			inPullRequest = false
			inDiscussion = false
			inIssueComment = false
			currentSection = "issues"
			result = append(result, line)
			continue
		}
		if strings.Contains(line, "discussion:") {
			inDiscussion = true
			inPullRequest = false
			inIssues = false
			inIssueComment = false
			currentSection = "discussion"
			result = append(result, line)
			continue
		}
		if strings.Contains(line, "issue_comment:") {
			inIssueComment = true
			inPullRequest = false
			inIssues = false
			inDiscussion = false
			currentSection = "issue_comment"
			result = append(result, line)
			continue
		}

		// Check if we're leaving the pull_request, issues, discussion, or issue_comment section (new top-level key or end of indent)
		if inPullRequest || inIssues || inDiscussion || inIssueComment {
			// If line is not indented or is a new top-level key, we're out of the section
			if strings.TrimSpace(line) != "" && !strings.HasPrefix(line, "    ") && !strings.HasPrefix(line, "\t") {
				inPullRequest = false
				inIssues = false
				inDiscussion = false
				inIssueComment = false
				inForksArray = false
				currentSection = ""
			}
		}

		trimmedLine := strings.TrimSpace(line)

		// Skip marker lines in the YAML output
		if (inPullRequest || inIssues || inDiscussion || inIssueComment) && strings.Contains(trimmedLine, "__gh_aw_native_label_filter__:") {
			// Don't include the marker line in the output
			continue
		}

		// Check if we're entering the forks array
		if inPullRequest && strings.HasPrefix(trimmedLine, "forks:") {
			inForksArray = true
		}

		// Check if we're entering skip-if-match object
		if !inPullRequest && !inIssues && !inDiscussion && !inIssueComment && !inSkipIfMatch {
			// Check both uncommented and commented forms
			if (strings.HasPrefix(trimmedLine, "skip-if-match:") && trimmedLine == "skip-if-match:") ||
				(strings.HasPrefix(trimmedLine, "# skip-if-match:") && strings.Contains(trimmedLine, "pre-activation job")) {
				inSkipIfMatch = true
			}
		}

		// Check if we're entering skip-if-no-match object
		if !inPullRequest && !inIssues && !inDiscussion && !inIssueComment && !inSkipIfNoMatch {
			// Check both uncommented and commented forms
			if (strings.HasPrefix(trimmedLine, "skip-if-no-match:") && trimmedLine == "skip-if-no-match:") ||
				(strings.HasPrefix(trimmedLine, "# skip-if-no-match:") && strings.Contains(trimmedLine, "pre-activation job")) {
				inSkipIfNoMatch = true
			}
		}

		// Check if we're leaving skip-if-match object (encountering another top-level field)
		// Skip this check if we just entered skip-if-match on this line
		if inSkipIfMatch && strings.TrimSpace(line) != "" &&
			!strings.HasPrefix(trimmedLine, "skip-if-match:") &&
			!strings.HasPrefix(trimmedLine, "# skip-if-match:") {
			// Get the indentation of the current line
			lineIndent := len(line) - len(strings.TrimLeft(line, " \t"))
			// If this is a field at same level as skip-if-match (2 spaces) and not a comment, we're out of skip-if-match
			if lineIndent == 2 && !strings.HasPrefix(trimmedLine, "#") {
				inSkipIfMatch = false
			}
		}

		// Check if we're leaving skip-if-no-match object (encountering another top-level field)
		// Skip this check if we just entered skip-if-no-match on this line
		if inSkipIfNoMatch && strings.TrimSpace(line) != "" &&
			!strings.HasPrefix(trimmedLine, "skip-if-no-match:") &&
			!strings.HasPrefix(trimmedLine, "# skip-if-no-match:") {
			// Get the indentation of the current line
			lineIndent := len(line) - len(strings.TrimLeft(line, " \t"))
			// If this is a field at same level as skip-if-no-match (2 spaces) and not a comment, we're out of skip-if-no-match
			if lineIndent == 2 && !strings.HasPrefix(trimmedLine, "#") {
				inSkipIfNoMatch = false
			}
		}

		// Check if we're leaving the forks array by encountering another top-level field at the same level
		if inForksArray && inPullRequest && strings.TrimSpace(line) != "" {
			// Get the indentation of the current line
			lineIndent := len(line) - len(strings.TrimLeft(line, " \t"))

			// If this is a non-dash line at the same level as the forks field (4 spaces), we're out of the array
			if lineIndent == 4 && !strings.HasPrefix(trimmedLine, "-") && !strings.HasPrefix(trimmedLine, "forks:") {
				inForksArray = false
			}
		}

		// Determine if we should comment out this line
		shouldComment := false
		var commentReason string

		// Check for top-level fields that should be commented out (not inside pull_request, issues, discussion, or issue_comment)
		if !inPullRequest && !inIssues && !inDiscussion && !inIssueComment {
			if strings.HasPrefix(trimmedLine, "manual-approval:") {
				shouldComment = true
				commentReason = " # Manual approval processed as environment field in activation job"
			} else if strings.HasPrefix(trimmedLine, "stop-after:") {
				shouldComment = true
				commentReason = " # Stop-after processed as stop-time check in pre-activation job"
			} else if strings.HasPrefix(trimmedLine, "skip-if-match:") {
				shouldComment = true
				commentReason = " # Skip-if-match processed as search check in pre-activation job"
			} else if inSkipIfMatch && (strings.HasPrefix(trimmedLine, "query:") || strings.HasPrefix(trimmedLine, "max:")) {
				// Comment out nested fields in skip-if-match object
				shouldComment = true
				commentReason = ""
			} else if strings.HasPrefix(trimmedLine, "skip-if-no-match:") {
				shouldComment = true
				commentReason = " # Skip-if-no-match processed as search check in pre-activation job"
			} else if inSkipIfNoMatch && (strings.HasPrefix(trimmedLine, "query:") || strings.HasPrefix(trimmedLine, "min:")) {
				// Comment out nested fields in skip-if-no-match object
				shouldComment = true
				commentReason = ""
			} else if strings.HasPrefix(trimmedLine, "reaction:") {
				shouldComment = true
				commentReason = " # Reaction processed as activation job step"
			}
		}

		if !shouldComment && inPullRequest && strings.Contains(trimmedLine, "draft:") {
			shouldComment = true
			commentReason = " # Draft filtering applied via job conditions"
		} else if inPullRequest && strings.HasPrefix(trimmedLine, "forks:") {
			shouldComment = true
			commentReason = " # Fork filtering applied via job conditions"
		} else if inForksArray && strings.HasPrefix(trimmedLine, "-") {
			shouldComment = true
			commentReason = " # Fork filtering applied via job conditions"
		} else if (inPullRequest || inIssues || inDiscussion || inIssueComment) && strings.HasPrefix(trimmedLine, "lock-for-agent:") {
			shouldComment = true
			commentReason = " # Lock-for-agent processed as issue locking in activation job"
		} else if (inPullRequest || inIssues || inDiscussion || inIssueComment) && strings.HasPrefix(trimmedLine, "names:") {
			// Only comment out names if NOT using native label filtering for this section
			if !nativeLabelFilterSections[currentSection] {
				shouldComment = true
				commentReason = " # Label filtering applied via job conditions"
			}
		} else if (inPullRequest || inIssues || inDiscussion || inIssueComment) && line != "" {
			// Check if we're in a names array (after "names:" line)
			// Look back to see if the previous uncommented line was "names:"
			// Only do this if NOT using native label filtering for this section
			if !nativeLabelFilterSections[currentSection] {
				if len(result) > 0 {
					for i := len(result) - 1; i >= 0; i-- {
						prevLine := result[i]
						prevTrimmed := strings.TrimSpace(prevLine)

						// Skip empty lines
						if prevTrimmed == "" {
							continue
						}

						// If we find "names:", and current line is an array item, comment it
						if strings.Contains(prevTrimmed, "names:") && strings.Contains(prevTrimmed, "# Label filtering") {
							if strings.HasPrefix(trimmedLine, "-") {
								shouldComment = true
								commentReason = " # Label filtering applied via job conditions"
							}
							break
						}

						// If we find a different field or commented names array item, break
						if !strings.HasPrefix(prevTrimmed, "#") || !strings.Contains(prevTrimmed, "Label filtering") {
							break
						}

						// If it's a commented names array item, continue
						if strings.HasPrefix(prevTrimmed, "# -") && strings.Contains(prevTrimmed, "Label filtering") {
							if strings.HasPrefix(trimmedLine, "-") {
								shouldComment = true
								commentReason = " # Label filtering applied via job conditions"
							}
							continue
						}

						break
					}
				}
			} // Close native filter check
		}

		if shouldComment {
			// Preserve the original indentation and comment out the line
			indentation := ""
			trimmed := strings.TrimLeft(line, " \t")
			if len(line) > len(trimmed) {
				indentation = line[:len(line)-len(trimmed)]
			}

			commentedLine := indentation + "# " + trimmed + commentReason
			result = append(result, commentedLine)
		} else {
			result = append(result, line)
		}
	}

	return strings.Join(result, "\n")
}

// addZizmorIgnoreForWorkflowRun adds a zizmor ignore comment for workflow_run triggers
// The comment is added after the workflow_run: line to suppress dangerous-triggers warnings
// since the compiler adds proper role and fork validation to secure these triggers
func (c *Compiler) addZizmorIgnoreForWorkflowRun(yamlStr string) string {
	// Check if the YAML contains workflow_run trigger
	if !strings.Contains(yamlStr, "workflow_run:") {
		return yamlStr
	}

	lines := strings.Split(yamlStr, "\n")
	var result []string
	annotationAdded := false // Track if we've already added the annotation

	for _, line := range lines {
		result = append(result, line)

		// Skip if we've already added the annotation (prevents duplicates)
		if annotationAdded {
			continue
		}

		// Check if this is a non-comment workflow_run: key at the correct YAML level
		trimmedLine := strings.TrimSpace(line)

		// Skip if the line is a comment
		if strings.HasPrefix(trimmedLine, "#") {
			continue
		}

		// Match lines that are only 'workflow_run:' (possibly with trailing whitespace or a comment)
		// e.g., 'workflow_run:', 'workflow_run: # comment', '  workflow_run:'
		// But not 'someworkflow_run:', 'workflow_run: value', etc.
		if idx := strings.Index(trimmedLine, "workflow_run:"); idx == 0 {
			after := strings.TrimSpace(trimmedLine[len("workflow_run:"):])
			// Only allow if nothing or only a comment follows
			if after == "" || strings.HasPrefix(after, "#") {
				// Get the indentation of the workflow_run line
				indentation := ""
				if len(line) > len(trimmedLine) {
					indentation = line[:len(line)-len(trimmedLine)]
				}

				// Add zizmor ignore comment with proper indentation
				// The comment explains that the trigger is secured with role and fork validation
				comment := indentation + "  # zizmor: ignore[dangerous-triggers] - workflow_run trigger is secured with role and fork validation"
				result = append(result, comment)
				annotationAdded = true
			}
		}
	}

	return strings.Join(result, "\n")
}

// extractPermissions extracts permissions from frontmatter using the permission parser
func (c *Compiler) extractPermissions(frontmatter map[string]any) string {
	permissionsValue, exists := frontmatter["permissions"]
	if !exists {
		return ""
	}

	// Check if this is an "all: read" case by using the parser
	parser := NewPermissionsParserFromValue(permissionsValue)

	// If it's "all: read", use the parser to expand it
	if parser.hasAll && parser.allLevel == "read" {
		frontmatterLog.Print("Expanding 'all: read' permissions to individual scopes")
		permissions := parser.ToPermissions()
		yaml := permissions.RenderToYAML()

		// Adjust indentation from 6 spaces to 2 spaces for workflow-level permissions
		// RenderToYAML uses 6 spaces for job-level rendering
		lines := strings.Split(yaml, "\n")
		for i := 1; i < len(lines); i++ {
			if strings.HasPrefix(lines[i], "      ") {
				lines[i] = "  " + lines[i][6:]
			}
		}
		return strings.Join(lines, "\n")
	}

	// For all other cases, use standard extraction
	return c.extractTopLevelYAMLSection(frontmatter, "permissions")
}

// extractIfCondition extracts the if condition from frontmatter, returning just the expression
// without the "if: " prefix
func (c *Compiler) extractIfCondition(frontmatter map[string]any) string {
	value, exists := frontmatter["if"]
	if !exists {
		return ""
	}

	// Convert the value to string - it should be just the expression
	if strValue, ok := value.(string); ok {
		return c.extractExpressionFromIfString(strValue)
	}

	return ""
}

// extractExpressionFromIfString extracts the expression part from a string that might
// contain "if: expression" or just "expression", returning just the expression
func (c *Compiler) extractExpressionFromIfString(ifString string) string {
	if ifString == "" {
		return ""
	}

	// Check if the string starts with "if: " and strip it
	if strings.HasPrefix(ifString, "if: ") {
		return strings.TrimSpace(ifString[4:]) // Remove "if: " prefix
	}

	// Return the string as-is (it's just the expression)
	return ifString
}

// extractCommandConfig extracts command configuration from frontmatter including name and events
func (c *Compiler) extractCommandConfig(frontmatter map[string]any) (commandNames []string, commandEvents []string) {
	// Check new format: on.slash_command or on.slash_command.name (preferred)
	// Also check legacy format: on.command or on.command.name (deprecated)
	if onValue, exists := frontmatter["on"]; exists {
		if onMap, ok := onValue.(map[string]any); ok {
			var commandValue any
			var hasCommand bool
			var isDeprecated bool

			// Check for slash_command first (preferred)
			if slashCommandValue, hasSlashCommand := onMap["slash_command"]; hasSlashCommand {
				commandValue = slashCommandValue
				hasCommand = true
				isDeprecated = false
			} else if legacyCommandValue, hasLegacyCommand := onMap["command"]; hasLegacyCommand {
				// Fall back to command (deprecated)
				commandValue = legacyCommandValue
				hasCommand = true
				isDeprecated = true
			}

			if hasCommand {
				// Show deprecation warning if using old field name
				if isDeprecated {
					fmt.Fprintln(os.Stderr, console.FormatWarningMessage("The 'command:' trigger field is deprecated. Please use 'slash_command:' instead."))
					c.IncrementWarningCount()
				}

				// Check if command is a string (shorthand format)
				if commandStr, ok := commandValue.(string); ok {
					return []string{commandStr}, nil // nil means default (all events)
				}
				// Check if command is a map with a name key (object format)
				if commandMap, ok := commandValue.(map[string]any); ok {
					var names []string
					var events []string

					if nameValue, hasName := commandMap["name"]; hasName {
						// Handle string or array of strings
						if nameStr, ok := nameValue.(string); ok {
							names = []string{nameStr}
						} else if nameArray, ok := nameValue.([]any); ok {
							for _, nameItem := range nameArray {
								if nameItemStr, ok := nameItem.(string); ok {
									names = append(names, nameItemStr)
								}
							}
						}
					}

					// Extract events field
					if eventsValue, hasEvents := commandMap["events"]; hasEvents {
						events = ParseCommandEvents(eventsValue)
					}

					return names, events
				}
			}
		}
	}

	return nil, nil
}
