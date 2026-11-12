package workflow

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"github.com/github/gh-aw/pkg/sliceutil"
	"github.com/github/gh-aw/pkg/workflow/actionslanguageservice"
	"github.com/goccy/go-yaml"
)

// github-script requires a nonempty token even when no API operations are needed.
const promptGitHubScriptToken = "no-authentication"

func promptValueUsesCredentials(value string) bool {
	return slices.ContainsFunc(interpolationReferences(value), isCredentialReference)
}

func isCredentialReference(path []string) bool {
	return referenceHasPrefix(path, []string{"secrets"}) ||
		referenceHasPrefix(path, []string{"github", "token"}) ||
		referenceHasPrefix(path, []string{"github", "*"}) ||
		slices.EqualFunc(path, []string{"github"}, strings.EqualFold)
}

func isSecretsSerializationCall(node *actionslanguageservice.Expression) bool {
	if node.Kind != "call" || !strings.EqualFold(node.Token.Text, "toJSON") || len(node.Children) != 1 {
		return false
	}
	argument := node.Child(0)
	for argument.Kind == "group" && len(argument.Children) == 1 {
		argument = argument.Child(0)
	}
	path, ok := actionslanguageservice.Reference(argument)
	return ok && slices.EqualFunc(path, []string{"secrets"}, strings.EqualFold)
}

func expressionUsesCredentialReference(expression *actionslanguageservice.Expression, ignoreSecretsSerialization bool) bool {
	found := false
	actionslanguageservice.Walk(expression, func(node *actionslanguageservice.Expression) bool {
		if ignoreSecretsSerialization && isSecretsSerializationCall(node) {
			return false
		}
		path, ok := actionslanguageservice.Reference(node)
		if !ok {
			return true
		}
		found = found || isCredentialReference(path)
		return false
	})
	return found
}

func valueUsesCredentialReference(value string) bool {
	spans, err := actionslanguageservice.ExtractExpressions(value)
	if err != nil {
		return true
	}
	for _, span := range spans {
		expression, err := actionslanguageservice.ParseSyntax(span.Content)
		if err != nil || expression == nil {
			return true
		}
		if expressionUsesCredentialReference(expression, false) {
			return true
		}
	}
	return false
}

func validatePromptSecretBindings(data *WorkflowData, localImports string) error {
	env := parseEnvYAMLSection(data.Env)
	prompt := removeXMLComments(data.MarkdownContent + "\n" + data.MainWorkflowMarkdown + "\n" + data.PromptMarkdown + "\n" + data.ImportedMarkdown + "\n" + localImports)
	for _, ref := range interpolationReferences(wrapExpressionsInTemplateConditionals(prompt)) {
		if len(ref) < 2 || !strings.EqualFold(ref[0], "env") { //nolint:uncheckedsliceindex // Short-circuiting checks the context bounds.
			continue
		}
		name := ref[1] //nolint:uncheckedsliceindex // The context and variable name are checked above.
		credential := false
		switch strings.ToUpper(name) {
		case "GITHUB_TOKEN", "GH_TOKEN", "COPILOT_GITHUB_TOKEN", "GH_AW_GITHUB_TOKEN", "GH_AW_GITHUB_MCP_SERVER_TOKEN", "ACTIONS_RUNTIME_TOKEN", "ACTIONS_ID_TOKEN_REQUEST_TOKEN":
			credential = true
		}
		for key, value := range env {
			if strings.EqualFold(key, name) {
				if text, ok := value.(string); ok {
					credential = credential || promptValueUsesCredentials(text)
				}
			}
		}
		if credential {
			return fmt.Errorf("prompt expression env.%s references a credential; prompt creation has no secrets available. Use a non-secret environment variable or pass credentials only to the step that needs them", name)
		}
	}
	return nil
}

func configureCredentialFreePromptAction(step *WorkflowStep) error {
	script, ok := step.With["script"].(string)
	if !ok || !strings.HasPrefix(step.Uses, "actions/github-script@") {
		return fmt.Errorf("prompt step %q requires an unsupported action", step.Name)
	}
	encoded, err := json.Marshal(sliceutil.SortedKeys(step.Env))
	if err != nil {
		return fmt.Errorf("encode prompt environment allowlist: %w", err)
	}
	var lines []string
	for line := range strings.SplitSeq(script, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "const { setupGlobals } = require(") || strings.HasPrefix(trimmed, "setupGlobals(") {
			continue
		}
		lines = append(lines, line)
	}
	script = strings.Join(lines, "\n")
	script = strings.ReplaceAll(script, "'"+SetupActionDestination+"/", "process.env.GH_AW_ACTIONS_DIR + '/")
	step.Env["GH_AW_ACTIONS_DIR"] = SetupActionDestination
	step.Env["GH_AW_PROMPT_ENV"] = string(encoded)
	step.With["github-token"] = promptGitHubScriptToken
	step.With["script"] = "const { setupPromptGlobals } = require(process.env.GH_AW_ACTIONS_DIR + '/setup_prompt_globals.cjs');\n" +
		"setupPromptGlobals(core, context);\n" + script
	return nil
}

// Prompt modules receive no authenticated API client or inherited credentials.
func writeCredentialFreePromptSteps(output *strings.Builder, content string, data *WorkflowData) error {
	var steps []WorkflowStep
	comments := yaml.CommentMap{}
	if err := yaml.UnmarshalWithOptions([]byte(content), &steps, yaml.CommentToMap(comments)); err != nil {
		return fmt.Errorf("parse generated prompt steps: %w", err)
	}
	for i := range steps {
		step := &steps[i]
		if step.Env == nil {
			step.Env = make(map[string]string)
		}
		for key, value := range step.Env {
			if promptValueUsesCredentials(value) {
				return fmt.Errorf("prompt step %q cannot receive credentials through %s", step.Name, key)
			}
		}
		if step.Uses != "" {
			if err := configureCredentialFreePromptAction(step); err != nil {
				return err
			}
		}
		// Inherited workflow env is not a prompt capability. Explicit expression
		// bindings are evaluated by Actions against the enclosing env context.
		for key := range parseEnvYAMLSection(data.Env) {
			if _, selected := step.Env[key]; !selected {
				step.Env[key] = ""
			}
		}
	}
	options := append([]yaml.EncodeOption{yaml.WithComment(comments)}, DefaultMarshalOptions...)
	encoded, err := yaml.MarshalWithOptions(steps, options...)
	if err != nil {
		return fmt.Errorf("encode credential-free prompt steps: %w", err)
	}
	for line := range strings.SplitSeq(strings.TrimRight(string(encoded), "\n"), "\n") {
		output.WriteString("      " + line + "\n")
	}
	return nil
}
