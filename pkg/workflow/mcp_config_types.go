package workflow

import (
	"github.com/github/gh-aw/pkg/constants"
)

// WellKnownContainer represents a container configuration for a well-known command
type WellKnownContainer struct {
	Image      string // Container image (e.g., "node:lts-alpine")
	Entrypoint string // Entrypoint command (e.g., "npx")
}

// getWellKnownContainer returns the appropriate container configuration for well-known commands
// This enables automatic containerization of stdio MCP servers based on their command
func getWellKnownContainer(command string) *WellKnownContainer {
	wellKnownContainers := map[string]*WellKnownContainer{
		"npx": {
			Image:      constants.DefaultNodeAlpineLTSImage,
			Entrypoint: "npx",
		},
		"uvx": {
			Image:      constants.DefaultPythonAlpineLTSImage,
			Entrypoint: "uvx",
		},
	}

	return wellKnownContainers[command]
}

// MCPConfigRenderer contains configuration options for rendering MCP config
type MCPConfigRenderer struct {
	// IndentLevel controls the indentation level for properties (e.g., "                " for JSON, "          " for TOML)
	IndentLevel string
	// Format specifies the output format ("json" for JSON-like, "toml" for TOML-like)
	Format string
	// RequiresCopilotFields indicates if the engine requires "type" and "tools" fields (true for copilot engine)
	RequiresCopilotFields bool
	// RewriteLocalhostToDocker indicates if localhost URLs should be rewritten to host.docker.internal
	// This is needed when the agent runs inside a firewall container and needs to access MCP servers on the host
	RewriteLocalhostToDocker bool
}

// ToolConfig represents a tool configuration interface for type safety
type ToolConfig interface {
	GetString(key string) (string, bool)
	GetStringArray(key string) ([]string, bool)
	GetStringMap(key string) (map[string]string, bool)
	GetAny(key string) (any, bool)
}

// MapToolConfig implements ToolConfig for map[string]any
type MapToolConfig map[string]any

func (m MapToolConfig) GetString(key string) (string, bool) {
	if value, exists := m[key]; exists {
		if str, ok := value.(string); ok {
			return str, true
		}
	}
	return "", false
}

func (m MapToolConfig) GetStringArray(key string) ([]string, bool) {
	if value, exists := m[key]; exists {
		if arr, ok := value.([]any); ok {
			result := make([]string, 0, len(arr))
			for _, item := range arr {
				if str, ok := item.(string); ok {
					result = append(result, str)
				}
			}
			return result, true
		}
		if arr, ok := value.([]string); ok {
			return arr, true
		}
	}
	return nil, false
}

func (m MapToolConfig) GetStringMap(key string) (map[string]string, bool) {
	if value, exists := m[key]; exists {
		if mapVal, ok := value.(map[string]any); ok {
			result := make(map[string]string)
			for k, v := range mapVal {
				if str, ok := v.(string); ok {
					result[k] = str
				}
			}
			return result, true
		}
		if mapVal, ok := value.(map[string]string); ok {
			return mapVal, true
		}
	}
	return nil, false
}

func (m MapToolConfig) GetAny(key string) (any, bool) {
	value, exists := m[key]
	return value, exists
}
