package importinpututil

import (
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"strings"

	"github.com/github/gh-aw/pkg/logger"
)

var importInputLog = logger.New("importinpututil:import_input")

// ResolvePathValue resolves either a top-level input key ("count") or a one-level
// dotted object sub-key ("config.apiKey") from import inputs.
func ResolvePathValue(inputs map[string]any, inputPath string) (any, bool) {
	top, sub, hasDot := strings.Cut(inputPath, ".")
	if !hasDot {
		value, ok := inputs[top]
		importInputLog.Printf("ResolvePathValue: top-level key %q found=%t", top, ok)
		return value, ok
	}
	topVal, topOK := inputs[top]
	if !topOK {
		importInputLog.Printf("ResolvePathValue: parent key %q not found for path %q", top, inputPath)
		return nil, false
	}
	obj, isMap := topVal.(map[string]any)
	if !isMap {
		importInputLog.Printf("ResolvePathValue: parent key %q is not an object for path %q", top, inputPath)
		return nil, false
	}
	value, ok := obj[sub]
	importInputLog.Printf("ResolvePathValue: sub-key %q under %q found=%t", sub, top, ok)
	return value, ok
}

// FormatResolvedValue formats a resolved import input value for textual
// substitution. []any/map[string]any and typed slices/maps are normalized and
// JSON-marshaled, nil returns ("", false), and scalars use fmt.Sprintf("%v", v).
func FormatResolvedValue(value any) (string, bool) {
	switch v := value.(type) {
	case []any:
		return marshalValue(v)
	case map[string]any:
		return marshalValue(v)
	case nil:
		importInputLog.Print("FormatResolvedValue: nil value, no substitution")
		return "", false
	default:
		return formatReflectiveValue(v)
	}
}

func formatReflectiveValue(value any) (string, bool) {
	rv := reflect.ValueOf(value)
	switch rv.Kind() {
	case reflect.Slice:
		return marshalValue(normalizeSlice(rv))
	case reflect.Map:
		return marshalValue(normalizeMap(rv))
	default:
		return fmt.Sprintf("%v", value), true
	}
}

func marshalValue(value any) (string, bool) {
	b, err := json.Marshal(value)
	if err != nil {
		return "", false
	}
	return string(b), true
}

func normalizeSlice(rv reflect.Value) []any {
	normalized := make([]any, 0, rv.Len())
	for i := range rv.Len() {
		normalized = append(normalized, rv.Index(i).Interface())
	}
	return normalized
}

func normalizeMap(rv reflect.Value) map[string]any {
	keys := make([]string, 0, rv.Len())
	for _, key := range rv.MapKeys() {
		keys = append(keys, key.String())
	}
	sort.Strings(keys)
	normalized := make(map[string]any, rv.Len())
	for _, k := range keys {
		normalized[k] = rv.MapIndex(reflect.ValueOf(k)).Interface()
	}
	return normalized
}
