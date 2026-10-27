package actionslanguageservice

import (
	"strings"

	"go.yaml.in/yaml/v3"
)

// YAML columns count Unicode code points; source slices use byte offsets.
func sourceColumnOffset(source string, column int) int {
	for offset := range source {
		column--
		if column == 0 {
			return offset
		}
	}
	return len(source)
}

// yaml.v3 accepts some flow syntax rejected by the upstream YAML 1.2 reader.
// Check the original spelling, rather than trying to recover it from values.
func (p *workflowParser) validateYAMLSource(node *yaml.Node, lines []string, indent, depth int) error {
	if depth > 100 {
		return p.error(node, "workflow exceeds the template depth limit")
	}
	if node.Kind == yaml.AliasNode {
		return nil
	}
	if node.Style&yaml.FlowStyle != 0 || node.Style&(yaml.SingleQuotedStyle|yaml.DoubleQuotedStyle) != 0 {
		if err := p.validateFlowSource(node, lines, indent); err != nil {
			return err
		}
	}
	if (node.Kind == yaml.MappingNode || node.Kind == yaml.SequenceNode) && node.Style&yaml.FlowStyle == 0 {
		indent = node.Column - 1
	}
	for _, child := range node.Content {
		if err := p.validateYAMLSource(child, lines, indent, depth+1); err != nil {
			return err
		}
	}
	return nil
}

func (p *workflowParser) validateFlowSource(node *yaml.Node, lines []string, indent int) error {
	quote := byte(0)
	nesting := 0
	for line := node.Line - 1; line < len(lines); line++ {
		text := lines[line]
		column := 0
		if line == node.Line-1 {
			column = sourceColumnOffset(text, node.Column)
		} else if quote != 0 && strings.TrimSpace(text) != "" && len(text)-len(strings.TrimLeft(text, " ")) <= indent {
			return p.error(node, "a multiline quoted scalar must be indented beyond its parent")
		}

		for i := column; i < len(text); i++ {
			c := text[i]
			if quote != 0 {
				if c == '\\' && quote == '"' {
					i++
				} else if c == quote {
					if quote == '\'' && i+1 < len(text) && text[i+1] == '\'' {
						i++
						continue
					}
					quote = 0
					if nesting == 0 {
						return nil
					}
				}
				continue
			}
			switch c {
			case '\'', '"':
				quote = c
			case '[', '{':
				nesting++
			case ']', '}':
				nesting--
				if nesting == 0 {
					if i+1 < len(text) && text[i+1] == '#' {
						return p.error(node, "comments must be separated from flow collections by whitespace")
					}
					return nil
				}
			case '#':
				if i == 0 || text[i-1] == ' ' || text[i-1] == '\t' {
					i = len(text)
				}
			}
		}
	}
	return nil
}
