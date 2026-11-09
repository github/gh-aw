package parser

import (
	"errors"
	"strings"

	"github.com/goccy/go-yaml"
	"github.com/goccy/go-yaml/ast"
	yamlparser "github.com/goccy/go-yaml/parser"
)

// CompileMarkdownMetadata produces JSON-compatible author metadata for the
// JavaScript runtime. YAML syntax is interpreted only during compilation.
func CompileMarkdownMetadata(source string) (any, error) {
	if len(source) > 4<<20 {
		return nil, errors.New("markdown metadata exceeds the bounded input limit")
	}
	source = strings.ReplaceAll(source, "\r\n", "\n")
	if !strings.HasSuffix(source, "\n") {
		source += "\n"
	}
	file, err := yamlparser.ParseBytes([]byte(source), yamlparser.ParseComments)
	if err != nil || len(file.Docs) > 1 {
		return nil, errors.New("invalid or ambiguous document metadata")
	}
	var body ast.Node
	for _, document := range file.Docs {
		body = document.Body
	}
	if body == nil {
		return map[string]any{}, nil
	}
	count := 0
	if err := validateMarkdownMetadataNode(body, 0, &count); err != nil {
		return nil, err
	}
	var value any
	if err := yaml.NodeToValue(body, &value); err != nil {
		return nil, errors.New("invalid document metadata")
	}
	return value, nil
}

func validateMarkdownMetadataNode(node ast.Node, depth int, count *int) error {
	*count++
	if depth > 100 || *count > 10000 {
		return errors.New("document metadata exceeds structural limits")
	}
	switch item := node.(type) {
	case *ast.AnchorNode, *ast.AliasNode, *ast.TagNode:
		return errors.New("anchors, aliases, and tags are not allowed in Markdown metadata")
	case *ast.MappingNode:
		for _, pair := range item.Values {
			if err := validateMarkdownMetadataNode(pair, depth+1, count); err != nil {
				return err
			}
		}
	case *ast.MappingValueNode:
		if _, ok := item.Key.(*ast.StringNode); !ok {
			return errors.New("markdown metadata keys must be strings")
		}
		if err := validateMarkdownMetadataNode(item.Key, depth+1, count); err != nil {
			return err
		}
		return validateMarkdownMetadataNode(item.Value, depth+1, count)
	case *ast.SequenceNode:
		for _, value := range item.Values {
			if err := validateMarkdownMetadataNode(value, depth+1, count); err != nil {
				return err
			}
		}
	}
	return nil
}
