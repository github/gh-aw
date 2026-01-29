//go:build !integration

package workflow

import (
	"testing"
)

func TestEngineRegistry(t *testing.T) {
	registry := NewEngineRegistry()

	// Test that built-in engines are registered
	supportedEngines := registry.GetSupportedEngines()
	if len(supportedEngines) != 4 {
		t.Errorf("Expected 4 supported engines, got %d", len(supportedEngines))
	}

	// Test getting engines by ID
	claudeEngine, err := registry.GetEngine("claude")
	if err != nil {
		t.Errorf("Expected to find claude engine, got error: %v", err)
	}
	if claudeEngine.GetID() != "claude" {
		t.Errorf("Expected claude engine ID, got '%s'", claudeEngine.GetID())
	}

	codexEngine, err := registry.GetEngine("codex")
	if err != nil {
		t.Errorf("Expected to find codex engine, got error: %v", err)
	}
	if codexEngine.GetID() != "codex" {
		t.Errorf("Expected codex engine ID, got '%s'", codexEngine.GetID())
	}

	customEngine, err := registry.GetEngine("custom")
	if err != nil {
		t.Errorf("Expected to find custom engine, got error: %v", err)
	}
	if customEngine.GetID() != "custom" {
		t.Errorf("Expected custom engine ID, got '%s'", customEngine.GetID())
	}

	// Test getting non-existent engine
	_, err = registry.GetEngine("nonexistent")
	if err == nil {
		t.Error("Expected error when getting non-existent engine")
	}

	// Test IsValidEngine
	if !registry.IsValidEngine("claude") {
		t.Error("Expected claude to be valid engine")
	}

	if !registry.IsValidEngine("codex") {
		t.Error("Expected codex to be valid engine")
	}

	if !registry.IsValidEngine("custom") {
		t.Error("Expected custom to be valid engine")
	}

	if registry.IsValidEngine("nonexistent") {
		t.Error("Expected nonexistent to be invalid engine")
	}

	// Test GetDefaultEngine
	defaultEngine := registry.GetDefaultEngine()
	if defaultEngine.GetID() != "copilot" {
		t.Errorf("Expected default engine to be copilot, got '%s'", defaultEngine.GetID())
	}

	// Test GetEngineByPrefix
	engineByPrefix, err := registry.GetEngineByPrefix("codex-experimental")
	if err != nil {
		t.Errorf("Expected to find engine by prefix 'codex-experimental', got error: %v", err)
	}
	if engineByPrefix.GetID() != "codex" {
		t.Errorf("Expected engine ID 'codex' from prefix, got '%s'", engineByPrefix.GetID())
	}

	// Test GetEngineByPrefix with non-matching prefix
	_, err = registry.GetEngineByPrefix("nonexistent-prefix")
	if err == nil {
		t.Error("Expected error when getting engine by non-matching prefix")
	}
}

func TestEngineRegistryCustomEngine(t *testing.T) {
	registry := NewEngineRegistry()

	// Create a custom engine for testing
	customEngine := &ClaudeEngine{
		BaseEngine: BaseEngine{
			id:                     "test-custom",
			displayName:            "Test Custom Engine",
			description:            "A test custom engine",
			experimental:           true,
			supportsToolsAllowlist: false,
		},
	}

	// Register the custom engine
	registry.Register(customEngine)

	// Test that it's now available
	engine, err := registry.GetEngine("test-custom")
	if err != nil {
		t.Errorf("Expected to find test-custom engine, got error: %v", err)
	}

	if engine.GetID() != "test-custom" {
		t.Errorf("Expected test-custom engine ID, got '%s'", engine.GetID())
	}

	if !engine.IsExperimental() {
		t.Error("Expected test-custom engine to be experimental")
	}

	// Test that supported engines list is updated
	supportedEngines := registry.GetSupportedEngines()
	if len(supportedEngines) != 5 {
		t.Errorf("Expected 5 supported engines after adding test-custom, got %d", len(supportedEngines))
	}
}
