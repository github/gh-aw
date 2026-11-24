// Package codemod provides source-preserving edits to the authored AW AST used
// by workflow linters. Transformations never operate on compiled defaults.
package codemod

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/github/gh-aw/pkg/workflow/actions"
	"github.com/github/gh-aw/pkg/workflow/aw"
	"github.com/goccy/go-yaml"
	yamlv3 "go.yaml.in/yaml/v3"
)

// Frontmatter keeps the authored document and its source in sync after each edit.
// Read configuration through Document; mutate it through Edit or the path helpers.
type Frontmatter struct {
	Document *aw.Document
	content  string
}

// Apply runs an atomic transformation. Errors return the original content, even
// if earlier operations in the callback succeeded. No-op input is byte-identical.
func Apply(content string, transform func(*Frontmatter) error) (string, bool, error) {
	document, err := aw.Parse(content)
	if err != nil {
		return content, false, err
	}
	for _, line := range document.Location.FrontmatterLines {
		if !utf8.ValidString(line) {
			return content, false, errors.New("workflow frontmatter must be valid UTF-8")
		}
	}
	editor := &Frontmatter{Document: document, content: content}
	if err := transform(editor); err != nil {
		return content, false, err
	}
	return editor.content, editor.content != content, nil
}

// Lookup resolves an exact mapping path, distinguishing absent fields from null.
func (f *Frontmatter) Lookup(path ...string) (any, bool) {
	var value any = f.Document.Frontmatter
	for _, key := range path {
		mapping, ok := value.(map[string]any)
		if !ok {
			return nil, false
		}
		value, ok = mapping[key]
		if !ok {
			return nil, false
		}
	}
	return value, true
}

// Edit applies arbitrary semantic edits to an independent AW document, retaining
// unchanged YAML nodes through the Actions source merger and preserving the body.
func (f *Frontmatter) Edit(transform func(*aw.Document) error) error {
	document, err := aw.Parse(f.content)
	if err != nil {
		return err
	}
	// YAML aliases may share Go maps. Codemods edit authored fields, not live
	// alias bindings, so give the callback independent values at every path.
	document.Frontmatter = document.FrontmatterResult().Frontmatter
	if err := transform(document); err != nil {
		return err
	}
	if document.Markdown != f.Document.Markdown {
		return errors.New("frontmatter codemods cannot change Markdown")
	}
	equal, err := equalValues(document.Frontmatter, f.Document.Frontmatter)
	if err != nil {
		return err
	}
	if equal {
		return nil
	}
	if f.Document.Location.FrontmatterStart == 0 {
		updated, err := aw.Emit(document)
		if err != nil {
			return err
		}
		return f.replace(0, len(f.content), updated)
	}
	start, end, err := f.bounds()
	if err != nil {
		return err
	}
	if len(f.Document.Frontmatter) == 0 {
		encoded, err := yaml.MarshalWithOptions(document.Frontmatter, yaml.IndentSequence(true))
		if err != nil {
			return err
		}
		text := strings.ReplaceAll(string(encoded), "\n", f.newline())
		return f.replace(start, end, f.content[start:end]+text)
	}
	updated, err := actions.EditSource(f.content[start:end], f.Document.Frontmatter, document.Frontmatter)
	if err != nil {
		return err
	}
	return f.replace(start, end, updated)
}

// Rename changes only the key token, including in flow mappings and quoted keys.
// An existing destination is authoritative, including an explicit null value.
func (f *Frontmatter) Rename(path []string, newKey string) error {
	if len(path) == 0 || newKey == "" {
		return errors.New("frontmatter rename requires a path and destination key")
	}
	if _, found := f.Lookup(path...); !found {
		return nil
	}
	destination := append(slices.Clone(path[:len(path)-1]), newKey)
	if _, found := f.Lookup(destination...); found {
		return nil
	}
	root, err := f.syntax()
	if err != nil {
		return err
	}
	if hasAliases(root) {
		return f.Edit(func(document *aw.Document) error {
			parent := document.Frontmatter
			for _, key := range path[:len(path)-1] {
				next, ok := parent[key].(map[string]any)
				if !ok {
					return errors.New("frontmatter rename path is not a mapping")
				}
				parent = next
			}
			parent[newKey] = parent[lastPathKey(path)]
			delete(parent, lastPathKey(path))
			return nil
		})
	}
	key, _, err := f.entry(path)
	if err != nil {
		return err
	}
	start, end, found := f.scalarSpan(key)
	if !found {
		return errors.New("cannot locate frontmatter key token")
	}
	var replacement string
	if key.Style == yamlv3.SingleQuotedStyle || key.Style == yamlv3.DoubleQuotedStyle {
		if key.Style == yamlv3.SingleQuotedStyle {
			replacement = "'" + strings.ReplaceAll(newKey, "'", "''") + "'"
		} else {
			encoded, err := yaml.MarshalWithOptions(newKey, yaml.JSON())
			if err != nil {
				return err
			}
			replacement = strings.TrimSpace(string(encoded))
		}
	} else {
		encoded, err := yaml.Marshal(newKey)
		if err != nil {
			return err
		}
		replacement = strings.TrimSpace(string(encoded))
	}
	return f.replace(start, end, replacement)
}

// Set sets a mapping path without overwriting non-mapping ancestors.
func (f *Frontmatter) Set(path []string, value any) error {
	if len(path) == 0 {
		return errors.New("frontmatter set requires a path")
	}
	if handled, err := f.setSource(path, value); handled || err != nil {
		return err
	}
	return f.Edit(func(document *aw.Document) error {
		mapping := document.Frontmatter
		for _, key := range path[:len(path)-1] {
			child, exists := mapping[key]
			if !exists {
				child = map[string]any{}
				mapping[key] = child
			}
			next, ok := child.(map[string]any)
			if !ok {
				return fmt.Errorf("frontmatter path %q is not a mapping", key)
			}
			mapping = next
		}
		mapping[lastPathKey(path)] = value
		return nil
	})
}

// Remove deletes exactly one field. Empty parents are retained by default.
func (f *Frontmatter) Remove(path ...string) error {
	if len(path) == 0 {
		return errors.New("frontmatter removal requires a path")
	}

	if _, found := f.Lookup(path...); !found {
		return nil
	}
	if handled, err := f.removeSource(path); handled || err != nil {
		return err
	}
	return f.Edit(func(document *aw.Document) error {
		mapping := document.Frontmatter
		for _, key := range path[:len(path)-1] {
			child, ok := mapping[key].(map[string]any)
			if !ok {
				return fmt.Errorf("frontmatter path %q is not a mapping", key)
			}
			mapping = child
		}
		delete(mapping, lastPathKey(path))
		return nil
	})
}

// PruneEmpty removes an empty mapping only when it has no authored comments.
func (f *Frontmatter) PruneEmpty(path ...string) error {
	if len(path) == 0 {
		return errors.New("frontmatter pruning requires a path")
	}
	value, found := f.Lookup(path...)
	mapping, ok := value.(map[string]any)
	if !found || !ok || len(mapping) != 0 {
		return nil
	}
	key, node, err := f.entry(path)
	if err != nil {
		return err
	}
	if hasComments(key) || hasComments(node) {
		return nil
	}
	return f.Remove(path...)
}

func hasComments(node *yamlv3.Node) bool {
	if node == nil {
		return false
	}
	if node.HeadComment != "" || node.LineComment != "" || node.FootComment != "" {
		return true
	}
	return slices.ContainsFunc(node.Content, hasComments)
}

func (f *Frontmatter) bounds() (int, int, error) {
	if f.Document.Location.FrontmatterStart == 0 {
		return 0, 0, errors.New("workflow has no frontmatter")
	}
	start := strings.IndexByte(f.content, '\n') + 1
	end := start
	for range len(f.Document.Location.FrontmatterLines) {
		next := strings.IndexByte(f.content[end:], '\n')
		if next < 0 {
			return 0, 0, errors.New("cannot locate closing frontmatter delimiter")
		}
		end += next + 1
	}
	return start, end, nil
}

func (f *Frontmatter) entry(path []string) (*yamlv3.Node, *yamlv3.Node, error) {
	node, err := f.syntax()
	if err != nil {
		return nil, nil, err
	}
	var keyNode *yamlv3.Node
	for _, key := range path {
		if node.Kind != yamlv3.MappingNode {
			return nil, nil, errors.New("frontmatter source path is not a mapping")
		}
		keyNode, node = mappingEntry(node, key)
		if node == nil {
			return nil, nil, errors.New("cannot locate frontmatter source path")
		}
	}
	return keyNode, node, nil
}

func (f *Frontmatter) offset(line, column int) (int, error) {
	start := strings.IndexByte(f.content, '\n') + 1
	for range line - 1 {
		next := strings.IndexByte(f.content[start:], '\n')
		if next < 0 {
			return 0, errors.New("frontmatter source line is out of bounds")
		}
		start += next + 1
	}
	for range column - 1 {
		if start >= len(f.content) {
			return 0, errors.New("frontmatter source column is out of bounds")
		}
		_, size := utf8.DecodeRuneInString(f.content[start:])
		start += size
	}
	return start, nil
}

// RewriteText supports lexical migrations that intentionally include YAML
// comments. AW source boundaries keep the Markdown body outside the transform.
func (f *Frontmatter) RewriteText(transform func(string) string) error {
	if f.Document.Location.FrontmatterStart == 0 {
		return nil
	}
	start, end, err := f.bounds()
	if err != nil {
		return err
	}
	text := transform(f.content[start:end])
	if text == f.content[start:end] {
		return nil
	}
	return f.replace(start, end, text)
}

func (f *Frontmatter) replace(start, end int, text string) error {
	content := f.content[:start] + text + f.content[end:]
	document, err := aw.Parse(content)
	if err != nil {
		return fmt.Errorf("validating frontmatter edit: %w", err)
	}
	f.content, f.Document = content, document
	return nil
}

// Comment appends a single-line annotation to a mapping field without replacing
// existing comments or changing the document's configuration.
func (f *Frontmatter) Comment(path []string, text string) error {
	if len(path) == 0 || strings.ContainsAny(text, "\r\n") {
		return errors.New("frontmatter comment requires a path and single-line text")
	}
	key, _, err := f.entry(path)
	if err != nil {
		return err
	}
	start, err := f.offset(key.Line, 1)
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
