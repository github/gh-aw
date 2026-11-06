//go:build !integration

package workflow

import (
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/constants"
)

func TestDefaultRuntimeImagePins(t *testing.T) {
	t.Parallel()
	images := []string{"ghcr.io/github/gh-aw-mcpg:" + string(constants.DefaultMCPGatewayVersion)}
	for _, role := range []string{"agent", "squid", "api-proxy", "cli-proxy", "router", "enclave-agent", "enclave-script", "enclave-mcp-server"} {
		images = append(images, constants.DefaultFirewallRegistry+"/"+role+":"+getAWFImageTag(nil))
	}
	for _, image := range images {
		t.Run(image, func(t *testing.T) {
			if digest := lookupContainerDigest(image, nil); !awfPinnedImagePattern.MatchString(image + "@" + digest) {
				t.Fatalf("default runtime image %s lacks an immutable digest pin", image)
			}
		})
	}
}

// TestGetAWFImageTag tests the getAWFImageTag helper function
func TestGetAWFImageTag(t *testing.T) {
	t.Run("returns default version without v prefix when firewall config is nil", func(t *testing.T) {
		result := getAWFImageTag(nil)
		// DefaultFirewallVersion is "v0.7.0", but getAWFImageTag strips the "v" prefix
		expected := strings.TrimPrefix(string(constants.DefaultFirewallVersion), "v")
		if result != expected {
			t.Errorf("Expected %s, got %s", expected, result)
		}
	})

	t.Run("returns default version without v prefix when version is empty", func(t *testing.T) {
		config := &FirewallConfig{
			Enabled: true,
			Version: "",
		}
		result := getAWFImageTag(config)
		expected := strings.TrimPrefix(string(constants.DefaultFirewallVersion), "v")
		if result != expected {
			t.Errorf("Expected %s, got %s", expected, result)
		}
	})

	t.Run("returns custom version without v prefix when specified", func(t *testing.T) {
		customVersion := "v0.5.0"
		config := &FirewallConfig{
			Enabled: true,
			Version: customVersion,
		}
		result := getAWFImageTag(config)
		expected := "0.5.0" // v prefix stripped
		if result != expected {
			t.Errorf("Expected %s, got %s", expected, result)
		}
	})

	t.Run("returns version unchanged when no v prefix present", func(t *testing.T) {
		customVersion := "0.6.0"
		config := &FirewallConfig{
			Enabled: true,
			Version: customVersion,
		}
		result := getAWFImageTag(config)
		if result != customVersion {
			t.Errorf("Expected %s, got %s", customVersion, result)
		}
	})
}

// TestClaudeEngineAWFImageTag tests that Claude engine includes image tag in AWF config JSON
func TestClaudeEngineAWFImageTag(t *testing.T) {
	t.Run("AWF config JSON includes imageTag with default version", func(t *testing.T) {
		workflowData := &WorkflowData{
			Name: "test-workflow",
			EngineConfig: &EngineConfig{
				ID: "claude",
			},
			NetworkPermissions: &NetworkPermissions{
				Firewall: &FirewallConfig{
					Enabled: true,
				},
			},
		}

		engine := NewClaudeEngine()
		steps := engine.GetExecutionSteps(workflowData, "test.log")

		if len(steps) == 0 {
			t.Fatal("Expected at least one execution step")
		}

		stepContent := strings.Join(steps[0], "\n")

		// With config file support (default AWF version), image tag is in the JSON config
		// rather than as a --image-tag CLI flag.
		expectedVersion := strings.TrimPrefix(string(constants.DefaultFirewallVersion), "v")
		if !strings.Contains(stepContent, expectedVersion) {
			t.Errorf("Expected AWF config JSON to contain version '%s'", expectedVersion)
		}
		if !strings.Contains(stepContent, "imageTag") {
			t.Error("Expected AWF config JSON to contain 'imageTag' field")
		}
	})
}

// TestCodexEngineAWFImageTag tests that Codex engine includes image tag in AWF config JSON
func TestCodexEngineAWFImageTag(t *testing.T) {
	t.Run("AWF config JSON includes imageTag with default version", func(t *testing.T) {
		workflowData := &WorkflowData{
			Name: "test-workflow",
			EngineConfig: &EngineConfig{
				ID: "codex",
			},
			NetworkPermissions: &NetworkPermissions{
				Firewall: &FirewallConfig{
					Enabled: true,
				},
			},
		}

		engine := NewCodexEngine()
		steps := engine.GetExecutionSteps(workflowData, "test.log")

		if len(steps) == 0 {
			t.Fatal("Expected at least one execution step")
		}

		stepContent := strings.Join(steps[0], "\n")

		// With config file support (default AWF version), image tag is in the JSON config
		expectedVersion := strings.TrimPrefix(string(constants.DefaultFirewallVersion), "v")
		if !strings.Contains(stepContent, expectedVersion) {
			t.Errorf("Expected AWF config JSON to contain version '%s'", expectedVersion)
		}
		if !strings.Contains(stepContent, "imageTag") {
			t.Error("Expected AWF config JSON to contain 'imageTag' field")
		}
	})
}
