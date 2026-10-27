package cli

import (
	"fmt"
	"hash/fnv"
	"io"
	"strings"

	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/setutil"
	"github.com/github/gh-aw/pkg/workflow/actionslanguageservice"
	"github.com/github/gh-aw/pkg/workflow/aw"
	"github.com/github/gh-aw/pkg/workflow/codemod"
)

var stepsRunExpressionsLog = logger.New("cli:codemod_steps_run_secrets_env")

// getStepsRunSecretsToEnvCodemod hoists run expressions into step-level environment bindings.
func getStepsRunSecretsToEnvCodemod() Codemod {
	return Codemod{
		ID:           "steps-run-secrets-to-env",
		Name:         "Move step run expressions to env bindings",
		Description:  "Rewrites all ${{ ... }} expressions interpolated directly in run commands to $VARS (or $env:VARS for PowerShell steps) and adds step-level env bindings for strict-mode compatibility. Note: expressions inside single-quoted strings are rewritten too; since single quotes suppress shell variable expansion, those sections should be double-quoted if the substituted value is required.",
		IntroducedIn: "0.26.0",
		Apply: func(content string, frontmatter map[string]any) (string, bool, error) {
			sections := []string{"pre-steps", "steps", "post-steps", "pre-agent-steps"}
			if !hasTopLevelSection(frontmatter, sections) {
				return content, false, nil
			}
			return codemod.Apply(content, func(f *codemod.Frontmatter) error {
				return f.Edit(func(document *aw.Document) error {
					for _, section := range sections {
						steps, ok := document.Frontmatter[section].([]any)
						if !ok {
							continue
						}
						for _, value := range steps {
							step, ok := value.(map[string]any)
							if !ok {
								continue
							}
							hoistStepRunExpressions(step)
						}
					}
					return nil
				})
			})
		},
	}
}

func hoistStepRunExpressions(step map[string]any) {
	run, ok := step["run"].(string)
	if !ok {
		return
	}
	env, ok := step["env"].(map[string]any)
	if !ok {
		if value, exists := step["env"]; exists && value != nil {
			return
		}
		env = map[string]any{}
	}
	shell, shellIsString := step["shell"].(string)
	powershell := shellIsString && (strings.EqualFold(shell, "pwsh") || strings.EqualFold(shell, "powershell"))
	bindings := map[string]string{}
	lines := strings.Split(run, "\n")
	changed := false
	for i, line := range lines {
		// As before, comment-only shell lines (including heredoc comments) stay untouched.
		if strings.HasPrefix(strings.TrimSpace(line), "#") {
			continue
		}
		code, comment := splitRunShellComment(line)
		updated, added := replaceStepExpressionRefs(code, powershell, bindings)
		if len(added) == 0 {
			continue
		}
		lines[i] = updated + comment
		changed = true
		for _, binding := range added {
			bindings[binding.Name] = binding.Expression
			if _, exists := env[binding.Name]; !exists {
				env[binding.Name] = binding.Expression
			}
		}
	}
	if changed {
		step["run"] = strings.Join(lines, "\n")
		step["env"] = env
	}
}

// Folded YAML joins authored shell-comment lines onto the preceding command.
// Keep that comment suffix out of expression hoisting without treating quoted
// hashes or hashes embedded in shell words as comments.
func splitRunShellComment(line string) (string, string) {
	var quote, previous rune
	escaped := false
	for i, ch := range line {
		if escaped {
			escaped = false
			previous = ch
			continue
		}
		if ch == '\\' && quote != '\'' {
			escaped = true
			previous = ch
			continue
		}
		if quote != 0 {
			if ch == quote {
				quote = 0
			}
			previous = ch
			continue
		}
		switch ch {
		case '\'', '"':
			quote = ch
		case '#':
			if i == 0 || previous == ' ' || previous == '\t' {
				return line[:i], line[i:]
			}
		}
		previous = ch
	}
	return line, ""
}

type stepExpressionBinding struct {
	Name       string
	Expression string
}

func replaceStepExpressionRefs(line string, shellIsPowerShell bool, existingBindings map[string]string) (string, []stepExpressionBinding) { //nolint:largefunc // Keep the existing expression deduplication and collision-resolution state machine together.
	matches, err := actionslanguageservice.ExtractExpressions(line)
	if err != nil {
		stepsRunExpressionsLog.Printf("Cannot scan run expressions: %v", err)
		return line, nil
	}
	if len(matches) == 0 {
		return line, nil
	}

	var result strings.Builder
	last := 0
	// Deduplicate expression bodies and detect sanitized-name collisions within this line.
	bodyToName := make(map[string]string)
	localNames := make(map[string]string)
	registeredNames := make(map[string]struct{})
	ordered := make([]stepExpressionBinding, 0, len(matches))

	for _, match := range matches {
		fullStart, fullEnd := match.Start, match.End
		fullExpression := line[fullStart:fullEnd]
		body := strings.TrimSpace(match.Content)

		result.WriteString(line[last:fullStart])

		if cachedName, done := bodyToName[body]; done {
			if shellIsPowerShell {
				result.WriteString("$env:" + cachedName)
			} else {
				result.WriteString("$" + cachedName)
			}
			last = fullEnd
			continue
		}

		envName, canonicalExpression, ok := mapRunExpressionToEnvBinding(body)
		if !ok {
			result.WriteString(fullExpression)
			last = fullEnd
			continue
		}

		if crossLine := existingBindings[envName]; (crossLine != "" && crossLine != canonicalExpression) ||
			(localNames[envName] != "" && localNames[envName] != canonicalExpression) {
			envName = hashedBindingName("EXPR", body)
			canonicalExpression = fmt.Sprintf("${{ %s }}", body)
		}

		bodyToName[body] = envName
		localNames[envName] = canonicalExpression

		if shellIsPowerShell {
			result.WriteString("$env:" + envName)
		} else {
			result.WriteString("$" + envName)
		}
		if !setutil.Contains(registeredNames, envName) {
			registeredNames[envName] = struct{}{}
			ordered = append(ordered, stepExpressionBinding{
				Name:       envName,
				Expression: canonicalExpression,
			})
		}
		last = fullEnd
	}

	result.WriteString(line[last:])
	return result.String(), ordered
}

func mapRunExpressionToEnvBinding(body string) (string, string, bool) {
	expression, err := actionslanguageservice.ParseSyntax(body)
	if err != nil || expression == nil {
		stepsRunExpressionsLog.Printf("Cannot parse run expression: %v", err)
		return "", "", false
	}
	path, static := actionslanguageservice.Reference(expression)
	if static && len(path) == 2 {
		if path[0] == "secrets" && stepBindingIdentifier(path[1], false) {
			return path[1], fmt.Sprintf("${{ secrets.%s }}", path[1]), true
		}
		if path[0] == "env" && stepBindingIdentifier(path[1], false) {
			return "GH_AW_ENV_" + path[1], fmt.Sprintf("${{ env.%s }}", path[1]), true
		}
		if path[0] == "github" && path[1] == "token" {
			return "GH_AW_GITHUB_TOKEN", "${{ github.token }}", true
		}
	}
	references := actionslanguageservice.References(expression)
	for _, context := range []string{"secrets", "env", "github"} {
		for _, reference := range references {
			if len(reference) < 2 || reference[0] != context {
				continue
			}
			if context == "github" && reference[1] == "token" {
				return hashedBindingName("GH_AW_GITHUB_TOKEN", body), fmt.Sprintf("${{ %s }}", body), true
			}
			if context != "github" && stepBindingIdentifier(reference[1], false) {
				prefix := "GH_AW_ENV_"
				if context == "secrets" {
					prefix = "GH_AW_SECRET_"
				}
				return hashedBindingName(prefix+reference[1], body), fmt.Sprintf("${{ %s }}", body), true
			}
		}
	}
	readable := static
	for _, segment := range path {
		readable = readable && stepBindingIdentifier(segment, true)
	}
	if readable {
		name := "EXPR_" + strings.ToUpper(strings.ReplaceAll(strings.Join(path, "_"), "-", "_"))
		return name, fmt.Sprintf("${{ %s }}", body), true
	}
	name := hashedBindingName("EXPR", body)
	return name, fmt.Sprintf("${{ %s }}", body), true
}

// stepBindingIdentifier constrains generated shell identifiers, not Actions syntax.
func stepBindingIdentifier(value string, allowHyphen bool) bool {
	if value == "" {
		return false
	}
	for i, ch := range value {
		if ch == '_' || ch >= 'A' && ch <= 'Z' || ch >= 'a' && ch <= 'z' || i > 0 && (ch >= '0' && ch <= '9' || allowHyphen && ch == '-') {
			continue
		}
		return false
	}
	return true
}

// hashedBindingName suffixes a binding name with a stable expression-body hash.
func hashedBindingName(prefix, body string) string {
	h := fnv.New32a()
	_, _ = io.WriteString(h, body)
	return fmt.Sprintf("%s_%08x", prefix, h.Sum32())
}

// parseYAMLMapKey is retained for the permission codemods that share it.
func parseYAMLMapKey(trimmedLine string) string {
	if strings.HasPrefix(trimmedLine, "- ") {
		return ""
	}
	key, _, found := strings.Cut(trimmedLine, ":")
	if !found {
		return ""
	}
	return strings.TrimSpace(key)
}
