//go:build !integration

package workflow

import "testing"

func TestCollectEngineVersionsForMetadata(t *testing.T) {
	compiler := NewCompiler()

	t.Run("only active main engine versions are included", func(t *testing.T) {
		data := &WorkflowData{
			AI: "copilot",
			EngineConfig: &EngineConfig{
				ID:         "copilot",
				Version:    "1.2.3-custom",
				CopilotSDK: true,
			},
		}

		versions := compiler.collectEngineVersionsForMetadata(data)
		if versions["copilot"] != "1.2.3-custom" {
			t.Fatalf("Expected copilot override version, got: %q", versions["copilot"])
		}
		if versions["copilot-sdk"] == "" {
			t.Fatal("Expected copilot-sdk version when copilot-sdk is enabled")
		}
		if _, exists := versions["claude"]; exists {
			t.Fatal("Did not expect inactive claude engine version in metadata map")
		}
	})

	t.Run("includes active detection engine version", func(t *testing.T) {
		data := &WorkflowData{
			AI: "copilot",
			EngineConfig: &EngineConfig{
				ID: "copilot",
			},
			SafeOutputs: &SafeOutputsConfig{
				ThreatDetection: &ThreatDetectionConfig{
					EngineConfig: &EngineConfig{
						ID:      "claude",
						Version: "2.2.0-custom",
					},
				},
			},
		}

		versions := compiler.collectEngineVersionsForMetadata(data)
		if versions["copilot"] == "" {
			t.Fatal("Expected active main engine version for copilot")
		}
		if versions["claude"] != "2.2.0-custom" {
			t.Fatalf("Expected active detection engine override version, got: %q", versions["claude"])
		}
	})

	t.Run("includes normalized detection engine version", func(t *testing.T) {
		data := &WorkflowData{
			AI:           "pi",
			EngineConfig: &EngineConfig{ID: "pi"},
			SafeOutputs: &SafeOutputsConfig{
				ThreatDetection: &ThreatDetectionConfig{},
			},
		}

		versions := compiler.collectEngineVersionsForMetadata(data)
		if versions["pi"] == "" {
			t.Fatal("Expected active main engine version for pi")
		}
		if versions["copilot"] == "" {
			t.Fatal("Expected normalized detection engine version for copilot")
		}
	})
}

func TestResolveAgentImageRunnerIdentifier(t *testing.T) {
	t.Run("string value", func(t *testing.T) {
		frontmatter := map[string]any{"runs-on": "ubuntu-latest"}
		if got := resolveAgentImageRunnerIdentifier(frontmatter); got != "ubuntu-latest" {
			t.Fatalf("Expected string runner identifier, got: %q", got)
		}
	})

	t.Run("array value", func(t *testing.T) {
		frontmatter := map[string]any{"runs-on": []any{"self-hosted", "linux"}}
		if got := resolveAgentImageRunnerIdentifier(frontmatter); got != `["self-hosted","linux"]` {
			t.Fatalf("Expected serialized array runner identifier, got: %q", got)
		}
	})
}
