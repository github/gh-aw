package actions

import (
	"fmt"
	"strings"

	"github.com/goccy/go-yaml"
	"github.com/goccy/go-yaml/ast"
	"github.com/goccy/go-yaml/token"
)

type sourceLiteralIndentVisitor int

func sourceLiteralIndent(origin string) int {
	indent := -1
	for line := range strings.SplitSeq(origin, "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		column := len(line) - len(strings.TrimLeft(line, " "))
		if indent < 0 || column < indent {
			indent = column
		}
	}
	return max(indent, 0)
}

func (shift sourceLiteralIndentVisitor) Visit(node ast.Node) ast.Visitor {
	if literal, ok := node.(*ast.LiteralNode); ok {
		lines := strings.Split(literal.Value.Token.Origin, "\n")
		for index, line := range lines {
			if line == "" {
				continue
			}
			if shift > 0 {
				lines[index] = strings.Repeat(" ", int(shift)) + line
			} else {
				indent := len(line) - len(strings.TrimLeft(line, " "))
				lines[index] = line[min(indent, -int(shift)):]
			}
		}
		literal.Value.Token.Origin = strings.Join(lines, "\n")
	}
	return shift
}

func shiftSourceNode(node ast.Node, column int) {
	// LiteralNode renders its original body rather than its token columns.
	// Move that body with the syntax node so nested replacements stay valid.
	ast.Walk(sourceLiteralIndentVisitor(column), node)
	node.AddColumn(column)
}

func preserveSourceLiteralNewlines(node ast.Node, followingBlankLine bool) {
	switch value := node.(type) {
	case *ast.MappingNode:
		for index, field := range value.Values {
			blank := followingBlankLine
			next := index + 1
			if next >= 0 && next < len(value.Values) {
				blank = strings.HasPrefix(value.Values[next].String(), "\n")
			}
			preserveSourceLiteralNewlines(field, blank)
		}
	case *ast.MappingValueNode:
		preserveSourceLiteralNewlines(value.Value, followingBlankLine && value.FootComment == nil)
	case *ast.SequenceNode:
		for index, child := range value.Values {
			blank := followingBlankLine && value.FootComment == nil
			next := index + 1
			if next >= 0 && next < len(value.Values) {
				blank = strings.HasPrefix(value.Values[next].String(), "\n")
			}
			preserveSourceLiteralNewlines(child, blank)
		}
	case *ast.AnchorNode:
		preserveSourceLiteralNewlines(value.Value, followingBlankLine)
	case *ast.TagNode:
		preserveSourceLiteralNewlines(value.Value, followingBlankLine)
	case *ast.LiteralNode:
		preserveSourceLiteralSuffix(value, followingBlankLine)
	}
}

func preserveSourceLiteralSuffix(literal *ast.LiteralNode, followingBlankLine bool) {
	if !strings.Contains(literal.Start.Value, "+") {
		return
	}
	text := literal.Value.Value
	newlines := len(text) - len(strings.TrimRight(text, "\n"))
	if newlines <= 1 {
		return
	}
	origin := literal.Value.Token.Origin
	lines := strings.Split(origin, "\n")
	end := 0
	for index, line := range lines {
		if strings.TrimSpace(line) != "" {
			end = index + 1
		}
	}
	lines = lines[:end]
	// LiteralNode.String trims terminal newlines, while the next mapping key
	// can retain one blank line through its token positions. Account for both
	// only after merging, when the actual following syntax is known.
	if followingBlankLine {
		newlines--
	}
	blank := "\n" + strings.Repeat(" ", sourceLiteralIndent(origin))
	literal.Value.Token.Origin = strings.Join(lines, "\n") + strings.Repeat(blank, newlines-1) + "\n"
}

func sourceScalarReplacement(source, replacement ast.Node) (ast.Node, error) {
	original, literal := source.(*ast.LiteralNode)
	if !literal {
		return replacement, nil
	}
	text, ok := replacement.(*ast.StringNode)
	if !ok || !strings.ContainsAny(text.Value, "\r\n") {
		return replacement, nil
	}
	// Implicit literal indentation cannot preserve leading indentation or CR
	// characters. Keep those values quoted rather than normalizing their data.
	if strings.Contains(text.Value, "\r") || sourceLiteralIndent(original.Value.Token.Origin) == 0 {
		return replacement, nil
	}
	for line := range strings.SplitSeq(text.Value, "\n") {
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, " ") || strings.HasPrefix(line, "\t") {
			return replacement, nil
		}
		break
	}
	content, err := yaml.MarshalWithOptions(map[string]any{"value": text.Value}, yaml.UseLiteralStyleIfMultiline(true))
	if err != nil {
		return nil, fmt.Errorf("encoding replacement multiline scalar: %w", err)
	}
	_, document, err := sourceSyntax(string(content))
	if err != nil {
		return nil, err
	}
	result := sourceMapping(document.Body)["value"].Value
	if multiline, ok := result.(*ast.LiteralNode); ok {
		shiftSourceNode(multiline, sourceLiteralIndent(original.Value.Token.Origin)-sourceLiteralIndent(multiline.Value.Token.Origin))
		multiline.Start.Position.Column = original.Start.Position.Column
	}
	return result, nil
}

func flowSourceNode(node ast.Node) (ast.Node, error) {
	switch value := node.(type) {
	case *ast.StringNode:
		if value.Token.Type != token.SingleQuoteType && value.Token.Type != token.DoubleQuoteType &&
			strings.ContainsAny(value.Value, "{}[],\r\n") {
			value.Token.Type = token.DoubleQuoteType
		}
	case *ast.LiteralNode:
		var text string
		if err := yaml.NodeToValue(value, &text); err != nil {
			return nil, fmt.Errorf("decoding multiline flow scalar: %w", err)
		}
		position := *value.Start.Position
		scalar := ast.String(token.New(text, text, &position))
		scalar.Token.Type = token.DoubleQuoteType
		if err := scalar.SetComment(value.GetComment()); err != nil {
			return nil, err
		}
		return scalar, nil
	case *ast.MappingNode:
		value.SetIsFlowStyle(true)
		for _, field := range value.Values {
			if _, err := flowSourceNode(field); err != nil {
				return nil, err
			}
		}
	case *ast.MappingValueNode:
		value.SetIsFlowStyle(true)
		if _, err := flowSourceNode(value.Key); err != nil {
			return nil, err
		}
		child, err := flowSourceNode(value.Value)
		if err != nil {
			return nil, err
		}
		value.Value = child
	case *ast.SequenceNode:
		value.SetIsFlowStyle(true)
		for index, child := range value.Values {
			replacement, err := flowSourceNode(child)
			if err != nil {
				return nil, err
			}
			value.Values[index] = replacement
		}
	case *ast.AnchorNode:
		child, err := flowSourceNode(value.Value)
		if err != nil {
			return nil, err
		}
		value.Value = child
	case *ast.TagNode:
		child, err := flowSourceNode(value.Value)
		if err != nil {
			return nil, err
		}
		value.Value = child
	}
	return node, nil
}
