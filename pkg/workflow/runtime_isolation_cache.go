package workflow

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"strings"

	"github.com/github/gh-aw/pkg/constants"
	"gopkg.in/yaml.v3"
)

// prepareScriptRuntimeCacheSaves also bridges private paths in both action
// modes. Actions cache hashes its path input and archives paths relative to
// the workspace, so stage an archive under a stable workspace-relative path.
func prepareScriptRuntimeCacheSaves(content string) (string, error) {
	if !strings.Contains(content, "actions/cache") {
		return content, nil
	}
	var document yaml.Node
	if err := yaml.Unmarshal([]byte(content), &document); err != nil {
		return "", err
	}
	if len(document.Content) != 1 {
		return content, nil
	}
	jobs := runtimeMappingValue(document.Content[0], "jobs") //nolint:uncheckedsliceindex // Exactly one document is required above.
	if jobs == nil {
		return content, nil
	}
	lines := strings.SplitAfter(content, "\n")
	var edits []runtimeYAMLEdit
	for i := 1; i < len(jobs.Content); i += 2 {
		job := jobs.Content[i] //nolint:uncheckedsliceindex // i is bounded by the loop.
		steps := runtimeMappingValue(job, "steps")
		if steps == nil || steps.Kind != yaml.SequenceNode {
			continue
		}
		jobEdits, err := runtimeCacheJobEdits(lines, job, steps)
		if err != nil {
			return "", err
		}
		edits = append(edits, jobEdits...)
	}
	return applyRuntimeYAMLEdits(lines, edits), nil
}

func runtimeCacheJobEdits(lines []string, job, steps *yaml.Node) ([]runtimeYAMLEdit, error) {
	runner := runtimeMappingValue(job, "runs-on")
	windows := runner != nil && runner.Value == "windows-latest"
	cleanupLine := -1
	finalLine := 0
	for _, step := range steps.Content {
		finalLine = runtimeCacheStepEnd(lines, step)
		name := runtimeMappingValue(step, "name")
		run := runtimeMappingValue(step, "run")
		if run != nil && (name != nil && name.Value == "Clean Scripts" || strings.Contains(run.Value, "/actions/setup/clean.sh")) {
			cleanupLine = step.Line - 1
		}
	}
	var edits []runtimeYAMLEdit
	var saves strings.Builder
	for index, step := range steps.Content {
		uses := runtimeMappingValue(step, "uses")
		inputs := runtimeMappingValue(step, "with")
		path := runtimeMappingValue(inputs, "path")
		if uses == nil || path == nil || path.Kind != yaml.ScalarNode {
			continue
		}
		action, ref, ok := strings.Cut(uses.Value, "@")
		if !ok || action != "actions/cache" && action != "actions/cache/restore" && action != "actions/cache/save" {
			continue
		}
		stage := newRuntimeCacheStage(path.Value, index, windows)
		if stage == nil && (cleanupLine < 0 || action != "actions/cache" || !needsRuntimeIsolation(path.Value) && !strings.Contains(path.Value, "${{")) {
			continue
		}
		replacement, save, err := renderRuntimeCacheActions(step, action, ref, index, stage)
		if err != nil {
			return nil, err
		}
		edits = append(edits, runtimeYAMLEdit{start: step.Line - 1, end: runtimeCacheStepEnd(lines, step), text: replacement})
		saves.WriteString(save)
	}
	if saves.Len() > 0 {
		if cleanupLine < 0 {
			cleanupLine = finalLine
		}
		edits = append(edits, runtimeYAMLEdit{start: cleanupLine, end: cleanupLine, text: saves.String()})
	}
	return edits, nil
}

func runtimeCacheStepEnd(lines []string, step *yaml.Node) int {
	end := step.Line
	indent := step.Column - 2
	for end < len(lines) && (strings.TrimSpace(lines[end]) == "" || leadingSpaces(lines[end]) > indent) { //nolint:uncheckedsliceindex // end advances within lines.
		end++
	}
	return end
}

type runtimeCacheStage struct {
	dir, patterns, actionPath, id string
	windows                       bool
}

func newRuntimeCacheStage(value string, index int, windows bool) *runtimeCacheStage {
	var paths, patterns []string
	for line := range strings.SplitSeq(value, "\n") {
		line = strings.TrimSpace(line)
		exclude := strings.HasPrefix(line, "!")
		pattern := strings.TrimPrefix(line, "!")
		for _, prefix := range []string{constants.TmpGhAwDirSlash, runtimeRootExpression + "/", windowsRuntimeRootExpression + "/"} {
			if suffix, found := strings.CutPrefix(pattern, prefix); found {
				if exclude {
					suffix = "!" + suffix
				}
				patterns = append(patterns, suffix)
				pattern = ""
				break
			}
		}
		if pattern != "" {
			paths = append(paths, line)
		}
	}
	if len(patterns) == 0 {
		return nil
	}
	hash := sha256.Sum256([]byte(strings.Join(patterns, "\n")))
	dir := fmt.Sprintf(".gh-aw-cache-v1-%x", hash[:12])
	return &runtimeCacheStage{
		dir: dir, patterns: strings.Join(patterns, "\n"), windows: windows,
		actionPath: strings.Join(append(paths, dir+"/runtime.tar"), "\n"),
		id:         fmt.Sprintf("gh-aw-runtime-cache-%d", index),
	}
}

func renderRuntimeCacheActions(step *yaml.Node, action, ref string, index int, stage *runtimeCacheStage) (string, string, error) {
	var fields map[string]any
	if err := step.Decode(&fields); err != nil {
		return "", "", err
	}
	if action == "actions/cache/save" {
		rendered, err := renderStagedRuntimeCache(fields, stage, true)
		return rendered, "", err
	}
	if action == "actions/cache/restore" {
		rendered, err := renderStagedRuntimeCache(fields, stage, false)
		return rendered, "", err
	}
	fields["uses"] = "actions/cache/restore@" + ref
	id, ok := fields["id"].(string)
	if !ok {
		id = fmt.Sprintf("gh-aw-runtime-cache-%d", index)
		fields["id"] = id
	}
	// Copy the input settings before rendering the restore, which changes path
	// to the archive and conditions the action on staging being available.
	inputs := runtimeMappingValue(step, "with")
	condition := fmt.Sprintf("success() && steps.%s.outcome == 'success' && steps.%s.outputs.cache-primary-key != '' && steps.%s.outputs.cache-hit != 'true'", id, id, id)
	saveInputs := map[string]any{
		"path": runtimeMappingValue(inputs, "path").Value,
		"key":  fmt.Sprintf("${{ steps.%s.outputs.cache-primary-key }}", id),
	}
	for _, name := range []string{"enableCrossOsArchive", "upload-chunk-size"} {
		if value := runtimeMappingValue(inputs, name); value != nil {
			saveInputs[name] = value.Value
		}
	}
	save := map[string]any{
		"name": "Save isolated runtime cache", "if": "${{ " + condition + " }}",
		"uses": "actions/cache/save@" + ref, "with": saveInputs,
	}
	if env, ok := fields["env"]; ok {
		save["env"] = env
	}
	rendered, err := renderStagedRuntimeCache(fields, stage, false)
	if err != nil {
		return "", "", err
	}
	saved, err := renderStagedRuntimeCache(save, stage, true)
	return rendered, saved, err
}

func renderStagedRuntimeCache(fields map[string]any, stage *runtimeCacheStage, save bool) (string, error) {
	if stage == nil {
		return renderRuntimeCacheStep(fields)
	}
	inputs, ok := fields["with"].(map[string]any)
	if !ok {
		return "", errors.New("runtime cache action must have mapping inputs")
	}
	inputs["path"] = stage.actionPath
	id := stage.id + "-restore-stage"
	if save {
		id = stage.id + "-save-stage"
	}
	condition := "${{ always() && steps." + id + ".outputs.identity != '' }}"
	stageEnv := map[string]any{
		"GH_AW_CACHE_STAGE":    stage.dir,
		"GH_AW_CACHE_STAGE_ID": "${{ steps." + id + ".outputs.identity }}",
	}
	acquire := stage.runStep("Prepare isolated runtime cache", acquireRuntimeCacheStage, map[string]any{"GH_AW_CACHE_STAGE": stage.dir})
	acquire["id"] = id
	if original, ok := fields["if"]; ok {
		acquire["if"] = original
	}
	ready := "${{ always() && steps." + id + ".outcome == 'success' }}"
	fields["if"] = ready
	var steps []map[string]any
	steps = append(steps, acquire)
	if save {
		env := map[string]any{
			"GH_AW_CACHE_STAGE": stageEnv["GH_AW_CACHE_STAGE"], "GH_AW_CACHE_STAGE_ID": stageEnv["GH_AW_CACHE_STAGE_ID"],
			"GH_AW_CACHE_PATTERNS": stage.patterns,
		}
		pack := stage.runStep("Pack isolated runtime cache", packRuntimeCacheStage, env)
		pack["if"] = ready
		pack["id"] = id + "-packed"
		fields["if"] = "${{ always() && steps." + id + "-packed.outcome == 'success' }}"
		steps = append(steps, pack, fields)
	} else {
		restoreID, ok := fields["id"].(string)
		if !ok {
			restoreID = stage.id
			fields["id"] = restoreID
		}
		env := map[string]any{
			"GH_AW_CACHE_STAGE": stageEnv["GH_AW_CACHE_STAGE"], "GH_AW_CACHE_STAGE_ID": stageEnv["GH_AW_CACHE_STAGE_ID"],
			"GH_AW_CACHE_LOOKUP": fmt.Sprint(inputs["lookup-only"]),
		}
		unpack := stage.runStep("Unpack isolated runtime cache", unpackRuntimeCacheStage, env)
		unpack["if"] = "${{ always() && steps." + restoreID + ".outcome == 'success' }}"
		steps = append(steps, fields, unpack)
	}
	release := stage.runStep("Release isolated runtime cache staging", releaseRuntimeCacheStage, stageEnv)
	release["if"] = condition
	steps = append(steps, release)

	var result strings.Builder
	for _, step := range steps {
		rendered, err := renderRuntimeCacheStep(step)
		if err != nil {
			return "", err
		}
		result.WriteString(rendered)
	}
	return result.String(), nil
}

func (stage *runtimeCacheStage) runStep(name, script string, env map[string]any) map[string]any {
	fields := map[string]any{"name": name, "env": env, "shell": "bash", "run": script}
	if stage.windows {
		var names []string
		for name := range env {
			names = append(names, name)
		}
		fields["shell"] = "pwsh"
		fields["run"] = strings.Join(windowsWSLRunWrapper(script, names), "\n")
	}
	return fields
}

const acquireRuntimeCacheStage = `set -euo pipefail
umask 077
stage="$GITHUB_WORKSPACE/$GH_AW_CACHE_STAGE"
mkdir -- "$stage"
identity="$(stat -c '%d:%i' -- "$stage")"
printf 'identity=%s\n' "$identity" >> "$GITHUB_OUTPUT"
`

const checkRuntimeCacheStage = `set -euo pipefail
stage="$GITHUB_WORKSPACE/$GH_AW_CACHE_STAGE"
[[ ! -L "$stage" && -d "$stage" && "$(stat -c '%d:%i' -- "$stage")" == "$GH_AW_CACHE_STAGE_ID" ]]
`

const packRuntimeCacheStage = checkRuntimeCacheStage + `cd -- "${GH_AW_RUNTIME_ROOT:?}"
shopt -s globstar nullglob dotglob
declare -a paths=() excludes=()
while IFS= read -r pattern; do
  exclusion=false
  if [[ "$pattern" == '!'* ]]; then exclusion=true; pattern="${pattern:1}"; fi
  if [[ -z "$pattern" || "$pattern" == /* || "/$pattern/" == */../* ]]; then
    echo "Invalid isolated runtime cache path" >&2
    exit 1
  fi
  if [[ "$exclusion" == true ]]; then excludes+=("--exclude=$pattern"); continue; fi
  IFS=''
  for match in $pattern; do
    [[ "$match" != . && "$match" != ./ ]] || continue
    [[ -e "$match" || -L "$match" ]] || continue
    parent="$(realpath -e -- "$(dirname -- "$match")")"
    [[ "$parent" == "$PWD" || "$parent" == "$PWD/"* ]] || continue
    paths+=("$match")
  done
done <<< "$GH_AW_CACHE_PATTERNS"
if (( ${#paths[@]} > 0 )); then
  tar --create --file "$stage/runtime.tar" --exclude=.gh-aw-runtime-owner "${excludes[@]}" -- "${paths[@]}"
fi
`

const unpackRuntimeCacheStage = checkRuntimeCacheStage + `if [[ "${GH_AW_CACHE_LOOKUP,,}" == true || ! -f "$stage/runtime.tar" ]]; then exit 0; fi
[[ ! -L "$stage/runtime.tar" ]]
tar --extract --file "$stage/runtime.tar" --directory "${GH_AW_RUNTIME_ROOT:?}" --no-same-owner --no-overwrite-dir --exclude=.gh-aw-runtime-owner
`

const releaseRuntimeCacheStage = checkRuntimeCacheStage + `rm -f -- "$stage/runtime.tar"
rmdir -- "$stage"
`

func renderRuntimeCacheStep(fields map[string]any) (string, error) {
	var step yaml.Node
	if err := step.Encode(fields); err != nil {
		return "", err
	}
	encoded, err := yaml.Marshal(&yaml.Node{Kind: yaml.SequenceNode, Content: []*yaml.Node{&step}})
	if err != nil {
		return "", err
	}
	var result strings.Builder
	for line := range strings.SplitSeq(strings.TrimSuffix(string(encoded), "\n"), "\n") {
		result.WriteString("      " + line + "\n")
	}
	return result.String(), nil
}
