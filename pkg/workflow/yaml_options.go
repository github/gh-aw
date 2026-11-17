package workflow

import (
	"strings"

	"github.com/goccy/go-yaml"
	"github.com/goccy/go-yaml/ast"
	"github.com/goccy/go-yaml/token"
)

// DefaultMarshalOptions provides standard YAML formatting options
// used throughout gh-aw for workflow and frontmatter generation.
//
// These options ensure consistent YAML output that follows GitHub Actions
// conventions and best practices:
//   - yaml.Indent(2): Use 2-space indentation (GitHub Actions standard)
//   - yaml.UseLiteralStyleIfMultiline(true): Use literal block scalars (|)
//     for multiline strings to preserve formatting and readability
var DefaultMarshalOptions = []yaml.EncodeOption{
	yaml.Indent(2),                        // Use 2-space indentation
	yaml.UseLiteralStyleIfMultiline(true), // Use literal block scalars for multiline strings
}

// marshalYAMLWithOptions preserves multiline values that bare literal blocks
// would misindent or normalize. Ordinary multiline strings retain literal style.
func marshalYAMLWithOptions(value any, options ...yaml.EncodeOption) ([]byte, error) {
	node, err := yaml.NewEncoder(nil, options...).EncodeToNode(value)
	if err != nil {
		return nil, err
	}
	repaired := false
	for _, candidate := range ast.Filter(ast.StringType, node) {
		text, ok := candidate.(*ast.StringNode)
		if !ok {
			continue
		}
		if !strings.ContainsAny(text.Value, "\r\n") {
			continue
		}
		firstContent := strings.TrimLeft(text.Value, "\n")
		unsafe := firstContent == "" || strings.Contains(text.Value, "\r") || strings.HasPrefix(firstContent, " ") ||
			strings.HasPrefix(firstContent, "\t") ||
			strings.HasSuffix(text.Value, " ") || strings.HasSuffix(text.Value, "\t")
		repaired = repaired || unsafe
		// Reparsing an AST literal can also strip its final whitespace.
		if unsafe || strings.HasSuffix(text.Value, "\n\n") {
			text.Token.Type = token.DoubleQuoteType
		}
	}
	if !repaired {
		return yaml.MarshalWithOptions(value, options...)
	}
	return yaml.MarshalWithOptions(node, options...)
}
