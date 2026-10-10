package workflow

import (
	"fmt"
	"os"
	"strings"

	"github.com/goccy/go-yaml"

	"github.com/github/gh-aw/pkg/console"
	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/parser"
)

var frontmatterLog = logger.New("workflow:frontmatter_extraction")

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
	var result strings.Builder
	result.WriteString(lines[0])
	for i := 1; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) != "" {
			result.WriteString("\n" + indent + lines[i])
		} else {
			// Emit a bare newline for blank/whitespace-only lines so we don't
			// carry the surrounding indentation as trailing whitespace, which
			// yamllint flags as trailing-spaces.
			result.WriteString("\n")
		}
	}

	return result.String()
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
		// For the "on" section, exclude gh-aw-specific keys that are processed
		// separately and must not appear in the compiled GitHub Actions workflow.
		// The "needs" key (on.needs) controls job dependency wiring and is invalid
		// as a top-level on: trigger key in GitHub Actions.
		if key == "on" {
			valueMap = excludeMapKeys(valueMap, "needs")
		}
		// Use OrderMapFields for alphabetical sorting (empty priority list = all alphabetical)
		orderedValue := OrderMapFields(valueMap, []string{})
		// Wrap the ordered value with the key using MapSlice
		wrappedData := yaml.MapSlice{{Key: key, Value: orderedValue}}
		marshalOptions := DefaultMarshalOptions
		if key == "on" || key == "services" {
			// Indent sequence items (e.g. schedule cron lists, event `types:`
			// arrays, and service `ports:` lists) under their parent key so that
			// yamllint's default indentation rule (indent-sequences: true) is
			// satisfied. Scoped to the `on:` and `services:` sections so that
			// custom `steps:` marshaling elsewhere is unaffected.
			marshalOptions = append(append([]yaml.EncodeOption{}, DefaultMarshalOptions...), yaml.IndentSequence(true))
		}
		yamlBytes, err = yaml.MarshalWithOptions(wrappedData, marshalOptions...)
		if err != nil {
			return ""
		}
	} else {
		// Use standard marshaling for non-map types
		marshalOptions := DefaultMarshalOptions
		if key == "on" || key == "services" {
			marshalOptions = append(append([]yaml.EncodeOption{}, DefaultMarshalOptions...), yaml.IndentSequence(true))
		}
		yamlBytes, err = yaml.MarshalWithOptions(map[string]any{key: value}, marshalOptions...)
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

	// For top-level env values, quote plain scalars containing ": " because YAML
	// treats this token sequence as a mapping separator in plain style.
	if key == "env" {
		yamlStr = quoteEnvValuesContainingColonSpace(yamlStr)
	}

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

// extractPermissions extracts permissions from frontmatter using the permission parser
func (c *Compiler) extractPermissions(frontmatter map[string]any) string {
	permissionsValue, exists := frontmatter["permissions"]
	if !exists {
		frontmatterLog.Print("No permissions field found in frontmatter")
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
// without the "if: " prefix. Also merges any condition derived from on.deployment_status.state
// and on.workflow_run.conclusion.
func (c *Compiler) extractIfCondition(frontmatter map[string]any) (string, error) {
	var ifExpr string
	if value, exists := frontmatter["if"]; exists {
		if strValue, ok := value.(string); ok {
			// Strip "if: " prefix and ${{ }} wrapper to get a bare expression for safe merging
			ifExpr = stripExpressionWrapper(c.extractExpressionFromIfString(strValue))
			frontmatterLog.Printf("Extracted if condition from frontmatter: %s", ifExpr)
		}
	}

	// Merge any condition generated from on.deployment_status.state
	stateCondition, err := extractDeploymentStatusStateCondition(frontmatter)
	if err != nil {
		return "", err
	}
	if stateCondition != "" {
		frontmatterLog.Printf("Merging deployment_status state condition: %s", stateCondition)
		if ifExpr != "" {
			ifExpr = "(" + ifExpr + ") && (" + stateCondition + ")"
		} else {
			ifExpr = stateCondition
		}
	}

	// Merge any condition generated from on.workflow_run.conclusion
	conclusionCondition, err := extractWorkflowRunConclusionCondition(frontmatter)
	if err != nil {
		return "", err
	}
	if conclusionCondition != "" {
		frontmatterLog.Printf("Merging workflow_run conclusion condition: %s", conclusionCondition)
		if ifExpr != "" {
			ifExpr = "(" + ifExpr + ") && (" + conclusionCondition + ")"
		} else {
			ifExpr = conclusionCondition
		}
	}

	return ifExpr, nil
}

// extractExpressionFromIfString extracts the expression part from a string that might
// contain "if: expression" or just "expression", returning just the expression
func (c *Compiler) extractExpressionFromIfString(ifString string) string {
	if ifString == "" {
		return ""
	}

	// Check if the string starts with "if: " and strip it
	if strings.HasPrefix(ifString, "if: ") {
		expr := strings.TrimSpace(ifString[4:]) // Remove "if: " prefix
		frontmatterLog.Printf("Stripped 'if: ' prefix from if condition: %s", expr)
		return expr
	}

	// Return the string as-is (it's just the expression)
	return ifString
}

// extractCommandConfig extracts command configuration from frontmatter including name, events,
// centralized routing strategy, and optional footer placeholder for slash_command.
func (c *Compiler) extractCommandConfig(frontmatter map[string]any) (commandNames []string, commandEvents []string, commandCentralized bool, commandPlaceholder string) {
	frontmatterLog.Print("Extracting command configuration from frontmatter")
	// Check new format: on.slash_command or on.slash_command.name (preferred)
	// Also check legacy format: on.command or on.command.name (deprecated)
	commandValue, hasCommand := extractOnTriggerValue(frontmatter, "slash_command")
	isDeprecated := false
	if !hasCommand {
		commandValue, hasCommand = extractOnTriggerValue(frontmatter, "command")
		isDeprecated = hasCommand
	}
	if hasCommand {
		// Show deprecation warning if using old field name
		if isDeprecated {
			fmt.Fprintln(os.Stderr, console.FormatWarningMessage("The 'command:' trigger field is deprecated. Please use 'slash_command:' instead."))
			c.IncrementWarningCount()
		}

		// Check if command is a string (shorthand format)
		if commandStr, ok := commandValue.(string); ok {
			frontmatterLog.Printf("Extracted command name (shorthand): %s", commandStr)
			return []string{commandStr}, nil, false, "" // nil means default (all events)
		}
		// Check if command is a map with a name key (object format)
		if commandMap, ok := commandValue.(map[string]any); ok {
			var events []string
			centralized := false
			placeholder := ""
			names := normalizeStringOrStringSlice(commandMap["name"])

			// Extract events field
			if eventsValue, hasEvents := commandMap["events"]; hasEvents {
				events = ParseCommandEvents(eventsValue)
			}

			if strategyRaw, hasStrategy := commandMap["strategy"]; hasStrategy {
				if strategy, ok := strategyRaw.(string); ok && strings.EqualFold(strings.TrimSpace(strategy), "centralized") {
					centralized = true
				}
			}

			// Extract optional placeholder for footer hint text
			if placeholderRaw, hasPlaceholder := commandMap["placeholder"]; hasPlaceholder {
				if placeholderStr, ok := placeholderRaw.(string); ok {
					if trimmed := strings.TrimSpace(placeholderStr); trimmed != "" {
						placeholder = trimmed
					}
				}
			}

			frontmatterLog.Printf("Extracted command config: names=%v, events=%v, centralized=%v, placeholder=%q", names, events, centralized, placeholder)
			return names, events, centralized, placeholder
		}
	}

	return nil, nil, false, ""
}

// extractLabelCommandConfig extracts the label-command configuration from frontmatter
// including label name(s), the events field, strategy, and the remove_label flag.
// It reads on.label_command which can be:
//   - a string: label name directly (e.g. label_command: "deploy")
//   - a map with "name" or "names", optional "events", optional "strategy", and optional "remove_label" fields
//
// Returns (labelNames, labelEvents, decentralized, removeLabel) where labelEvents is nil for default (all events)
// and removeLabel defaults to true when not specified.
func (c *Compiler) extractLabelCommandConfig(frontmatter map[string]any) (labelNames []string, labelEvents []string, decentralized bool, removeLabel bool) {
	frontmatterLog.Print("Extracting label-command configuration from frontmatter")
	labelCommandValue, hasLabelCommand := extractOnTriggerValue(frontmatter, "label_command")
	if !hasLabelCommand {
		return nil, nil, false, true
	}

	// Simple string form: label_command: "my-label"
	if nameStr, ok := labelCommandValue.(string); ok {
		frontmatterLog.Printf("Extracted label-command name (shorthand): %s", nameStr)
		return []string{nameStr}, nil, false, true
	}

	// Map form: label_command: {name: "...", names: [...], events: [...], remove_label: bool}
	if lcMap, ok := labelCommandValue.(map[string]any); ok {
		var events []string
		decentralized := false
		removeLabelVal := true // default to true
		names := normalizeStringOrStringSlice(lcMap["name"])

		if namesVal, hasNames := lcMap["names"]; hasNames {
			names = append(names, normalizeStringOrStringSlice(namesVal)...)
		}

		if eventsVal, hasEvents := lcMap["events"]; hasEvents {
			events = ParseCommandEvents(eventsVal)
		}

		if strategyVal, hasStrategy := lcMap["strategy"]; hasStrategy {
			if strategy, ok := strategyVal.(string); ok && strings.EqualFold(strings.TrimSpace(strategy), "decentralized") {
				decentralized = true
			}
		}

		if removeLabelField, hasRemoveLabel := lcMap["remove_label"]; hasRemoveLabel {
			if b, ok := removeLabelField.(bool); ok {
				removeLabelVal = b
			}
		}

		frontmatterLog.Printf("Extracted label-command config: names=%v, events=%v, decentralized=%v, remove_label=%v", names, events, decentralized, removeLabelVal)
		return names, events, decentralized, removeLabelVal
	}

	return nil, nil, false, true
}
