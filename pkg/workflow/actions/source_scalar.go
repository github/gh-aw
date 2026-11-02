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

func preserveSourceLiteralNewlines(node ast.Node, following ast.Node) {
	switch value := node.(type) {
	case *ast.MappingNode:
		for index, field := range value.Values {
			nextNode := following
			next := index + 1
			if next >= 0 && next < len(value.Values) {
				nextNode = value.Values[next]
			}
			preserveSourceLiteralNewlines(field, nextNode)
		}
	case *ast.MappingValueNode:
		if value.FootComment != nil {
			following = value.FootComment
		}
		preserveSourceLiteralNewlines(value.Value, following)
	case *ast.SequenceNode:
		for index, child := range value.Values {
			nextNode := following
			if value.FootComment != nil {
				nextNode = value.FootComment
			}
			next := index + 1
			if next >= 0 && next < len(value.Values) {
				nextNode = value.Values[next]
			}
			if literal, ok := child.(*ast.LiteralNode); ok && strings.Contains(literal.Start.Value, "+") {
				// SequenceNode.String reindents short whitespace-only lines.
				// Pad syntax indentation first so it cannot become scalar data.
				origin := strings.ReplaceAll(literal.Value.Token.Origin, "\r\n", "\n")
				indent := sourceLiteralIndent(origin)
				if indent == 0 && strings.Trim(literal.Value.Value, "\n") == "" {
					// Keep an empty body indented after the sequence renderer
					// removes one block indent from every literal line.
					indent = 2 * (value.Start.Position.Column + 1)
				}
				lines := strings.Split(origin, "\n")
				for index, line := range lines {
					if strings.TrimSpace(line) == "" && len(line) < indent {
						lines[index] = strings.Repeat(" ", indent)
					}
				}
				literal.Value.Token.Origin = strings.Join(lines, "\n")
			}
			preserveSourceLiteralNewlines(child, nextNode)
		}
	case *ast.AnchorNode:
		preserveSourceLiteralNewlines(value.Value, following)
	case *ast.TagNode:
		preserveSourceLiteralNewlines(value.Value, following)
	case *ast.LiteralNode:
		preserveSourceLiteralSuffix(value, following)
	}
}

func clearSourceLeadingBlankLine(node ast.Node) {
	if comment := node.GetComment(); comment != nil && len(comment.Comments) > 0 {
		comment.Comments[0].Token.Prev = nil
		return
	}
	switch value := node.(type) {
	case *ast.MappingNode:
		if len(value.Values) > 0 {
			clearSourceLeadingBlankLine(value.Values[0])
		}
	case *ast.MappingValueNode:
		value.Key.GetToken().Prev = nil
	case *ast.CommentGroupNode:
		if len(value.Comments) > 0 {
			value.Comments[0].Token.Prev = nil
		}
	}
}

func preserveSourceLiteralSuffix(literal *ast.LiteralNode, following ast.Node) {
	if !strings.Contains(literal.Start.Value, "+") {
		return
	}
	origin := strings.ReplaceAll(literal.Value.Token.Origin, "\r\n", "\n")
	if strings.HasSuffix(literal.Value.Value, "\n") {
		// The lexer can include indentation belonging to the following syntax.
		if end := strings.LastIndexByte(origin, '\n'); end >= 0 && end < len(origin) {
			origin = origin[:end+1]
		}
	}
	body := strings.TrimSuffix(origin, "\n")
	followingText := ""
	if following != nil {
		followingText = following.String()
		if comment, ok := following.(*ast.CommentGroupNode); ok {
			followingText = comment.StringWithSpace(0)
		}
	}
	if strings.HasPrefix(followingText, "\n") {
		// The scalar body already includes its trailing blank lines. Do not
		// duplicate one from the following key's original token positions.
		if shortened, ok := strings.CutSuffix(body, "\n"); ok {
			body = shortened
		} else {
			clearSourceLeadingBlankLine(following)
		}
	}
	// LiteralNode.String trims terminal spaces, then terminal newlines.
	// Retain the original suffix, including excess indentation as scalar data,
	// and pad only a terminal empty line so those trims cannot remove it.
	if strings.HasSuffix(body, "\n") {
		body += strings.Repeat(" ", max(sourceLiteralIndent(origin), 1))
	}
	literal.Value.Token.Origin = body + "\n"
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
