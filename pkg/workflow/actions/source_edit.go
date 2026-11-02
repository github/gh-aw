package actions

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/goccy/go-yaml"
)

// EditSource applies semantic YAML changes using the same source-preserving
// syntax merger as the Actions AST. It also supports AW frontmatter mappings.
func EditSource(content string, before, after any) (string, error) {
	oldYAML, err := snapshotSourceEditValue(before)
	if err != nil {
		return "", err
	}
	newYAML, err := snapshotSourceEditValue(after)
	if err != nil {
		return "", err
	}
	source := &workflowSource{text: content, lowered: oldYAML}
	return source.emitWithValidation(newYAML, func(content, expected string) error {
		actual, err := snapshotYAMLSource(content)
		if err != nil {
			return err
		}
		wanted, err := snapshotYAMLSource(expected)
		if err != nil {
			return err
		}
		if actual != wanted {
			return errors.New("edited YAML source does not match the requested AST")
		}
		return nil
	})
}

func snapshotSourceEditValue(value any) (string, error) {
	// Retain block-style mappings and plain keys for inserted syntax, but quote
	// multiline scalars so the block encoder cannot discard suffix whitespace.
	content, err := yaml.MarshalWithOptions(value, yaml.CustomMarshaler[string](func(text string) ([]byte, error) {
		if strings.ContainsAny(text, "\r\n") {
			return json.Marshal(text)
		}
		return yaml.Marshal(text)
	}))
	if err != nil {
		return "", fmt.Errorf("serializing YAML source snapshot: %w", err)
	}
	return string(content), nil
}

func snapshotYAMLSource(content string) (string, error) {
	var value any
	if err := yaml.Unmarshal([]byte(content), &value); err != nil {
		return "", fmt.Errorf("validating edited YAML source: %w", err)
	}
	return snapshotYAMLValue(value)
}
