package workflow

import (
	"github.com/github/gh-aw/pkg/parser"
)

func extractToolProfiles(tools map[string]any) ([]string, error) {
	value, exists := tools["profile"]
	if !exists {
		return nil, nil
	}

	switch value := value.(type) {
	case map[string]any:
		// MCP servers are merged into the tools map for runtime rendering.
		// Preserve a custom server named "profile".
		return nil, nil
	default:
		return parser.NormalizeToolProfiles(value)
	}
}
