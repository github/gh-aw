package jsonunmarshalbytesconversion

import "encoding/json"

func bad(s string) error {
	var v map[string]any
	return json.Unmarshal([]byte(s), &v) // want `json\.Unmarshal\(\[\]byte\(s\), \.\.\.\) copies the string`
}

func badParen(s string) error {
	var v map[string]any
	return json.Unmarshal(([]byte(s)), &v) // want `copies the string`
}

func suppressed(s string) error {
	var v map[string]any
	return json.Unmarshal([]byte(s), &v) //nolint:jsonunmarshalbytesconversion
}

func good(b []byte) error {
	var v map[string]any
	return json.Unmarshal(b, &v)
}

func goodOtherConversion(b []byte) error {
	var v map[string]any
	return json.Unmarshal([]byte(b), &v)
}

type other struct{}

func (other) Unmarshal(b []byte, v any) error { return nil }

func goodOtherReceiver(s string) error {
	var o other
	return o.Unmarshal([]byte(s), nil)
}
