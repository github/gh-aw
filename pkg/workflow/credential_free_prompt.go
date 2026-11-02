package workflow

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/github/gh-aw/pkg/sliceutil"
	"github.com/goccy/go-yaml"
)

// github-script requires a nonempty token even when no API operations are needed.
const promptGitHubScriptToken = "no-authentication"

func promptValueUsesCredentials(value string) bool {
	for _, source := range interpolationReferences(value) {
		if len(source) == 0 {
			continue
		}
		if strings.EqualFold(source[0], "secrets") || (len(source) > 1 && strings.EqualFold(source[0], "github") && strings.EqualFold(source[1], "token")) { //nolint:uncheckedsliceindex // The context and property bounds are checked above.
			return true
		}
	}
	return false
}

func validatePromptSecretBindings(data *WorkflowData, localImports string) error {
	env := parseEnvYAMLSection(data.Env)
	prompt := removeXMLComments(data.MarkdownContent + "\n" + data.MainWorkflowMarkdown + "\n" + data.ImportedMarkdown + "\n" + localImports)
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
	keys := map[string]string{
		"PATH": "", "RUNNER_TEMP": "", "RUNNER_OS": "", "RUNNER_ARCH": "",
		"GITHUB_WORKSPACE": "", "GITHUB_EVENT_PATH": "", "GITHUB_EVENT_NAME": "",
		"GITHUB_REPOSITORY": "", "GITHUB_ACTOR": "", "GITHUB_SHA": "", "GITHUB_REF": "",
		"GITHUB_RUN_ID": "", "GITHUB_RUN_NUMBER": "", "GITHUB_RUN_ATTEMPT": "",
		"GITHUB_WORKFLOW": "", "GITHUB_JOB": "", "GITHUB_ACTION": "",
		"GITHUB_SERVER_URL": "", "GITHUB_API_URL": "", "GITHUB_GRAPHQL_URL": "",
		"GITHUB_STEP_SUMMARY": "", "GITHUB_OUTPUT": "", "GH_AW_ACTIONS_DIR": "",
	}
	for key := range step.Env {
		keys[key] = ""
	}
	encoded, err := json.Marshal(sliceutil.SortedKeys(keys))
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
	step.With["github-token"] = promptGitHubScriptToken
	step.With["script"] = "const promptEnvironment = new Set(" + string(encoded) + ");\n" +
		"for (const name of Object.keys(process.env)) {\n" +
		"  if (!promptEnvironment.has(name)) delete process.env[name];\n" +
		"}\nglobal.core = core;\nglobal.context = context;\n" + script
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
