//go:build !integration

package workflow

import (
	"slices"
	"testing"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCollectDockerImages_CliProxy(t *testing.T) {
	// Use a version without "v" prefix — getAWFImageTag strips it
	awfImageTag := "0.25.20"

	t.Run("includes cli-proxy image when gh-proxy mode is enabled", func(t *testing.T) {
		workflowData := &WorkflowData{
			AI: "claude",
			NetworkPermissions: &NetworkPermissions{
				Firewall: &FirewallConfig{
					Enabled: true,
					Version: awfImageTag,
				},
			},
			Tools: map[string]any{
				"github": map[string]any{"mode": "gh-proxy"},
			},
		}

		images := collectDockerImages(nil, workflowData, ActionModeRelease)

		cliProxyImage := constants.DefaultFirewallRegistry + "/cli-proxy:" + awfImageTag
		assert.True(t, slices.Contains(images, cliProxyImage),
			"Expected cli-proxy image %q in collected images, got: %v", cliProxyImage, images)
	})

	t.Run("excludes cli-proxy image when gh-proxy mode is absent", func(t *testing.T) {
		workflowData := &WorkflowData{
			AI: "claude",
			NetworkPermissions: &NetworkPermissions{
				Firewall: &FirewallConfig{
					Enabled: true,
					Version: awfImageTag,
				},
			},
		}

		images := collectDockerImages(nil, workflowData, ActionModeRelease)

		cliProxyImage := constants.DefaultFirewallRegistry + "/cli-proxy:" + awfImageTag
		assert.False(t, slices.Contains(images, cliProxyImage),
			"Did not expect cli-proxy image %q in collected images without gh-proxy mode, got: %v", cliProxyImage, images)
	})

	t.Run("excludes cli-proxy image when AWF version is too old", func(t *testing.T) {
		workflowData := &WorkflowData{
			AI: "claude",
			NetworkPermissions: &NetworkPermissions{
				Firewall: &FirewallConfig{
					Enabled: true,
					Version: "v0.25.16", // older than AWFCliProxyMinVersion
				},
			},
			Tools: map[string]any{
				"github": map[string]any{"mode": "gh-proxy"},
			},
		}

		images := collectDockerImages(nil, workflowData, ActionModeRelease)

		// Should not include cli-proxy for an old AWF version
		for _, img := range images {
			assert.NotContains(t, img, "/cli-proxy:",
				"Should not include cli-proxy image when AWF version is too old")
		}
	})

	t.Run("cli-proxy image uses correct AWF image tag", func(t *testing.T) {
		customTag := "0.26.0"
		workflowData := &WorkflowData{
			AI: "copilot",
			NetworkPermissions: &NetworkPermissions{
				Firewall: &FirewallConfig{
					Enabled: true,
					Version: customTag,
				},
			},
			Tools: map[string]any{
				"github": map[string]any{"mode": "gh-proxy"},
			},
		}

		images := collectDockerImages(nil, workflowData, ActionModeRelease)

		expectedImage := constants.DefaultFirewallRegistry + "/cli-proxy:" + customTag
		require.True(t, slices.Contains(images, expectedImage),
			"Expected cli-proxy image %q with custom tag, got: %v", expectedImage, images)
	})
}
