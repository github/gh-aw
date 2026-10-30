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
	state := yamlFlowSourceState{nodeStart: true}
	for line := node.Line - 1; line < len(lines); line++ {
		text := lines[line]
		column := 0
		if line == node.Line-1 {
			column = sourceColumnOffset(text, node.Column)
		} else if state.quote != 0 && strings.TrimSpace(text) != "" && len(text)-len(strings.TrimLeft(text, " ")) <= indent {
			return p.error(node, "a multiline quoted scalar must be indented beyond its parent")
		}

		for i := column; i < len(text); i++ {
			c := text[i]
			if state.quote != 0 {
				if c == '\\' && state.quote == '"' {
					i++
				} else if c == state.quote {
					if state.quote == '\'' && i+1 < len(text) && text[i+1] == '\'' {
						i++
						continue
					}
					state.quote = 0
					state.nodeStart = false
					if state.nesting == 0 {
						return nil
					}
				}
				continue
			}
			i = state.consumeUnquoted(text, i)
			if state.nesting == 0 && (c == ']' || c == '}') {
				if i+1 < len(text) && text[i+1] == '#' {
					return p.error(node, "comments must be separated from flow collections by whitespace")
				}
				return nil
			}
		}
	}
	return nil
}

type yamlFlowSourceState struct {
	quote       byte
	nesting     int
	nodeStart   bool
	plainScalar bool
}

func (s *yamlFlowSourceState) consumeUnquoted(text string, i int) int {
	switch c := text[i]; c {
	case '\'', '"':
		if s.nodeStart {
			s.quote = c
		}
		s.nodeStart = false
	case '[', '{':
		s.nesting++
		s.nodeStart = true
		s.plainScalar = false
	case ']', '}':
		s.nesting--
		s.nodeStart = false
	case ',':
		s.nodeStart = true
		s.plainScalar = false
	case ':':
		s.nodeStart = !s.plainScalar || i+1 == len(text) || strings.ContainsRune(" \t\r[]{},", rune(text[i+1]))
		if s.nodeStart {
			s.plainScalar = false
		}
	case '#':
		if i == 0 || text[i-1] == ' ' || text[i-1] == '\t' {
			return len(text)
		}
		s.nodeStart = false
		s.plainScalar = true
	case ' ', '\t', '\r':
	default:
		s.nodeStart = false
		s.plainScalar = true
	}
	return i
}
