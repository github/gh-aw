package jsonunmarshalbytesconversion

import (
	"encoding/json"
	jsonalias "encoding/json"
)

type namedString string

func BadString(data string, target any) error {
	return json.Unmarshal([]byte(data), target) // want `json.Unmarshal\(\[\]byte\(s\), \.\.\.\) copies the string; .*second Decode that returns io\.EOF`
}

func BadParenthesized(data string, target any) error {
	return json.Unmarshal(([]byte)((data)), target) // want `json.Unmarshal\(\[\]byte\(s\), \.\.\.\) copies the string`
}

func BadNamedString(data namedString, target any) error {
	return jsonalias.Unmarshal([]byte(data), target) // want `json.Unmarshal\(\[\]byte\(s\), \.\.\.\) copies the string`
}

func Suppressed(data string, target any) error {
	return json.Unmarshal([]byte(data), target) //nolint:jsonunmarshalbytesconversion
}

func GoodByteSlice(data []byte, target any) error {
	return json.Unmarshal([]byte(data), target)
}

func GoodDifferentPackage(data string, target any) error {
	return unmarshal([]byte(data), target)
}

func unmarshal([]byte, any) error {
	return nil
}
