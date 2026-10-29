package cli

import (
	"encoding/json"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/workflow"
	"github.com/github/gh-aw/pkg/workflow/aw"
	"github.com/github/gh-aw/pkg/workflow/codemod"
)

var postStepsSafeOutputsAppendLog = logger.New("cli:codemod_post_steps_safe_outputs_append")

// Match a complete command, not a fragment of a script. Only literal single-quoted
// payloads are safe to translate without evaluating shell expansions.
var postStepsSafeOutputsAppendRE = regexp.MustCompile(`\A[ \t]*(echo|printf '%s\\n')[ \t]+'([^'\r\n]*)'[ \t]*>>[ \t]*("\$(?:GH_AW_SAFE_OUTPUTS|\{GH_AW_SAFE_OUTPUTS\}|GH_AW_AGENT_OUTPUT|\{GH_AW_AGENT_OUTPUT\})"|\$(?:GH_AW_SAFE_OUTPUTS|\{GH_AW_SAFE_OUTPUTS\}|GH_AW_AGENT_OUTPUT|\{GH_AW_AGENT_OUTPUT\})|"/tmp/gh-aw/agent_output\.json"|/tmp/gh-aw/agent_output\.json)[ \t]*(\n?)\z`)

func getPostStepsSafeOutputsAppendCodemod() Codemod {
	return Codemod{
		ID:           "post-steps-safe-outputs-append",
		Name:         "Rewrite literal post-step safe-output appends to safeoutputs tool calls",
		Description:  "Replaces leading standalone echo/printf appends of literal create_issue, add_comment, or noop JSON with safeoutputs tool calls; ambiguous scripts still require manual fixes.",
		IntroducedIn: "1.29.13",
		Apply: func(content string, frontmatter map[string]any) (string, bool, error) {
			if _, ok := frontmatter["post-steps"].([]any); !ok {
				return content, false, nil
			}
			return codemod.Apply(content, func(f *codemod.Frontmatter) error {
				return f.Edit(func(document *aw.Document) error {
					steps, ok := document.Frontmatter["post-steps"].([]any)
					if !ok {
						return nil
					}
					for _, rawStep := range steps {
						step, ok := rawStep.(map[string]any)
						if !ok {
							break
						}
						if workflow.IsLiteralSafeOutputsToolStep(step) {
							continue
						}
						run, ok := step["run"].(string)
						if !ok {
							break
						}
						replacement, ok := rewritePostStepSafeOutputsAppend(run)
						if !ok {
							break
						}
						step["run"] = replacement
						if !workflow.IsLiteralSafeOutputsToolStep(step) {
							step["run"] = run
							break
						}
						postStepsSafeOutputsAppendLog.Print("Rewrote literal safe-output append to safeoutputs CLI")
					}
					return nil
				})
			})
		},
	}
}

func rewritePostStepSafeOutputsAppend(run string) (string, bool) {
	if !utf8.ValidString(run) || strings.Contains(run, "${{") {
		return run, false
	}
	matches := postStepsSafeOutputsAppendRE.FindStringSubmatch(run)
	if len(matches) != 5 {
		return run, false
	}
	var command, literal, newline string
	for index, value := range matches {
		switch index {
		case 1:
			command = value
		case 2:
			literal = value
		case 4:
			newline = value
		}
	}
	// echo may interpret backslashes (depending on the shell/options). printf's
	// fixed %s format does not interpret backslashes in its payload argument.
	if command == "echo" && strings.Contains(literal, `\`) {
		return run, false
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal([]byte(literal), &fields); err != nil {
		return run, false
	}
	var tool string
	if err := json.Unmarshal(fields["type"], &tool); err != nil {
		return run, false
	}
	// Other message types can use different tool names or require generated
	// files (for example pull-request patches); leave those for manual migration.
	switch tool {
	case "create_issue", "add_comment", "noop":
	default:
		return run, false
	}
	delete(fields, "type")
	args, err := json.Marshal(fields)
	if err != nil {
		return run, false
	}
	// Keep JSON a single shell argument, even if a future encoder emits apostrophes.
	payload := strings.ReplaceAll(string(args), "'", `\u0027`)
	return "safeoutputs " + tool + " '" + payload + "'" + newline, true
}
