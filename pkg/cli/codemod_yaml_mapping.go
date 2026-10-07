package cli

import (
	"bytes"
	"errors"
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
)

func removeYAMLMappingPath(content string, path []string, keepEmptyMapping bool) (string, bool, error) {
	if len(path) < 2 {
		return content, false, errors.New("YAML mapping path must contain at least two keys")
	}

	targetKey := lastYAMLPathKey(path)
	frontmatterYAML, suffix, err := splitFrontmatterForFormatting(content)
	if err != nil {
		return content, false, err
	}

	var document yaml.Node
	if err := yaml.Unmarshal([]byte(frontmatterYAML), &document); err != nil {
		return content, false, fmt.Errorf("failed to parse frontmatter YAML: %w", err)
	}
	root := yamlDocumentRoot(&document)
	if root == nil {
		return content, false, nil
	}

	mapping := root
	mappingPath := []string{}
	for _, key := range path[:len(path)-1] {
		value, ok := findYAMLMappingValue(mapping, key)
		if !ok || value.Kind != yaml.MappingNode {
			return content, false, nil
		}
		mapping = value
		mappingPath = append(mappingPath, key)
	}

	keyIndex, keyNode, valueNode, ok := findYAMLMappingEntry(mapping, targetKey)
	if !ok {
		return content, false, nil
	}

	for _, comment := range collectYAMLComments(keyNode, valueNode) {
		preserveYAMLFootComment(mapping, comment)
	}
	removeYAMLMappingEntryAt(mapping, keyIndex)

	if err := normalizeEmptyYAMLMapping(root, mapping, mappingPath, keyNode, valueNode, keepEmptyMapping); err != nil {
		return content, false, err
	}

	updated, err := encodeUpdatedFrontmatter(content, suffix, &document)
	if err != nil {
		return content, false, err
	}
	return updated, true, nil
}

func normalizeEmptyYAMLMapping(root, mapping *yaml.Node, path []string, removedKey, removedValue *yaml.Node, keepEmpty bool) error {
	if len(mapping.Content) > 0 {
		return nil
	}
	parentMapping, parentKeyNode, found := findYAMLMappingParent(root, path)
	if !found {
		return fmt.Errorf("unable to locate parent mapping for %q", lastYAMLPathKey(path))
	}
	if keepEmpty || hasYAMLComments(mapping) || hasYAMLComments(removedKey) || hasYAMLComments(removedValue) || hasYAMLComments(parentKeyNode) {
		preserveYAMLHeadComment(mapping, parentKeyNode.HeadComment)
		preserveYAMLLineComment(mapping, parentKeyNode.LineComment)
		preserveYAMLFootComment(mapping, parentKeyNode.FootComment)
		mapping.Style |= yaml.FlowStyle
		return nil
	}
	parentIndex, _, _, found := findYAMLMappingEntry(parentMapping, lastYAMLPathKey(path))
	if !found {
		return fmt.Errorf("unable to locate parent mapping entry %q", lastYAMLPathKey(path))
	}
	removeYAMLMappingEntryAt(parentMapping, parentIndex)
	return nil
}

func yamlDocumentRoot(document *yaml.Node) *yaml.Node {
	for _, node := range document.Content {
		if node.Kind == yaml.MappingNode {
			return node
		}
	}
	return nil
}

func lastYAMLPathKey(path []string) string {
	lastKey := ""
	for _, key := range path {
		lastKey = key
	}
	return lastKey
}

func encodeUpdatedFrontmatter(content, suffix string, document *yaml.Node) (string, error) {
	var output bytes.Buffer
	encoder := yaml.NewEncoder(&output)
	encoder.SetIndent(2)
	if err := encoder.Encode(document); err != nil {
		return "", fmt.Errorf("failed to encode updated frontmatter: %w", err)
	}
	if err := encoder.Close(); err != nil {
		return "", fmt.Errorf("failed to encode updated frontmatter: %w", err)
	}
	firstNewline := strings.IndexByte(content, '\n')
	if firstNewline < 0 {
		return "", errors.New("unable to locate frontmatter text in workflow content")
	}
	return content[:firstNewline+1] + output.String() + "---" + suffix, nil
}

func findYAMLMappingEntry(mapping *yaml.Node, key string) (int, *yaml.Node, *yaml.Node, bool) {
	if mapping == nil || mapping.Kind != yaml.MappingNode {
		return -1, nil, nil, false
	}
	entryIndex := 0
	var keyNode *yaml.Node
	for index, node := range mapping.Content {
		if keyNode == nil {
			keyNode = node
			entryIndex = index
			continue
		}
		if keyNode.Kind == yaml.ScalarNode && keyNode.Value == key {
			return entryIndex, keyNode, node, true
		}
		keyNode = nil
	}
	return -1, nil, nil, false
}

func removeYAMLMappingEntryAt(mapping *yaml.Node, keyIndex int) {
	if mapping == nil || keyIndex < 0 || keyIndex+1 >= len(mapping.Content) {
		return
	}
	remaining := make([]*yaml.Node, 0, len(mapping.Content)-2)
	for index, node := range mapping.Content {
		if index == keyIndex || index == keyIndex+1 {
			continue
		}
		remaining = append(remaining, node)
	}
	mapping.Content = remaining
}

func findYAMLMappingValue(mapping *yaml.Node, key string) (*yaml.Node, bool) {
	_, _, value, ok := findYAMLMappingEntry(mapping, key)
	return value, ok
}

func findYAMLMappingParent(root *yaml.Node, path []string) (*yaml.Node, *yaml.Node, bool) {
	if len(path) == 0 {
		return nil, nil, false
	}
	mapping := root
	for _, key := range path[:len(path)-1] {
		value, ok := findYAMLMappingValue(mapping, key)
		if !ok || value.Kind != yaml.MappingNode {
			return nil, nil, false
		}
		mapping = value
	}
	_, keyNode, _, ok := findYAMLMappingEntry(mapping, lastYAMLPathKey(path))
	return mapping, keyNode, ok
}

func hasYAMLComments(node *yaml.Node) bool {
	return node != nil && (node.HeadComment != "" || node.LineComment != "" || node.FootComment != "")
}

func collectYAMLComments(nodes ...*yaml.Node) []string {
	var comments []string
	var visit func(*yaml.Node)
	visit = func(node *yaml.Node) {
		if node == nil {
			return
		}
		for _, comment := range []string{node.HeadComment, node.FootComment} {
			if comment != "" {
				comments = append(comments, comment)
			}
		}
		for _, child := range node.Content {
			visit(child)
		}
	}
	for _, node := range nodes {
		visit(node)
	}
	return comments
}

func preserveYAMLHeadComment(node *yaml.Node, comment string) {
	if comment == "" {
		return
	}
	if node.HeadComment == "" {
		node.HeadComment = comment
		return
	}
	node.HeadComment = comment + "\n" + node.HeadComment
}

func preserveYAMLLineComment(node *yaml.Node, comment string) {
	if comment == "" {
		return
	}
	if node.LineComment == "" {
		node.LineComment = comment
		return
	}
	node.LineComment += " " + comment
}

func preserveYAMLFootComment(node *yaml.Node, comment string) {
	if comment == "" {
		return
	}
	if node.FootComment == "" {
		node.FootComment = comment
		return
	}
	node.FootComment += "\n" + comment
}
