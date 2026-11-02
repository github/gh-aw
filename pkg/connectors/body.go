package connectors

import (
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"strings"
)

func validateValueDepth(value Value, inputs map[string]Input, depth int) error {
	if depth > 16 {
		return errors.New("structured connector values exceed the maximum depth of 16")
	}
	count := 0
	for _, present := range []bool{value.Input != "", value.Literal != nil, value.Object != nil, value.Array != nil} {
		if present {
			count++
		}
	}
	if count != 1 {
		return errors.New("value requires exactly one input, non-null literal, object or array")
	}
	if value.Input != "" {
		field, ok := inputs[value.Input]
		if !ok {
			return fmt.Errorf("value references unknown input %q", value.Input)
		}
		if value.Encoding != "" && (field.Type != "string" || (value.Encoding != "jira-adf" && value.Encoding != "confluence-storage")) {
			return errors.New("encoding requires a string input and jira-adf or confluence-storage")
		}
	} else if value.Encoding != "" {
		return errors.New("encoding is supported only on input references")
	}
	for _, child := range value.Object {
		if err := validateValueDepth(child, inputs, depth+1); err != nil {
			return err
		}
	}
	for _, child := range value.Array {
		if err := validateValueDepth(child, inputs, depth+1); err != nil {
			return err
		}
	}
	return nil
}

func primitiveLiteral(value any) bool {
	switch value.(type) {
	case nil, string, bool, json.Number, float64, int:
		return true
	}
	return false
}

func resolveValue(value Value, args map[string]any) (any, bool, error) {
	var result any
	switch {
	case value.Input != "":
		input, ok := args[value.Input]
		if !ok {
			return nil, false, nil
		}
		result = input
		if value.Encoding != "" {
			text, isString := input.(string)
			if !isString {
				return nil, false, errors.New("encoded input must be a string")
			}
			encoded, err := encodeText(value.Encoding, text)
			if err != nil {
				return nil, false, err
			}
			result = encoded
		}
	case value.Object != nil:
		object, err := resolveValues(value.Object, args)
		if err != nil {
			return nil, false, err
		}
		result = object
	case value.Array != nil:
		array, err := resolveArray(value.Array, args)
		if err != nil {
			return nil, false, err
		}
		result = array
	default:
		result = value.Literal
	}
	encoded, err := json.Marshal(result)
	if err != nil || len(encoded) > MaxDocumentBytes {
		return nil, false, errors.New("resolved connector value exceeds the JSON byte bound")
	}
	return result, true, nil
}

func resolveArray(values []Value, args map[string]any) ([]any, error) {
	result := make([]any, 0, len(values))
	size := 2
	for _, value := range values {
		resolved, present, err := resolveValue(value, args)
		if err != nil {
			return nil, err
		}
		if !present {
			return nil, errors.New("structured array input is missing")
		}
		encoded, err := json.Marshal(resolved)
		if err != nil {
			return nil, fmt.Errorf("encode structured array value: %w", err)
		}
		size += len(encoded) + 1
		if size > MaxDocumentBytes {
			return nil, errors.New("structured array exceeds the JSON byte bound")
		}
		result = append(result, resolved)
	}
	return result, nil
}

func encodeText(encoding, text string) (any, error) {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	if encoding == "confluence-storage" {
		return "<p>" + strings.ReplaceAll(html.EscapeString(text), "\n", "<br />") + "</p>", nil
	}
	if encoding != "jira-adf" {
		return nil, errors.New("unsupported connector text encoding")
	}
	content := []any{}
	for index, line := range strings.Split(text, "\n") {
		if index != 0 {
			content = append(content, map[string]any{"type": "hardBreak"})
		}
		if line != "" {
			content = append(content, map[string]any{"type": "text", "text": line})
		}
	}
	return map[string]any{"version": 1, "type": "doc", "content": []any{map[string]any{"type": "paragraph", "content": content}}}, nil
}
