package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"regexp"
	"sort"
	"strings"
	"unicode/utf16"
	"unicode/utf8"

	"github.com/goccy/go-yaml"
	"github.com/goccy/go-yaml/ast"
	"github.com/goccy/go-yaml/lexer"
	"github.com/goccy/go-yaml/parser"
	"github.com/goccy/go-yaml/token"
)

const maxInputBytes = 4 << 20

type request struct {
	Operation       string            `json:"operation"`
	Content         string            `json:"content"`
	Key             string            `json:"key"`
	Value           string            `json:"value"`
	Replacements    map[string]string `json:"replacements"`
	AllowDuplicates bool              `json:"allowDuplicates"`
}

type scalar struct {
	Value string `json:"value"`
	Start int    `json:"start"`
	End   int    `json:"end"`
}

type response struct {
	Value   any               `json:"value"`
	Scalars map[string]scalar `json:"scalars,omitempty"`
	Content string            `json:"content,omitempty"`
	Error   string            `json:"error,omitempty"`
}

var frontmatterPattern = regexp.MustCompile(`\A---[ \t]*\r?\n(?:([\s\S]*?)\r?\n)?---[ \t]*\r?(?:\n|$)`)
var plainModelPattern = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9._/?=&+-]*$`)

func main() {
	input, err := io.ReadAll(io.LimitReader(os.Stdin, maxInputBytes+1))
	if err != nil || len(input) > maxInputBytes {
		fmt.Fprintln(os.Stderr, "Markdown metadata request exceeds the bounded input limit")
		os.Exit(1)
	}
	var req request
	if err := json.Unmarshal(input, &req); err != nil {
		fmt.Fprintln(os.Stderr, "Invalid Markdown metadata JSON request")
		os.Exit(1)
	}
	result, err := process(req)
	if err != nil {
		result = response{Error: err.Error()}
	}
	if err := json.NewEncoder(os.Stdout).Encode(result); err != nil {
		fmt.Fprintln(os.Stderr, "Cannot encode Markdown metadata response")
		os.Exit(1)
	}
}

func parse(content string, allowDuplicates bool) (*ast.File, any, error) {
	options := []parser.Option{}
	if allowDuplicates {
		options = append(options, parser.AllowDuplicateMapKey())
	}
	normalized := strings.ReplaceAll(content, "\r\n", "\n")
	if !strings.HasSuffix(normalized, "\n") {
		normalized += "\n"
	}
	file, err := parser.ParseBytes([]byte(normalized), parser.ParseComments, options...)
	if err != nil {
		return nil, nil, errors.New("invalid or ambiguous document metadata")
	}
	if len(file.Docs) > 1 {
		return nil, nil, errors.New("multiple metadata documents are not allowed")
	}
	var value any
	if len(file.Docs) == 1 && file.Docs[0].Body != nil {
		nodes := 0
		if err := validateNodes(file.Docs[0].Body, 0, &nodes); err != nil {
			return nil, nil, err
		}
		if err := yaml.NodeToValue(file.Docs[0].Body, &value); err != nil {
			return nil, nil, errors.New("invalid or unbounded document metadata")
		}
	}
	return file, value, nil
}

func process(req request) (response, error) {
	if req.Operation != "document" && req.Operation != "agent" && req.Operation != "ensure-name" && req.Operation != "restore" {
		return response{}, errors.New("unsupported Markdown metadata operation")
	}
	if req.Operation == "document" {
		_, value, err := parse(req.Content, req.AllowDuplicates)
		return response{Value: value}, err
	}
	bounds := frontmatterPattern.FindStringSubmatchIndex(req.Content)
	if bounds == nil {
		if req.Operation == "ensure-name" {
			return response{Content: "---\nname: " + quote(req.Value) + "\n---\n" + req.Content}, nil
		}
		return response{Value: map[string]any{}, Content: req.Content}, nil
	}
	start := strings.Index(req.Content, "\n") + 1
	end := start
	if bounds[2] >= 0 {
		start, end = bounds[2], bounds[3]
	}
	text := req.Content[start:end]
	for key := range req.Replacements {
		text = strings.ReplaceAll(text, key, strings.ReplaceAll(key, "\x00", "_"))
	}
	file, value, err := parse(text, false)
	if err != nil {
		return response{}, err
	}
	var root ast.Node
	if len(file.Docs) > 0 {
		root = file.Docs[0].Body
	}
	result := response{Value: value, Content: req.Content}
	switch req.Operation {
	case "agent":
		if value == nil {
			result.Value = map[string]any{}
			return result, nil
		}
		if _, ok := value.(map[string]any); !ok {
			return response{}, errors.New("agent frontmatter must be a mapping")
		}
		result.Scalars = map[string]scalar{}
		if mapping, ok := root.(*ast.MappingNode); ok {
			for _, pair := range mapping.Values {
				key, ok := pair.Key.(*ast.StringNode)
				if !ok {
					continue
				}
				item, found := stringScalar(pair.Value)
				if !found {
					continue
				}
				left, right, found := scalarSpan(text, pair.Value)
				if found {
					result.Scalars[key.Value] = scalar{item, utf16Length(req.Content[:start+left]), utf16Length(req.Content[:start+right])}
				}
			}
		}
	case "ensure-name":
		if root == nil {
			result.Content = req.Content[:start] + "name: " + quote(req.Value) + "\n" + req.Content[start:]
			return result, nil
		}
		mapping, ok := root.(*ast.MappingNode)
		if !ok {
			return result, nil
		}
		for _, pair := range mapping.Values {
			if key, ok := pair.Key.(*ast.StringNode); ok && key.Value == "name" {
				return result, nil
			}
		}
		position := offset(text, mapping.GetToken().Position) + start
		if !mapping.IsFlowStyle && len(mapping.Values) > 0 {
			position = offset(text, mapping.Values[0].Key.GetToken().Position) + start
		}
		entry := "name: " + quote(req.Value)
		if mapping.IsFlowStyle {
			position++
			if len(mapping.Values) > 0 {
				entry += ", "
			}
		} else {
			indent := req.Content[strings.LastIndex(req.Content[:position], "\n")+1 : position]
			entry += "\n" + indent
		}
		result.Content = req.Content[:position] + entry + req.Content[position:]
	case "restore":
		edits, err := restorationEdits(root, text, req.Replacements)
		if err != nil {
			return response{}, err
		}
		sort.Slice(edits, func(i, j int) bool { return edits[i].start > edits[j].start })
		for _, edit := range edits {
			text = text[:edit.start] + edit.text + text[edit.end:]
		}
		result.Content = req.Content[:start] + text + req.Content[end:]
	default:
		return response{}, errors.New("unsupported Markdown metadata operation")
	}
	return result, nil
}

func quote(value string) string {
	encoded, _ := json.Marshal(value)
	return string(encoded)
}

func utf16Length(value string) int {
	return len(utf16.Encode([]rune(value)))
}

func offset(source string, position *token.Position) int {
	lineStart := 0
	for line := 1; line < position.Line; line++ {
		index := strings.IndexByte(source[lineStart:], '\n')
		if index < 0 {
			return len(source)
		}
		lineStart += index + 1
	}
	end := lineStart
	for column := 1; column < position.Column && end < len(source); column++ {
		_, size := utf8.DecodeRuneInString(source[end:])
		end += size
	}
	return end
}

func stringScalar(node ast.Node) (string, bool) {
	switch value := node.(type) {
	case *ast.StringNode:
		return value.Value, true
	case *ast.LiteralNode:
		return value.Value.Value, true
	}

	return "", false
}

func validateNodes(node ast.Node, depth int, count *int) error {
	*count++
	if depth > 100 || *count > 10000 {
		return errors.New("document metadata exceeds structural limits")
	}
	switch value := node.(type) {
	case *ast.AnchorNode, *ast.AliasNode, *ast.TagNode:
		return errors.New("document metadata cannot contain anchors, aliases, or tags")
	case *ast.MappingNode:
		for _, pair := range value.Values {
			if err := validateNodes(pair.Key, depth+1, count); err != nil {
				return err
			}
			if err := validateNodes(pair.Value, depth+1, count); err != nil {
				return err
			}
		}
	case *ast.SequenceNode:
		for _, item := range value.Values {
			if err := validateNodes(item, depth+1, count); err != nil {
				return err
			}
		}
	}
	return nil
}

func scalarSpan(source string, node ast.Node) (int, int, bool) {
	position := node.GetToken().Position
	left := offset(source, position)
	active := false
	block := false
	if _, ok := node.(*ast.LiteralNode); ok {
		block = true
	}
	for _, item := range lexer.Tokenize(strings.ReplaceAll(source, "\r\n", "\n")) {
		if active {
			if block && item.Position.Line == position.Line {
				continue
			}
			if block && item.Type == token.StringType {
				block = false
				continue
			}
			right := offset(source, item.Position)
			part := strings.TrimRight(source[left:right], " \t\r\n")
			return left, left + len(part), len(part) > 0
		}
		active = item.Position.Line == position.Line && item.Position.Column == position.Column
	}
	part := strings.TrimRight(source[left:], " \t\r\n")
	return left, left + len(part), len(part) > 0
}

type edit struct {
	start, end int
	text       string
}

func restorationEdits(root ast.Node, source string, replacements map[string]string) ([]edit, error) {
	var edits []edit
	restore := func(value string) string {
		for key, replacement := range replacements {
			value = strings.ReplaceAll(value, strings.ReplaceAll(key, "\x00", "_"), replacement)
		}
		return value
	}
	var visit func(ast.Node) error
	visit = func(node ast.Node) error {
		if node == nil {
			return nil
		}
		switch value := node.(type) {
		case *ast.MappingNode:
			for _, pair := range value.Values {
				if key, ok := stringScalar(pair.Key); ok && restore(key) != key {
					return errors.New("Inline metadata keys must be author-owned")
				}
				if err := visit(pair.Value); err != nil {
					return err
				}
			}
		case *ast.SequenceNode:
			for _, item := range value.Values {
				if err := visit(item); err != nil {
					return err
				}
			}
		default:
			original, ok := stringScalar(node)
			if !ok || restore(original) == original {
				return nil
			}
			left, right, found := scalarSpan(source, node)
			if !found {
				return errors.New("cannot locate literal metadata scalar")
			}
			restored := restore(original)
			rendered := quote(restored)
			if node.GetToken().Type == token.StringType && plainModelPattern.MatchString(restored) && restored != "true" && restored != "false" && restored != "null" {
				rendered = restored
			}
			edits = append(edits, edit{left, right, rendered})
		}
		return nil
	}
	err := visit(root)
	return edits, err
}
