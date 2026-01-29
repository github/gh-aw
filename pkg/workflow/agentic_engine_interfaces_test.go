//go:build !integration

package workflow

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestInterfaceSegregation validates that the interface segregation is properly implemented
func TestInterfaceSegregation(t *testing.T) {
	t.Run("all engines implement CodingAgentEngine composite interface", func(t *testing.T) {
		registry := NewEngineRegistry()
		engines := registry.GetAllEngines()

		for _, engine := range engines {
			// Verify each engine implements the composite interface
			// All engines are returned as CodingAgentEngine from registry
			assert.NotNil(t, engine, "Engine should not be nil")
		}
	})

	t.Run("all engines implement Engine core interface", func(t *testing.T) {
		registry := NewEngineRegistry()
		engines := registry.GetAllEngines()

		for _, engine := range engines {
			// Verify core Engine interface
			_, ok := engine.(Engine)
			assert.True(t, ok, "Engine %s should implement Engine interface", engine.GetID())

			// Test required methods
			assert.NotEmpty(t, engine.GetID(), "GetID should return non-empty string")
			assert.NotEmpty(t, engine.GetDisplayName(), "GetDisplayName should return non-empty string")
			assert.NotEmpty(t, engine.GetDescription(), "GetDescription should return non-empty string")
			// IsExperimental can be true or false, just verify it exists
			_ = engine.IsExperimental()
		}
	})

	t.Run("all engines implement CapabilityProvider interface", func(t *testing.T) {
		registry := NewEngineRegistry()
		engines := registry.GetAllEngines()

		for _, engine := range engines {
			// Verify CapabilityProvider interface
			_, ok := engine.(CapabilityProvider)
			assert.True(t, ok, "Engine %s should implement CapabilityProvider", engine.GetID())

			// Test capability methods (values can be true/false, just verify they exist)
			_ = engine.SupportsToolsAllowlist()
			_ = engine.SupportsHTTPTransport()
			_ = engine.SupportsMaxTurns()
			_ = engine.SupportsWebFetch()
			_ = engine.SupportsWebSearch()
			_ = engine.SupportsFirewall()
		}
	})

	t.Run("all engines implement WorkflowExecutor interface", func(t *testing.T) {
		registry := NewEngineRegistry()
		engines := registry.GetAllEngines()

		for _, engine := range engines {
			// Verify WorkflowExecutor interface
			_, ok := engine.(WorkflowExecutor)
			assert.True(t, ok, "Engine %s should implement WorkflowExecutor", engine.GetID())

			// Create minimal workflow data for testing
			workflowData := &WorkflowData{
				Name:        "test-workflow",
				ParsedTools: &ToolsConfig{},
				Tools:       map[string]any{},
			}

			// Test GetDeclaredOutputFiles (can return empty list)
			outputFiles := engine.GetDeclaredOutputFiles()
			assert.NotNil(t, outputFiles, "GetDeclaredOutputFiles should not return nil")

			// Test GetInstallationSteps (can return empty list)
			installSteps := engine.GetInstallationSteps(workflowData)
			assert.NotNil(t, installSteps, "GetInstallationSteps should not return nil")

			// Test GetExecutionSteps (can return empty list)
			execSteps := engine.GetExecutionSteps(workflowData, "/tmp/test.log")
			assert.NotNil(t, execSteps, "GetExecutionSteps should not return nil")
		}
	})

	t.Run("all engines implement MCPConfigProvider interface", func(t *testing.T) {
		registry := NewEngineRegistry()
		engines := registry.GetAllEngines()

		for _, engine := range engines {
			// Verify MCPConfigProvider interface
			_, ok := engine.(MCPConfigProvider)
			assert.True(t, ok, "Engine %s should implement MCPConfigProvider", engine.GetID())

			// Test RenderMCPConfig (method exists, may write nothing or something)
			var yaml strings.Builder
			workflowData := &WorkflowData{
				Name:        "test-workflow",
				ParsedTools: &ToolsConfig{},
				Tools:       map[string]any{},
			}
			// Should not panic
			engine.RenderMCPConfig(&yaml, map[string]any{}, []string{}, workflowData)
		}
	})

	t.Run("all engines implement LogParser interface", func(t *testing.T) {
		registry := NewEngineRegistry()
		engines := registry.GetAllEngines()

		for _, engine := range engines {
			// Verify LogParser interface
			_, ok := engine.(LogParser)
			assert.True(t, ok, "Engine %s should implement LogParser", engine.GetID())

			// Test GetLogParserScriptId (can return empty string)
			scriptId := engine.GetLogParserScriptId()
			_ = scriptId // Can be empty or non-empty

			// Test GetLogFileForParsing (should return non-empty path)
			logFile := engine.GetLogFileForParsing()
			assert.NotEmpty(t, logFile, "GetLogFileForParsing should return non-empty path")

			// Test ParseLogMetrics (can return empty metrics)
			metrics := engine.ParseLogMetrics("test log content", false)
			assert.NotNil(t, metrics, "ParseLogMetrics should not return nil")
		}
	})

	t.Run("all engines implement SecurityProvider interface", func(t *testing.T) {
		registry := NewEngineRegistry()
		engines := registry.GetAllEngines()

		for _, engine := range engines {
			// Verify SecurityProvider interface
			_, ok := engine.(SecurityProvider)
			assert.True(t, ok, "Engine %s should implement SecurityProvider", engine.GetID())

			// Test GetDefaultDetectionModel (can return empty string)
			model := engine.GetDefaultDetectionModel()
			_ = model // Can be empty or non-empty

			// Test GetRequiredSecretNames (can return empty list)
			workflowData := &WorkflowData{
				Name:        "test-workflow",
				ParsedTools: &ToolsConfig{},
				Tools:       map[string]any{},
			}
			secrets := engine.GetRequiredSecretNames(workflowData)
			assert.NotNil(t, secrets, "GetRequiredSecretNames should not return nil for engine %s", engine.GetID())
		}
	})
}

// TestInterfaceComposition validates that interface composition works correctly
func TestInterfaceComposition(t *testing.T) {
	t.Run("CodingAgentEngine composes all sub-interfaces", func(t *testing.T) {
		// Get an engine instance
		registry := NewEngineRegistry()
		engine, err := registry.GetEngine("copilot")
		require.NoError(t, err)

		// Verify it can also be cast to individual interfaces
		_, ok := engine.(Engine)
		assert.True(t, ok, "Should be able to cast CodingAgentEngine to Engine")

		_, ok = engine.(CapabilityProvider)
		assert.True(t, ok, "Should be able to cast CodingAgentEngine to CapabilityProvider")

		_, ok = engine.(WorkflowExecutor)
		assert.True(t, ok, "Should be able to cast CodingAgentEngine to WorkflowExecutor")

		_, ok = engine.(MCPConfigProvider)
		assert.True(t, ok, "Should be able to cast CodingAgentEngine to MCPConfigProvider")

		_, ok = engine.(LogParser)
		assert.True(t, ok, "Should be able to cast CodingAgentEngine to LogParser")

		_, ok = engine.(SecurityProvider)
		assert.True(t, ok, "Should be able to cast CodingAgentEngine to SecurityProvider")
	})
}

// TestSpecificInterfaceUsage demonstrates using specific interfaces
func TestSpecificInterfaceUsage(t *testing.T) {
	t.Run("using only Engine interface", func(t *testing.T) {
		registry := NewEngineRegistry()

		// Function that only needs Engine interface
		checkEngineIdentity := func(e Engine) bool {
			return e.GetID() != "" && e.GetDisplayName() != ""
		}

		// All engines should satisfy this
		for _, engine := range registry.GetAllEngines() {
			assert.True(t, checkEngineIdentity(engine), "Engine %s should have valid identity", engine.GetID())
		}
	})

	t.Run("using only CapabilityProvider interface", func(t *testing.T) {
		registry := NewEngineRegistry()

		// Function that only needs CapabilityProvider interface
		checkCapabilities := func(cp CapabilityProvider) map[string]bool {
			return map[string]bool{
				"tools_allowlist": cp.SupportsToolsAllowlist(),
				"http_transport":  cp.SupportsHTTPTransport(),
				"max_turns":       cp.SupportsMaxTurns(),
				"web_fetch":       cp.SupportsWebFetch(),
				"web_search":      cp.SupportsWebSearch(),
				"firewall":        cp.SupportsFirewall(),
			}
		}

		// All engines should satisfy this
		for _, engine := range registry.GetAllEngines() {
			caps := checkCapabilities(engine)
			assert.NotNil(t, caps, "Engine %s should have capabilities", engine.GetID())
			assert.Len(t, caps, 6, "Should have 6 capability flags")
		}
	})

	t.Run("using only WorkflowExecutor interface", func(t *testing.T) {
		registry := NewEngineRegistry()

		// Function that only needs WorkflowExecutor interface
		canExecuteWorkflow := func(we WorkflowExecutor) bool {
			workflowData := &WorkflowData{
				Name:        "test",
				ParsedTools: &ToolsConfig{},
				Tools:       map[string]any{},
			}
			installSteps := we.GetInstallationSteps(workflowData)
			execSteps := we.GetExecutionSteps(workflowData, "/tmp/test.log")
			return installSteps != nil && execSteps != nil
		}

		// All engines should satisfy this
		for _, engine := range registry.GetAllEngines() {
			assert.True(t, canExecuteWorkflow(engine), "Engine %s should be able to execute workflows", engine.GetID())
		}
	})
}

// TestBaseEngineImplementsAllInterfaces verifies BaseEngine provides default implementations
func TestBaseEngineImplementsAllInterfaces(t *testing.T) {
	// Create a BaseEngine instance
	base := &BaseEngine{
		id:                     "test",
		displayName:            "Test Engine",
		description:            "A test engine",
		experimental:           false,
		supportsToolsAllowlist: true,
		supportsHTTPTransport:  true,
		supportsMaxTurns:       true,
		supportsWebFetch:       true,
		supportsWebSearch:      true,
		supportsFirewall:       true,
	}

	// Verify Engine interface methods
	assert.Equal(t, "test", base.GetID())
	assert.Equal(t, "Test Engine", base.GetDisplayName())
	assert.Equal(t, "A test engine", base.GetDescription())
	assert.False(t, base.IsExperimental())

	// Verify CapabilityProvider interface methods
	assert.True(t, base.SupportsToolsAllowlist())
	assert.True(t, base.SupportsHTTPTransport())
	assert.True(t, base.SupportsMaxTurns())
	assert.True(t, base.SupportsWebFetch())
	assert.True(t, base.SupportsWebSearch())
	assert.True(t, base.SupportsFirewall())

	// Verify default implementations
	assert.Empty(t, base.GetDeclaredOutputFiles())
	assert.Empty(t, base.GetDefaultDetectionModel())
	assert.Equal(t, "/tmp/gh-aw/agent-stdio.log", base.GetLogFileForParsing())

	workflowData := &WorkflowData{
		Name:        "test",
		ParsedTools: &ToolsConfig{},
		Tools:       map[string]any{},
	}
	assert.Empty(t, base.GetRequiredSecretNames(workflowData))
}

// TestEngineCapabilityVariety validates that different engines have different capabilities
func TestEngineCapabilityVariety(t *testing.T) {
	registry := NewEngineRegistry()

	copilot, _ := registry.GetEngine("copilot")
	claude, _ := registry.GetEngine("claude")
	codex, _ := registry.GetEngine("codex")
	custom, _ := registry.GetEngine("custom")

	// Test that capabilities differ across engines
	t.Run("copilot capabilities", func(t *testing.T) {
		assert.True(t, copilot.SupportsToolsAllowlist())
		assert.True(t, copilot.SupportsHTTPTransport())
		assert.False(t, copilot.SupportsMaxTurns())
		assert.True(t, copilot.SupportsWebFetch())
		assert.False(t, copilot.SupportsWebSearch())
		assert.True(t, copilot.SupportsFirewall())
		assert.False(t, copilot.IsExperimental())
	})

	t.Run("claude capabilities", func(t *testing.T) {
		assert.True(t, claude.SupportsToolsAllowlist())
		assert.True(t, claude.SupportsHTTPTransport())
		assert.True(t, claude.SupportsMaxTurns())
		assert.True(t, claude.SupportsWebFetch())
		assert.True(t, claude.SupportsWebSearch())
		assert.True(t, claude.SupportsFirewall())
		assert.False(t, claude.IsExperimental())
	})

	t.Run("codex capabilities", func(t *testing.T) {
		assert.True(t, codex.SupportsToolsAllowlist())
		assert.True(t, codex.SupportsHTTPTransport())
		assert.False(t, codex.SupportsMaxTurns())
		assert.False(t, codex.SupportsWebFetch())
		assert.True(t, codex.SupportsWebSearch())
		assert.True(t, codex.SupportsFirewall())
		assert.False(t, codex.IsExperimental())
	})

	t.Run("custom capabilities", func(t *testing.T) {
		assert.False(t, custom.SupportsToolsAllowlist())
		assert.False(t, custom.SupportsHTTPTransport())
		assert.True(t, custom.SupportsMaxTurns())
		assert.False(t, custom.SupportsWebFetch())
		assert.False(t, custom.SupportsWebSearch())
		assert.False(t, custom.IsExperimental())
	})
}

// TestEngineRegistryAcceptsEngineInterface validates that EngineRegistry works with the Engine interface
func TestEngineRegistryAcceptsEngineInterface(t *testing.T) {
	registry := NewEngineRegistry()

	// Create a minimal engine that only implements Engine interface (via BaseEngine)
	minimalEngine := &ClaudeEngine{
		BaseEngine: BaseEngine{
			id:           "minimal-test",
			displayName:  "Minimal Test Engine",
			description:  "Minimal engine for testing",
			experimental: false,
		},
	}

	// Should be able to register it
	registry.Register(minimalEngine)

	// Should be able to retrieve it
	retrieved, err := registry.GetEngine("minimal-test")
	require.NoError(t, err)
	assert.Equal(t, "minimal-test", retrieved.GetID())
	assert.Equal(t, "Minimal Test Engine", retrieved.GetDisplayName())
}
