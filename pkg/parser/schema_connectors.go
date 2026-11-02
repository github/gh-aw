package parser

import "errors"

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

// Connector authority is rooted in the main workflow's repository. Shared AW
// imports must not silently lose declarations or widen that authority.
func validateSharedConnectorFields(frontmatter map[string]any) error {
	if _, exists := frontmatter["connectors"]; exists {
		return errors.New("connectors must be registered in the main workflow, not a shared AW import; reference an external ConnectorSpec with connectors.<name>.definition")
	}
	return nil
}
