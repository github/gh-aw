//go:build !integration

package workflow

import (
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/constants"
)

func TestExternalDetectorSDKVersionMetadata(t *testing.T) {
	compiler := NewCompiler()
	for _, tc := range []struct {
		name        string
		driver      string
		cliPin      string
		wantCLI     string
		wantVersion string
		wantInstall string
	}{
		{
			name:        "custom command is ignored",
			wantCLI:     string(constants.DefaultCopilotSDKNPMCLIVersion),
			wantVersion: string(constants.DefaultCopilotSDKNPMVersion),
			wantInstall: copilotSDKNodeInstallCommand,
		},
		{
			name:        "explicit Python driver is retained",
			driver:      "driver.py",
			wantCLI:     string(constants.DefaultCopilotVersion),
			wantVersion: string(constants.DefaultCopilotSDKVersion),
			wantInstall: "github-copilot-sdk==" + string(constants.DefaultCopilotSDKVersion),
		},
		{
			name:        "explicit CLI pin wins",
			cliPin:      "1.0.101",
			wantCLI:     "1.0.101",
			wantVersion: string(constants.DefaultCopilotSDKNPMVersion),
			wantInstall: copilotSDKNodeInstallCommand,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			data := &WorkflowData{
				AI:       "claude",
				Features: map[string]any{string(constants.GHAWDetectionFeatureFlag): true},
				SafeOutputs: &SafeOutputsConfig{
					ThreatDetection: &ThreatDetectionConfig{
						EngineConfig: &EngineConfig{
							ID:         "copilot",
							CopilotSDK: true,
							Version:    tc.cliPin,
							Command:    "python3 driver.py",
							Driver:     tc.driver,
						},
					},
				},
			}
			install := strings.Join(compiler.buildInstallDetectionEngineForExternalDetectorStep(data), "\n")
			if !strings.Contains(install, tc.wantInstall) {
				t.Fatalf("Expected detector installation to contain %q, got:\n%s", tc.wantInstall, install)
			}
			if !strings.Contains(install, `install_copilot_cli.sh" `+tc.wantCLI+" ") {
				t.Fatalf("Expected detector to install CLI version %q, got:\n%s", tc.wantCLI, install)
			}
			versions := compiler.collectEngineVersionsForMetadata(data)
			if got := versions["copilot"]; got != tc.wantCLI {
				t.Fatalf("Expected detector CLI version %q in metadata, got %q", tc.wantCLI, got)
			}
			if got := versions["copilot-sdk"]; got != tc.wantVersion {
				t.Fatalf("Expected detector SDK version %q in metadata, got %q", tc.wantVersion, got)
			}
		})
	}
}

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
		versions := compiler.collectEngineVersionsForMetadata(data)
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

		versions := compiler.collectEngineVersionsForMetadata(data)
		if versions["copilot"] == "" {
			t.Fatal("Expected active main engine version for copilot")
		}
		if versions["claude"] != "2.2.0-custom" {
			t.Fatalf("Expected active detection engine override version, got: %q", versions["claude"])
		}
	})

	t.Run("inline detection does not record an SDK version", func(t *testing.T) {
		data := &WorkflowData{
			AI:       "claude",
			Features: map[string]any{string(constants.GHAWDetectionFeatureFlag): false},
			SafeOutputs: &SafeOutputsConfig{
				ThreatDetection: &ThreatDetectionConfig{
					EngineConfig: &EngineConfig{ID: "copilot", CopilotSDK: true},
				},
			},
		}
		versions := compiler.collectEngineVersionsForMetadata(data)
		if got := versions["copilot"]; got != string(constants.DefaultCopilotVersion) {
			t.Fatalf("Expected CLI-only version for inline detection, got %q", got)
		}
		if got, exists := versions["copilot-sdk"]; exists {
			t.Fatalf("Did not expect an SDK version for inline detection, got %q", got)
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
