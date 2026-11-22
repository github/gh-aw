//go:build !integration

package workflow

import (
	"testing"

	"github.com/github/gh-aw/pkg/constants"
)

func TestCollectEngineVersionsForMetadata(t *testing.T) {
	t.Run("only active main engine versions are included", func(t *testing.T) {
		data := &WorkflowData{
			AI: "copilot",
			EngineConfig: &EngineConfig{
				ID:         "copilot",
				Version:    "1.2.3-custom",
				CopilotSDK: true,
			},
		}

		versions := collectEngineVersionsForMetadata(data, GetGlobalEngineRegistry())
		if versions["copilot"] != "1.2.3-custom" {
			t.Fatalf("Expected copilot override version, got: %q", versions["copilot"])
		}
		if versions["copilot-sdk"] != string(constants.DefaultCopilotSDKNPMVersion) {
			t.Fatalf("Expected npm SDK version, got: %q", versions["copilot-sdk"])
		}
		if _, exists := versions["claude"]; exists {
			t.Fatal("Did not expect inactive claude engine version in metadata map")
		}
	})

	t.Run("non-npm drivers keep their own SDK version", func(t *testing.T) {
		data := &WorkflowData{
			AI:           "copilot",
			EngineConfig: &EngineConfig{ID: "copilot", CopilotSDK: true, Driver: "driver.py"},
		}
		versions := collectEngineVersionsForMetadata(data, GetGlobalEngineRegistry())
		if versions["copilot-sdk"] != string(constants.DefaultCopilotSDKVersion) {
			t.Fatalf("Expected non-npm SDK version, got: %q", versions["copilot-sdk"])
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

		versions := collectEngineVersionsForMetadata(data, GetGlobalEngineRegistry())
		if versions["copilot"] == "" {
			t.Fatal("Expected active main engine version for copilot")
		}
		if versions["claude"] != "2.2.0-custom" {
			t.Fatalf("Expected active detection engine override version, got: %q", versions["claude"])
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
