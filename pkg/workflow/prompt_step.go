package workflow

import (
	"strings"

	"github.com/github/gh-aw/pkg/logger"
)

var promptStepLog = logger.New("workflow:prompt_step")

// appendPromptStep generates a workflow step that appends content to the prompt file.
// It encapsulates the common YAML scaffolding for prompt-related steps, reducing duplication
// across multiple prompt generation helpers.
//
// Parameters:
//   - yaml: The string builder to write the YAML to
//   - stepName: The name of the workflow step (e.g., "Append XPIA security instructions to prompt")
//   - renderer: A function that writes the actual prompt content to the YAML
//   - condition: Optional condition string to add an 'if:' clause (empty string means no condition)
//   - indent: The indentation to use for nested content (typically "          ")
func appendPromptStep(yaml *strings.Builder, stepName string, renderer func(*strings.Builder, string), condition string, indent string) {
	promptStepLog.Printf("Appending prompt step: name=%s, hasCondition=%v", stepName, condition != "")

	yaml.WriteString("      - name: " + stepName + "\n")

	// Add conditional if provided
	if condition != "" {
		promptStepLog.Printf("Adding condition: %s", condition)
		yaml.WriteString("        if: " + condition + "\n")
	}

	yaml.WriteString("        env:\n")
	yaml.WriteString("          GH_AW_PROMPT: /tmp/gh-aw/aw-prompts/prompt.txt\n")
	yaml.WriteString("        run: |\n")

	// Call the renderer to write the actual content
	renderer(yaml, indent)
	promptStepLog.Print("Prompt step appended successfully")
}

// appendPromptStepWithHeredoc generates a workflow step that appends content to the prompt file
// using a heredoc (cat << 'PROMPT_EOF' >> "$GH_AW_PROMPT" pattern).
// This is used by compiler functions that need to embed static structured content without variable substitution.
//
// Parameters:
//   - yaml: The string builder to write the YAML to
//   - stepName: The name of the workflow step
//   - renderer: A function that writes the content between the heredoc markers
func appendPromptStepWithHeredoc(yaml *strings.Builder, stepName string, renderer func(*strings.Builder)) {
	promptStepLog.Printf("Appending prompt step with heredoc: name=%s", stepName)

	yaml.WriteString("      - name: " + stepName + "\n")
	yaml.WriteString("        env:\n")
	yaml.WriteString("          GH_AW_PROMPT: /tmp/gh-aw/aw-prompts/prompt.txt\n")
	yaml.WriteString("        run: |\n")
	yaml.WriteString("          cat << 'PROMPT_EOF' >> \"$GH_AW_PROMPT\"\n")

	// Call the renderer to write the content
	renderer(yaml)

	yaml.WriteString("          PROMPT_EOF\n")
	promptStepLog.Print("Heredoc prompt step appended successfully")
}
