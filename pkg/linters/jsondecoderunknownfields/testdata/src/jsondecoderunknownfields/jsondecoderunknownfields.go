package jsondecoderunknownfields

import (
	"encoding/json"
	jsonalias "encoding/json"
	"strings"
)

func reportsDefaultDecoder(input string) error {
	var value struct{ Name string }
	decoder := json.NewDecoder(strings.NewReader(input)) // want `json.NewDecoder accepts unknown fields by default`
	return decoder.Decode(&value)
}

func reportsAliasedDecoder(input string) error {
	var value any
	decoder := jsonalias.NewDecoder(strings.NewReader(input)) // want `json.NewDecoder accepts unknown fields by default`
	return decoder.Decode(&value)
}

func acceptsStrictDecoder(input string) error {
	var value struct{ Name string }
	decoder := json.NewDecoder(strings.NewReader(input))
	decoder.DisallowUnknownFields()
	return decoder.Decode(&value)
}

func reportsStrictnessEnabledAfterDecode(input string) error {
	var value struct{ Name string }
	decoder := json.NewDecoder(strings.NewReader(input)) // want `json.NewDecoder accepts unknown fields by default`
	_ = decoder.Decode(&value)
	decoder.DisallowUnknownFields()
	return nil
}

func reportsReassignedDecoder(input string) error {
	var value any
	decoder := json.NewDecoder(strings.NewReader(input))
	decoder.DisallowUnknownFields()
	decoder = json.NewDecoder(strings.NewReader(input)) // want `json.NewDecoder accepts unknown fields by default`
	return decoder.Decode(&value)
}

func reportsInlineDecoder(input string) error {
	var value any
	return json.NewDecoder(strings.NewReader(input)).Decode(&value) // want `json.NewDecoder accepts unknown fields by default`
}

func suppressedDecoder(input string) error {
	var value any
	decoder := json.NewDecoder(strings.NewReader(input)) //nolint:jsondecoderunknownfields
	return decoder.Decode(&value)
}

func reportsUnusedDecoder(input string) {
	json.NewDecoder(strings.NewReader(input)) // want `json.NewDecoder accepts unknown fields by default`
}

func unrelatedReader(input string) {
	_ = strings.NewReader(input)
}
