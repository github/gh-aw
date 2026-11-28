package codemod

import (
	"errors"
	"fmt"
	"strings"

	yamlv3 "go.yaml.in/yaml/v3"
)

// TransferInlineComment copies an inline YAML comment to a mapping field under
// a sequence, such as an import input, before the source field is removed.
func (f *Frontmatter) TransferInlineComment(source []string, destination []any) error {
	if len(source) == 0 || len(destination) == 0 {
		return errors.New("frontmatter comment transfer requires source and destination paths")
	}
	key, value, err := f.entry(source)
	if err != nil {
		return err
	}
	var comments []string
	for _, comment := range []string{key.LineComment, value.LineComment} {
		comment = strings.TrimSpace(strings.TrimPrefix(comment, "#"))
		if comment != "" {
			comments = append(comments, comment)
		}
	}
	if len(comments) == 0 {
		return nil
	}

	node, err := f.syntax()
	if err != nil {
		return err
	}
	line := 0
	for _, part := range destination {
		switch part := part.(type) {
		case string:
			keyNode, valueNode := mappingEntry(node, part)
			if keyNode == nil || valueNode == nil {
				return fmt.Errorf("cannot locate frontmatter comment destination %q", part)
			}
			line, node = keyNode.Line, valueNode
		case int:
			if node.Kind != yamlv3.SequenceNode || part < 0 {
				return errors.New("frontmatter comment destination index is not a sequence item")
			}
			var item *yamlv3.Node
			for index, candidate := range node.Content {
				if index == part {
					item = candidate
					break
				}
			}
			if item == nil {
				return errors.New("frontmatter comment destination index is not a sequence item")
			}
			node, line = item, item.Line
		default:
			return errors.New("frontmatter comment destination path must contain only keys and indexes")
		}
	}
	for _, comment := range comments {
		if err := f.appendInlineComment(line, comment); err != nil {
			return err
		}
	}
	return nil
}

func (f *Frontmatter) appendInlineComment(line int, text string) error {
	if strings.ContainsAny(text, "\r\n") {
		return errors.New("frontmatter comment requires single-line text")
	}
	start, err := f.offset(line, 1)
	if err != nil {
		return err
	}
	newline := strings.IndexByte(f.content[start:], '\n')
	if newline < 0 {
		return errors.New("cannot locate frontmatter comment line")
	}
	end := start + newline
	if strings.HasSuffix(f.content[start:end], "\r") {
		end--
	}
	if strings.Contains(f.content[start:end], text) {
		return nil
	}
	return f.replace(end, end, " # "+text)
}
