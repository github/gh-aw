// Package apm implements reusable OpenAPM manifest and lockfile document APIs.
//
// It validates embedded draft schemas and document semantics, preserves original
// bytes, and exposes copied dependency and target views. It has no gh-aw compiler,
// filesystem, network, process, or Microsoft APM dependencies. Source resolution,
// policy execution and runtime deployment are deliberately separate concerns.
package apm

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"strconv"
	"strings"

	"go.yaml.in/yaml/v3"
)

// MaxDocumentBytes bounds documents before YAML parsing.
const MaxDocumentBytes = 1 << 20

type document struct {
	raw    []byte
	fields map[string]any
}

func parseDocument(content []byte, filename string) (*document, error) {
	if len(content) > MaxDocumentBytes {
		return nil, fmt.Errorf("%s exceeds the %d-byte document limit", filename, MaxDocumentBytes)
	}
	decoder := yaml.NewDecoder(bytes.NewReader(content))
	var root yaml.Node
	if err := decoder.Decode(&root); err != nil {
		return nil, fmt.Errorf("%s: invalid YAML: %w", filename, err)
	}
	var extra yaml.Node
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("%s must contain a single YAML document", filename)
	}
	if len(root.Content) != 1 || root.Content[0].Kind != yaml.MappingNode { //nolint:uncheckedsliceindex // Short-circuiting proves the single root exists.
		return nil, fmt.Errorf("%s must contain a top-level mapping", filename)
	}
	mapping := root.Content[0] //nolint:uncheckedsliceindex // The root count and kind were validated above.
	strictScalars := false
	for i, key := range mapping.Content {
		if i%2 == 0 && key.Value == "$schema" {
			strictScalars = true
		}
	}
	value, err := safeValue(mapping, 0, strictScalars)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", filename, err)
	}
	fields, ok := value.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("%s must contain a top-level mapping", filename)
	}
	return &document{raw: bytes.Clone(content), fields: fields}, nil
}

// OpenAPM req-mf-020 uses string scalars unless a canonical type is explicit.
// Walking nodes also rejects aliases, anchors, custom tags and duplicate keys
// before they can be lost by decoding into a Go map.
func safeValue(node *yaml.Node, depth int, strictScalars bool) (any, error) {
	if depth > 64 {
		return nil, errors.New("YAML nesting exceeds 64 levels")
	}
	if node.Anchor != "" || node.Kind == yaml.AliasNode {
		return nil, fmt.Errorf("line %d: YAML anchors and aliases are not supported", node.Line)
	}
	if node.Style&yaml.TaggedStyle != 0 {
		switch node.ShortTag() {
		case "!!str", "!!int", "!!float", "!!bool", "!!null", "!!map", "!!seq":
		default:
			return nil, fmt.Errorf("line %d: custom YAML tags are not supported", node.Line)
		}
	}
	switch node.Kind {
	case yaml.MappingNode:
		fields := make(map[string]any, len(node.Content)/2)
		if len(node.Content)%2 != 0 {
			return nil, fmt.Errorf("line %d: mapping must contain key/value pairs", node.Line)
		}
		for i, keyNode := range node.Content {
			if i%2 != 0 {
				continue
			}
			key, err := safeValue(keyNode, depth+1, true)
			if err != nil {
				return nil, err
			}
			name, ok := key.(string)
			if !ok || name == "<<" {
				return nil, fmt.Errorf("line %d: mapping keys must be strings, without YAML merge keys", keyNode.Line)
			}
			if _, exists := fields[name]; exists {
				return nil, fmt.Errorf("line %d: duplicate key %q", keyNode.Line, name)
			}
			value, err := safeValue(node.Content[i+1], depth+1, strictScalars) //nolint:uncheckedsliceindex // An even-length mapping and even i prove a value node exists.
			if err != nil {
				return nil, err
			}
			fields[name] = value
		}
		return fields, nil
	case yaml.SequenceNode:
		values := make([]any, 0, len(node.Content))
		for _, child := range node.Content {
			value, err := safeValue(child, depth+1, strictScalars)
			if err != nil {
				return nil, err
			}
			values = append(values, value)
		}
		return values, nil
	case yaml.ScalarNode:
		return safeScalar(node, strictScalars)
	default:
		return nil, fmt.Errorf("line %d: unsupported YAML node", node.Line)
	}
}

func safeScalar(node *yaml.Node, strictScalars bool) (any, error) {
	if (strictScalars && node.Style&yaml.TaggedStyle == 0) || node.ShortTag() == "!!str" || node.ShortTag() == "!!timestamp" {
		return node.Value, nil
	}
	var value any
	var err error
	switch node.ShortTag() {
	case "!!int":
		// Base 10 deliberately prevents YAML 1.1's implicit octal conversion.
		number := strings.ReplaceAll(node.Value, "_", "")
		unsigned := strings.TrimLeft(number, "+-")
		base := 10
		if strings.HasPrefix(unsigned, "0x") || strings.HasPrefix(unsigned, "0o") || strings.HasPrefix(unsigned, "0b") {
			base = 0
		}
		value, err = strconv.ParseInt(number, base, 64)
	case "!!float":
		var number float64
		number, err = strconv.ParseFloat(strings.ReplaceAll(node.Value, "_", ""), 64)
		if err == nil && (math.IsNaN(number) || math.IsInf(number, 0)) {
			err = errors.New("expected a finite JSON-compatible number")
		}
		value = number
	case "!!bool":
		if node.Value != "true" && node.Value != "false" {
			err = errors.New("expected true or false")
		} else {
			value = node.Value == "true"
		}
	case "!!null":
		if node.Value != "null" && node.Value != "~" && node.Value != "" {
			err = errors.New("expected null")
		}
	default:
		err = fmt.Errorf("unsupported canonical tag %q", node.Tag)
	}
	if err != nil {
		return nil, fmt.Errorf("line %d: invalid explicitly typed scalar: %w", node.Line, err)
	}
	return value, nil
}

func decodeFields[T any](fields map[string]any) (T, error) {
	var result T
	content, err := json.Marshal(fields)
	if err != nil {
		return result, err
	}
	err = json.Unmarshal(content, &result)
	return result, err
}

func cloneField(value any) any {
	switch value := value.(type) {
	case map[string]any:
		return cloneMap(value)
	case []any:
		cloned := make([]any, 0, len(value))
		for _, child := range value {
			cloned = append(cloned, cloneField(child))
		}
		return cloned
	default:
		return value
	}
}

func cloneMap(value map[string]any) map[string]any {
	if value == nil {
		return nil
	}
	cloned := make(map[string]any, len(value))
	for key, child := range value {
		cloned[key] = cloneField(child)
	}
	return cloned
}
