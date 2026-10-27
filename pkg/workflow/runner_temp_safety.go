package workflow

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/github/gh-aw/pkg/workflow/actionslanguageservice"
)

const (
	unsafeRunnerTempActionsPath = "${{ runner.temp }}/gh-aw/actions"
	safeActionsDirLine          = "const actionsDir = path.join(process.env.RUNNER_TEMP, 'gh-aw', 'actions');"
)

var (
	githubScriptActionRequirePrefixRE = regexp.MustCompile(`^(\s*)(.*\brequire\()'`)
	shellActionCommandPrefixRE        = regexp.MustCompile(`\b(node|bash|sh) $`)
	shellActionCommandPathRE          = regexp.MustCompile("^/gh-aw/actions/([^ \\t\\r\\n;&|()<>'\"\\\\`]+)")
	blockScalarHeaderRE               = regexp.MustCompile(`^(\s*)(?:-\s+)?(script|run):\s*[|>]`)
	singleLineRunRE                   = regexp.MustCompile(`^\s*(?:-\s+)?run:\s+`)
	singleLineExecutableRE            = regexp.MustCompile(`^\s*(?:-\s+)?(script|run):\s+`)
)

const githubScriptActionRequirePathPrefix = "${{ runner.temp }}/gh-aw/actions/"

func rewriteShellActionCommands(line string) string {
	var result strings.Builder
	last := 0
	spans, _ := actionslanguageservice.ExtractExpressions(line)
	for _, span := range spans {
		expression, ok := actionslanguageservice.WrappedExpression(line[span.Start:span.End])
		path, reference := actionslanguageservice.Reference(expression)
		if !ok || !reference || len(path) != 2 || !referenceHasPrefix(path, []string{"runner", "temp"}) {
			continue
		}
		command := shellActionCommandPrefixRE.FindStringSubmatchIndex(line[:span.Start])
		action := shellActionCommandPathRE.FindStringSubmatchIndex(line[span.End:])
		if command == nil || action == nil || command[0] < last {
			continue
		}
		result.WriteString(line[last:command[0]])
		result.WriteString(line[command[2]:command[3]])
		result.WriteString(` "${RUNNER_TEMP}/gh-aw/actions/`)
		result.WriteString(line[span.End+action[2] : span.End+action[3]])
		result.WriteByte('"')
		last = span.End + action[1]
	}
	result.WriteString(line[last:])
	return result.String()
}

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
			rewrittenLine = rewriteShellActionCommands(lineWithoutNewline)
		}
		if inScriptBlock {
			if strings.Contains(rewrittenLine, safeActionsDirLine) {
				scriptBlockHasActionsDir = true
			}
			if requireLine, ok := rewriteGitHubScriptActionRequire(rewrittenLine); ok {
				indent := strings.Repeat(" ", leadingSpaces(rewrittenLine))
				if !scriptBlockHasActionsDir {
					out.WriteString(indent)
					out.WriteString("const path = require('path');\n")
					out.WriteString(indent)
					out.WriteString(safeActionsDirLine)
					out.WriteString("\n")
					scriptBlockHasActionsDir = true
				}
				rewrittenLine = requireLine
			}
		}

		out.WriteString(rewrittenLine)
		out.WriteString(newline)
	}

	return out.String()
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
