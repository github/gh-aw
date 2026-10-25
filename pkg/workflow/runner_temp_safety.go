package workflow

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

const (
	unsafeRunnerTempActionsPath = "${{ runner.temp }}/gh-aw/actions"
	safeActionsDirLine          = "const actionsDir = path.join(process.env.RUNNER_TEMP, 'gh-aw', 'actions');"
)

var (
	githubScriptActionRequirePrefixRE = regexp.MustCompile(`^(\s*)(.*\brequire\()'`)
	shellActionCommandRE              = regexp.MustCompile(`\b(node|bash|sh) \$\{\{\s*runner\.temp\s*\}\}/gh-aw/actions/([^ \t\r\n]+)`)
	blockScalarHeaderRE               = regexp.MustCompile(`^(\s*)(?:-\s+)?(script|run):\s*[|>]`)
	singleLineRunRE                   = regexp.MustCompile(`^\s*(?:-\s+)?run:\s+`)
	singleLineExecutableRE            = regexp.MustCompile(`^\s*(?:-\s+)?(script|run):\s+`)
)

const githubScriptActionRequirePathPrefix = "${{ runner.temp }}/gh-aw/actions/"

// singleQuotedJS renders s as a single-quoted JavaScript string literal for
// embedding in generated workflow scripts. strconv.Quote produces valid
// JavaScript string escaping; only the quote characters need adjusting, since
// JavaScript single-quoted strings escape ' rather than ".
func singleQuotedJS(s string) string {
	quoted := strconv.Quote(s)
	body := quoted[1 : len(quoted)-1]
	body = strings.ReplaceAll(body, `\"`, `"`)
	body = strings.ReplaceAll(body, `'`, `\'`)
	return "'" + body + "'"
}

func rewriteGitHubScriptActionRequire(line string) (string, bool) {
	matches := githubScriptActionRequirePrefixRE.FindStringSubmatchIndex(line)
	if matches == nil {
		return line, false
	}

	literalBody, suffix, ok := splitSimpleSingleQuotedJSLiteral(line[matches[1]:])
	if !ok || !strings.HasPrefix(literalBody, githubScriptActionRequirePathPrefix) || !strings.HasPrefix(suffix, ")") {
		return line, false
	}

	actionPath := strings.TrimPrefix(literalBody, githubScriptActionRequirePathPrefix)
	return line[:matches[3]] + line[matches[4]:matches[5]] + "path.join(actionsDir, " + singleQuotedJS(actionPath) + ")" + suffix, true
}

func splitSimpleSingleQuotedJSLiteral(s string) (string, string, bool) {
	escaped := false
	for i := range len(s) {
		switch {
		case escaped:
			escaped = false
		case s[i] == '\\':
			escaped = true
		case s[i] == '\'':
			body := s[:i]
			if strings.Contains(body, `\`) {
				return "", "", false
			}
			return body, s[i+1:], true
		}
	}
	return "", "", false
}

func rewriteRunnerTempInExecutableBodies(yamlContent string) string {
	lines := strings.SplitAfter(yamlContent, "\n")
	var out strings.Builder
	out.Grow(len(yamlContent))

	inScriptBlock := false
	inRunBlock := false
	blockIndent := -1
	scriptBlockHasActionsDir := false

	for _, line := range lines {
		lineWithoutNewline := strings.TrimSuffix(line, "\n")
		newline := ""
		if line != lineWithoutNewline {
			newline = "\n"
		}

		if inScriptBlock || inRunBlock {
			trimmed := strings.TrimSpace(lineWithoutNewline)
			currentIndent := leadingSpaces(lineWithoutNewline)
			if trimmed != "" && currentIndent <= blockIndent {
				inScriptBlock = false
				inRunBlock = false
				blockIndent = -1
				scriptBlockHasActionsDir = false
			}
		}

		if matches := blockScalarHeaderRE.FindStringSubmatch(lineWithoutNewline); matches != nil {
			inScriptBlock = matches[2] == "script"
			inRunBlock = matches[2] == "run"
			blockIndent = len(matches[1])
			if inScriptBlock {
				scriptBlockHasActionsDir = false
			}
		}

		rewrittenLine := lineWithoutNewline
		if inRunBlock || singleLineRunRE.MatchString(lineWithoutNewline) {
			rewrittenLine = shellActionCommandRE.ReplaceAllString(lineWithoutNewline, `${1} "$${RUNNER_TEMP}/gh-aw/actions/${2}"`)
		}
		if inScriptBlock {
			rewrittenLine = rewriteScriptActionsRequire(&out, rewrittenLine, &scriptBlockHasActionsDir)
		}

		out.WriteString(rewrittenLine)
		out.WriteString(newline)
	}

	return out.String()
}

func rewriteScriptActionsRequire(out *strings.Builder, line string, hasActionsDir *bool) string {
	if strings.Contains(line, safeActionsDirLine) {
		*hasActionsDir = true
	}
	if requireLine, ok := rewriteGitHubScriptActionRequire(line); ok {
		indent := strings.Repeat(" ", leadingSpaces(line))
		if !*hasActionsDir {
			out.WriteString(indent)
			out.WriteString("const path = require('path');\n")
			out.WriteString(indent)
			out.WriteString(safeActionsDirLine)
			out.WriteString("\n")
			*hasActionsDir = true
		}
		return requireLine
	}
	return line
}

func finalizeRunnerTempSafety(yamlContent string) (string, error) {
	yamlContent = rewriteRunnerTempInExecutableBodies(yamlContent)
	if err := validateNoRunnerTempInExecutableBodies(yamlContent); err != nil {
		return "", err
	}
	return yamlContent, nil
}

func validateNoRunnerTempInExecutableBodies(yamlContent string) error {
	lines := strings.Split(yamlContent, "\n")
	inExecutableBlock := false
	executableBlockIndent := -1

	for i, line := range lines {
		if inExecutableBlock {
			trimmed := strings.TrimSpace(line)
			currentIndent := leadingSpaces(line)
			if trimmed != "" && currentIndent <= executableBlockIndent {
				inExecutableBlock = false
				executableBlockIndent = -1
			}
		}

		if matches := blockScalarHeaderRE.FindStringSubmatch(line); matches != nil {
			inExecutableBlock = true
			executableBlockIndent = len(matches[1])
			continue
		}

		if strings.Contains(line, unsafeRunnerTempActionsPath) &&
			(inExecutableBlock || singleLineExecutableRE.MatchString(line)) {
			return fmt.Errorf("generated executable step contains unsafe %s expression on line %d", unsafeRunnerTempActionsPath, i+1)
		}
	}

	return nil
}

func leadingSpaces(s string) int {
	count := 0
	for _, r := range s {
		if r != ' ' {
			break
		}
		count++
	}
	return count
}
