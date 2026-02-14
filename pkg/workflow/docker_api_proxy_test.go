package workflow

import (
	"testing"

	"github.com/github/gh-aw/pkg/constants"
)

func TestCollectDockerImages_APIProxyForEnginesWithLLMGateway(t *testing.T) {
	awfImageTag := "0.16.5"

	tests := []struct {
		name           string
		engine         string
		expectAPIProxy bool
	}{
		{
			name:           "Claude engine includes api-proxy image (supports LLM gateway)",
			engine:         "claude",
			expectAPIProxy: true,
		},
		{
			name:           "Copilot engine does not include api-proxy image (no LLM gateway support)",
			engine:         "copilot",
			expectAPIProxy: false,
		},
		{
			name:           "Codex engine includes api-proxy image (supports LLM gateway)",
			engine:         "codex",
			expectAPIProxy: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			workflowData := &WorkflowData{
				AI: tt.engine,
				NetworkPermissions: &NetworkPermissions{
					Firewall: &FirewallConfig{
						Enabled: true,
						Version: awfImageTag,
					},
				},
			}

			images := collectDockerImages(nil, workflowData, ActionModeRelease)

			apiProxyImage := constants.DefaultFirewallRegistry + "/api-proxy:" + awfImageTag
			found := false
			for _, img := range images {
				if img == apiProxyImage {
					found = true
					break
				}
			}

			if found != tt.expectAPIProxy {
				if tt.expectAPIProxy {
					t.Errorf("Expected api-proxy image %q in collected images, but it was not found. Images: %v", apiProxyImage, images)
				} else {
					t.Errorf("Did not expect api-proxy image %q in collected images for engine %q, but it was found. Images: %v", apiProxyImage, tt.engine, images)
				}
			}

			// Verify squid and agent are always present for all engines with firewall
			squidImage := constants.DefaultFirewallRegistry + "/squid:" + awfImageTag
			agentImage := constants.DefaultFirewallRegistry + "/agent:" + awfImageTag
			hasSquid := false
			hasAgent := false
			for _, img := range images {
				if img == squidImage {
					hasSquid = true
				}
				if img == agentImage {
					hasAgent = true
				}
			}
			if !hasSquid {
				t.Errorf("Expected squid image %q in collected images", squidImage)
			}
			if !hasAgent {
				t.Errorf("Expected agent image %q in collected images", agentImage)
			}
		})
	}
}
