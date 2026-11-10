package workflow

import (
	"encoding/base64"
	"fmt"
	"strings"

	"go.yaml.in/yaml/v3"
)

const windowsWSLBootstrapStep = `      - name: Initialize WSL runtime
        shell: pwsh
        run: |
          $ErrorActionPreference = 'Stop'
          $wsl = Get-Command wsl.exe -ErrorAction SilentlyContinue
          if (-not $wsl) { throw 'WSL is required for Windows agentic workflows, but wsl.exe was not found.' }
          & $wsl.Source --status *> $null
          if ($LASTEXITCODE -ne 0) { throw 'WSL is unavailable. Enable WSL2 and install a Linux distribution on this runner.' }
          $env:WSLENV = 'GITHUB_WORKSPACE/p:RUNNER_TEMP/p:RUNNER_TOOL_CACHE/p'
          & $wsl.Source --exec bash -lc 'set -e; test -d "$GITHUB_WORKSPACE"; test -d "$RUNNER_TEMP"; mkdir -p "$RUNNER_TEMP/gh-aw"; if [ -L /tmp/gh-aw ]; then [ "$(readlink -f /tmp/gh-aw)" = "$(readlink -f "$RUNNER_TEMP/gh-aw")" ]; elif [ -e /tmp/gh-aw ]; then echo "WSL bootstrap cannot map /tmp/gh-aw: the path already exists."; exit 1; else ln -s "$RUNNER_TEMP/gh-aw" /tmp/gh-aw; fi'
          if ($LASTEXITCODE -ne 0) { throw 'WSL bootstrap failed to map the workspace, runner temp, or /tmp/gh-aw paths.' }
`

func renderStepForRunner(jobYAML, runsOn string) string {
	if strings.TrimSpace(strings.TrimPrefix(runsOn, "runs-on:")) != "windows-latest" {
		return jobYAML
	}

	stepsHeader := "    steps:\n"
	headerIndex := strings.Index(jobYAML, stepsHeader)
	if headerIndex < 0 {
		return jobYAML
	}

	headerEnd := headerIndex + len(stepsHeader)
	var steps []string
	var currentStep strings.Builder
	for _, line := range strings.SplitAfter(jobYAML[headerEnd:], "\n") {
		if strings.HasPrefix(line, "      - ") && currentStep.Len() > 0 {
			steps = append(steps, currentStep.String())
			currentStep.Reset()
		}
		currentStep.WriteString(line)
	}
	if currentStep.Len() > 0 {
		steps = append(steps, currentStep.String())
	}

	hasWSLRun := false
	for _, step := range steps {
		if isWindowsPOSIXRunStep(step) {
			hasWSLRun = true
			break
		}
	}
	if !hasWSLRun {
		return jobYAML
	}

	var renderedSteps strings.Builder
	renderedSteps.WriteString(windowsWSLBootstrapStep)
	for _, step := range steps {
		step = rewriteWindowsActionTempPath(step)
		renderedSteps.WriteString(renderWindowsPOSIXRunStep(step))
	}
	return jobYAML[:headerEnd] + renderedSteps.String()
}

func isWindowsPOSIXRunStep(step string) bool {
	fields, ok := parseWindowsStep(step)
	if !ok {
		return false
	}
	if _, ok := fields["run"].(string); !ok {
		return false
	}
	shell, _ := fields["shell"].(string)
	return isWindowsPOSIXShell(shell)
}

func isWindowsPOSIXShell(shell string) bool {
	shell = strings.ToLower(strings.TrimSpace(shell))
	return shell == "" || shell == "bash" || strings.HasPrefix(shell, "bash ") || shell == "sh" || strings.HasPrefix(shell, "sh ")
}

func parseWindowsStep(step string) (map[string]any, bool) {
	var yamlStep strings.Builder
	yamlStep.WriteString("steps:\n")
	for _, line := range strings.SplitAfter(step, "\n") {
		yamlStep.WriteString("  ")
		yamlStep.WriteString(strings.TrimPrefix(line, "    "))
	}

	var parsed struct {
		Steps []map[string]any `yaml:"steps"`
	}
	if err := yaml.Unmarshal([]byte(yamlStep.String()), &parsed); err != nil || len(parsed.Steps) != 1 {
		return nil, false
	}
	return parsed.Steps[0], true
}

func rewriteWindowsActionTempPath(step string) string {
	fields, ok := parseWindowsStep(step)
	if !ok {
		return step
	}
	if _, ok := fields["uses"].(string); !ok {
		return step
	}
	with, ok := fields["with"].(map[string]any)
	if !ok {
		return step
	}
	path, ok := with["path"].(string)
	if !ok || !strings.HasPrefix(path, "/tmp/gh-aw") {
		return step
	}

	lines := strings.SplitAfter(step, "\n")
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, "path:") {
			continue
		}
		value := strings.TrimSpace(strings.TrimPrefix(trimmed, "path:"))
		if strings.HasPrefix(value, "|") || strings.HasPrefix(value, ">") {
			pathIndent := len(line) - len(strings.TrimLeft(line, " "))
			for j := i + 1; j < len(lines); j++ {
				lineWithoutNewline := strings.TrimSuffix(lines[j], "\n")
				if strings.TrimSpace(lineWithoutNewline) != "" &&
					len(lineWithoutNewline)-len(strings.TrimLeft(lineWithoutNewline, " ")) <= pathIndent {
					break
				}
				lines[j] = rewriteWindowsArtifactPathLine(lines[j])
			}
			break
		}
		newline := ""
		if strings.HasSuffix(line, "\n") {
			newline = "\n"
			line = strings.TrimSuffix(line, "\n")
		}
		indent := line[:len(line)-len(strings.TrimLeft(line, " "))]
		replacement := "${{ runner.temp }}/gh-aw" + strings.TrimPrefix(strings.Trim(value, `"'`), "/tmp/gh-aw")
		lines[i] = indent + "path: " + replacement + newline
		break
	}
	return strings.Join(lines, "")
}

func rewriteWindowsArtifactPathLine(line string) string {
	newline := ""
	if strings.HasSuffix(line, "\n") {
		newline = "\n"
		line = strings.TrimSuffix(line, "\n")
	}
	indent := line[:len(line)-len(strings.TrimLeft(line, " "))]
	path := strings.TrimSpace(line)
	if !strings.HasPrefix(path, "/tmp/gh-aw") {
		return line + newline
	}
	return indent + "${{ runner.temp }}/gh-aw" + strings.TrimPrefix(path, "/tmp/gh-aw") + newline
}

func renderWindowsPOSIXRunStep(step string) string {
	fields, ok := parseWindowsStep(step)
	if !ok {
		return step
	}
	script, ok := fields["run"].(string)
	if !ok {
		return step
	}
	shell, _ := fields["shell"].(string)
	if !isWindowsPOSIXShell(shell) {
		return step
	}

	lines := strings.SplitAfter(step, "\n")
	runLine := -1
	shellLine := -1
	runIndent := 0
	blockRun := false
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if _, value, ok := strings.Cut(trimmed, "run:"); ok && strings.HasPrefix(trimmed, "run:") {
			runLine = i
			runIndent = len(line) - len(strings.TrimLeft(line, " "))
			value = strings.TrimSpace(value)
			blockRun = strings.HasPrefix(value, "|") || strings.HasPrefix(value, ">")
		}
		if _, _, ok := strings.Cut(trimmed, "shell:"); ok && strings.HasPrefix(trimmed, "shell:") {
			shellLine = i
		}
	}
	if runLine < 0 {
		return step
	}

	runEnd := runLine + 1
	if blockRun {
		for runEnd < len(lines) {
			line := strings.TrimSuffix(lines[runEnd], "\n")
			if strings.TrimSpace(line) != "" && len(line)-len(strings.TrimLeft(line, " ")) <= runIndent {
				break
			}
			runEnd++
		}
	}

	wrapper := windowsWSLRunWrapper(script)
	var rendered strings.Builder
	for i := 0; i < len(lines); i++ {
		switch {
		case i == shellLine:
			indent := lines[i][:len(lines[i])-len(strings.TrimLeft(lines[i], " "))]
			rendered.WriteString(indent + "shell: pwsh\n")
		case i == runLine:
			if shellLine < 0 {
				rendered.WriteString(strings.Repeat(" ", runIndent) + "shell: pwsh\n")
			}
			rendered.WriteString(strings.Repeat(" ", runIndent) + "run: |\n")
			for _, wrapperLine := range wrapper {
				rendered.WriteString(strings.Repeat(" ", runIndent+2) + wrapperLine + "\n")
			}
			i = runEnd - 1
		default:
			rendered.WriteString(lines[i])
		}
	}
	return rendered.String()
}

func windowsWSLRunWrapper(script string) []string {
	encodedScript := base64.StdEncoding.EncodeToString([]byte(script))
	return []string{
		"$ErrorActionPreference = 'Stop'",
		"$pathVariables = @('GITHUB_WORKSPACE', 'RUNNER_TEMP', 'RUNNER_TOOL_CACHE', 'GITHUB_ENV', 'GITHUB_OUTPUT', 'GITHUB_PATH', 'GITHUB_STEP_SUMMARY', 'GITHUB_EVENT_PATH', 'HOME', 'TEMP', 'TMP', 'TMPDIR')",
		"$pathVariableSet = [System.Collections.Generic.HashSet[string]]::new([System.StringComparer]::OrdinalIgnoreCase)",
		"foreach ($name in $pathVariables) { [void]$pathVariableSet.Add($name) }",
		"$environmentNames = [System.Environment]::GetEnvironmentVariables('Process').Keys | Where-Object { $_ -notin @('PATH', 'WSLENV') }",
		"$env:WSLENV = (($environmentNames | ForEach-Object { if ($pathVariableSet.Contains($_)) { \"$_/p\" } else { $_ } }) -join ':')",
		fmt.Sprintf("$scriptContent = [System.Text.Encoding]::UTF8.GetString([System.Convert]::FromBase64String('%s'))", encodedScript),
		`$scriptContent = 'cd "$GITHUB_WORKSPACE"' + [System.Environment]::NewLine + $scriptContent`,
		"$scriptDirectory = Join-Path $env:RUNNER_TEMP 'gh-aw'",
		"New-Item -ItemType Directory -Force -Path $scriptDirectory | Out-Null",
		"$hostScriptPath = Join-Path $scriptDirectory ('wsl-' + [guid]::NewGuid().ToString('N') + '.sh')",
		"[System.IO.File]::WriteAllText($hostScriptPath, $scriptContent, [System.Text.UTF8Encoding]::new($false))",
		"$wslScriptPath = (& wsl.exe --exec wslpath -u $hostScriptPath).Trim()",
		"if ($LASTEXITCODE -ne 0) { throw 'WSL could not map the generated script path.' }",
		"$exitCode = 0",
		"try {",
		"  & wsl.exe --exec bash --noprofile --norc -e -o pipefail $wslScriptPath",
		"  $exitCode = $LASTEXITCODE",
		"} finally {",
		"  Remove-Item -LiteralPath $hostScriptPath -Force -ErrorAction SilentlyContinue",
		"}",
		"if ($exitCode -ne 0) { exit $exitCode }",
	}
}
