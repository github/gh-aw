package actions

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/goccy/go-yaml"
	"github.com/goccy/go-yaml/ast"
	"github.com/goccy/go-yaml/parser"
)

type workflowSource struct {
	text    string
	lowered string
}

func retainWorkflowSource(workflow *Workflow, content []byte) error {
	lowered, err := serializedWorkflow(workflow)
	if err != nil {
		return err
	}
	workflow.source = &workflowSource{text: string(content), lowered: lowered}
	return nil
}

func serializedWorkflow(workflow *Workflow) (string, error) {
	value, err := Lower(workflow)
	if err != nil {
		return "", err
	}
	return snapshotYAMLValue(value)
}

func snapshotYAMLValue(value any) (string, error) {
	content, err := yaml.MarshalWithOptions(value, yaml.JSON())
	if err != nil {
		return "", fmt.Errorf("serializing workflow snapshot: %w", err)
	}
	// Bound syntax-node columns while retaining literal CR/LF characters.
	// Rendering a large single-line flow mapping otherwise becomes quadratic.
	var formatted bytes.Buffer
	if err := json.Indent(&formatted, content, "", "  "); err != nil {
		return "", fmt.Errorf("formatting workflow snapshot: %w", err)
	}
	return formatted.String(), nil
}

func sourceSyntax(content string) (*ast.File, *ast.DocumentNode, error) {
	file, err := parser.ParseBytes([]byte(content), parser.ParseComments)
	if err != nil {
		return nil, nil, fmt.Errorf("parsing workflow source syntax: %w", err)
	}
	var body *ast.DocumentNode
	for _, document := range file.Docs {
		switch document.Body.(type) {
		case nil, *ast.CommentGroupNode, *ast.CommentNode:
			continue
		}
		if body != nil {
			return nil, nil, errors.New("workflow source must contain one nonempty YAML document")
		}
		body = document
	}
	if body == nil {
		return nil, nil, errors.New("workflow source must contain one nonempty YAML document")
	}
	return file, body, nil
}

func (source *workflowSource) emit(lowered string) (string, error) {
	return source.emitWithValidation(lowered, validateSourceEmission)
}

func (source *workflowSource) emitWithValidation(lowered string, validate func(string, string) error) (string, error) {
	if source.lowered == lowered {
		return source.text, nil
	}
	file, body, err := sourceSyntax(source.text)
	if err != nil {
		return "", err
	}
	_, before, err := sourceSyntax(source.lowered)
	if err != nil {
		return "", err
	}
	_, after, err := sourceSyntax(lowered)
	if err != nil {
		return "", err
	}
	// A parsed AST has independent values, not live YAML alias bindings.
	// Materialize aliases before edits so changing an anchor cannot change
	// an otherwise untouched job that previously referenced it.
	ast.Walk(sourceCommentVisitor{}, body.Body)
	body.Body, err = materializeSourceAliases(body.Body, source.text)
	if err != nil {
		return "", err
	}
	body.Body, err = mergeSourceNode(body.Body, before.Body, after.Body)
	if err != nil {
		return "", err
	}
	preserveSourceLiteralNewlines(body.Body, nil)
	content := file.String()
	if !strings.HasSuffix(source.text, "\n") {
		content = strings.TrimSuffix(content, "\n")
	}
	if strings.Count(source.text, "\n") == strings.Count(source.text, "\r\n") {
		content = strings.ReplaceAll(content, "\r\n", "\n")
		content = strings.ReplaceAll(content, "\n", "\r\n")
	}
	if err := validate(content, lowered); err != nil {
		return "", err
	}
	return content, nil
}

func validateSourceEmission(content, expected string) error {
	workflow, err := Parse([]byte(content))
	if err != nil {
		return fmt.Errorf("validating edited workflow source: %w", err)
	}
	actual, err := serializedWorkflow(workflow)
	if err != nil {
		return err
	}
	if actual != expected {
		return errors.New("edited workflow source does not match the requested Actions AST")
	}
	return nil
}

func materializeSourceAliases(node ast.Node, content string) (ast.Node, error) {
	if len(ast.Filter(ast.AliasType, node)) == 0 {
		return node, nil
	}
	var values any
	if err := yaml.Unmarshal([]byte(content), &values); err != nil {
		return nil, fmt.Errorf("decoding workflow aliases: %w", err)
	}
	snapshot, err := snapshotYAMLValue(values)
	if err != nil {
		return nil, fmt.Errorf("materializing workflow aliases: %w", err)
	}
	_, decoded, err := sourceSyntax(snapshot)
	if err != nil {
		return nil, err
	}
	return detachSourceAliases(node, decoded.Body)
}

func detachSourceAliases(node, decoded ast.Node) (ast.Node, error) {
	switch value := node.(type) {
	case *ast.AliasNode:
		return replaceSourceNode(node, decoded)
	case *ast.AnchorNode:
		child, err := detachSourceAliases(value.Value, decoded)
		value.Value = child
		return value, err
	case *ast.MappingNode, *ast.MappingValueNode:
		fields := sourceMapping(decoded)
		for _, field := range sourceMapping(node) {
			target := fields[sourceKey(field.Key)]
			if field.Key.IsMergeKey() {
				replacement, err := replaceSourceNode(field.Value, decoded)
				if err != nil {
					return nil, err
				}
				shiftSourceNode(replacement, field.Value.GetToken().Position.Column-replacement.GetToken().Position.Column)
				field.Value = replacement
				continue
			}
			if target == nil {
				return nil, fmt.Errorf("missing decoded workflow field %q", sourceKey(field.Key))
			}
			child, err := detachSourceAliases(field.Value, target.Value)
			if err != nil {
				return nil, err
			}
			shiftSourceNode(child, field.Value.GetToken().Position.Column-child.GetToken().Position.Column)
			field.Value = child
		}
	case *ast.SequenceNode:
		sequence, ok := decoded.(*ast.SequenceNode)
		if !ok || len(sequence.Values) != len(value.Values) {
			return nil, errors.New("decoded workflow alias sequence does not match its source")
		}
		for index, child := range value.Values {
			if index >= 0 && index < len(sequence.Values) {
				replacement, err := detachSourceAliases(child, sequence.Values[index])
				if err != nil {
					return nil, err
				}
				shiftSourceNode(replacement, value.Values[index].GetToken().Position.Column-replacement.GetToken().Position.Column)
				value.Values[index] = replacement
			}
		}
	}
	return node, nil
}
