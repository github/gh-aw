package workflow

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/github/gh-aw/pkg/sliceutil"
	"github.com/goccy/go-yaml"
)

var promptEnvironmentName = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

const credentialFreeEnvironmentCommand = "/usr/bin/env -i"

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

func credentialFreePromptScript(step *WorkflowStep) (string, string, error) {
	if step.Uses == "" {
		return "bash --noprofile --norc -e -c", step.Run, nil
	}
	script, ok := step.With["script"].(string)
	if !ok || !strings.HasPrefix(step.Uses, "actions/github-script@") {
		return "", "", fmt.Errorf("prompt step %q requires an unsupported action", step.Name)
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
	step.Uses = ""
	step.With = nil
	step.Env["GH_AW_ACTIONS_DIR"] = SetupActionDestination
	return "node -e", "require(process.env.GH_AW_ACTIONS_DIR + '/shim.cjs');\n(async () => {\n" + script +
		"\n})().catch(error => { console.error(error); process.exitCode = 1; });\n", nil
}

// Prompt rendering has no API client or inherited credential environment.
func writeCredentialFreePromptSteps(output *strings.Builder, content string, data *WorkflowData) error {
	var steps []WorkflowStep
	if err := yaml.Unmarshal([]byte(content), &steps); err != nil {
		return fmt.Errorf("parse generated prompt steps: %w", err)
	}
	for i := range steps {
		step := &steps[i]
		if step.Env == nil {
			step.Env = make(map[string]string)
		}
		command, script, err := credentialFreePromptScript(step)
		if err != nil {
			return err
		}
		keys := map[string]string{
			"PATH": "", "RUNNER_TEMP": "", "RUNNER_OS": "", "RUNNER_ARCH": "",
			"GITHUB_WORKSPACE": "", "GITHUB_EVENT_PATH": "", "GITHUB_EVENT_NAME": "",
			"GITHUB_REPOSITORY": "", "GITHUB_ACTOR": "", "GITHUB_SHA": "", "GITHUB_REF": "",
			"GITHUB_RUN_ID": "", "GITHUB_RUN_NUMBER": "", "GITHUB_RUN_ATTEMPT": "",
			"GITHUB_WORKFLOW": "", "GITHUB_JOB": "", "GITHUB_ACTION": "",
			"GITHUB_SERVER_URL": "", "GITHUB_API_URL": "", "GITHUB_GRAPHQL_URL": "",
			"GITHUB_STEP_SUMMARY": "",
		}
		for key, value := range step.Env {
			if !promptEnvironmentName.MatchString(key) {
				return fmt.Errorf("prompt step %q has an invalid environment variable name", step.Name)
			}
			if promptValueUsesCredentials(value) {
				return fmt.Errorf("prompt step %q cannot receive credentials through %s", step.Name, key)
			}
			keys[key] = ""
		}
		// Inherited workflow env is not a prompt capability. Explicit expression
		// bindings are evaluated by Actions against the enclosing env context.
		for key := range parseEnvYAMLSection(data.Env) {
			if _, selected := step.Env[key]; !selected {
				step.Env[key] = ""
			}
		}
		var run strings.Builder
		run.WriteString(credentialFreeEnvironmentCommand + " \\\n")
		for _, key := range sliceutil.SortedKeys(keys) {
			fmt.Fprintf(&run, "  %s=\"${%s-}\" \\\n", key, key) //nolint:fprintferrorunchecked // strings.Builder writes cannot fail.
		}
		step.Env["GH_AW_PROMPT_SCRIPT"] = script
		run.WriteString("  " + command + " \"$GH_AW_PROMPT_SCRIPT\"\n")
		step.Shell = "bash"
		step.Run = run.String()
	}
	encoded, err := yaml.MarshalWithOptions(steps, DefaultMarshalOptions...)
	if err != nil {
		return fmt.Errorf("encode credential-free prompt steps: %w", err)
	}
	for line := range strings.SplitSeq(strings.TrimRight(string(encoded), "\n"), "\n") {
		output.WriteString("      " + line + "\n")
	}
	return nil
}
