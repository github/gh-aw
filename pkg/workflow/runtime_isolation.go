package workflow

import (
	"errors"
	"fmt"
	"maps"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/github/gh-aw/pkg/constants"
	"gopkg.in/yaml.v3"
)

const runtimeRootExpression = "${{ env.GH_AW_RUNTIME_ROOT }}"
const setupRootExpression = "${{ env.GH_AW_SETUP_ROOT }}"
const windowsRuntimeRootExpression = "//wsl.localhost/Ubuntu" + runtimeRootExpression

var logicalRuntimePathRE = regexp.MustCompile(constants.TmpGhAwDir + `($|[^A-Za-z0-9_.-])`)
var logicalSetupPathRE = regexp.MustCompile(`(?:\$\{\{ runner.temp \}\}|\$\{RUNNER_TEMP\}|\$RUNNER_TEMP)/gh-aw($|[^A-Za-z0-9_.-])`)
var logicalSetupJSPathRE = regexp.MustCompile(`['"]\$\{\{ env\.GH_AW_SETUP_ROOT \}\}(/[A-Za-z0-9_./-]*)?['"]`)
var logicalSetupShellPathRE = regexp.MustCompile(`'(\$\{GH_AW_SETUP_ROOT\}[/A-Za-z0-9_.:-]*)'`)
var isolatedExecutionBindings = strings.NewReplacer(
	runtimeRootExpression, "${GH_AW_RUNTIME_ROOT}",
	"${{ env.GH_AW_RESOURCE_SUFFIX }}", "${GH_AW_RESOURCE_SUFFIX}",
)
var logicalContainerNameRE = regexp.MustCompile(`\b(awmg-mcpg|awmg-cli-proxy|awmg-proxy)($|[^A-Za-z0-9_.-])`)

// Logical paths remain stable inside generation helpers. Relocate at the final
// workflow boundary. Consumers of executable strings choose how to bind the
// setup root, which may contain arbitrary RUNNER_TEMP characters.
func relocateRuntimeText(text string) string {
	text = strings.ReplaceAll(text, "https://localhost:18443", "https://localhost:${{ env.GH_AW_DIFC_PROXY_PORT }}")
	text = strings.ReplaceAll(text, "host.docker.internal:18443", "host.docker.internal:${{ env.GH_AW_CLI_PROXY_PORT }}")
	text = strings.ReplaceAll(text, `\/tmp\/gh-aw`, constants.TmpGhAwDir)
	text = strings.ReplaceAll(text, constants.TmpGhAwDir+"/actions-source", setupRootExpression+"/actions-source")
	text = strings.ReplaceAll(text, "process.env.RUNNER_TEMP, 'gh-aw',", "process.env.GH_AW_SETUP_ROOT,")
	text = strings.ReplaceAll(text, `process.env.RUNNER_TEMP, "gh-aw",`, "process.env.GH_AW_SETUP_ROOT,")
	text = strings.ReplaceAll(text, "process.env.RUNNER_TEMP, 'gh-aw-setup',", "process.env.GH_AW_SETUP_ROOT,")
	for _, root := range []string{"gh-aw", "gh-aw-setup"} {
		text = strings.ReplaceAll(text, "${process.env.RUNNER_TEMP}/"+root+"/actions", "${process.env.GH_AW_SETUP_ROOT}/actions")
	}
	text = strings.ReplaceAll(text, constants.GhAwRootDir+"-setup/actions", setupRootExpression+"/actions")
	text = strings.ReplaceAll(text, constants.GhAwRootDirShell+"-setup/actions", setupRootExpression+"/actions")
	text = strings.ReplaceAll(text, "$RUNNER_TEMP/gh-aw-setup/actions", setupRootExpression+"/actions")
	text = strings.ReplaceAll(text, conclusionWorkQueueActivationArtifactDir, setupRootExpression+"/activation")
	for _, root := range []string{constants.GhAwRootDir, constants.GhAwRootDirShell, "$RUNNER_TEMP/gh-aw"} {
		text = strings.ReplaceAll(text, root+"-protected-capture-", setupRootExpression+"/protected-capture-")
	}
	text = strings.ReplaceAll(text, `'"${RUNNER_TEMP}"'/gh-aw/`, `'"`+setupRootExpression+`"'/`)
	text = logicalSetupPathRE.ReplaceAllString(text, setupRootExpression+"$1")
	text = logicalRuntimePathRE.ReplaceAllString(text, runtimeRootExpression+"$1")
	return logicalContainerNameRE.ReplaceAllString(text, "$1-${{ env.GH_AW_RESOURCE_SUFFIX }}$2")
}

func needsRuntimeIsolation(text string) bool {
	return relocateRuntimeText(text) != text || strings.Contains(text, "actions/setup@") ||
		strings.Contains(text, "uses: ./actions/setup") || strings.Contains(text, "GH_AW_RUNTIME_ROOT")
}

const runtimeAllocationScript = `set -euo pipefail
umask 077
GH_AW_RUNTIME_ROOT=""
GH_AW_SETUP_ROOT=""
trap 'status=$?; if [[ -n "$GH_AW_SETUP_ROOT" ]]; then rm -rf -- "$GH_AW_SETUP_ROOT"; fi; if [[ -n "$GH_AW_RUNTIME_ROOT" ]]; then rm -rf -- "$GH_AW_RUNTIME_ROOT"; fi; exit "$status"' EXIT
GH_AW_RUNTIME_ROOT="$(mktemp -d /tmp/gh-aw-run.XXXXXXXXXX)"
setup_parent="$(cd -- "${RUNNER_TEMP:?}" && pwd -P)"
GH_AW_SETUP_ROOT="$(mktemp -d "$setup_parent/gh-aw-setup.XXXXXXXXXX")"
GH_AW_RESOURCE_SUFFIX="${GH_AW_RUNTIME_ROOT##*/gh-aw-run.}"
GH_AW_RUNTIME_TOKEN="$(openssl rand -hex 32)"
printf '::add-mask::%s\n' "$GH_AW_RUNTIME_TOKEN"
owner_hash="$(printf '%s' "$GH_AW_RUNTIME_TOKEN" | sha256sum | cut -d ' ' -f1)"
printf '%s' "$owner_hash" > "$GH_AW_RUNTIME_ROOT/.gh-aw-runtime-owner"
printf '%s' "$owner_hash" > "$GH_AW_SETUP_ROOT/.gh-aw-runtime-owner"
case "$GH_AW_SETUP_ROOT" in *$'\n'*|*$'\r'*) echo "Invalid setup root" >&2; exit 1 ;; esac
printf 'GH_AW_RUNTIME_ROOT=%s\nGH_AW_SETUP_ROOT=%s\nGH_AW_RESOURCE_SUFFIX=%s\nGH_AW_RUNTIME_TOKEN=%s\n' "$GH_AW_RUNTIME_ROOT" "$GH_AW_SETUP_ROOT" "$GH_AW_RESOURCE_SUFFIX" "$GH_AW_RUNTIME_TOKEN" >> "$GITHUB_ENV"
printf 'GH_AW_RUNTIME_ID=%s\nGH_AW_SETUP_ID=%s\n' "$(stat -c '%d:%i' "$GH_AW_RUNTIME_ROOT")" "$(stat -c '%d:%i' "$GH_AW_SETUP_ROOT")" >> "$GITHUB_ENV"
trap - EXIT
`

const runtimeEarlyCleanupScript = `set -euo pipefail
if [[ -z "${GH_AW_RUNTIME_ROOT:-}" || -z "${GH_AW_SETUP_ROOT:-}" || -z "${GH_AW_RUNTIME_TOKEN:-}" ]]; then exit 0; fi
if [[ ! "$GH_AW_RUNTIME_ROOT" =~ ^/tmp/gh-aw-run\.[A-Za-z0-9]+$ ]]; then echo "::warning::Invalid runtime cleanup root"; exit 0; fi
owner_hash="$(printf '%s' "$GH_AW_RUNTIME_TOKEN" | sha256sum | cut -d ' ' -f1)" || exit 0
for kind in runtime setup; do
  if [[ "$kind" == runtime ]]; then root="$GH_AW_RUNTIME_ROOT"; identity="${GH_AW_RUNTIME_ID:-}"; else root="$GH_AW_SETUP_ROOT"; identity="${GH_AW_SETUP_ID:-}"; fi
  if [[ ! -e "$root" && ! -L "$root" ]]; then continue; fi
  if [[ -L "$root" || ! -d "$root" || "$(stat -c '%d:%i' "$root")" != "$identity" || "$(stat -c '%u' "$root")" != "$(id -u)" || "$(stat -c '%a' "$root")" != 700 ]]; then echo "::warning::Refusing unowned runtime cleanup"; continue; fi
  marker="$root/.gh-aw-runtime-owner"
  if [[ -L "$marker" || ! -f "$marker" || "$(cat -- "$marker")" != "$owner_hash" ]]; then echo "::warning::Refusing runtime cleanup without ownership"; continue; fi
  rm -rf -- "$root" || echo "::warning::Could not fully remove isolated $kind directory"
done
`

func runtimeEarlyCleanupStep(windows bool) string {
	var step strings.Builder
	step.WriteString("      - name: Clean isolated runtime directories\n        if: ${{ always() && env.GH_AW_SETUP_ACTION_COMPLETE != 'true' }}\n")
	if windows {
		step.WriteString("        shell: pwsh\n        run: |\n")
		for _, line := range windowsWSLRawRunWrapper(runtimeEarlyCleanupScript, []string{"GH_AW_RUNTIME_ID", "GH_AW_SETUP_ID"}) {
			step.WriteString("          " + line + "\n")
		}
		return step.String()
	}
	step.WriteString("        shell: bash\n        run: |\n")
	for line := range strings.SplitSeq(strings.TrimSuffix(runtimeEarlyCleanupScript, "\n"), "\n") {
		step.WriteString("          " + line + "\n")
	}
	return step.String()
}

type runtimeYAMLEdit struct {
	start, end int
	text       string
}

func runtimeMappingValue(node *yaml.Node, name string) *yaml.Node {
	if node == nil || node.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(node.Content); i += 2 {
		if node.Content[i].Value == name { //nolint:uncheckedsliceindex // i+1 is bounded by the loop.
			return node.Content[i+1] //nolint:uncheckedsliceindex // i+1 is bounded by the loop.
		}
	}
	return nil
}

// Runtime-dependent job/workflow env cannot use the env expression context.
// Move just those entries to GITHUB_ENV in the allocator; step env continues to
// override them normally, and non-runtime job overrides suppress inheritance.
func runtimeEnvironmentEdits(lines []string, env *yaml.Node) (map[string]string, []runtimeYAMLEdit, error) {
	values := make(map[string]string)
	if env == nil || env.Kind != yaml.MappingNode {
		return values, nil, nil
	}
	remaining := *env
	remaining.Content = nil
	for i := 0; i+1 < len(env.Content); i += 2 {
		key, value := env.Content[i], env.Content[i+1] //nolint:uncheckedsliceindex // i+1 is bounded by the loop.
		if !needsRuntimeIsolation(value.Value) {
			remaining.Content = append(remaining.Content, key, value)
			continue
		}
		values[key.Value] = value.Value
	}
	if len(values) == 0 {
		return values, nil, nil
	}
	start := env.Line - 1
	for start > 0 && !strings.HasPrefix(strings.TrimSpace(lines[start]), "env:") { //nolint:uncheckedsliceindex // The node's original line is within this source.
		start--
	}
	indent := leadingSpaces(lines[start]) //nolint:uncheckedsliceindex // start is a nonnegative original source line.
	end := start + 1
	for end < len(lines) && (strings.TrimSpace(lines[end]) == "" || leadingSpaces(lines[end]) > indent) { //nolint:uncheckedsliceindex // end starts after start and is bounded above.
		end++
	}
	var replacement strings.Builder
	if len(remaining.Content) > 0 {
		encoded, err := yaml.Marshal(&remaining)
		if err != nil {
			return nil, nil, fmt.Errorf("cannot render scalar runtime environment: %w", err)
		}
		replacement.WriteString(strings.Repeat(" ", indent) + "env:\n")
		for line := range strings.SplitSeq(strings.TrimSuffix(string(encoded), "\n"), "\n") {
			replacement.WriteString(strings.Repeat(" ", indent+2) + line + "\n")
		}
	}
	return values, []runtimeYAMLEdit{{start: start, end: end, text: replacement.String()}}, nil
}

func runtimeAllocationStep(values map[string]string) string {
	var step strings.Builder
	step.WriteString("      - name: Allocate isolated runtime directories\n        id: gh-aw-runtime\n        shell: bash\n")
	if len(values) > 0 {
		step.WriteString("        env:\n")
		for i, key := range sortedRuntimeEnvKeys(values) {
			fmt.Fprintf(&step, "          GH_AW_INITIAL_ENV_%d: %s\n", i, strconv.Quote(values[key])) //nolint:fprintferrorunchecked // strings.Builder writes cannot fail.
		}
	}
	step.WriteString("        run: |\n")
	for line := range strings.SplitSeq(strings.TrimSuffix(runtimeAllocationScript, "\n"), "\n") {
		step.WriteString("          " + line + "\n")
	}
	for i, key := range sortedRuntimeEnvKeys(values) {
		fmt.Fprintf(&step, "          value=\"$GH_AW_INITIAL_ENV_%d\"\n", i) //nolint:fprintferrorunchecked // strings.Builder writes cannot fail.
		// Treat inherited values as data, never shell source.
		step.WriteString("          value=\"${value//\\/tmp\\/gh-aw/$GH_AW_RUNTIME_ROOT}\"\n")
		step.WriteString("          source=\"${RUNNER_TEMP}/gh-aw\"\n          value=\"${value//\"$source\"/$GH_AW_SETUP_ROOT}\"\n")
		for _, prefix := range []string{constants.GhAwRootDirShell, "$RUNNER_TEMP/gh-aw"} {
			fmt.Fprintf(&step, "          source=%s\n", shellEscapeArg(prefix)) //nolint:fprintferrorunchecked // strings.Builder writes cannot fail.
			step.WriteString("          value=\"${value//\"$source\"/$GH_AW_SETUP_ROOT}\"\n")
		}
		fmt.Fprintf(&step, "          printf '%%s<<%%s\\n%%s\\n%%s\\n' %s \"$GH_AW_RUNTIME_TOKEN\" \"$value\" \"$GH_AW_RUNTIME_TOKEN\" >> \"$GITHUB_ENV\"\n", shellEscapeArg(key)) //nolint:fprintferrorunchecked // strings.Builder writes cannot fail.
	}
	return step.String()
}

func windowsRuntimeAllocationStep(values map[string]string) string {
	fields, _ := parseWindowsStep(runtimeAllocationStep(values))
	script, ok := fields["run"].(string)
	if !ok {
		return ""
	}
	script = strings.Replace(script, `"$GH_AW_RUNTIME_ROOT" "$GH_AW_SETUP_ROOT" "$GH_AW_RESOURCE_SUFFIX"`,
		`"$GH_AW_RUNTIME_ROOT" "$(wslpath -w "$GH_AW_SETUP_ROOT")" "$GH_AW_RESOURCE_SUFFIX"`, 1)
	script = strings.Replace(script, `mktemp -d "$setup_parent/gh-aw-setup.XXXXXXXXXX"`, "mktemp -d /tmp/gh-aw-setup.XXXXXXXXXX", 1)
	var step strings.Builder
	step.WriteString("      - name: Allocate isolated runtime directories\n        id: gh-aw-runtime\n        shell: pwsh\n")
	var names []string
	if len(values) > 0 {
		step.WriteString("        env:\n")
		for i, key := range sortedRuntimeEnvKeys(values) {
			name := fmt.Sprintf("GH_AW_INITIAL_ENV_%d", i)
			names = append(names, name)
			fmt.Fprintf(&step, "          %s: %s\n", name, strconv.Quote(values[key])) //nolint:fprintferrorunchecked // strings.Builder writes cannot fail.
		}
	}
	step.WriteString("        run: |\n")
	for _, line := range windowsWSLRawRunWrapper(script, names) {
		step.WriteString("          " + line + "\n")
	}
	return step.String()
}

func sortedRuntimeEnvKeys(values map[string]string) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	return keys
}

func finalizeRuntimeIsolation(content string) (string, error) {
	// This finalizer is also used on individual step fragments.
	if !strings.Contains(content, "\njobs:") && !strings.HasPrefix(content, "jobs:") {
		return content, nil
	}
	var err error
	content, err = prepareScriptRuntimeCacheSaves(content)
	if err != nil {
		return "", err
	}
	var document yaml.Node
	if err := yaml.Unmarshal([]byte(content), &document); err != nil {
		return "", fmt.Errorf("cannot isolate runtime paths: %w", err)
	}
	if len(document.Content) != 1 {
		return content, nil
	}
	root := document.Content[0] //nolint:uncheckedsliceindex // Exactly one document is required above.
	jobs := runtimeMappingValue(root, "jobs")
	if jobs == nil || jobs.Kind != yaml.MappingNode {
		return content, nil
	}
	lines := strings.SplitAfter(content, "\n")
	inherited, edits, err := runtimeEnvironmentEdits(lines, runtimeMappingValue(root, "env"))
	if err != nil {
		return "", err
	}
	for i := 0; i+1 < len(jobs.Content); i += 2 {
		job := jobs.Content[i+1] //nolint:uncheckedsliceindex // i+1 is bounded by the loop.
		if runtimeMappingValue(job, "uses") != nil && len(inherited) > 0 {
			return "", errors.New("runtime-dependent workflow env cannot be inherited by a reusable workflow caller; move those values to steps in the called workflow")
		}
		steps := runtimeMappingValue(job, "steps")
		if steps == nil || steps.Kind != yaml.SequenceNode || len(steps.Content) == 0 {
			continue // Reusable callers do not execute runtime steps.
		}
		start := jobs.Content[i].Line - 1 //nolint:uncheckedsliceindex // i+1 is bounded by the loop.
		end := len(lines)
		if i+2 < len(jobs.Content) {
			end = jobs.Content[i+2].Line - 1 //nolint:uncheckedsliceindex // i+2 is bounded by the guard.
		}
		if !needsRuntimeIsolation(strings.Join(lines[start:end], "")) && len(inherited) == 0 {
			continue
		}
		jobEdits, err := runtimeJobIsolationEdits(lines, job, steps, end, inherited)
		if err != nil {
			return "", err
		}
		edits = append(edits, jobEdits...)
	}
	return applyRuntimeYAMLEdits(lines, edits), nil
}

func runtimeJobIsolationEdits(lines []string, job, steps *yaml.Node, end int, inherited map[string]string) ([]runtimeYAMLEdit, error) {
	values := maps.Clone(inherited)
	env := runtimeMappingValue(job, "env")
	if env != nil && env.Kind == yaml.MappingNode {
		for j := 0; j+1 < len(env.Content); j += 2 {
			delete(values, env.Content[j].Value) //nolint:uncheckedsliceindex // j+1 is bounded by the loop.
		}
	}
	jobValues, edits, err := runtimeEnvironmentEdits(lines, env)
	if err != nil {
		return nil, err
	}
	maps.Copy(values, jobValues)
	// Allocate before checkouts, downloads and hooks, not merely before setup.
	stepStart := steps.Content[0].Line - 1 //nolint:uncheckedsliceindex // The caller requires a nonempty sequence.
	allocation := runtimeAllocationStep(values)
	runner := runtimeMappingValue(job, "runs-on")
	windows := runner != nil && runner.Value == "windows-latest"
	if windows {
		// WSL tool installation consumes no runtime files.
		name := runtimeMappingValue(steps.Content[0], "name") //nolint:uncheckedsliceindex // The caller requires a nonempty sequence.
		if len(steps.Content) < 2 || name == nil || name.Value != "Initialize WSL runtime" {
			return nil, errors.New("windows runtime job requires the WSL bootstrap before allocation")
		}
		stepStart = steps.Content[1].Line - 1 //nolint:uncheckedsliceindex // The preceding guard requires two steps.
		allocation = windowsRuntimeAllocationStep(values)
	}
	edits = append(edits, runtimeYAMLEdit{start: stepStart, end: stepStart, text: allocation})
	edits = append(edits, runtimeYAMLEdit{start: end, end: end, text: runtimeEarlyCleanupStep(windows)})
	return append(edits, runtimeStepRelocationEdits(lines, stepStart, end)...), nil
}

func runtimeStepRelocationEdits(lines []string, start, end int) []runtimeYAMLEdit {
	var edits []runtimeYAMLEdit
	inExecutable := false
	javascript := false
	agentExecution := false
	executableIndent := -1
	for line := start; line < end; line++ {
		source := lines[line] //nolint:uncheckedsliceindex // Parsed job positions bound start and end within this source.
		rewritten := relocateRuntimeText(source)
		if strings.HasPrefix(source, "      - ") {
			agentExecution = false
		}
		if strings.TrimSpace(source) == "id: agentic_execution" {
			agentExecution = true
		}
		if inExecutable && strings.TrimSpace(source) != "" && leadingSpaces(source) <= executableIndent {
			inExecutable = false
		}
		if matches := blockScalarHeaderRE.FindStringSubmatch(strings.TrimSuffix(source, "\n")); matches != nil {
			inExecutable = true
			javascript = matches[2] == "script"
			executableIndent = len(matches[1]) //nolint:uncheckedsliceindex // A successful match includes the declared capture group.
		}
		if (inExecutable && javascript) || (singleLineExecutableRE.MatchString(source) && strings.HasPrefix(strings.TrimSpace(source), "script:")) {
			rewritten = logicalSetupJSPathRE.ReplaceAllString(rewritten, "`$${process.env.GH_AW_SETUP_ROOT}$1`")
		} else if inExecutable || singleLineExecutableRE.MatchString(source) {
			// Setup roots inherit arbitrary RUNNER_TEMP characters: keep them
			// data-bound, never splice them into executable Actions expressions.
			rewritten = strings.ReplaceAll(rewritten, setupRootExpression, "${GH_AW_SETUP_ROOT}")
			rewritten = logicalSetupShellPathRE.ReplaceAllString(rewritten, `"$1"`)
			if agentExecution {
				// AWF arguments and JSON expand in Bash. Keeping the generated
				// execution body expression-free avoids Actions' scalar limit.
				rewritten = isolatedExecutionBindings.Replace(rewritten)
			}
		}
		if rewritten != source {
			edits = append(edits, runtimeYAMLEdit{start: line, end: line + 1, text: rewritten})
		}
	}
	return edits
}

func applyRuntimeYAMLEdits(lines []string, edits []runtimeYAMLEdit) string {
	slices.SortStableFunc(edits, func(a, b runtimeYAMLEdit) int {
		if a.start != b.start {
			return a.start - b.start
		}
		return a.end - b.end
	})
	var result strings.Builder
	last := 0
	for _, edit := range edits {
		result.WriteString(strings.Join(lines[last:edit.start], ""))
		result.WriteString(edit.text)
		last = edit.end
	}
	result.WriteString(strings.Join(lines[last:], ""))
	return result.String()
}
