package parser

import (
	"errors"
	"fmt"
	"sort"
)

// HasConnectorTriggers distinguishes a main workflow with connector triggers
// from a shared workflow that intentionally omits an on section.
func HasConnectorTriggers(frontmatter map[string]any) bool {
	registrations, ok := frontmatter["connectors"].(map[string]any)
	if !ok {
		return false
	}
	for _, value := range registrations {
		if config, ok := value.(map[string]any); ok {
			if _, exists := config["on"]; exists {
				return true
			}
		}
	}
	return false
}

// GetSafeOutputFieldKeys includes operation and configuration names reserved by
// the built-in safe-output grammar.
func GetSafeOutputFieldKeys() ([]string, error) {
	keys, err := GetSafeOutputTypeKeys()
	if err != nil {
		return nil, err
	}
	for field := range safeOutputMetaFields {
		keys = append(keys, field)
	}
	sort.Strings(keys)
	return keys, nil
}

// Imports register services but cannot select capabilities on the consumer's behalf.
func validateSharedConnectorFields(frontmatter map[string]any) error {
	value, present := frontmatter["connectors"]
	if !present {
		return nil
	}
	registrations, ok := value.(map[string]any)
	if !ok {
		return errors.New("connectors must be an object")
	}
	for alias, value := range registrations {
		config, ok := value.(map[string]any)
		if !ok {
			return fmt.Errorf("connector %q must be an object", alias)
		}
		for _, field := range []string{"on", "tools", "context", "safe-outputs"} {
			if _, selected := config[field]; selected {
				return fmt.Errorf("connector %q: select %s in the main workflow; shared imports may only register definition and connection", alias, field)
			}
		}
	}
	return nil
}
