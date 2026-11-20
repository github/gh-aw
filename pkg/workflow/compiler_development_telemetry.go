package workflow

import (
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"

	goyaml "github.com/goccy/go-yaml"
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
	before := *result
	before.RawFrontmatter = maps.Clone(result.RawFrontmatter)
	if result.ParsedFrontmatter != nil {
		frontmatter := *result.ParsedFrontmatter
		before.ParsedFrontmatter = &frontmatter
	}
	defer logDryRunWorkflowChanges(&before, result)
	// Remove authored code and credentials before job construction and token wiring.
	result.CustomSteps = ""
	result.PostSteps = ""
	if err := prepareDryRunWorkflowDispatch(result); err != nil {
		return nil, err
	}
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
	if result.EngineConfig != nil {
		engine := *result.EngineConfig
		engine.Env = maps.Clone(engine.Env)
		for name := range engine.Env {
			if isDryRunTelemetryEnv(name) {
				delete(engine.Env, name)
			}
		}
		result.EngineConfig = &engine
	}
	return result, nil
}

func prepareDryRunWorkflowDispatch(data *WorkflowData) error {
	var document yaml.Node
	if err := yaml.Unmarshal([]byte(data.On), &document); err != nil {
		return fmt.Errorf("cannot enable dry-run workflow_dispatch: %w", err)
	}
	root := &yaml.Node{Kind: yaml.MappingNode}
	if len(document.Content) > 0 {
		root = document.Content[0]
	}
	if root.Kind != yaml.MappingNode {
		return errors.New("dry-run workflow triggers require a workflow mapping")
	}
	on := artifactMappingValue(root, "on")
	if on == nil {
		on = &yaml.Node{Kind: yaml.MappingNode}
		root.Content = append(root.Content, &yaml.Node{Kind: yaml.ScalarNode, Value: "on"}, on)
	}
	changed := on.Kind != yaml.MappingNode
	if err := normalizeDryRunTriggers(on); err != nil {
		return err
	}
	if artifactMappingValue(on, "workflow_dispatch") == nil {
		dispatch := &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!null", Value: ""}
		if call := artifactMappingValue(on, "workflow_call"); call != nil {
			if inputs := artifactMappingValue(call, "inputs"); inputs != nil {
				dispatch = &yaml.Node{Kind: yaml.MappingNode, Content: []*yaml.Node{
					{Kind: yaml.ScalarNode, Value: "inputs"}, inputs,
				}}
			}
		}
		on.Content = append(on.Content, &yaml.Node{Kind: yaml.ScalarNode, Value: "workflow_dispatch"}, dispatch)
		changed = true
	}
	if changed {
		var rendered strings.Builder
		encoder := yaml.NewEncoder(&rendered)
		encoder.SetIndent(2)
		if err := encoder.Encode(root); err != nil {
			return fmt.Errorf("cannot render dry-run workflow_dispatch: %w", err)
		}
		data.On = strings.TrimSuffix(rendered.String(), "\n")
	}
	var parsed map[string]map[string]any
	if err := goyaml.Unmarshal([]byte(data.On), &parsed); err != nil {
		return fmt.Errorf("cannot read dry-run workflow_dispatch: %w", err)
	}
	rawOn := maps.Clone(parsed["on"])
	if configured, ok := data.RawFrontmatter["on"].(map[string]any); ok && configured != nil {
		rawOn = maps.Clone(configured)
	}
	rawOn["workflow_dispatch"] = parsed["on"]["workflow_dispatch"]
	rawOn["roles"] = data.Roles
	delete(rawOn, "bots")
	if data.RawFrontmatter == nil {
		data.RawFrontmatter = make(map[string]any)
	}
	data.RawFrontmatter["on"] = rawOn
	if data.ParsedFrontmatter != nil {
		data.ParsedFrontmatter.On = rawOn
	}
	return nil
}

// normalizeDryRunTriggers operates on YAML nodes to preserve trigger comments,
// including zizmor suppressions that a map round trip would discard.
func normalizeDryRunTriggers(on *yaml.Node) error {
	switch on.Kind {
	case yaml.MappingNode:
	case yaml.ScalarNode:
		if on.Tag != "!!str" && on.Tag != "!!null" {
			return errors.New("dry-run workflow triggers must be a mapping, event name, or event list")
		}
		event := *on
		on.Kind, on.Tag, on.Value, on.Style = yaml.MappingNode, "", "", 0
		on.Content = nil
		if event.Tag != "!!null" {
			event.HeadComment, event.LineComment, event.FootComment = "", "", ""
			on.Content = append(on.Content, &event, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!null", Value: ""})
		}
	case yaml.SequenceNode:
		events := on.Content
		on.Kind, on.Tag, on.Style, on.Content = yaml.MappingNode, "", 0, nil
		for _, event := range events {
			if event.Kind != yaml.ScalarNode || event.Tag != "!!str" {
				return errors.New("dry-run workflow triggers must be event names")
			}
			on.Content = append(on.Content, event, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!null", Value: ""})
		}
	default:
		return errors.New("dry-run workflow triggers must be a mapping, event name, or event list")
	}
	return nil
}

func isDryRunTelemetryEnv(name string) bool {
	return strings.HasPrefix(name, "OTEL_") ||
		strings.HasPrefix(name, "GH_AW_OTLP_") ||
		strings.HasPrefix(name, "GITHUB_AW_OTEL_") ||
		name == "TRACEPARENT" || name == "TRACESTATE" || name == "BAGGAGE" ||
		name == "CLAUDE_CODE_ENABLE_TELEMETRY"
}

// Rewrite only environment mappings so executable scalar contents remain intact.
func removeDryRunTelemetryEnv(content string) (string, error) {
	return rewriteDryRunTelemetryEnv(content, nil)
}

func disableDryRunTelemetryEnv(content string) (string, error) {
	return rewriteDryRunTelemetryEnv(content, map[string]string{
		"DO_NOT_TRACK":                             "1",
		"OTEL_SDK_DISABLED":                        "true",
		"GH_TELEMETRY":                             "false",
		"DISABLE_TELEMETRY":                        "1",
		"DISABLE_ERROR_REPORTING":                  "1",
		"CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC": "1",
	})
}

func rewriteDryRunTelemetryEnv(content string, optOuts map[string]string) (string, error) {
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
	if len(optOuts) > 0 && artifactMappingValue(root, "env") == nil {
		rendered, err := MarshalWithFieldOrder(map[string]any{"env": optOuts}, []string{"env"})
		if err != nil {
			return "", fmt.Errorf("cannot render dry-run telemetry opt-outs: %w", err)
		}
		edits = append(edits, dryRunEnvEdit{content: string(rendered) + "\n"})
	}
	for _, owner := range dryRunEnvOwners(root) {
		if owner.Kind != yaml.MappingNode {
			continue
		}
		for i, key := range owner.Content {
			if i%2 != 0 || key.Value != "env" || i+1 >= len(owner.Content) {
				continue
			}
			value := owner.Content[i+1] //nolint:uncheckedsliceindex // The value index is checked above.
			edit, err := dryRunTelemetryEnvEdit(key, value, lines, optOuts, owner == root)
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

func dryRunTelemetryEnvEdit(key, value *yaml.Node, lines []string, optOuts map[string]string, workflowEnv bool) (*dryRunEnvEdit, error) {
	var env map[string]string
	if err := value.Decode(&env); err != nil {
		return nil, fmt.Errorf("dry-run telemetry filtering requires an env mapping on line %d: %w", key.Line, err)
	}
	env, changed := applyDryRunTelemetryOptOuts(env, optOuts, workflowEnv, key.Line)
	for name := range env {
		if isDryRunTelemetryEnv(name) && optOuts[name] == "" {
			compilerDevelopmentLog.Printf("Dry-run mutation: removed env key %q on line %d", name, key.Line)
			delete(env, name)
			changed = true
		}
	}
	if !changed {
		return nil, nil
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
	if len(env) == 0 {
		return &dryRunEnvEdit{start: start, end: end}, nil
	}
	rendered, err := MarshalWithFieldOrder(map[string]any{"env": env}, []string{"env"})
	if err != nil {
		return nil, fmt.Errorf("cannot render dry-run environment on line %d: %w", key.Line, err)
	}
	rendered = []byte(quoteEnvValuesContainingColonSpace(string(rendered)))
	if value.Anchor != "" {
		rendered = []byte(strings.Replace(string(rendered), "env:", "env: &"+value.Anchor, 1))
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

func applyDryRunTelemetryOptOuts(env, optOuts map[string]string, workflowEnv bool, line int) (map[string]string, bool) {
	if env == nil && workflowEnv && len(optOuts) > 0 {
		env = make(map[string]string)
	}
	changed := false
	for name, value := range optOuts {
		current, exists := env[name]
		if (workflowEnv || exists) && current != value {
			compilerDevelopmentLog.Printf("Dry-run mutation: disabled telemetry with env key %q on line %d", name, line)
			env[name] = value
			changed = true
		}
	}
	return env, changed
}
