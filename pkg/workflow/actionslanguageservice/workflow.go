package actionslanguageservice

import (
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"

	"go.yaml.in/yaml/v3"
)

// Workflow retains the YAML tree and parsed expressions without evaluating them.
type Workflow struct {
	Document    *yaml.Node
	Expressions map[*yaml.Node][]*Expression
	conditions  []condition
}

type condition struct {
	node    *yaml.Node
	context []string
}

// ValidateWorkflow adds implicit if-condition parsing to the template reader.
// Dependency resolution and policy checks belong to the compiler's Actions AST.
func ValidateWorkflow(name, content string) error {
	workflow, err := ParseWorkflow(name, content)
	if err != nil {
		return err
	}
	for _, condition := range workflow.conditions {
		source := scalarText(condition.node)
		if strings.TrimSpace(source) == "" {
			source = "success()"
		}
		if _, err := ParseExpression(source, condition.context); err != nil {
			return &Diagnostic{File: name, Line: condition.node.Line, Column: condition.node.Column, Message: err.Error()}
		}
	}
	return nil
}

// Diagnostic identifies a position in the Actions YAML, not in AW markdown.
type Diagnostic struct {
	File    string
	Line    int
	Column  int
	Message string
}

func (d *Diagnostic) Error() string {
	return fmt.Sprintf("%s (Line: %d, Col: %d): %s", d.File, d.Line, d.Column, d.Message)
}

type workflowParser struct {
	name   string
	schema map[string]definition
	result *Workflow
	stack  map[*yaml.Node]bool
	nodes  int
}

// ParseWorkflow ports the upstream template reader using its unmodified schema.
// The Go compiler's graph, policy, and security validators remain independent.
func ParseWorkflow(name, content string) (*Workflow, error) {
	schema, err := loadSchema()
	if err != nil {
		return nil, err
	}
	decoder := yaml.NewDecoder(strings.NewReader(content))
	var document yaml.Node
	if err := decoder.Decode(&document); err != nil {
		return nil, fmt.Errorf("%s: parsing Actions YAML: %w", name, err)
	}
	var extra yaml.Node
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err != nil {
			return nil, fmt.Errorf("%s: parsing Actions YAML: %w", name, err)
		}
		return nil, fmt.Errorf("%s: workflow must contain exactly one YAML document", name)
	}
	if len(document.Content) != 1 {
		return nil, fmt.Errorf("%s: workflow is empty", name)
	}
	result := &Workflow{Document: &document, Expressions: make(map[*yaml.Node][]*Expression)}
	p := workflowParser{name: name, schema: schema, result: result, stack: make(map[*yaml.Node]bool)}
	if err := p.validateYAMLSource(document.Content[0], strings.Split(content, "\n"), 0, 0); err != nil {
		return nil, err
	}
	if err := p.read(document.Content[0], "workflow-root-strict", nil, 0); err != nil {
		return nil, err
	}
	return result, nil
}

func (p *workflowParser) read(node *yaml.Node, name string, inherited []string, depth int) error {
	p.nodes++
	if depth > 100 || p.nodes > 1000000 {
		return p.error(node, "workflow exceeds the template depth or size limit")
	}
	if node.Kind == yaml.AliasNode {
		if node.Alias == nil || p.stack[node.Alias] {
			return p.error(node, "circular or unresolved YAML alias")
		}
		p.stack[node.Alias] = true
		defer delete(p.stack, node.Alias)
		return p.read(node.Alias, name, inherited, depth+1)
	}
	context := slices.Concat(inherited, p.schema[name].Context)
	definitions, err := expandDefinitions(p.schema, name)
	if err != nil {
		return err
	}
	switch node.Kind {
	case yaml.ScalarNode:
		if node.Style&yaml.TaggedStyle != 0 && node.Tag != "!!str" && node.Tag != "!!bool" &&
			node.Tag != "!!int" && node.Tag != "!!float" && node.Tag != "!!null" {
			return p.error(node, "unsupported YAML tag "+node.Tag)
		}
		if node.Style == 0 && node.Tag == "!!str" {
			if _, err := parseNumber(node.Value); err == nil {
				copy := *node
				copy.Tag = "!!float"
				node = &copy
			}
		}
		return p.scalar(node, name, definitions, context)
	case yaml.SequenceNode:
		for _, d := range definitions {
			if d.Sequence != nil {
				for _, item := range node.Content {
					if err := p.read(item, d.Sequence.ItemType, context, depth+1); err != nil {
						return err
					}
				}
				return nil
			}
		}
		return p.error(node, "a sequence was not expected")
	case yaml.MappingNode:
		var mappings []definition
		for _, d := range definitions {
			if d.Mapping != nil {
				mappings = append(mappings, d)
			}
		}
		if len(mappings) == 0 {
			return p.error(node, "a mapping was not expected")
		}
		return p.mapping(node, mappings, context, depth)
	default:
		return p.error(node, "expected a scalar, sequence, or mapping")
	}
}

func (p *workflowParser) scalar(node *yaml.Node, name string, definitions []definition, context []string) error {
	if node.Tag == "!!str" {
		expression, literal, err := p.template(node, context)
		if err != nil {
			return err
		}
		if expression {
			return nil
		}
		// Literal string expressions are escaped text, not template evaluation.
		if literal != nil {
			copy := *node
			copy.Value = *literal
			node = &copy
		}
	}
	if name == "job-if" || name == "step-if" {
		p.result.conditions = append(p.result.conditions, condition{node: node, context: context})
	}
	for _, d := range definitions {
		if d.Null != nil && node.Tag == "!!null" ||
			d.Boolean != nil && node.Tag == "!!bool" ||
			d.Number != nil && (node.Tag == "!!int" || node.Tag == "!!float") {
			return nil
		}
		// Like the upstream template reader, scalar values can coerce to strings.
		if d.String != nil {
			text := scalarText(node)
			if d.String.Constant != "" && d.String.Constant != text {
				continue
			}
			if !d.String.RequireNonEmpty || text != "" {
				return nil
			}
		}
	}
	return p.error(node, fmt.Sprintf("unexpected value %q", scalarText(node)))
}

func scalarText(node *yaml.Node) string {
	if node.Tag == "!!null" {
		return ""
	}
	if node.Tag == "!!bool" {
		return strings.ToLower(node.Value)
	}
	return node.Value
}

func (p *workflowParser) mapping(node *yaml.Node, candidates []definition, context []string, depth int) error {
	seen := make(map[string]struct{})
	expressionKey := false
	looseKey, looseValue := candidates[0].Mapping.LooseKey, candidates[0].Mapping.LooseValue
	for i := 0; i < len(node.Content); i += 2 {
		key, value := node.Content[i], node.Content[i+1]
		if key.Kind != yaml.ScalarNode {
			return p.error(key, "a mapping key must be a scalar")
		}
		expression, literal, err := p.template(key, context)
		if err != nil {
			return err
		}
		if expression {
			expressionKey = true
			valueType := "any"
			if len(candidates) == 1 && len(candidates[0].Mapping.Properties) == 0 && looseValue != "" {
				valueType = looseValue
			}
			if err := p.read(value, valueType, context, depth+1); err != nil {
				return err
			}
			continue
		}
		text := scalarText(key)
		if literal != nil {
			text = *literal
		}
		upper := strings.ToUpper(text)
		if _, exists := seen[upper]; exists && text != "" {
			return p.error(key, fmt.Sprintf("%q is already defined", text))
		}
		seen[upper] = struct{}{}
		candidates, err = p.mappingProperty(key, value, text, candidates, context, looseKey, looseValue, depth)
		if err != nil {
			return err
		}
	}
	if len(candidates) > 1 {
		return p.error(node, "not enough information to determine the mapping type")
	}
	if !expressionKey {
		var required []string
		for name, prop := range candidates[0].Mapping.Properties {
			if _, exists := seen[strings.ToUpper(name)]; prop.Required && !exists {
				required = append(required, name)
			}
		}
		slices.Sort(required)
		if len(required) > 0 {
			return p.error(node, "required property is missing: "+required[0])
		}
	}
	return nil
}

func (p *workflowParser) mappingProperty(key, value *yaml.Node, text string, candidates []definition, context []string, looseKey, looseValue string, depth int) ([]definition, error) {
	var matching []definition
	var field property
	for _, candidate := range candidates {
		if prop, ok := candidate.Mapping.Properties[text]; ok {
			matching, field = append(matching, candidate), prop
		}
	}
	if len(matching) > 0 {
		// Background-step syntax is behind an upstream feature flag, disabled
		// by default. Do not accidentally enable it by importing the schema.
		if text == "background" || text == "wait" || text == "wait-all" || text == "cancel" {
			return nil, p.error(key, fmt.Sprintf("unexpected value %q", text))
		}
		return matching, p.read(value, field.Type, context, depth+1)
	}
	if looseKey == "" {
		return nil, p.error(key, fmt.Sprintf("unexpected value %q", text))
	}
	copy := *key
	copy.Tag, copy.Value = "!!str", text
	if err := p.read(&copy, looseKey, context, depth+1); err != nil {
		return nil, err
	}
	return candidates, p.read(value, looseValue, context, depth+1)
}

func (p *workflowParser) error(node *yaml.Node, message string) error {
	return &Diagnostic{File: p.name, Line: node.Line, Column: node.Column, Message: message}
}
