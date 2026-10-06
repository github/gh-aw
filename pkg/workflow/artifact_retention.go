package workflow

import (
	"bytes"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/github/gh-aw/pkg/workflow/compilerenv"
	"gopkg.in/yaml.v3"
)

func artifactRetentionDays(config *RepoConfig, fallback string) string {
	if config != nil && config.ArtifactRetentionDays != nil {
		return config.ArtifactRetentionDays.String()
	}
	if isExpression(fallback) {
		fallback = "(" + strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(fallback, "${{"), "}}")) + ")"
	} else {
		fallback = "'" + strings.ReplaceAll(fallback, "'", "''") + "'"
	}
	return fmt.Sprintf("${{ vars.%s || %s }}", compilerenv.DefaultArtifactRetentionDays, fallback)
}

// applyArtifactRetention edits only upload inputs, leaving scripts and other YAML
// byte-for-byte intact. Matching configured mirrors covers their pinned versions.
// Generated jobs use six-space step indentation; parse only upload steps so this
// pass does not take over validation of unrelated workflow fields.
func applyArtifactRetention(content string, config *RepoConfig) (string, error) {
	lines := strings.SplitAfter(content, "\n")
	var result strings.Builder
	inSteps := false
	for i := 0; i < len(lines); i++ {
		line := lines[i] //nolint:uncheckedsliceindex // The loop bounds check i.
		trimmed := strings.TrimSpace(line)
		if trimmed != "" && !strings.HasPrefix(trimmed, "#") && leadingSpaces(line) <= 4 {
			inSteps = line == "    steps:\n"
		}
		if !inSteps || leadingSpaces(line) != 6 || !strings.HasPrefix(trimmed, "- ") {
			result.WriteString(line)
			continue
		}
		end := i + 1
		for end < len(lines) {
			next := lines[end] //nolint:uncheckedsliceindex // end starts positive and is bounded by len(lines).
			text := strings.TrimSpace(next)
			if text != "" && !strings.HasPrefix(text, "#") &&
				(leadingSpaces(next) < 6 || leadingSpaces(next) == 6 && strings.HasPrefix(text, "- ")) {
				break
			}
			end++
		}
		step, err := applyStepArtifactRetention(lines[i:end], config)
		if err != nil {
			return "", err
		}
		result.WriteString(step)
		i = end - 1
	}
	return result.String(), nil
}

func applyStepArtifactRetention(lines []string, config *RepoConfig) (string, error) {
	upload := false
	for _, line := range lines {
		if leadingSpaces(line) != 6 && leadingSpaces(line) != 8 {
			continue
		}
		if value, ok := strings.CutPrefix(strings.TrimPrefix(strings.TrimSpace(line), "- "), "uses:"); ok {
			var uses string
			if err := yaml.Unmarshal([]byte(value), &uses); err != nil {
				return "", fmt.Errorf("cannot read artifact upload action reference: %w", err)
			}
			upload = isRetentionUploadAction(uses, config)
			break
		}
	}
	if !upload {
		return strings.Join(lines, ""), nil
	}
	var document yaml.Node
	if err := yaml.Unmarshal([]byte(strings.Join(lines, "")), &document); err != nil {
		return "", fmt.Errorf("cannot apply artifact retention to upload step: %w", err)
	}
	if len(document.Content) != 1 {
		return "", fmt.Errorf("expected one artifact upload step document, got %d", len(document.Content))
	}
	sequence := document.Content[0] //nolint:uncheckedsliceindex // Exactly one document is required above.
	steps := sequence.Content
	if sequence.Kind != yaml.SequenceNode || len(steps) != 1 {
		return "", fmt.Errorf("expected one artifact upload step in a YAML sequence, got %d", len(steps))
	}
	step := steps[0] //nolint:uncheckedsliceindex // Exactly one step is required above.
	start, end, replacement, err := artifactRetentionInputs(lines, step, config)
	if err != nil {
		return "", err
	}
	return strings.Join(lines[:start], "") + replacement + strings.Join(lines[end:], ""), nil
}

func isRetentionUploadAction(uses string, config *RepoConfig) bool {
	repo := extractActionRepo(uses)
	if strings.EqualFold(repo, "actions/upload-artifact") {
		return true
	}
	if config != nil {
		for source, target := range config.ActionPins {
			if strings.EqualFold(extractActionRepo(source), "actions/upload-artifact") && strings.EqualFold(repo, extractActionRepo(target)) {
				return true
			}
		}
	}
	return false
}

func artifactMappingValue(node *yaml.Node, key string) *yaml.Node {
	if node.Kind != yaml.MappingNode {
		return nil
	}
	for i, child := range node.Content {
		if i%2 == 0 && child.Value == key && i+1 < len(node.Content) {
			return node.Content[i+1] //nolint:uncheckedsliceindex // The mapping value index is checked above.
		}
	}
	return nil
}

func artifactRetentionInputs(lines []string, step *yaml.Node, config *RepoConfig) (int, int, string, error) {
	key, with, start, end, indent, err := artifactRetentionInputRange(lines, step)
	if err != nil {
		return 0, 0, "", err
	}
	fallback := "0" // actions/upload-artifact uses the repository default for zero.
	if retention := artifactMappingValue(with, "retention-days"); retention != nil {
		fallback = retention.Value
	}
	rendered, err := renderArtifactRetentionInputs(key, with, artifactRetentionDays(config, fallback))
	if err != nil {
		return 0, 0, "", err
	}
	var result strings.Builder
	for line := range strings.SplitAfterSeq(rendered, "\n") {
		if line != "" {
			result.WriteString(strings.Repeat(" ", indent) + line)
		}
	}
	return start, end, result.String(), nil
}

func artifactRetentionInputRange(lines []string, step *yaml.Node) (*yaml.Node, *yaml.Node, int, int, int, error) {
	with := artifactMappingValue(step, "with")
	start := artifactMappingValue(step, "uses").Line
	end := start
	indent := step.Column - 1
	key := &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: "with"}
	if with != nil {
		for i, child := range step.Content {
			if i%2 == 0 && child.Value == "with" {
				key = child
				break
			}
		}
		start = key.Line - 1
		indent = key.Column - 1
		end = start + 1
		for end < len(lines) {
			line := lines[end] //nolint:uncheckedsliceindex // end follows a parsed YAML line and is bounded by len(lines).
			if strings.TrimSpace(line) != "" && leadingSpaces(line) <= indent {
				break
			}
			end++
		}
		for end > start+1 && strings.TrimSpace(lines[end-1]) == "" { //nolint:uncheckedsliceindex // end is bounded above and start is a parsed YAML line.
			end--
		}
		if with.Kind == yaml.AliasNode {
			aliased := *with.Alias
			aliased.Anchor = ""
			with = &aliased
		}
		if with.Kind != yaml.MappingNode {
			return nil, nil, 0, 0, 0, fmt.Errorf("artifact upload with must be an input mapping on line %d; use with: {path: report.txt}", key.Line)
		}
	} else {
		with = &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
	}
	return key, with, start, end, indent, nil
}

func renderArtifactRetentionInputs(key, with *yaml.Node, value string) (string, error) {
	retention := artifactMappingValue(with, "retention-days")
	replacement := &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: value}
	if _, err := strconv.Atoi(value); err == nil {
		replacement.Tag = "!!int"
	}
	// Copy the mapping so YAML aliases do not change unrelated steps.
	inputs := *with
	inputs.Style = 0
	inputs.Content = slices.Clone(with.Content)
	if retention == nil {
		inputs.Content = append(inputs.Content, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: "retention-days"}, replacement)
	} else {
		for i, child := range inputs.Content {
			if i%2 == 1 && child == retention {
				inputs.Content[i] = replacement
			}
		}
	}
	var rendered bytes.Buffer
	encoder := yaml.NewEncoder(&rendered)
	encoder.SetIndent(2)
	renderedKey := *key
	renderedKey.HeadComment = ""
	if err := encoder.Encode(&yaml.Node{Kind: yaml.MappingNode, Content: []*yaml.Node{&renderedKey, &inputs}}); err != nil {
		return "", fmt.Errorf("cannot render artifact retention inputs: %w", err)
	}
	return rendered.String(), nil
}
