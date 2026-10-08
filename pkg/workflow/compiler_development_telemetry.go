package workflow

import (
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"

	"gopkg.in/yaml.v3"
)

type dryRunEnvEdit struct {
	start, end int
	content    string
}

func (c *Compiler) prepareDryRunWorkflowData(data *WorkflowData) (*WorkflowData, error) {
	if !c.dryRun {
		return data, nil
	}
	result := c.dryRunWorkflowData(data)
	if result.Env != "" {
		env, err := removeDryRunTelemetryEnv(result.Env)
		if err != nil {
			return nil, err
		}
		result.Env = env
	}
	result.EnvSources = maps.Clone(data.EnvSources)
	for name := range result.EnvSources {
		if isDryRunTelemetryEnv(name) {
			delete(result.EnvSources, name)
		}
	}
	return result, nil
}

func isDryRunTelemetryEnv(name string) bool {
	return strings.HasPrefix(name, "OTEL_") || strings.HasPrefix(name, "GH_AW_OTLP_")
}

// Rewrite only environment mappings so executable scalar contents remain intact.
func removeDryRunTelemetryEnv(content string) (string, error) {
	var document yaml.Node
	if err := yaml.Unmarshal([]byte(content), &document); err != nil {
		return "", fmt.Errorf("cannot remove dry-run telemetry environment variables: %w", err)
	}
	if len(document.Content) != 1 {
		return "", errors.New("dry-run telemetry filtering requires one workflow YAML document")
	}
	root := document.Content[0] //nolint:uncheckedsliceindex // Exactly one document is required above.
	lines := strings.SplitAfter(content, "\n")
	var edits []dryRunEnvEdit
	for _, owner := range dryRunEnvOwners(root) {
		if owner.Kind != yaml.MappingNode {
			continue
		}
		for i, key := range owner.Content {
			if i%2 != 0 || key.Value != "env" || i+1 >= len(owner.Content) {
				continue
			}
			value := owner.Content[i+1] //nolint:uncheckedsliceindex // The value index is checked above.
			edit, err := dryRunTelemetryEnvEdit(key, value, lines)
			if err != nil {
				return "", err
			}
			if edit != nil {
				edits = append(edits, *edit)
			}
		}
	}
	slices.SortFunc(edits, func(a, b dryRunEnvEdit) int { return a.start - b.start })
	var result strings.Builder
	start := 0
	for _, edit := range edits {
		if edit.start < start {
			return "", fmt.Errorf("dry-run telemetry environment mappings overlap on line %d", edit.start+1)
		}
		result.WriteString(strings.Join(lines[start:edit.start], ""))
		result.WriteString(edit.content)
		start = edit.end
	}
	result.WriteString(strings.Join(lines[start:], ""))
	return result.String(), nil
}

func dryRunEnvOwners(root *yaml.Node) []*yaml.Node {
	owners := []*yaml.Node{root}
	if jobs := artifactMappingValue(root, "jobs"); jobs != nil {
		for i, job := range jobs.Content {
			if i%2 == 0 {
				continue
			}
			owners = append(owners, job)
			if container := artifactMappingValue(job, "container"); container != nil {
				owners = append(owners, container)
			}
			if services := artifactMappingValue(job, "services"); services != nil {
				for i, service := range services.Content {
					if i%2 != 0 {
						owners = append(owners, service)
					}
				}
			}
			if steps := artifactMappingValue(job, "steps"); steps != nil {
				owners = append(owners, steps.Content...)
			}
		}
	}
	return owners
}

func dryRunTelemetryEnvEdit(key, value *yaml.Node, lines []string) (*dryRunEnvEdit, error) {
	var env map[string]any
	if err := value.Decode(&env); err != nil {
		return nil, fmt.Errorf("dry-run telemetry filtering requires an env mapping on line %d: %w", key.Line, err)
	}
	removed := false
	for name := range env {
		if isDryRunTelemetryEnv(name) {
			delete(env, name)
			removed = true
		}
	}
	if !removed {
		return nil, nil
	}
	rendered, err := MarshalWithFieldOrder(map[string]any{"env": env}, []string{"env"})
	if err != nil {
		return nil, fmt.Errorf("cannot render dry-run environment on line %d: %w", key.Line, err)
	}
	if value.Anchor != "" {
		rendered = []byte(strings.Replace(string(rendered), "env:", "env: &"+value.Anchor, 1))
	}
	start := key.Line - 1
	indent := key.Column - 1
	end := start + 1
	for end < len(lines) {
		line := lines[end] //nolint:uncheckedsliceindex // end is bounded by len(lines).
		if strings.TrimSpace(line) != "" && leadingSpaces(line) <= indent {
			break
		}
		end++
	}
	for end > start+1 && strings.TrimSpace(lines[end-1]) == "" { //nolint:uncheckedsliceindex // end is bounded above.
		end--
	}
	var replacement strings.Builder
	for i, line := range strings.SplitAfter(string(rendered), "\n") {
		if line == "" {
			continue
		}
		if i == 0 {
			replacement.WriteString(lines[start][:indent]) //nolint:uncheckedsliceindex // Positions come from the parsed YAML key.
		} else {
			replacement.WriteString(strings.Repeat(" ", indent))
		}
		replacement.WriteString(line)
	}
	return &dryRunEnvEdit{start: start, end: end, content: replacement.String()}, nil
}
