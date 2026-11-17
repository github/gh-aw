package workflow

import (
	"regexp"
	"slices"
	"strings"

	"github.com/github/gh-aw/pkg/workflow/actionslanguageservice"
)

const runtimeDirectoryExpression = "${{ env.GH_AW_RUNTIME_DIR }}"
const runtimeIdentityExpression = "${{ env.GH_AW_RUNTIME_ID }}"

// Generators use a logical runtime namespace so validation and path matching do
// not depend on a particular job. Lower it only after job construction, when all
// steps (including typed actions and artifact consumers) have been assembled.
var logicalRuntimePath = regexp.MustCompile(`/tmp/gh-aw($|[^a-zA-Z0-9_.-])`)
var quotedLogicalRuntimePath = regexp.MustCompile(`'(/tmp/gh-aw[^']*)'|"(/tmp/gh-aw[^"]*)"`)
var logicalRuntimeContainer = regexp.MustCompile(`awmg-(?:mcpg|cli-proxy|proxy)($|[^a-zA-Z0-9_-])`)

func resolveRuntimeStepPaths(step string) string {
	step = resolveRuntimePathsInRunBlock(step)
	spans, _ := actionslanguageservice.ExtractExpressions(step)
	var resolved strings.Builder
	offset := 0
	for _, span := range spans {
		resolved.WriteString(resolveRuntimeText(step[offset:span.Start]))
		resolved.WriteString("${{")
		resolved.WriteString(resolveRuntimeExpression(span.Content))
		resolved.WriteString("}}")
		offset = span.End
	}
	resolved.WriteString(resolveRuntimeText(step[offset:]))
	return resolved.String()
}

func resolveRuntimePathsInRunBlock(step string) string {
	shell := ""
	for _, line := range strings.Split(step, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "shell:") {
			shell = strings.Trim(strings.TrimSpace(strings.TrimPrefix(trimmed, "shell:")), `"'`)
		}
	}

	directoryReference, identityReference := runtimeShellReferences(shell)
	lines := strings.SplitAfter(step, "\n")
	var resolved strings.Builder
	runIndent := -1
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		indent := len(line) - len(strings.TrimLeft(line, " "))
		if runIndent >= 0 {
			if trimmed == "" || indent > runIndent {
				resolved.WriteString(resolveRuntimeShellText(line, directoryReference, identityReference))
				continue
			}
			runIndent = -1
		}

		key := strings.TrimLeft(line, " ")
		fieldOffset := 0
		if strings.HasPrefix(key, "- ") {
			fieldOffset = 2
			key = key[fieldOffset:]
		}
		if strings.HasPrefix(key, "run:") {
			value := strings.TrimSpace(strings.TrimPrefix(key, "run:"))
			if value == "" || strings.HasPrefix(value, "|") || strings.HasPrefix(value, ">") {
				runIndent = indent + fieldOffset
				resolved.WriteString(line)
				continue
			}
			prefixLength := len(line) - len(key) + len("run:")
			resolved.WriteString(line[:prefixLength])
			resolved.WriteString(resolveRuntimeShellText(line[prefixLength:], directoryReference, identityReference))
			continue
		}
		resolved.WriteString(line)
	}
	return resolved.String()
}

func runtimeShellReferences(shell string) (string, string) {
	shell = strings.ToLower(strings.TrimSpace(shell))
	switch {
	case shell == "pwsh" || strings.HasPrefix(shell, "pwsh ") || shell == "powershell" || strings.HasPrefix(shell, "powershell "):
		return "$env:GH_AW_RUNTIME_DIR", "$env:GH_AW_RUNTIME_ID"
	case shell == "cmd" || strings.HasPrefix(shell, "cmd "):
		return "%GH_AW_RUNTIME_DIR%", "%GH_AW_RUNTIME_ID%"
	case shell == "" || shell == "bash" || strings.HasPrefix(shell, "bash ") || shell == "sh" || strings.HasPrefix(shell, "sh "):
		return "$GH_AW_RUNTIME_DIR", "$GH_AW_RUNTIME_ID"
	default:
		return runtimeDirectoryExpression, runtimeIdentityExpression
	}
}

func resolveRuntimeShellText(text, directoryReference, identityReference string) string {
	text = quotedLogicalRuntimePath.ReplaceAllStringFunc(text, func(match string) string {
		path := match[1 : len(match)-1]
		path = strings.Replace(path, "/tmp/gh-aw", directoryReference, 1)
		return `"` + path + `"`
	})
	text = logicalRuntimePath.ReplaceAllStringFunc(text, func(match string) string {
		return directoryReference + strings.TrimPrefix(match, "/tmp/gh-aw")
	})
	return logicalRuntimeContainer.ReplaceAllStringFunc(text, func(match string) string {
		for _, name := range []string{"awmg-mcpg", "awmg-cli-proxy", "awmg-proxy"} {
			if suffix, ok := strings.CutPrefix(match, name); ok {
				return name + "-" + identityReference + suffix
			}
		}
		return match
	})
}

func resolveRuntimeText(step string) string {
	step = logicalRuntimePath.ReplaceAllStringFunc(step, func(match string) string {
		return runtimeDirectoryExpression + strings.TrimPrefix(match, "/tmp/gh-aw")
	})
	return logicalRuntimeContainer.ReplaceAllStringFunc(step, func(match string) string {
		for _, name := range []string{"awmg-mcpg", "awmg-cli-proxy", "awmg-proxy"} {
			if suffix, ok := strings.CutPrefix(match, name); ok {
				return name + "-" + runtimeIdentityExpression + suffix
			}
		}
		return match
	})
}

// Expressions need a format call, not a nested interpolation. Tokenizing keeps
// escaped quotes and braces in authored string literals intact.
func resolveRuntimeExpression(expression string) string {
	tokens, err := actionslanguageservice.Tokenize(expression)
	if err != nil {
		return expression
	}
	for i := range slices.Backward(tokens) {
		token := tokens[i]
		if token.Kind != "string" {
			continue
		}
		value, ok := token.Value.(string)
		if !ok || (!logicalRuntimePath.MatchString(value) && !logicalRuntimeContainer.MatchString(value)) {
			continue
		}
		template := strings.NewReplacer("{", "{{", "}", "}}").Replace(value)
		template = resolveRuntimeText(template)
		template = strings.NewReplacer(runtimeDirectoryExpression, "{0}", runtimeIdentityExpression, "{1}", "'", "''").Replace(template)
		replacement := "format('" + template + "', env.GH_AW_RUNTIME_DIR, env.GH_AW_RUNTIME_ID)"
		expression = expression[:token.Offset] + replacement + expression[token.Offset+len(token.Text):]
	}
	return expression
}

func (c *Compiler) resolveJobRuntimePaths() {
	for _, job := range c.jobManager.GetAllJobs() {
		for i, step := range job.Steps {
			job.Steps[i] = resolveRuntimeStepPaths(step)
		}
	}
}
