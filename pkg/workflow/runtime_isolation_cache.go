package workflow

import (
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
)

// Script-mode cleanup has no post action. Save caches of private runtime data
// explicitly before cleanup rather than deleting their files before cache@post.
func prepareScriptRuntimeCacheSaves(content string) (string, error) {
	if !strings.Contains(content, "name: Clean Scripts") {
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
		steps := runtimeMappingValue(jobs.Content[i], "steps") //nolint:uncheckedsliceindex // i is bounded by the loop.
		if steps == nil {
			continue
		}
		jobEdits, err := scriptRuntimeCacheEdits(steps)
		if err != nil {
			return "", err
		}
		edits = append(edits, jobEdits...)
	}
	return applyRuntimeYAMLEdits(lines, edits), nil
}

func scriptRuntimeCacheEdits(steps *yaml.Node) ([]runtimeYAMLEdit, error) {
	cleanupLine := -1
	for _, step := range steps.Content {
		if run := runtimeMappingValue(step, "run"); run != nil && strings.Contains(run.Value, "/actions/setup/clean.sh") {
			cleanupLine = step.Line - 1
		}
	}
	if cleanupLine < 0 {
		return nil, nil
	}
	var edits []runtimeYAMLEdit
	var saves strings.Builder
	for index, step := range steps.Content {
		uses := runtimeMappingValue(step, "uses")
		if uses == nil || !strings.HasPrefix(uses.Value, "actions/cache@") {
			continue
		}
		path := runtimeMappingValue(runtimeMappingValue(step, "with"), "path")
		// Expressions may indirectly select runtime paths.
		if path == nil || !needsRuntimeIsolation(path.Value) && !strings.Contains(path.Value, "${{") {
			continue
		}
		restore, save, err := splitScriptRuntimeCache(step, index, strings.TrimPrefix(uses.Value, "actions/cache@"))
		if err != nil {
			return nil, err
		}
		end := cleanupLine
		if index+1 < len(steps.Content) {
			end = steps.Content[index+1].Line - 1 //nolint:uncheckedsliceindex // index+1 is bounded by the guard.
		}
		edits = append(edits, runtimeYAMLEdit{start: step.Line - 1, end: end, text: restore})
		saves.WriteString(save)
	}
	if saves.Len() > 0 {
		edits = append(edits, runtimeYAMLEdit{start: cleanupLine, end: cleanupLine, text: saves.String()})
	}
	return edits, nil
}

func splitScriptRuntimeCache(step *yaml.Node, index int, ref string) (string, string, error) {
	restore := *step
	restore.Content = append([]*yaml.Node(nil), step.Content...)
	for j := 0; j+1 < len(restore.Content); j += 2 {
		if restore.Content[j].Value == "uses" { //nolint:uncheckedsliceindex // j+1 is bounded by the loop.
			value := *restore.Content[j+1] //nolint:uncheckedsliceindex // j+1 is bounded by the loop.
			value.Value = "actions/cache/restore@" + ref
			restore.Content[j+1] = &value //nolint:uncheckedsliceindex // j+1 is bounded by the loop.
		}
	}
	id := fmt.Sprintf("gh-aw-runtime-cache-%d", index)
	if original := runtimeMappingValue(step, "id"); original != nil {
		id = original.Value
	} else {
		restore.Content = append(restore.Content,
			&yaml.Node{Kind: yaml.ScalarNode, Value: "id"},
			&yaml.Node{Kind: yaml.ScalarNode, Value: id})
	}
	rendered, err := renderRuntimeYAMLStep(&restore)
	if err != nil {
		return "", "", err
	}
	inputs := runtimeMappingValue(step, "with")
	if lookup := runtimeMappingValue(inputs, "lookup-only"); lookup != nil && lookup.Value == "true" {
		return rendered, "", nil
	}
	save, err := renderScriptRuntimeCacheSave(step, inputs, id, ref)
	return rendered, save, err
}

func renderScriptRuntimeCacheSave(step, inputs *yaml.Node, id, ref string) (string, error) {
	path := runtimeMappingValue(inputs, "path")
	condition := fmt.Sprintf("success() && steps.%s.outcome == 'success' && steps.%s.outputs.cache-primary-key != '' && steps.%s.outputs.cache-hit != 'true'", id, id, id)
	saveInputs := map[string]any{"path": path.Value, "key": fmt.Sprintf("${{ steps.%s.outputs.cache-primary-key }}", id)}
	for _, name := range []string{"enableCrossOsArchive", "upload-chunk-size"} {
		if value := runtimeMappingValue(inputs, name); value != nil {
			saveInputs[name] = value.Value
		}
	}
	save := map[string]any{
		"name": "Save isolated runtime cache", "if": "${{ " + condition + " }}",
		"uses": "actions/cache/save@" + ref, "with": saveInputs,
	}
	if env := runtimeMappingValue(step, "env"); env != nil {
		save["env"] = env
	}
	var saveNode yaml.Node
	if err := saveNode.Encode(save); err != nil {
		return "", err
	}
	return renderRuntimeYAMLStep(&saveNode)
}

func renderRuntimeYAMLStep(step *yaml.Node) (string, error) {
	encoded, err := yaml.Marshal(&yaml.Node{Kind: yaml.SequenceNode, Content: []*yaml.Node{step}})
	if err != nil {
		return "", err
	}
	var result strings.Builder
	for line := range strings.SplitSeq(strings.TrimSuffix(string(encoded), "\n"), "\n") {
		result.WriteString("      " + line + "\n")
	}
	return result.String(), nil
}
