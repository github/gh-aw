package actions

import (
	"errors"
	"fmt"

	"github.com/goccy/go-yaml"
)

// EditSource applies semantic YAML changes using the same source-preserving
// syntax merger as the Actions AST. It also supports AW frontmatter mappings.
func EditSource(content string, before, after any) (string, error) {
	// YAML's block-style encoder can discard trailing whitespace-only data.
	// Snapshot values as JSON so both merging and validation remain lossless.
	oldYAML, err := snapshotYAMLValue(before)
	if err != nil {
		return "", err
	}
	newYAML, err := snapshotYAMLValue(after)
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

func snapshotYAMLSource(content string) (string, error) {
	var value any
	if err := yaml.Unmarshal([]byte(content), &value); err != nil {
		return "", fmt.Errorf("validating edited YAML source: %w", err)
	}
	return snapshotYAMLValue(value)
}
