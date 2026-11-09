package jsonutil

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"strings"

	"github.com/github/gh-aw/pkg/logger"
)

var jsonutilLog = logger.New("jsonutil:json")

// UnmarshalString decodes one complete JSON value without copying the input string.
func UnmarshalString(input string, target any) error {
	decoder := json.NewDecoder(strings.NewReader(input))
	if err := decoder.Decode(target); err != nil {
		return err
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err != nil {
			return err
		}
		return errors.New("JSON input contains multiple values")
	}
	return nil
}

// MarshalCompactNoHTMLEscape marshals a value to compact JSON without HTML escaping.
// It trims the trailing newline emitted by json.Encoder.
func MarshalCompactNoHTMLEscape(v any) (string, error) {
	var buf bytes.Buffer
	encoder := json.NewEncoder(&buf)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(v); err != nil {
		jsonutilLog.Printf("MarshalCompactNoHTMLEscape encode failed: %v", err)
		return "", err
	}

	result := strings.TrimSuffix(buf.String(), "\n")
	jsonutilLog.Printf("MarshalCompactNoHTMLEscape produced %d bytes", len(result))
	return result, nil
}
