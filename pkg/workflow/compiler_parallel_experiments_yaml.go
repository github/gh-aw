package workflow

import (
	"errors"
	"fmt"
	"strings"

	"go.yaml.in/yaml/v3"
)

func parallelScalar(value string) *yaml.Node {
	return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: value}
}

func setParallelNode(mapping *yaml.Node, key string, value *yaml.Node) {
	for i := 0; i < len(mapping.Content) && i+1 < len(mapping.Content); i += 2 {
		if mapping.Content[i].Value == key { //nolint:uncheckedsliceindex // The loop bounds check both entries of this mapping pair.
			mapping.Content[i+1] = value //nolint:uncheckedsliceindex // The loop bounds check both entries of this mapping pair.
			return
		}
	}
	mapping.Content = append(mapping.Content, parallelScalar(key), value)
}

func removeParallelNode(mapping *yaml.Node, keys ...string) {
	for _, key := range keys {
		for i := 0; i < len(mapping.Content) && i+1 < len(mapping.Content); i += 2 {
			if mapping.Content[i].Value == key { //nolint:uncheckedsliceindex // The loop bounds check both entries of this mapping pair.
				mapping.Content = append(mapping.Content[:i], mapping.Content[i+2:]...)
				break
			}
		}
	}
}

func parallelJobNode(job *Job) (*yaml.Node, error) {
	manager := NewJobManager()
	if err := manager.AddJob(job); err != nil {
		return nil, err
	}
	var rendered strings.Builder
	manager.WriteJobsYAML(&rendered)
	var document yaml.Node
	if err := yaml.Unmarshal([]byte(rendered.String()), &document); err != nil {
		return nil, err
	}
	if len(document.Content) == 0 {
		return nil, errors.New("parallel-experiments: generated job document is empty")
	}
	return artifactMappingValue(artifactMappingValue(document.Content[0], "jobs"), parallelExperimentsJobName), nil //nolint:uncheckedsliceindex // Empty documents are rejected immediately above.
}

func scopeParallelExperimentSteps(steps, branchEnv *yaml.Node, entry parallelExperimentEntry) error { //nolint:largefunc // Step scoping keeps identity, credentials, timing and upload guards together.
	if steps == nil || steps.Kind != yaml.SequenceNode {
		return errors.New("parallel-experiments: generated agent steps are missing")
	}
	prefix := strings.ReplaceAll(entry.ID, "-", "_") + "_"
	var replacements []string
	for _, step := range steps.Content {
		if id := artifactMappingValue(step, "id"); id != nil {
			replacements = append(replacements, "steps."+id.Value+".", "steps."+prefix+id.Value+".")
			id.Value = prefix + id.Value
		}
	}
	replacer := strings.NewReplacer(replacements...)
	var rewrite func(*yaml.Node)
	rewrite = func(node *yaml.Node) {
		if node.Kind == yaml.ScalarNode {
			node.Value = replacer.Replace(node.Value)
		}
		for _, child := range node.Content {
			rewrite(child)
		}
	}
	rewrite(steps)
	var result []*yaml.Node
	for _, step := range steps.Content {
		name := artifactMappingValue(step, "name")
		id := artifactMappingValue(step, "id")
		isExecution := id != nil && id.Value == prefix+"agentic_execution"
		uses := artifactMappingValue(step, "uses")
		isUpload := uses != nil && strings.HasPrefix(uses.Value, "actions/upload-artifact@")
		isSetup := uses != nil && (strings.Contains(uses.Value, "/actions/setup@") || uses.Value == "./actions/setup")
		isBootstrap := name != nil && (name.Value == "Checkout actions folder" || name.Value == "Setup Scripts")
		isResult := name != nil && (strings.HasPrefix(name.Value, "Parallel experiment ") ||
			strings.Contains(name.Value, "Redact secrets") || strings.Contains(name.Value, "Validate recovery envelopes"))
		if !entry.Admitted && !isSetup && !isBootstrap && !isResult && !isUpload {
			continue
		}
		if name != nil {
			name.Value = entry.ID + ": " + name.Value
		}
		if uses != nil && (strings.HasPrefix(uses.Value, "actions/cache@") || strings.HasPrefix(uses.Value, "actions/cache/save@")) {
			return errors.New("parallel-experiments: generated cache writes are not isolated")
		}
		if isUpload {
			with := artifactMappingValue(step, "with")
			artifactName := artifactMappingValue(with, "name")
			if artifactName == nil {
				return errors.New("parallel-experiments: upload artifact name is required for isolation")
			}
			artifactName.Value = "parallel-experiment-${{ github.run_attempt }}-${{ matrix.id }}-" + artifactName.Value
			// Only the guarded unified upload publishes the independently redacted bundle.
			if artifactMappingValue(with, "path") != nil && strings.HasSuffix(artifactName.Value, "-agent") {
				setParallelNode(with, "path", parallelScalar(parallelExperimentDirectory+"/"))
			} else {
				continue
			}
		}
		env := &yaml.Node{Kind: yaml.MappingNode}
		if branchEnv != nil {
			env.Content = append(env.Content, branchEnv.Content...)
		}
		if own := artifactMappingValue(step, "env"); own != nil {
			for i := 0; i < len(own.Content) && i+1 < len(own.Content); i += 2 {
				setParallelNode(env, own.Content[i].Value, own.Content[i+1]) //nolint:uncheckedsliceindex // The loop bounds check both entries of this mapping pair.
			}
		}
		setParallelNode(env, "GH_AW_PARALLEL_EXPERIMENT", parallelScalar("${{ toJSON(matrix) }}"))
		setParallelNode(env, "GH_AW_PARALLEL_EXPERIMENT_DIR", parallelScalar(parallelExperimentDirectory))
		if role := artifactMappingValue(env, "INPUT_JOB_NAME"); role != nil && role.Value == "${{ github.job }}" {
			setParallelNode(env, "INPUT_JOB_NAME", parallelScalar("agent"))
		}
		setParallelNode(step, "env", env)
		condition := "success()"
		if existing := artifactMappingValue(step, "if"); existing != nil {
			condition = strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(existing.Value, "${{"), "}}"))
		}
		setParallelNode(step, "if", parallelScalar(fmt.Sprintf("${{ matrix.id == '%s' && (%s) }}", entry.ID, condition)))
		if isExecution {
			run := artifactMappingValue(step, "run")
			const trapAnchor = `if [ "$gh_aw_exit_code" -ne 0 ]; then`
			if run == nil || !strings.Contains(run.Value, trapAnchor) {
				return errors.New("parallel-experiments: execution must provide the compiler-owned exit trap for timing")
			}
			run.Value = "node \"${RUNNER_TEMP}/gh-aw/actions/parallel_experiment.cjs\" start\n" +
				strings.Replace(run.Value, trapAnchor, `node "${RUNNER_TEMP}/gh-aw/actions/parallel_experiment.cjs" end || true; `+trapAnchor, 1)
		}
		result = append(result, step)
	}
	steps.Content = result
	return nil
}
