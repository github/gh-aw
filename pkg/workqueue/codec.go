package workqueue

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"math"
	"reflect"
	"slices"
	"strconv"
	"unicode/utf8"
)

// maxSafeInteger is the largest integer the canonical profile admits (2^53-1).
const maxSafeInteger int64 = 9007199254740991

// ProtocolError preserves a stable rejection code without requiring message matching.
type ProtocolError struct {
	Code    string
	Message string
}

func (e *ProtocolError) Error() string {
	return e.Code + ": " + e.Message
}

func queueError(code, message string, args ...any) error {
	return &ProtocolError{Code: code, Message: fmt.Sprintf(message, args...)}
}

// ErrorCode returns the protocol code of err, or "" when err is not a ProtocolError.
func ErrorCode(err error) string {
	var protocol *ProtocolError
	if errors.As(err, &protocol) {
		return protocol.Code
	}
	return ""
}

func readValue(decoder *json.Decoder, depth int) (any, error) {
	if depth > 64 {
		return nil, queueError("resource_limit", "JSON nesting exceeds 64")
	}
	token, err := decoder.Token()
	if err != nil {
		return nil, err
	}
	switch value := token.(type) {
	case json.Delim:
		switch value {
		case '{':
			return readObject(decoder, depth)
		case '[':
			result := []any{}
			for decoder.More() {
				if len(result) >= 16384 {
					return nil, queueError("resource_limit", "JSON array exceeds 16384 members")
				}
				child, err := readValue(decoder, depth+1)
				if err != nil {
					return nil, err
				}
				result = append(result, child)
			}
			if _, err := decoder.Token(); err != nil {
				return nil, err
			}
			return result, nil
		}
		return nil, errors.New("unexpected JSON delimiter")
	case json.Number:
		n, err := strconv.ParseInt(string(value), 10, 64)
		if err != nil || n < -maxSafeInteger || n > maxSafeInteger || strconv.FormatInt(n, 10) != string(value) {
			return nil, queueError("noncanonical_number", "use safe integer numbers or canonical decimal strings")
		}
		return value, nil
	default:
		return value, nil
	}
}

func readObject(decoder *json.Decoder, depth int) (any, error) {
	result := map[string]any{}
	for decoder.More() {
		key, err := decoder.Token()
		if err != nil {
			return nil, err
		}
		name, ok := key.(string)
		if !ok {
			return nil, errors.New("JSON object key must be a string")
		}
		if _, exists := result[name]; exists {
			return nil, queueError("duplicate_key", "%q", name)
		}
		child, err := readValue(decoder, depth+1)
		if err != nil {
			return nil, err
		}
		result[name] = child
	}
	if _, err := decoder.Token(); err != nil {
		return nil, err
	}
	return result, nil
}

// decodeStrict parses one JSON value under the integer-only, duplicate-free profile.
func decodeStrict(data []byte) (any, error) {
	if !utf8.Valid(data) {
		return nil, queueError("invalid_utf8", "JSON must be valid UTF-8")
	}
	// encoding/json replaces lone surrogate escapes; reject them instead.
	skipUntil := 0
	for i, char := range data {
		if i < skipUntil || char != '\\' || i+1 >= len(data) {
			continue
		}
		if bytes.HasPrefix(data[i:], []byte(`\\`)) {
			skipUntil = i + 2
			continue
		}
		if !bytes.HasPrefix(data[i:], []byte(`\u`)) || i+6 > len(data) {
			continue
		}
		unit, err := strconv.ParseUint(string(data[i+2:i+6]), 16, 16)
		if err != nil {
			continue
		}
		if unit >= 0xdc00 && unit <= 0xdfff {
			return nil, queueError("invalid_unicode", "unpaired surrogate escape")
		}
		if unit >= 0xd800 && unit <= 0xdbff {
			if i+12 > len(data) || string(data[i+6:i+8]) != `\u` {
				return nil, queueError("invalid_unicode", "unpaired surrogate escape")
			}
			low, err := strconv.ParseUint(string(data[i+8:i+12]), 16, 16)
			if err != nil || low < 0xdc00 || low > 0xdfff {
				return nil, queueError("invalid_unicode", "unpaired surrogate escape")
			}
			skipUntil = i + 12
		}
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	value, err := readValue(decoder, 0)
	if err != nil {
		return nil, err
	}
	if _, err := decoder.Token(); err != io.EOF {
		return nil, queueError("invalid_json", "trailing JSON content")
	}
	return value, nil
}

func writeCanonical(buffer *bytes.Buffer, value any) error {
	switch value := value.(type) {
	case map[string]any:
		keys := slices.Sorted(maps.Keys(value))
		buffer.WriteByte('{')
		for i, key := range keys {
			if i > 0 {
				buffer.WriteByte(',')
			}
			writeCanonicalString(buffer, key)
			buffer.WriteByte(':')
			if err := writeCanonical(buffer, value[key]); err != nil {
				return err
			}
		}
		buffer.WriteByte('}')
	case []any:
		buffer.WriteByte('[')
		for i, child := range value {
			if i > 0 {
				buffer.WriteByte(',')
			}
			if err := writeCanonical(buffer, child); err != nil {
				return err
			}
		}
		buffer.WriteByte(']')
	case string:
		writeCanonicalString(buffer, value)
	case json.Number:
		buffer.WriteString(string(value))
	case bool:
		buffer.WriteString(strconv.FormatBool(value))
	case nil:
		buffer.WriteString("null")
	default:
		return fmt.Errorf("unsupported canonical value %T", value)
	}
	return nil
}

func writeCanonicalString(buffer *bytes.Buffer, value string) {
	buffer.WriteByte('"')
	for _, char := range value {
		switch char {
		case '"':
			buffer.WriteString(`\"`)
		case '\\':
			buffer.WriteString(`\\`)
		case '\b':
			buffer.WriteString(`\b`)
		case '\f':
			buffer.WriteString(`\f`)
		case '\n':
			buffer.WriteString(`\n`)
		case '\r':
			buffer.WriteString(`\r`)
		case '\t':
			buffer.WriteString(`\t`)
		default:
			if char < 0x20 {
				fmt.Fprintf(buffer, `\u%04x`, char) //nolint:fprintferrorunchecked // bytes.Buffer writes cannot fail.
			} else {
				buffer.WriteRune(char)
			}
		}
	}
	buffer.WriteByte('"')
}

func canonicalGeneric(value any) ([]byte, error) {
	var buffer bytes.Buffer
	if err := writeCanonical(&buffer, value); err != nil {
		return nil, err
	}
	return buffer.Bytes(), nil
}

// canonicalStringObject is the canonical encoding of an object whose members are all strings.
func canonicalStringObject(members map[string]string) string {
	var buffer bytes.Buffer
	buffer.WriteByte('{')
	for i, key := range slices.Sorted(maps.Keys(members)) {
		if i > 0 {
			buffer.WriteByte(',')
		}
		writeCanonicalString(&buffer, key)
		buffer.WriteByte(':')
		writeCanonicalString(&buffer, members[key])
	}
	buffer.WriteByte('}')
	return buffer.String()
}

// canonicalStringArray is the canonical encoding of an array of strings.
func canonicalStringArray(values ...string) string {
	var buffer bytes.Buffer
	buffer.WriteByte('[')
	for i, value := range values {
		if i > 0 {
			buffer.WriteByte(',')
		}
		writeCanonicalString(&buffer, value)
	}
	buffer.WriteByte(']')
	return buffer.String()
}

// Canonical uses the contract's integer-only JSON profile and UTF-8 key order.
func Canonical(data []byte) ([]byte, error) {
	value, err := decodeStrict(data)
	if err != nil {
		return nil, err
	}
	return canonicalGeneric(value)
}

// CanonicalValue encodes typed JSON without repairing strings or losing numeric values.
func CanonicalValue(value any) ([]byte, error) {
	if err := validateTypedValue(reflect.ValueOf(value), 0); err != nil {
		return nil, err
	}
	data, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	return Canonical(data)
}

// unwrapTypedValue follows interfaces and pointers; ok is false for nil or invalid values.
func unwrapTypedValue(value reflect.Value) (reflect.Value, bool, error) {
	for wrappers := 0; value.IsValid() &&
		(value.Kind() == reflect.Interface || value.Kind() == reflect.Pointer); wrappers++ {
		if value.IsNil() {
			return value, false, nil
		}
		if wrappers > 64 {
			return value, false, queueError("resource_limit", "cyclic or excessive typed JSON indirection")
		}
		value = value.Elem()
	}
	return value, value.IsValid(), nil
}

func validateTypedValue(value reflect.Value, depth int) error {
	value, ok, err := unwrapTypedValue(value)
	if err != nil || !ok {
		return err
	}
	if depth > 64 {
		return queueError("resource_limit", "typed JSON nesting exceeds 64")
	}
	switch value.Kind() {
	case reflect.String:
		if !utf8.ValidString(value.String()) {
			return queueError("invalid_utf8", "JSON must be valid UTF-8")
		}
	case reflect.Float32, reflect.Float64:
		number := value.Float()
		if math.IsNaN(number) || math.IsInf(number, 0) || math.Abs(number) > float64(maxSafeInteger) ||
			number != math.Trunc(number) || number == 0 && math.Signbit(number) {
			return queueError("noncanonical_number", "typed numbers must be finite safe integers without negative zero")
		}
	case reflect.Map:
		for entries := value.MapRange(); entries.Next(); {
			key := entries.Key()
			if key.Kind() == reflect.String && !utf8.ValidString(key.String()) {
				return queueError("invalid_utf8", "JSON must be valid UTF-8")
			}
			if err := validateTypedValue(entries.Value(), depth+1); err != nil {
				return err
			}
		}
	case reflect.Slice, reflect.Array:
		if value.Type().Elem().Kind() == reflect.Uint8 {
			return nil
		}
		if value.Len() > 16384 {
			return queueError("resource_limit", "JSON array exceeds 16384 members")
		}
		for index := range value.Len() {
			if err := validateTypedValue(value.Index(index), depth+1); err != nil {
				return err
			}
		}
	case reflect.Struct:
		for index := range value.NumField() {
			field := value.Type().Field(index)
			if field.PkgPath != "" || field.Tag.Get("json") == "-" {
				continue
			}
			if err := validateTypedValue(value.Field(index), depth+1); err != nil {
				return err
			}
		}
	}
	return nil
}

// genericValue converts typed JSON into the strict generic representation.
func genericValue(value any) (any, error) {
	data, err := CanonicalValue(value)
	if err != nil {
		return nil, err
	}
	return decodeStrict(data)
}

func hashBytes(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func sha256Text(text string) string {
	return hashBytes([]byte(text))
}

// NodeID binds a Work identity to its graph and node key.
func NodeID(graphID, nodeKey string) string {
	return sha256Text(canonicalStringObject(map[string]string{"graph_id": graphID, "node_key": nodeKey}))
}

// IndependentGraphID derives a graph identity from a canonical payload.
func IndependentGraphID(payload []byte) (string, error) {
	data, err := Canonical(payload)
	if err != nil {
		return "", err
	}
	return hashBytes(data), nil
}
