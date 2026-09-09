package workflow

import "strings"

func renderStepForRunner(step, runsOn string) string {
	if strings.TrimSpace(strings.TrimPrefix(runsOn, "runs-on:")) != "windows-latest" || !containsRunField(step) {
		return step
	}

	return setBashShell(prefixShellScriptWithBash(step))
}

func containsRunField(step string) bool {
	for line := range strings.SplitSeq(step, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "run:") {
			return true
		}
	}
	return false
}

func prefixShellScriptWithBash(step string) string {
	lines := strings.SplitAfter(step, "\n")
	runBlock := false
	runIndent := 0
	heredoc := ""
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if heredoc != "" {
			if strings.TrimSpace(strings.TrimSuffix(line, "\n")) == heredoc {
				heredoc = ""
			}
			continue
		}

		indent := len(line) - len(strings.TrimLeft(line, " "))
		var commandLine string
		if run, ok := strings.CutPrefix(trimmed, "run:"); ok {
			commandLine = strings.TrimSpace(run)
			if commandLine == "|" || commandLine == ">" || commandLine == "|-" || commandLine == ">-" {
				runBlock = true
				runIndent = indent
				continue
			}
		} else if runBlock && trimmed != "" && indent > runIndent {
			commandLine = trimmed
		} else {
			runBlock = false
			continue
		}

		if delimiter := shellHereDocDelimiter(commandLine); delimiter != "" {
			heredoc = delimiter
		}
		command := firstShellWord(commandLine)
		if strings.HasSuffix(command, ".sh") {
			lines[i] = strings.Replace(line, commandLine, "bash "+commandLine, 1)
		}
	}
	return strings.Join(lines, "")
}

func firstShellWord(commandLine string) string {
	for {
		commandLine = strings.TrimSpace(commandLine)
		if commandLine == "" {
			return ""
		}
		word := commandLine
		if word[0] == '\'' || word[0] == '"' {
			quote := word[0]
			if end := strings.IndexByte(word[1:], quote); end >= 0 {
				word = word[:end+2]
				commandLine = commandLine[len(word):]
			} else {
				return ""
			}
		} else if end := strings.IndexAny(word, " \t"); end >= 0 {
			word = word[:end]
			commandLine = commandLine[len(word):]
		} else {
			commandLine = ""
		}
		if !strings.Contains(word, "=") || strings.HasPrefix(word, "./") {
			return strings.Trim(word, "'\"")
		}
	}
}

func shellHereDocDelimiter(commandLine string) string {
	_, delimiter, ok := strings.Cut(commandLine, "<<")
	if !ok {
		return ""
	}
	delimiter = strings.TrimSpace(delimiter)
	delimiter = strings.TrimPrefix(delimiter, "-")
	fields := strings.Fields(delimiter)
	if len(fields) == 0 {
		return ""
	}
	delimiter = fields[0]
	return strings.Trim(delimiter, "'\"")
}

func setBashShell(step string) string {
	lines := strings.SplitAfter(step, "\n")
	var result strings.Builder
	result.Grow(len(step))
	var currentStep strings.Builder

	for _, line := range lines {
		if strings.HasPrefix(line, "      - ") {
			result.WriteString(setBashShellForStep(currentStep.String()))
			currentStep.Reset()
		}
		currentStep.WriteString(line)
	}
	result.WriteString(setBashShellForStep(currentStep.String()))
	return result.String()
}

func setBashShellForStep(step string) string {
	if !strings.Contains(step, "\n        run:") {
		return step
	}
	if strings.Contains(step, "\n        shell:") {
		return step
	}
	return strings.Replace(step, "\n        run:", "\n        shell: bash\n        run:", 1)
}
