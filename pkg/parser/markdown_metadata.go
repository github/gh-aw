package parser

import (
	"errors"
	"regexp"
	"slices"
	"strings"

	"github.com/goccy/go-yaml"
	"github.com/goccy/go-yaml/ast"
	yamlparser "github.com/goccy/go-yaml/parser"
)

// MarkdownMetadataBlock is author frontmatter at a document or inline-artifact
// boundary. Source is normalized consistently with runtime digest lookups.
type MarkdownMetadataBlock struct {
	Source   string
	Document bool
	Error    string
}

var inlineMetadataHeading = regexp.MustCompile(`^##[ \t]+(?:agent|skill):`)

// ExtractMarkdownMetadataBlocks recognizes the engine reader's delimiter forms
// and always stops at the first closing fence, including an empty header.
func ExtractMarkdownMetadataBlocks(content string) []MarkdownMetadataBlock {
	lines := slices.Collect(strings.SplitSeq(strings.ReplaceAll(content, "\r\n", "\n"), "\n"))
	var blocks []MarkdownMetadataBlock
	for index := 0; index < len(lines); index++ {
		opening := index
		document := index == 0 && !inlineMetadataHeading.MatchString(lines[index]) //nolint:uncheckedsliceindex // The loop bounds index.
		if !document {
			if !inlineMetadataHeading.MatchString(lines[index]) { //nolint:uncheckedsliceindex // The loop bounds index.
				continue
			}
			opening++
			for _, line := range lines[opening:] {
				if strings.TrimSpace(line) != "" {
					break
				}
				opening++
			}
		}
		if opening < 0 || opening >= len(lines) {
			continue
		}
		if strings.TrimSpace(strings.TrimPrefix(lines[opening], "\uFEFF")) != "---" {
			continue
		}
		closing := opening + 1
		for _, line := range lines[closing:] {
			if strings.TrimSpace(line) == "---" {
				break
			}
			closing++
		}
		block := MarkdownMetadataBlock{Source: strings.Join(lines[opening+1:closing], "\n"), Document: document}
		if closing == len(lines) {
			block.Error = "unterminated frontmatter"
		}
		blocks = append(blocks, block)
		index = closing
	}
	return blocks
}

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
