package codemod

import (
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strings"

	"github.com/goccy/go-yaml"
	"github.com/goccy/go-yaml/lexer"
	"github.com/goccy/go-yaml/token"
	yamlv3 "go.yaml.in/yaml/v3"
)

func (f *Frontmatter) newline() string {
	first, _, _ := strings.Cut(f.content, "\n")
	if strings.HasSuffix(first, "\r") {
		return "\r\n"
	}
	return "\n"
}

func (f *Frontmatter) syntax() (*yamlv3.Node, error) {
	start, end, err := f.bounds()
	if err != nil {
		return nil, err
	}
	var document yamlv3.Node
	if err := yamlv3.Unmarshal([]byte(f.content[start:end]), &document); err != nil {
		return nil, fmt.Errorf("parsing frontmatter syntax: %w", err)
	}
	for _, node := range document.Content {
		return node, nil
	}
	return nil, errors.New("frontmatter has no mapping")
}

func mappingEntry(node *yamlv3.Node, key string) (*yamlv3.Node, *yamlv3.Node) {
	if node.Kind != yamlv3.MappingNode {
		return nil, nil
	}

	var keyNode *yamlv3.Node
	for _, child := range node.Content {
		if keyNode == nil {
			keyNode = child
			continue
		}
		if keyNode.Value == key {
			return keyNode, child
		}
		keyNode = nil
	}
	return nil, nil
}

func hasAliases(node *yamlv3.Node) bool {
	return node.Kind == yamlv3.AliasNode || slices.ContainsFunc(node.Content, hasAliases)
}

func (f *Frontmatter) scalarSpan(node *yamlv3.Node) (int, int, bool) {
	start, end, err := f.bounds()
	if err != nil {
		return 0, 0, false
	}
	offset := -1
	// Normalize line endings for lexer positions; offsets still use the original source.
	for _, item := range lexer.Tokenize(strings.ReplaceAll(f.content[start:end], "\r\n", "\n")) {
		if offset >= 0 {
			next, err := f.offset(item.Position.Line, item.Position.Column)
			if err != nil || next < offset {
				return 0, 0, false
			}
			text := strings.TrimRight(f.content[offset:next], " \t\r\n")
			return offset, offset + len(text), text != ""
		}
		if item.Position.Line != node.Line || item.Position.Column != node.Column {
			continue
		}
		if item.Type == token.MappingValueType || item.Type == token.CollectEntryType {
			return 0, 0, false
		}
		offset, err = f.offset(node.Line, node.Column)
		if err != nil {
			return 0, 0, false
		}
	}
	if offset >= 0 {
		text := strings.TrimRight(f.content[offset:end], " \t\r\n")
		return offset, offset + len(text), text != ""
	}
	return 0, 0, false
}

func (f *Frontmatter) nodeEnd(node *yamlv3.Node) (int, error) {
	if node.Kind == yamlv3.ScalarNode || node.Kind == yamlv3.AliasNode {
		_, end, found := f.scalarSpan(node)
		if found && node.Style != yamlv3.LiteralStyle && node.Style != yamlv3.FoldedStyle {
			return end, nil
		}
		return 0, errors.New("frontmatter source node requires structural rendering")
	}
	if node.Style&yamlv3.FlowStyle != 0 {
		start, end, err := f.bounds()
		if err != nil {
			return 0, err
		}
		depth, active := 0, false
		for _, item := range lexer.Tokenize(strings.ReplaceAll(f.content[start:end], "\r\n", "\n")) {
			if item.Position.Line == node.Line && item.Position.Column == node.Column {
				active = true
			}
			if !active {
				continue
			}
			switch item.Type {
			case token.MappingStartType, token.SequenceStartType:
				depth++
			case token.MappingEndType, token.SequenceEndType:
				depth--
				if depth == 0 {
					offset, err := f.offset(item.Position.Line, item.Position.Column)
					return offset + 1, err
				}
			}
		}
	}
	end := 0
	for _, child := range node.Content {
		childEnd, err := f.nodeEnd(child)
		if err != nil {
			return 0, err
		}
		end = max(end, childEnd)
	}
	if end == 0 {
		return 0, errors.New("cannot locate frontmatter source node end")
	}
	return end, nil
}

func (f *Frontmatter) setSource(path []string, value any) (bool, error) {
	original, exists := f.Lookup(path...)
	if exists {
		equal, err := equalValues(original, value)
		if err != nil || equal {
			return true, err
		}
	}
	root, err := f.syntax()
	if err != nil {
		return false, nil
	}
	if hasAliases(root) {
		return false, nil
	}
	parent := root
	for index, key := range path {
		keyNode, node := mappingEntry(parent, key)
		if node == nil {
			nested := value
			for _, part := range slices.Backward(path[index+1:]) {
				nested = map[string]any{part: nested}
			}
			return f.insertSource(parent, key, nested)
		}
		if index < len(path)-1 {
			parent = node
			continue
		}
		return f.setScalarSource(parent, keyNode, node, value)
	}
	return false, nil
}

func (f *Frontmatter) setScalarSource(parent, keyNode, node *yamlv3.Node, value any) (bool, error) {
	start, end, found := f.scalarSpan(node)
	if node.Kind != yamlv3.ScalarNode || node.Style == yamlv3.LiteralStyle || node.Style == yamlv3.FoldedStyle || !found {
		return false, nil
	}
	encoded, err := yaml.Marshal(value)
	if err != nil {
		return true, err
	}
	text := strings.TrimSuffix(string(encoded), "\n")
	collection := reflect.TypeOf(value) != nil && (reflect.TypeOf(value).Kind() == reflect.Map || reflect.TypeOf(value).Kind() == reflect.Slice)
	if strings.Contains(text, "\n") || collection {
		if parent.Style&yamlv3.FlowStyle != 0 {
			encoded, err = yaml.MarshalWithOptions(value, yaml.Flow(true))
			if err != nil {
				return true, err
			}
			text = strings.TrimSpace(string(encoded))
		} else {
			indent := strings.Repeat(" ", keyNode.Column+1)
			text = "\n" + indent + strings.ReplaceAll(text, "\n", "\n"+indent)
			start = len(strings.TrimRight(f.content[:start], " \t"))
		}
	} else if stringValue, ok := value.(string); ok && node.Style == yamlv3.SingleQuotedStyle {
		text = "'" + strings.ReplaceAll(stringValue, "'", "''") + "'"
	} else if _, ok := value.(string); ok && (node.Style == yamlv3.DoubleQuotedStyle || parent.Style&yamlv3.FlowStyle != 0) {
		encoded, err = yaml.MarshalWithOptions(value, yaml.JSON())
		if err != nil {
			return true, err
		}
		text = strings.TrimSpace(string(encoded))
	}
	text = strings.ReplaceAll(text, "\n", f.newline())
	return true, f.replace(start, end, text)
}

func (f *Frontmatter) insertSource(parent *yamlv3.Node, key string, value any) (bool, error) {
	if parent.Kind != yamlv3.MappingNode {
		return false, nil
	}
	if parent.Style&yamlv3.FlowStyle != 0 {
		encoded, err := yaml.MarshalWithOptions(map[string]any{key: value}, yaml.Flow(true))
		if err != nil {
			return true, err
		}
		text := strings.TrimSpace(string(encoded))
		text = strings.TrimSuffix(strings.TrimPrefix(text, "{"), "}")
		position, err := f.offset(parent.Line, parent.Column)
		if err != nil {
			return true, err
		}
		if len(parent.Content) > 0 {
			text += ", "
		}
		suffix := f.content[position+1:]
		end := position + 1 + len(suffix) - len(strings.TrimLeft(suffix, " "))
		return true, f.replace(position+1, end, text)
	}
	if len(parent.Content) == 0 {
		return false, nil
	}
	encoded, err := yaml.MarshalWithOptions(map[string]any{key: value}, yaml.IndentSequence(true))
	if err != nil {
		return true, err
	}
	first, err := firstMappingKey(parent)
	if err != nil {
		return true, err
	}
	position, err := f.offset(first.Line, 1)
	if err != nil {
		return true, err
	}
	indent := strings.Repeat(" ", first.Column-1)
	field := strings.TrimSuffix(string(encoded), "\n")
	if key == "on" {
		// AW/Actions convention leaves the reserved trigger key unquoted, unlike other YAML boolean-like keys.
		field = strings.TrimPrefix(field, `"on"`)
		field = "on" + field
	}
	text := indent + strings.ReplaceAll(field, "\n", "\n"+indent) + "\n"
	text = strings.ReplaceAll(text, "\n", f.newline())
	return true, f.replace(position, position, text)
}

func (f *Frontmatter) removeSource(path []string) (bool, error) {
	root, err := f.syntax()
	if err != nil {
		return false, nil
	}
	if hasAliases(root) {
		return false, nil
	}
	parent := root
	var key, node, parentKey *yamlv3.Node
	for index, part := range path {
		key, node = mappingEntry(parent, part)
		if node == nil {
			return false, nil
		}
		if index < len(path)-1 {
			parentKey = key
			parent = node
		}
	}
	if parent.Style&yamlv3.FlowStyle != 0 {
		return f.removeFlowSource(parent, key, node)
	}
	if node.Kind == yamlv3.ScalarNode && node.Value == "" && node.Line == key.Line {
		if len(parent.Content) == 2 && parent != root {
			return f.emptyParent(parentKey, key, node)
		}
		// Implicit null nodes have no scalar token.
		start, err := f.offset(key.Line, 1)
		if err != nil {
			return true, err
		}
		end := strings.IndexByte(f.content[start:], '\n')
		if end >= 0 {
			return true, f.replace(start, start+end+1, "")
		}
		return false, nil
	}
	end, err := f.nodeEnd(node)
	if err != nil {
		return false, nil
	}
	start, err := f.offset(key.Line, 1)
	if err != nil {
		return true, err
	}
	newline := strings.IndexByte(f.content[end:], '\n')
	if newline < 0 {
		return false, nil
	}
	end += newline + 1
	if len(parent.Content) == 2 && parent != root {
		return f.emptyParent(parentKey, key, node)
	}
	return true, f.replace(start, end, "")
}

func (f *Frontmatter) removeFlowSource(parent, key, node *yamlv3.Node) (bool, error) {
	end, err := f.nodeEnd(node)
	if err != nil {
		return false, nil
	}
	start, err := f.offset(key.Line, key.Column)
	if err != nil {
		return true, err
	}
	var previous, candidate *yamlv3.Node
	selected := false
	for _, child := range parent.Content {
		if candidate == nil {
			if selected {
				end, err = f.offset(child.Line, child.Column)
				if err != nil {
					return true, err
				}
				return true, f.replace(start, end, "")
			}
			candidate = child
			continue
		}
		if candidate == key {
			selected = true
		} else {
			previous = child
		}
		candidate = nil
	}
	if previous != nil {
		previousEnd, err := f.nodeEnd(previous)
		if err != nil {
			return false, nil
		}
		comma := strings.IndexByte(f.content[previousEnd:start], ',')
		if comma < 0 {
			return false, nil
		}
		start = previousEnd + comma
	}
	return true, f.replace(start, end, "")
}

func (f *Frontmatter) emptyParent(parentKey, key, node *yamlv3.Node) (bool, error) {
	if parentKey == nil {
		return false, nil
	}
	_, keyEnd, found := f.scalarSpan(parentKey)
	if !found {
		return false, nil
	}
	colon := strings.IndexByte(f.content[keyEnd:], ':')
	if colon < 0 {
		return false, nil
	}
	insert := keyEnd + colon + 1
	start, err := f.offset(key.Line, 1)
	if err != nil {
		return true, err
	}
	end, err := f.nodeEnd(node)
	if err != nil {
		if node.Kind != yamlv3.ScalarNode || node.Value != "" {
			return false, nil
		}
		end, err = f.offset(key.Line, key.Column)
		if err != nil {
			return true, err
		}
	}
	if newline := strings.IndexByte(f.content[end:], '\n'); newline >= 0 {
		end += newline + 1
	} else {
		return false, nil
	}
	return true, f.replace(insert, end, " {}"+f.content[insert:start])
}

// Move relocates a field without flattening nested values or losing its source
// spelling. Existing destinations are never overwritten.
func (f *Frontmatter) Move(path, destination []string) error {
	if len(path) == 0 || len(destination) == 0 {
		return errors.New("frontmatter move requires source and destination paths")
	}
	if len(destination) > len(path) && slices.Equal(path, destination[:len(path)]) {
		return errors.New("cannot move a frontmatter field into itself")
	}
	value, exists := f.Lookup(path...)
	if !exists {
		return nil
	}
	if _, exists := f.Lookup(destination...); exists {
		return nil
	}
	key, node, err := f.entry(path)
	if err != nil {
		return err
	}
	moveCanonical := func() error {
		if err := f.Set(destination, value); err != nil {
			return err
		}
		for _, comment := range []string{key.LineComment, node.LineComment} {
			if comment != "" {
				if err := f.Comment(destination, strings.TrimSpace(strings.TrimPrefix(comment, "#"))); err != nil {
					return err
				}
			}
		}
		return f.Remove(path...)
	}
	field, err := f.movedField(key, node, destination)
	if err != nil {
		return moveCanonical()
	}
	inserted, err := f.insertMovedField(path, destination, field)
	if err != nil {
		return err
	}
	if !inserted {
		return moveCanonical()
	}
	return f.Remove(path...)
}

func (f *Frontmatter) movedField(key, node *yamlv3.Node, destination []string) (string, error) {
	end, err := f.nodeEnd(node)
	if err != nil {
		return "", err
	}
	_, keyEnd, found := f.scalarSpan(key)
	if !found {
		return "", errors.New("cannot locate moved frontmatter key")
	}
	if node.LineComment != "" || key.LineComment != "" {
		if newline := strings.IndexByte(f.content[end:], '\n'); newline >= 0 {
			end += newline
		}
	}
	// Preserve the old value's exact syntax and inline comments, but replace its key.
	field := lastPathKey(destination) + strings.TrimSuffix(f.content[keyEnd:end], "\r")
	indent := strings.Repeat(" ", key.Column-1)
	return strings.ReplaceAll(field, "\n"+indent, "\n"), nil
}

func (f *Frontmatter) insertMovedField(path, destination []string, field string) (bool, error) {
	root, err := f.syntax()
	if err != nil {
		return false, err
	}
	if hasAliases(root) {
		return false, nil
	}
	parent := root
	for index, part := range destination[:len(destination)-1] {
		_, child := mappingEntry(parent, part)
		if child == nil {
			for _, part := range slices.Backward(destination[index : len(destination)-1]) {
				field = part + ":\n  " + strings.ReplaceAll(field, "\n", "\n  ")
			}
			break
		}
		parent = child
	}
	if parent.Kind != yamlv3.MappingNode || parent.Style&yamlv3.FlowStyle != 0 || len(parent.Content) == 0 {
		return false, nil
	}
	first, err := firstMappingKey(parent)
	if err != nil {
		return false, err
	}
	if len(destination) == 1 && len(path) > 1 {
		if sourceKey, _ := mappingEntry(root, firstPathKey(path)); sourceKey != nil {
			first = sourceKey
		}
	}
	position, err := f.offset(first.Line, 1)
	if err != nil {
		return false, err
	}
	newIndent := strings.Repeat(" ", first.Column-1)
	field = strings.ReplaceAll(field, "\r\n", "\n")
	field = newIndent + strings.ReplaceAll(field, "\n", "\n"+newIndent) + "\n"
	field = strings.ReplaceAll(field, "\n", f.newline())
	return true, f.replace(position, position, field)
}

func firstMappingKey(mapping *yamlv3.Node) (*yamlv3.Node, error) {
	for _, child := range mapping.Content {
		return child, nil
	}
	return nil, errors.New("cannot locate the first frontmatter mapping key")
}

func firstPathKey(path []string) string {
	for _, key := range path {
		return key
	}
	return ""
}

func lastPathKey(path []string) string {
	last := ""
	for _, key := range path {
		last = key
	}
	return last
}
