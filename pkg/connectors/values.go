package connectors

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"reflect"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"
)

func decodeJSON(data []byte, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return errors.New("expected exactly one JSON document")
	}
	return nil
}

func validateArguments(inputs map[string]Input, resources map[string]string, allowed map[string][]string, args map[string]any) error {
	for key := range args {
		if _, ok := inputs[key]; !ok {
			return fmt.Errorf("unknown connector argument %q", key)
		}
	}
	for key, input := range inputs {
		value, ok := args[key]
		if !ok {
			if input.Required {
				return fmt.Errorf("required connector argument %q is missing", key)
			}
			continue
		}
		if err := validateArgument(input, value); err != nil {
			return fmt.Errorf("connector argument %q: %w", key, err)
		}
		if resource, ok := resources[key]; ok {
			text, isString := value.(string)
			if !isString || !slices.Contains(allowed[resource], text) {
				return fmt.Errorf("connector argument %q is outside approved resources", key)
			}
		}
	}
	return nil
}

func validateArgument(input Input, value any) error {
	switch input.Type {
	case "string":
		text, ok := value.(string)
		if !ok || !utf8.ValidString(text) || utf8.RuneCountInString(text) > input.MaxLength {
			return errors.New("expected a bounded UTF-8 string")
		}
		if len(input.Enum) != 0 && !slices.Contains(input.Enum, text) {
			return errors.New("value is outside the declared enum")
		}
		if input.Pattern != "" {
			//nolint:regexpdynamicpattern // Pattern has already passed the bounded RE2 schema validation.
			pattern, err := regexp.Compile(input.Pattern)
			if err != nil || !pattern.MatchString(text) {
				return errors.New("value does not match the declared pattern")
			}
		}
	case "boolean":
		if _, ok := value.(bool); !ok {
			return errors.New("expected a boolean")
		}
	case "integer", "number":
		number, ok := numericValue(value)
		if !ok || math.IsNaN(number) || math.IsInf(number, 0) || (input.Type == "integer" && math.Trunc(number) != number) {
			return errors.New("expected a finite number of the declared type")
		}
		if (input.Minimum != nil && number < *input.Minimum) || (input.Maximum != nil && number > *input.Maximum) {
			return errors.New("number is outside declared bounds")
		}
	default:
		return errors.New("unsupported input type")
	}
	return nil
}

func numericValue(value any) (float64, bool) {
	if number, ok := value.(json.Number); ok {
		result, err := number.Float64()
		return result, err == nil
	}
	if value == nil {
		return 0, false
	}
	v := reflect.ValueOf(value)
	switch v.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return float64(v.Int()), true
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return float64(v.Uint()), true
	case reflect.Float32, reflect.Float64:
		return v.Float(), true
	}
	return 0, false
}

func lookupPointer(document any, pointer string) (any, bool) {
	if !validPointer(pointer) {
		return nil, false
	}
	current := document
	for token := range strings.SplitSeq(strings.TrimPrefix(pointer, "/"), "/") {
		token = strings.ReplaceAll(strings.ReplaceAll(token, "~1", "/"), "~0", "~")
		switch object := current.(type) {
		case map[string]any:
			var ok bool
			current, ok = object[token]
			if !ok {
				return nil, false
			}
		case []any:
			index, err := strconv.Atoi(token)
			if err != nil || strconv.Itoa(index) != token {
				return nil, false
			}
			if index < 0 || index >= len(object) {
				return nil, false
			}
			current = object[index] //nolint:uncheckedsliceindex // The preceding guard explicitly checks both nonnegative and upper bounds.
		default:
			return nil, false
		}
	}
	return current, true
}

func projectFields(document any, projection map[string]string) (map[string]any, error) {
	fields := make(map[string]any, len(projection))
	for field, pointer := range projection {
		value, ok := lookupPointer(document, pointer)
		if !ok || value == nil {
			return nil, fmt.Errorf("required projection evidence %q is missing", pointer)
		}
		fields[field] = value
	}
	return fields, nil
}
