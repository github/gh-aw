package workflow

import (
	"bytes"
	"compress/flate"
	"encoding/base64"
	"fmt"
	"io"
	"path"
	"slices"
	"strings"

	"github.com/github/gh-aw/pkg/constants"
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
          & $wsl.Source --set-default-version 2
          if ($LASTEXITCODE -ne 0) { throw 'WSL2 is unavailable. Enable the Windows Virtual Machine Platform feature.' }
          $distros = @(& $wsl.Source --list --quiet 2>$null) | ForEach-Object { ([string]$_).Replace([string][char]0, '').Trim() } | Where-Object { $_ }
          if ($distros -notcontains 'Ubuntu') {
            & $wsl.Source --install --web-download --distribution Ubuntu --no-launch
            if ($LASTEXITCODE -ne 0) { throw 'WSL could not install the Ubuntu distribution required by Windows agentic workflows.' }
          }
          & $wsl.Source --set-default Ubuntu
          if ($LASTEXITCODE -ne 0) { throw 'WSL could not select the Ubuntu distribution.' }
          $env:WSLENV = 'GITHUB_WORKSPACE/p:RUNNER_TEMP/p:RUNNER_TOOL_CACHE/p'
          & $wsl.Source --distribution Ubuntu --user root --exec bash -lc 'set -euo pipefail; test -d "$GITHUB_WORKSPACE"; test -d "$RUNNER_TEMP"; mkdir -p "$RUNNER_TEMP/gh-aw"; if [ -L /tmp/gh-aw ]; then [ "$(readlink -f /tmp/gh-aw)" = "$(readlink -f "$RUNNER_TEMP/gh-aw")" ]; elif [ -e /tmp/gh-aw ]; then echo "WSL bootstrap cannot map /tmp/gh-aw: the path already exists."; exit 1; else ln -s "$RUNNER_TEMP/gh-aw" /tmp/gh-aw; fi; export DEBIAN_FRONTEND=noninteractive; apt-get update -qq; apt-get install -y --no-install-recommends ca-certificates curl docker.io gh git gpgv jq nodejs npm xz-utils; if ! pgrep -x dockerd >/dev/null; then nohup dockerd --host=unix:///var/run/docker.sock >"$RUNNER_TEMP/gh-aw/dockerd.log" 2>&1 </dev/null & fi; for attempt in $(seq 1 60); do docker info >/dev/null 2>&1 && exit 0; sleep 1; done; echo "WSL Docker daemon did not become ready." >&2; tail -n 50 "$RUNNER_TEMP/gh-aw/dockerd.log"; exit 1'
          if ($LASTEXITCODE -ne 0) { throw 'WSL bootstrap failed to install Linux tools, start Docker, or map required paths.' }
`

func windowsWSLNodeBootstrapStep(nodeSetupID string) string {
	return fmt.Sprintf(`      - name: Setup Node.js in WSL
        shell: pwsh
        env:
          GH_AW_WSL_NODE_VERSION: ${{ steps.%s.outputs.node-version }}
        run: |
          $ErrorActionPreference = 'Stop'
          $env:WSLENV = 'RUNNER_TEMP/p:GH_AW_WSL_NODE_VERSION'
          & wsl.exe --distribution Ubuntu --user root --exec bash -lc 'set -euo pipefail; version="${GH_AW_WSL_NODE_VERSION#v}"; if ! [[ "$version" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]]; then echo "Invalid WSL Node.js version." >&2; exit 1; fi; archive="node-v${version}-linux-x64.tar.xz"; staging="$(mktemp -d)"; trap ''rm -rf "$staging"'' EXIT; curl -fsSL "https://nodejs.org/dist/v${version}/$archive" -o "$staging/$archive"; curl -fsSL "https://nodejs.org/dist/v${version}/SHASUMS256.txt.asc" -o "$staging/SHASUMS256.txt.asc"; curl -fsSL "https://raw.githubusercontent.com/nodejs/release-keys/481637f813e912c4aa3622d7964ab426c97b8e8d/gpg-only-active-keys/pubring.kbx" -o "$staging/nodejs-release-keyring.kbx"; gpgv --keyring "$staging/nodejs-release-keyring.kbx" --output "$staging/SHASUMS256.txt" "$staging/SHASUMS256.txt.asc"; cd "$staging"; grep -F "  $archive" SHASUMS256.txt | sha256sum -c -; install_dir="/usr/local/lib/nodejs/node-v${version}-linux-x64"; mkdir -p "$install_dir"; tar -xJf "$archive" -C "$install_dir" --strip-components=1; ln -sfn "$install_dir" /usr/local/lib/nodejs/current; ln -sfn "$install_dir/bin/node" /usr/local/bin/node; ln -sfn "$install_dir/bin/npm" /usr/local/bin/npm; ln -sfn "$install_dir/bin/npx" /usr/local/bin/npx'
          if ($LASTEXITCODE -ne 0) { throw 'WSL could not install the pinned Linux Node.js runtime.' }
`, nodeSetupID)
}

func renderStepForRunner(jobYAML, runsOn, jobName string) string {
	return renderStepForRunnerWithEnvironment(jobYAML, runsOn, jobName, windowsJobEnvironmentVariableNames(jobYAML))
}

func renderStepForRunnerWithEnvironment(jobYAML, runsOn, jobName string, jobEnvironmentVariables []string) string {
	if strings.TrimSpace(strings.TrimPrefix(runsOn, "runs-on:")) != "windows-latest" {
		return jobYAML
	}
	if _, isFrameworkJob := runtimeFeaturesBuiltInJobNames[jobName]; !isFrameworkJob {
		return jobYAML
	}

	stepsHeader := "    steps:\n"
	headerIndex := strings.Index(jobYAML, stepsHeader)
	if headerIndex < 0 {
		renderedSteps, hasWSLRun := renderWindowsSteps(jobYAML, jobEnvironmentVariables)
		if !hasWSLRun {
			return jobYAML
		}
		return renderedSteps
	}

	headerEnd := headerIndex + len(stepsHeader)
	renderedSteps, hasWSLRun := renderWindowsSteps(jobYAML[headerEnd:], jobEnvironmentVariables)
	if !hasWSLRun {
		return jobYAML
	}
	return jobYAML[:headerEnd] + windowsWSLBootstrapStep + renderedSteps
}

func renderWindowsSteps(stepsYAML string, jobEnvironmentVariables []string) (string, bool) {
	var steps []string
	var currentStep strings.Builder
	for _, line := range strings.SplitAfter(stepsYAML, "\n") {
		if strings.HasPrefix(line, "      - ") && currentStep.Len() > 0 {
			steps = append(steps, currentStep.String())
			currentStep.Reset()
		}
		currentStep.WriteString(line)
	}
	if currentStep.Len() > 0 {
		steps = append(steps, currentStep.String())
	}

	hasWSLRun := slices.ContainsFunc(steps, isWindowsPOSIXRunStep)
	if !hasWSLRun {
		return stepsYAML, false
	}

	var renderedSteps strings.Builder
	for _, step := range steps {
		step = rewriteWindowsSetupActionPath(step)
		step = rewriteWindowsActionTempPath(step)
		if isWindowsNodeSetupStep(step) {
			step, nodeSetupID := addWindowsNodeSetupID(step)
			renderedSteps.WriteString(step)
			renderedSteps.WriteString(windowsWSLNodeBootstrapStep(nodeSetupID))
			continue
		}
		renderedSteps.WriteString(renderWindowsPOSIXRunStep(step, jobEnvironmentVariables))
	}
	return renderedSteps.String(), true
}

func isWindowsNodeSetupStep(step string) bool {
	fields, ok := parseWindowsStep(step)
	if !ok {
		return false
	}
	uses, ok := fields["uses"].(string)
	return ok && strings.HasPrefix(uses, "actions/setup-node@")
}

func addWindowsNodeSetupID(step string) (string, string) {
	fields, ok := parseWindowsStep(step)
	if ok {
		if id, ok := fields["id"].(string); ok && id != "" {
			return step, id
		}
	}

	const nodeSetupID = "wsl_setup_node"
	var rendered strings.Builder
	inserted := false
	for _, line := range strings.SplitAfter(step, "\n") {
		if !inserted && strings.HasPrefix(strings.TrimSpace(line), "uses:") {
			indent := line[:len(line)-len(strings.TrimLeft(line, " "))]
			rendered.WriteString(indent + "id: " + nodeSetupID + "\n")
			inserted = true
		}
		rendered.WriteString(line)
	}
	if !inserted {
		return step, ""
	}
	return rendered.String(), nodeSetupID
}

func isWindowsPOSIXRunStep(step string) bool {
	fields, ok := parseWindowsStep(step)
	if !ok {
		return false
	}
	if _, ok := fields["run"].(string); !ok {
		return false
	}
	shell, ok := fields["shell"].(string)
	if !ok {
		shell = ""
	}
	return isWindowsPOSIXShell(shell)
}

func isWindowsPOSIXShell(shell string) bool {
	shell = strings.ToLower(strings.TrimSpace(shell))
	return shell == "" || shell == "bash" || strings.HasPrefix(shell, "bash ") || shell == "sh" || strings.HasPrefix(shell, "sh ")
}

func windowsJobEnvironmentVariableNames(jobYAML string) []string {
	var document map[string]any
	if err := yaml.Unmarshal([]byte(jobYAML), &document); err != nil {
		return nil
	}
	if environment, ok := document["env"].(map[string]any); ok {
		return validWindowsEnvironmentVariableNames(environment)
	}
	for _, value := range document {
		job, ok := value.(map[string]any)
		if !ok {
			continue
		}
		if environment, ok := job["env"].(map[string]any); ok {
			return validWindowsEnvironmentVariableNames(environment)
		}
	}
	return nil
}

func windowsStepEnvironmentVariableNames(fields map[string]any) []string {
	environment, ok := fields["env"].(map[string]any)
	if !ok {
		return nil
	}
	return validWindowsEnvironmentVariableNames(environment)
}

func validWindowsEnvironmentVariableNames(environment map[string]any) []string {
	names := make([]string, 0, len(environment))
	for name := range environment {
		if isWindowsEnvironmentVariableName(name) {
			names = append(names, name)
		}
	}
	slices.Sort(names)
	return names
}

func isWindowsEnvironmentVariableName(name string) bool {
	if name == "" {
		return false
	}
	for index, character := range name {
		if index == 0 {
			if character == '_' || character >= 'A' && character <= 'Z' || character >= 'a' && character <= 'z' {
				continue
			}
			return false
		}
		if character != '_' && (character < 'A' || character > 'Z') && (character < 'a' || character > 'z') && (character < '0' || character > '9') {
			return false
		}
	}
	return true
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
	for _, parsedStep := range parsed.Steps {
		return parsedStep, true
	}
	return nil, false
}

func rewriteWindowsActionTempPath(step string) string {
	fields, ok := parseWindowsStep(step)
	if !ok {
		return step
	}
	if _, ok := fields["uses"].(string); !ok {
		return step
	}
	pathValue := ""
	if with, ok := fields["with"].(map[string]any); ok {
		if path, ok := with["path"].(string); ok {
			pathValue = path
		}
	}
	rewriteActionPath := pathValue == constants.TmpGhAwDir || strings.HasPrefix(pathValue, constants.TmpGhAwDirSlash)
	_, rewriteEnvironmentPaths := fields["env"].(map[string]any)
	if !rewriteActionPath && !rewriteEnvironmentPaths {
		return step
	}

	var rendered strings.Builder
	pathBlockIndent := -1
	environmentIndent := -1
	for _, line := range strings.SplitAfter(step, "\n") {
		trimmed := strings.TrimSpace(line)
		indent := len(line) - len(strings.TrimLeft(line, " "))
		if pathBlockIndent >= 0 {
			if trimmed == "" || indent > pathBlockIndent {
				rendered.WriteString(rewriteWindowsArtifactPathLine(line))
				continue
			}
			pathBlockIndent = -1
		}
		if rewriteActionPath && strings.HasPrefix(trimmed, "path:") {
			value := strings.TrimSpace(strings.TrimPrefix(trimmed, "path:"))
			if strings.HasPrefix(value, "|") || strings.HasPrefix(value, ">") {
				pathBlockIndent = indent
				rendered.WriteString(line)
				continue
			}
			path := strings.Trim(value, `"'`)
			if path == constants.TmpGhAwDir || strings.HasPrefix(path, constants.TmpGhAwDirSlash) {
				newline := ""
				if strings.HasSuffix(line, "\n") {
					newline = "\n"
					line = strings.TrimSuffix(line, "\n")
				}
				indentation := line[:len(line)-len(strings.TrimLeft(line, " "))]
				replacement := constants.GhAwRootDir + strings.TrimPrefix(path, constants.TmpGhAwDir)
				line = indentation + "path: " + replacement + newline
			}
		}
		if rewriteEnvironmentPaths {
			line = rewriteWindowsActionEnvironmentLine(line, &environmentIndent)
		}
		rendered.WriteString(line)
	}
	return rendered.String()
}

func rewriteWindowsActionEnvironmentLine(line string, environmentIndent *int) string {
	trimmed := strings.TrimSpace(line)
	indent := len(line) - len(strings.TrimLeft(line, " "))
	if *environmentIndent < 0 && trimmed == "env:" {
		*environmentIndent = indent
	} else if *environmentIndent >= 0 {
		if trimmed != "" && indent <= *environmentIndent {
			*environmentIndent = -1
		} else {
			line = strings.ReplaceAll(line, constants.TmpGhAwDir, constants.GhAwRootDir)
		}
	}
	return line
}

func rewriteWindowsSetupActionPath(step string) string {
	step = strings.ReplaceAll(step, "gh-aw/actions", "gh-aw-setup/actions")
	return strings.ReplaceAll(step, "'gh-aw', 'actions'", "'gh-aw-setup', 'actions'")
}

func rewriteWindowsArtifactPathLine(line string) string {
	newline := ""
	if strings.HasSuffix(line, "\n") {
		newline = "\n"
		line = strings.TrimSuffix(line, "\n")
	}
	indent := line[:len(line)-len(strings.TrimLeft(line, " "))]
	path := strings.TrimSpace(line)
	if path != constants.TmpGhAwDir && !strings.HasPrefix(path, constants.TmpGhAwDirSlash) {
		return line + newline
	}
	return indent + constants.GhAwRootDir + strings.TrimPrefix(path, constants.TmpGhAwDir) + newline
}

func renderWindowsPOSIXRunStep(step string, jobEnvironmentVariables []string) string {
	fields, ok := parseWindowsStep(step)
	if !ok {
		return step
	}
	script, ok := fields["run"].(string)
	if !ok {
		return step
	}
	shell, ok := fields["shell"].(string)
	if !ok {
		shell = ""
	}
	if !isWindowsPOSIXShell(shell) {
		return step
	}
	stepName, ok := fields["name"].(string)
	if !ok {
		stepName = ""
	}
	script = rewriteWindowsPathOutputs(stepName, script)

	lines := strings.SplitAfter(step, "\n")
	runLine, shellLine, runIndent, runEnd, found := windowsRunStepBounds(lines)
	if !found {
		return step
	}

	environmentVariables := append(jobEnvironmentVariables, windowsStepEnvironmentVariableNames(fields)...)
	wrapper := windowsWSLRunWrapper(script, environmentVariables)
	var rendered strings.Builder
	for i, line := range lines {
		if i > runLine && i < runEnd {
			continue
		}
		switch i {
		case shellLine:
			indent := line[:len(line)-len(strings.TrimLeft(line, " "))]
			rendered.WriteString(indent + "shell: pwsh\n")
		case runLine:
			if shellLine < 0 {
				rendered.WriteString(strings.Repeat(" ", runIndent) + "shell: pwsh\n")
			}
			rendered.WriteString(strings.Repeat(" ", runIndent) + "run: |\n")
			for _, wrapperLine := range wrapper {
				rendered.WriteString(strings.Repeat(" ", runIndent+2) + wrapperLine + "\n")
			}
		default:
			rendered.WriteString(line)
		}
	}
	return rendered.String()
}

func windowsRunStepBounds(lines []string) (int, int, int, int, bool) {
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
		return 0, 0, 0, 0, false
	}

	runEnd := runLine + 1
	if blockRun {
		for i, line := range lines {
			if i <= runLine {
				continue
			}
			line = strings.TrimSuffix(line, "\n")
			if strings.TrimSpace(line) != "" && len(line)-len(strings.TrimLeft(line, " ")) <= runIndent {
				runEnd = i
				break
			}
			runEnd = i + 1
		}
	}
	return runLine, shellLine, runIndent, runEnd, true
}

func rewriteWindowsPathOutputs(stepName, script string) string {
	copyCommand := fmt.Sprintf("cp %q %s", awfConfigRuntimePathExpr, constants.AWFConfigFilePath)
	guardedCopy := fmt.Sprintf(
		"if [ %q -ef %q ]; then :; else %s; fi",
		awfConfigRuntimePathExpr,
		constants.AWFConfigFilePath,
		copyCommand,
	)
	script = strings.ReplaceAll(script, copyCommand, guardedCopy)

	switch stepName {
	case "Set runtime paths":
		for _, outputPath := range []struct {
			name string
			file string
		}{
			{name: "GH_AW_SAFE_OUTPUTS", file: "outputs.jsonl"},
			{name: "GH_AW_SAFE_OUTPUTS_CONFIG_PATH", file: "config.json"},
			{name: "GH_AW_SAFE_OUTPUTS_TOOLS_PATH", file: "tools.json"},
		} {
			artifactPath := path.Join(constants.GhAwRootDirShell, "safeoutputs", outputPath.file)
			source := fmt.Sprintf(`echo "%s=%s"`, outputPath.name, artifactPath)
			target := fmt.Sprintf(`echo "%s=$(wslpath -w "%s")"`, outputPath.name, artifactPath)
			script = strings.ReplaceAll(script, source, target)
		}
		return script
	case "Setup agent output environment variable":
		var updated strings.Builder
		for _, line := range strings.SplitAfter(script, "\n") {
			trimmed := strings.TrimSpace(line)
			const prefix = `echo "GH_AW_AGENT_OUTPUT=`
			const suffix = `" >> "$GITHUB_OUTPUT"`
			if strings.HasPrefix(trimmed, prefix) && strings.HasSuffix(trimmed, suffix) {
				path := strings.TrimSuffix(strings.TrimPrefix(trimmed, prefix), suffix)
				if path == constants.TmpGhAwDir || strings.HasPrefix(path, constants.TmpGhAwDirSlash) {
					hostPath := `"` + constants.GhAwRootDirShell + strings.TrimPrefix(path, constants.TmpGhAwDir) + `"`
					replacement := `echo "GH_AW_AGENT_OUTPUT=$(wslpath -w ` + hostPath + `)" >> "$GITHUB_OUTPUT"`
					line = strings.Replace(line, trimmed, replacement, 1)
				}
			}
			updated.WriteString(line)
		}
		return updated.String()
	default:
		return script
	}
}

func windowsWSLRunWrapper(script string, environmentVariables []string) []string {
	encodedScript, err := encodeWindowsWSLRunScript(script)
	if err != nil {
		message := strings.ReplaceAll(err.Error(), "'", "''")
		return []string{"throw 'Failed to prepare WSL command: " + message + "'"}
	}
	wrapper := windowsWSLEnvironmentSetup(environmentVariables)
	return append(wrapper, windowsWSLScriptSetup(encodedScript)...)
}

func encodeWindowsWSLRunScript(script string) (string, error) {
	var compressedScript bytes.Buffer
	compressor, err := flate.NewWriter(&compressedScript, flate.BestCompression)
	if err != nil {
		return "", err
	}
	_, writeErr := io.WriteString(compressor, script)
	closeErr := compressor.Close()
	if writeErr != nil {
		return "", writeErr
	}
	if closeErr != nil {
		return "", closeErr
	}
	return base64.StdEncoding.EncodeToString(compressedScript.Bytes()), nil
}

func windowsWSLEnvironmentSetup(environmentVariables []string) []string {
	allowedEnvironmentVariables := append([]string{
		"CI",
		"GITHUB_ACTION",
		"GITHUB_ACTIONS",
		"GITHUB_API_URL",
		"GITHUB_BASE_REF",
		"GITHUB_EVENT_NAME",
		"GITHUB_EVENT_PATH",
		"GITHUB_JOB",
		"GITHUB_REF",
		"GITHUB_REF_NAME",
		"GITHUB_REF_TYPE",
		"GITHUB_REPOSITORY",
		"GITHUB_REPOSITORY_OWNER",
		"GITHUB_RUN_ATTEMPT",
		"GITHUB_RUN_ID",
		"GITHUB_RUN_NUMBER",
		"GITHUB_SERVER_URL",
		"GITHUB_SHA",
		"GITHUB_WORKFLOW",
		"GITHUB_WORKSPACE",
		"RUNNER_ARCH",
		"RUNNER_NAME",
		"RUNNER_OS",
		"RUNNER_TEMP",
		"RUNNER_TOOL_CACHE",
		"GITHUB_ENV",
		"GITHUB_OUTPUT",
		"GITHUB_PATH",
		"GITHUB_STEP_SUMMARY",
		"GH_AW_AGENT_OUTPUT",
		"GH_AW_SAFE_OUTPUTS",
		"GH_AW_SAFE_OUTPUTS_CONFIG_PATH",
		"GH_AW_SAFE_OUTPUTS_TOOLS_PATH",
		"GH_AW_WSL_WORKING_DIRECTORY",
		"HOME",
		"TEMP",
		"TMP",
		"TMPDIR",
	}, environmentVariables...)
	slices.Sort(allowedEnvironmentVariables)
	allowedEnvironmentVariables = slices.Compact(allowedEnvironmentVariables)
	powerShellAllowedVariables := make([]string, 0, len(allowedEnvironmentVariables))
	for _, name := range allowedEnvironmentVariables {
		powerShellAllowedVariables = append(powerShellAllowedVariables, "'"+name+"'")
	}
	return []string{
		"$ErrorActionPreference = 'Stop'",
		"$pathVariables = @('GITHUB_WORKSPACE', 'RUNNER_TEMP', 'RUNNER_TOOL_CACHE', 'GITHUB_ENV', 'GITHUB_OUTPUT', 'GITHUB_PATH', 'GITHUB_STEP_SUMMARY', 'GITHUB_EVENT_PATH', 'GH_AW_AGENT_OUTPUT', 'GH_AW_SAFE_OUTPUTS', 'GH_AW_SAFE_OUTPUTS_CONFIG_PATH', 'GH_AW_SAFE_OUTPUTS_TOOLS_PATH', 'GH_AW_WSL_WORKING_DIRECTORY', 'HOME', 'TEMP', 'TMP', 'TMPDIR')",
		"$pathVariableSet = [System.Collections.Generic.HashSet[string]]::new([System.StringComparer]::OrdinalIgnoreCase)",
		"foreach ($name in $pathVariables) { [void]$pathVariableSet.Add($name) }",
		"$allowedEnvironmentVariables = @(" + strings.Join(powerShellAllowedVariables, ", ") + ")",
		"$workingDirectory = (& wsl.exe --distribution Ubuntu --user root --exec wslpath -u (Get-Location).Path).Trim()",
		"if ($LASTEXITCODE -ne 0) { throw 'WSL could not map the step working directory.' }",
		"$env:GH_AW_WSL_WORKING_DIRECTORY = $workingDirectory",
		"$processEnvironment = [System.Environment]::GetEnvironmentVariables('Process')",
		"$environmentNames = $allowedEnvironmentVariables | Where-Object { $processEnvironment.Contains($_) }",
		"$wslEnvironment = (($environmentNames | ForEach-Object { $value = [string]$processEnvironment[$_]; if ($pathVariableSet.Contains($_) -or $value -match '^(?:[A-Za-z]:[\\\\/]|\\\\\\\\)') { \"$_/p\" } else { $_ } }) -join ':')",
		"if ($wslEnvironment.Length -gt 32767) { throw 'The Windows job environment exceeds the WSL forwarding limit.' }; $env:WSLENV = $wslEnvironment",
	}
}

func windowsWSLScriptSetup(encodedScript string) []string {
	return []string{
		fmt.Sprintf("$compressedScript = [System.Convert]::FromBase64String('%s')", encodedScript),
		"$compressedStream = [System.IO.MemoryStream]::new($compressedScript)",
		"$decompressor = [System.IO.Compression.DeflateStream]::new($compressedStream, [System.IO.Compression.CompressionMode]::Decompress)",
		"$reader = [System.IO.StreamReader]::new($decompressor, [System.Text.Encoding]::UTF8)",
		"try { $scriptContent = $reader.ReadToEnd() } finally { $reader.Dispose() }",
		"$scriptContent = 'unset GH_AW_NODE_BIN; export PATH=\"/usr/local/lib/nodejs/current/bin:$PATH\"; cd \"$GH_AW_WSL_WORKING_DIRECTORY\"' + \"`n\" + $scriptContent",
		"$scriptDirectory = Join-Path $env:RUNNER_TEMP 'gh-aw'",
		"New-Item -ItemType Directory -Force -Path $scriptDirectory | Out-Null",
		"$hostScriptPath = Join-Path $scriptDirectory ('wsl-' + [guid]::NewGuid().ToString('N') + '.sh')",
		"[System.IO.File]::WriteAllText($hostScriptPath, $scriptContent, [System.Text.UTF8Encoding]::new($false))",
		"$wslScriptPath = (& wsl.exe --distribution Ubuntu --user root --exec wslpath -u $hostScriptPath).Trim()",
		"if ($LASTEXITCODE -ne 0) { throw 'WSL could not map the generated script path.' }",
		"$exitCode = 0",
		"try {",
		"  & wsl.exe --distribution Ubuntu --user root --exec bash --noprofile --norc -e -o pipefail $wslScriptPath",
		"  $exitCode = $LASTEXITCODE",
		"} finally {",
		"  Remove-Item -LiteralPath $hostScriptPath -Force -ErrorAction SilentlyContinue",
		"}",
		"if ($exitCode -ne 0) { exit $exitCode }",
	}
}
