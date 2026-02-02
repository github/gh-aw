package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/github/gh-aw/pkg/logger"
)

var vscodeConfigLog = logger.New("cli:vscode_config")

// VSCodeSettings represents the structure of .vscode/settings.json
type VSCodeSettings struct {
	YAMLSchemas map[string]any `json:"yaml.schemas,omitempty"`
	// Include other commonly used settings as any to preserve them
	Other map[string]any `json:"-"`
}

// UnmarshalJSON custom unmarshaler for VSCodeSettings to preserve unknown fields
func (s *VSCodeSettings) UnmarshalJSON(data []byte) error {
	// First unmarshal into a generic map to capture all fields
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}

	// Extract yaml.schemas if it exists
	if yamlSchemas, ok := raw["yaml.schemas"]; ok {
		if schemas, ok := yamlSchemas.(map[string]any); ok {
			s.YAMLSchemas = schemas
		}
		delete(raw, "yaml.schemas")
	}

	// Initialize Other if nil and store remaining fields
	if s.Other == nil {
		s.Other = make(map[string]any)
	}
	for k, v := range raw {
		s.Other[k] = v
	}

	return nil
}

// MarshalJSON custom marshaler for VSCodeSettings to include all fields
func (s VSCodeSettings) MarshalJSON() ([]byte, error) {
	// Create a map with all fields
	result := make(map[string]any)

	// Add all other fields first
	for k, v := range s.Other {
		result[k] = v
	}

	// Add yaml.schemas if present
	if len(s.YAMLSchemas) > 0 {
		result["yaml.schemas"] = s.YAMLSchemas
	}

	return json.Marshal(result)
}

// ensureVSCodeSettings creates or updates .vscode/settings.json
func ensureVSCodeSettings(verbose bool) error {
	vscodeConfigLog.Print("Creating or updating .vscode/settings.json")

	// Create .vscode directory if it doesn't exist
	vscodeDir := ".vscode"
	if err := os.MkdirAll(vscodeDir, 0755); err != nil {
		return fmt.Errorf("failed to create .vscode directory: %w", err)
	}
	vscodeConfigLog.Printf("Ensured directory exists: %s", vscodeDir)

	settingsPath := filepath.Join(vscodeDir, "settings.json")

	// Check if settings.json already exists
	if _, err := os.Stat(settingsPath); err == nil {
		vscodeConfigLog.Print("Settings file already exists, skipping creation")
		if verbose {
			fmt.Fprintf(os.Stderr, "Settings file already exists at %s\n", settingsPath)
		}
		return nil
	}

	// Create minimal settings file with just Copilot settings
	settings := VSCodeSettings{
		Other: map[string]any{
			"github.copilot.enable": map[string]bool{
				"markdown": true,
			},
		},
	}

	// Write settings file with proper indentation
	data, err := json.MarshalIndent(settings, "", "    ")
	if err != nil {
		return fmt.Errorf("failed to marshal settings.json: %w", err)
	}

	if err := os.WriteFile(settingsPath, data, 0644); err != nil {
		return fmt.Errorf("failed to write settings.json: %w", err)
	}
	vscodeConfigLog.Printf("Wrote settings to: %s", settingsPath)

	return nil
}
