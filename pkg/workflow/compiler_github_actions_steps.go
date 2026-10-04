package workflow

import (
	"fmt"
	"path"
	"strings"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/logger"
)

var compilerGitHubActionsStepsLog = logger.New("workflow:compiler_github_actions_steps")

// generateGitHubScriptWithRequire generates a github-script step that loads a module using require().
// Instead of repeating the global variable assignments inline, it uses the setup_globals helper function.
//
// Parameters:
//   - scriptPath: The path to the .cjs file to require (e.g., "check_stop_time.cjs")
//
// Returns a string containing the complete script content to be used in a github-script action's "script:" field.
func generateGitHubScriptWithRequire(scriptPath string) string {
	compilerGitHubActionsStepsLog.Printf("Generating GitHub script step with require: %s", scriptPath)
	var script strings.Builder

	// Use the setup_globals helper to store GitHub Actions objects in global scope
	script.WriteString("            const { setupGlobals } = require('" + path.Join(SetupActionDestination, "setup_globals.cjs") + "');\n")
	script.WriteString("            setupGlobals(core, github, context, exec, io, getOctokit);\n")
	script.WriteString("            const { main } = require('" + path.Join(SetupActionDestination, scriptPath) + "');\n")
	script.WriteString("            await main();\n")

	return script.String()
}

// generatePlaceholderSubstitutionStep generates a JavaScript-based step that performs
// safe placeholder substitution using the substitute_placeholders script.
// This replaces the multiple sed commands with a single JavaScript step.
func generatePlaceholderSubstitutionStep(yaml *strings.Builder, expressionMappings []*ExpressionMapping, indent string, data *WorkflowData) {
	if len(expressionMappings) == 0 {
		return
	}

	compilerGitHubActionsStepsLog.Printf("Generating placeholder substitution step with %d mappings", len(expressionMappings))

	// Use actions/github-script to perform the substitutions
	yaml.WriteString(indent + "- name: Substitute placeholders\n")
	fmt.Fprintf(yaml, indent+"  uses: %s\n", getCachedActionPin("actions/github-script", data))
	yaml.WriteString(indent + "  env:\n")
	fmt.Fprintf(yaml, indent+"    GH_AW_PROMPT: %s\n", constants.AwPromptsFileExpr)

	// Add all environment variables
	// For static values (wrapped in quotes), output them directly without ${{ }}
	// For GitHub expressions, wrap them in ${{ }}
	for _, mapping := range expressionMappings {
		content := mapping.Content
		// Check if this is a static quoted value (starts and ends with quotes)
		if (strings.HasPrefix(content, "'") && strings.HasSuffix(content, "'")) ||
			(strings.HasPrefix(content, "\"") && strings.HasSuffix(content, "\"")) {
			// Static value - output directly without ${{ }} wrapper
			// Check if inner value is multi-line; if so use a YAML double-quoted scalar
			// with escaped newlines to avoid invalid YAML.
			innerValue := content[1 : len(content)-1]
			if strings.Contains(innerValue, "\n") {
				escaped := strings.ReplaceAll(innerValue, `\`, `\\`)
				escaped = strings.ReplaceAll(escaped, `"`, `\"`)
				escaped = strings.ReplaceAll(escaped, "\n", `\n`)
				fmt.Fprintf(yaml, indent+"    %s: \"%s\"\n", mapping.EnvVar, escaped)
			} else {
				fmt.Fprintf(yaml, indent+"    %s: %s\n", mapping.EnvVar, content)
			}
		} else {
			// GitHub expression - wrap in ${{ }}
			fmt.Fprintf(yaml, indent+"    %s: ${{ %s }}\n", mapping.EnvVar, content)
		}
	}

	yaml.WriteString(indent + "  with:\n")
	yaml.WriteString(indent + "    script: |\n")

	// Use setup_globals helper to make GitHub Actions objects available globally
	yaml.WriteString(indent + "      const { setupGlobals } = require('" + path.Join(SetupActionDestination, "setup_globals.cjs") + "');\n")
	yaml.WriteString(indent + "      setupGlobals(core, github, context, exec, io, getOctokit);\n")
	yaml.WriteString(indent + "      \n")
	// Use require() to load script from copied files
	yaml.WriteString(indent + "      const substitutePlaceholders = require('" + path.Join(SetupActionDestination, "substitute_placeholders.cjs") + "');\n")
	yaml.WriteString(indent + "      \n")
	yaml.WriteString(indent + "      // Call the substitution function\n")
	yaml.WriteString(indent + "      return await substitutePlaceholders({\n")
	yaml.WriteString(indent + "        file: process.env.GH_AW_PROMPT,\n")
	yaml.WriteString(indent + "        substitutions: {\n")

	for i, mapping := range expressionMappings {
		comma := ","
		if i == len(expressionMappings)-1 {
			comma = ""
		}
		fmt.Fprintf(yaml, indent+"          %s: process.env.%s%s\n", mapping.EnvVar, mapping.EnvVar, comma)
	}

	yaml.WriteString(indent + "        }\n")
	yaml.WriteString(indent + "      });\n")
}
