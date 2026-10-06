package workflow

import (
	"fmt"
	"regexp"
	"slices"
	"strings"
)

var (
	postStepIssueCreateRE = regexp.MustCompile(`(?i)(?:^|[\s|;&])gh\s+(?:(?:-R|--repo)(?:=|\s+)[^\s]+\s+)*issue\s+create\b`)
	postStepIssueAPIRE    = regexp.MustCompile(`(?i)(?:^|[/\s"'=])(?:https://api\.github\.com/)?repos/(?:[^/\s"'?]+/[^/\s"'?]+|\$\{\{\s*github\.repository\s*\}\}|\$\{?GITHUB_REPOSITORY\}?)/issues(?:[?"'\s;|&]|$)`)
	postStepAPIPostRE     = regexp.MustCompile(`(?i)(?:^|\s)(?:-X\s*POST\b|--method(?:=|\s+)POST\b|-f(?:\s|=)|-F(?:\s|=)|--(?:raw-)?field(?:\s|=)|--input(?:\s|=))`)
	postStepAPIGetRE      = regexp.MustCompile(`(?i)(?:^|\s)(?:-X\s*GET\b|--method(?:=|\s+)GET\b)`)
	postStepCurlRE        = regexp.MustCompile(`(?i)(?:^|[\s|;&])curl(?:\s|$)`)
	postStepOctokitRE     = regexp.MustCompile(`\b(?:github|octokit)(?:\.rest)?\.issues\.create\s*\(`)
	postStepRequestRE     = regexp.MustCompile(`(?i)\b(?:github|octokit)\.request\s*\(\s*['"\x60]POST\s+/repos/[^/'"\x60]+/[^/'"\x60]+/issues(?:[?#\s'"\x60]|$)`)
	postStepOutputRE      = regexp.MustCompile(`(?i)(?:\bagent_output\.json\b|(?:safeoutputs|safe-outputs)/output\.json\b|\boutputs\.jsonl\b|\bsafeoutputs\.jsonl\b|\bGH_AW_AGENT_OUTPUT\b|\bGH_AW_SAFE_OUTPUTS\b|steps\.set-runtime-paths\.outputs\.GH_AW_SAFE_OUTPUTS)`)
	postStepSecretRE      = regexp.MustCompile(`\bsecrets\b`)
	postStepArtifactRoot  = regexp.MustCompile(`(?i)^(?:/tmp/gh-aw|\$\{?RUNNER_TEMP\}?/gh-aw|\$\{\{\s*runner\.temp\s*\}\}/gh-aw)(?:/\*\*)?/?$`)
)

var postStepFileInputActions = map[string][]string{
	"actions/cache":                 {"path"},
	"actions/upload-artifact":       {"path"},
	"actions/upload-pages-artifact": {"path"},
}

// validatePostStepsSafeOutputs checks only user-provided post-steps, not compiler-generated steps.
func validatePostStepsSafeOutputs(steps []any) error {
	for i, raw := range steps {
		step, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		location := fmt.Sprintf("post-steps step %d", i+1)
		if name, ok := step["name"].(string); ok && name != "" {
			location += fmt.Sprintf(" (%q)", name)
		}

		if err := validatePostStepInputs(step, location); err != nil {
			return err
		}

		scripts := []string{}
		if run, ok := step["run"].(string); ok {
			scripts = append(scripts, run)
		}
		if with, ok := step["with"].(map[string]any); ok {
			if script, ok := with["script"].(string); ok {
				scripts = append(scripts, script)
			}
		}

		for _, script := range scripts {
			if postStepOctokitRE.MatchString(script) || postStepRequestRE.MatchString(script) {
				return issueCreationError(location)
			}
			for _, command := range postStepLogicalCommands(script) {
				if postStepIssueCreateRE.MatchString(command) {
					return issueCreationError(location)
				}
				for _, match := range ghAPICmdRE.FindAllStringSubmatch(command, -1) {
					args := regexCapture(match, 1)
					if args != "" && postStepIssueAPIRE.MatchString(parsePostStepGHAPIEndpoint(args)) &&
						postStepAPIPostRE.MatchString(args) && !postStepAPIGetRE.MatchString(args) {
						return issueCreationError(location)
					}
				}
				if curlCreatesIssue(command) {
					return issueCreationError(location)
				}
			}
			if postStepOutputRE.MatchString(script) {
				return fmt.Errorf("%s accesses the safe outputs output file directly; use safe-outputs instead. See: https://github.github.com/gh-aw/reference/safe-outputs/", location)
			}
		}
	}
	return nil
}

func validatePostStepInputs(step map[string]any, location string) error {
	if containsPostStepSecretEnv(step["env"]) {
		return fmt.Errorf("%s exposes a secret through its env section; secrets are not allowed in post-steps", location)
	}
	uses, ok := step["uses"].(string)
	if !ok {
		return nil
	}
	action, _, _ := strings.Cut(uses, "@")
	with, ok := step["with"].(map[string]any)
	if !ok {
		return nil
	}
	for _, input := range postStepFileInputActions[strings.ToLower(action)] {
		if path, exists := with[input]; exists && fileInputContainsSafeOutput(path) {
			return fmt.Errorf("%s reads a safe outputs file or its containing directory; use a narrower file path", location)
		}
	}
	return nil
}

func issueCreationError(location string) error {
	return fmt.Errorf("%s attempts to create an issue directly; use safe-outputs.create-issue instead. See: https://github.github.com/gh-aw/reference/safe-outputs/", location)
}

func containsPostStepSecret(value string) bool {
	return slices.ContainsFunc(InlineExpressionPattern.FindAllString(value, -1), postStepSecretRE.MatchString)
}

func containsPostStepSecretEnv(env any) bool {
	switch values := env.(type) {
	case map[string]any:
		for _, value := range values {
			if text, ok := value.(string); ok && containsPostStepSecret(text) {
				return true
			}
		}
	case map[string]string:
		for _, value := range values {
			if containsPostStepSecret(value) {
				return true
			}
		}
	}
	return false
}

func fileInputContainsSafeOutput(path any) bool {
	switch value := path.(type) {
	case string:
		trimmed := strings.Trim(strings.TrimSpace(value), `"'`)
		return postStepOutputRE.MatchString(value) || postStepArtifactRoot.MatchString(trimmed)
	case []any:
		return slices.ContainsFunc(value, fileInputContainsSafeOutput)
	}
	return false
}

// postStepLogicalCommands joins shell lines continued with a trailing backslash.
func postStepLogicalCommands(script string) []string {
	var commands []string
	var current strings.Builder
	lines := strings.Split(script, "\n")
	for i, line := range lines {
		line = strings.TrimRight(line, " \t\r")
		continued := strings.HasSuffix(line, `\`) && i < len(lines)-1
		if continued {
			line = strings.TrimSuffix(line, `\`)
		}
		if current.Len() > 0 {
			current.WriteByte(' ')
		}
		current.WriteString(line)
		if !continued {
			commands = appendPostStepShellCommands(commands, current.String())
			current.Reset()
		}
	}
	if current.Len() > 0 {
		commands = appendPostStepShellCommands(commands, current.String())
	}
	return commands
}

func appendPostStepShellCommands(commands []string, line string) []string {
	var command []rune
	inSingle, inDouble, inBacktick := false, false, false
	escaped := false
	for _, char := range line {
		if escaped {
			command = append(command, char)
			escaped = false
			continue
		}
		if char == '\\' && !inSingle {
			command = append(command, char)
			escaped = true
			continue
		}
		switch char {
		case '\'':
			if !inDouble && !inBacktick {
				inSingle = !inSingle
			}
		case '"':
			if !inSingle && !inBacktick {
				inDouble = !inDouble
			}
		case '`':
			if !inSingle && !inDouble {
				inBacktick = !inBacktick
			}
		}
		if !inSingle && !inDouble && !inBacktick {
			if char == ';' || char == '|' || char == '\n' || char == '&' {
				if len(command) > 0 {
					commands = append(commands, string(command))
					command = nil
				}
				continue
			}
		}
		command = append(command, char)
	}
	if len(command) > 0 {
		commands = append(commands, string(command))
	}
	return commands
}

func curlCreatesIssue(command string) bool {
	match := postStepCurlRE.FindStringIndex(command)
	if match == nil {
		return false
	}
	tokens := splitShellTokens(strings.TrimSpace(command[sliceIntAt(match, 1):]))
	method, hasData, useGet, urls := parseCurlRequest(tokens)
	if method == "" {
		if hasData && !useGet {
			method = "POST"
		} else {
			method = "GET"
		}
	}
	return method == "POST" && slices.ContainsFunc(urls, postStepIssueAPIRE.MatchString)
}

func parseCurlRequest(tokens []string) (string, bool, bool, []string) {
	method := ""
	hasData, useGet := false, false
	var urls []string
	for i := 0; i < len(tokens); i++ {
		token, ok := shellToken(tokens, i)
		if !ok {
			break
		}
		token = strings.Trim(token, `"'`)
		if token == "" {
			continue
		}
		switch {
		case token == "-X" || token == "--request":
			if value, exists := shellToken(tokens, i+1); exists {
				i++
				method = strings.ToUpper(strings.Trim(value, `"'`))
			}
		case strings.HasPrefix(token, "--request="):
			method = strings.ToUpper(strings.TrimPrefix(token, "--request="))
		case strings.HasPrefix(token, "-X") && len(token) > 2:
			method = strings.ToUpper(strings.TrimPrefix(token, "-X"))
		case token == "-G" || token == "--get":
			useGet = true
		case token == "-d" || token == "--data" || token == "--data-raw" ||
			token == "--data-binary" || token == "--data-urlencode" ||
			token == "-F" || token == "--form" || token == "--json":
			hasData = true
			if value, exists := shellToken(tokens, i+1); exists && !strings.HasPrefix(value, "-") {
				i++
			}
		case strings.HasPrefix(token, "--data=") || strings.HasPrefix(token, "--data-raw=") ||
			strings.HasPrefix(token, "--data-binary=") || strings.HasPrefix(token, "--data-urlencode=") ||
			strings.HasPrefix(token, "--form=") || strings.HasPrefix(token, "--json="):
			hasData = true
		case token == "--url":
			if value, exists := shellToken(tokens, i+1); exists {
				i++
				urls = append(urls, strings.Trim(value, `"'`))
			}
		case strings.HasPrefix(token, "-"):
			if curlOptionConsumesValue(token) {
				if _, exists := shellToken(tokens, i+1); exists {
					i++
				}
			}
		default:
			urls = append(urls, token)
		}
	}
	return method, hasData, useGet, urls
}

func shellToken(tokens []string, index int) (string, bool) {
	if index < 0 || index >= len(tokens) {
		return "", false
	}
	return tokens[index], true
}

func regexCapture(matches []string, index int) string {
	if index < 0 || index >= len(matches) {
		return ""
	}
	return matches[index]
}

func sliceIntAt(values []int, index int) int {
	if index < 0 || index >= len(values) {
		return 0
	}
	return values[index]
}

func parsePostStepGHAPIEndpoint(args string) string {
	tokens := splitShellTokens(strings.TrimSpace(args))
	filtered := make([]string, 0, len(tokens))
	for i := 0; i < len(tokens); i++ {
		token, ok := shellToken(tokens, i)
		if !ok {
			break
		}
		if token == "--input" {
			i++
			continue
		}
		if strings.HasPrefix(token, "--input=") {
			continue
		}
		filtered = append(filtered, token)
	}
	return parseGHAPIEndpoint(strings.Join(filtered, " "))
}

func curlOptionConsumesValue(option string) bool {
	switch option {
	case "-H", "--header", "-u", "--user", "-o", "--output", "-b", "--cookie",
		"-c", "--cookie-jar", "-A", "--user-agent", "-e", "--referer", "-m",
		"--max-time", "-K", "--config", "-T", "--upload-file":
		return true
	default:
		return false
	}
}
