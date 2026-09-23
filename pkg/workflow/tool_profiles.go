package workflow

import (
	"errors"
	"fmt"
	"strings"
)

func extractToolProfiles(tools map[string]any) ([]string, error) {
	value, exists := tools["profile"]
	if !exists {
		return nil, nil
	}

	switch profiles := value.(type) {
	case string:
		if strings.TrimSpace(profiles) == "" {
			return nil, errors.New("tools.profile entries must be nonempty strings")
		}
		return []string{profiles}, nil
	case map[string]any:
		// MCP servers are merged into the tools map for runtime rendering.
		// Preserve a custom server named "profile".
		return nil, nil
	case []any:
		if len(profiles) == 0 {
			return nil, errors.New("tools.profile array must not be empty")
		}
		result := make([]string, 0, len(profiles))
		seen := make(map[string]struct{}, len(profiles))
		for _, profile := range profiles {
			name, ok := profile.(string)
			if !ok || strings.TrimSpace(name) == "" {
				return nil, errors.New("tools.profile entries must be nonempty strings")
			}
			if _, exists := seen[name]; exists {
				return nil, fmt.Errorf("tools.profile contains duplicate value %q", name)
			}
			result = append(result, name)
			seen[name] = struct{}{}
		}
		return result, nil
	default:
		return nil, errors.New("tools.profile must be a string or an array of strings")
	}
}
