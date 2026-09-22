//go:build !integration

package workflow

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	testPinnedSquid    = "registry.example.com/approved/squid:v0.28.4@sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	testPinnedAgent    = "registry.example.com/approved/agent:v0.28.4@sha256:1123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	testPinnedAPIProxy = "registry.example.com/approved/api-proxy:v0.28.4@sha256:2123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
)

// newImagesWorkflowData builds workflow data with the given image manifest and an
// AWF version that supports container.images.
func newImagesWorkflowData(images map[string]string) *WorkflowData {
	return &WorkflowData{
		EngineConfig: &EngineConfig{ID: "copilot"},
		NetworkPermissions: &NetworkPermissions{
			Firewall: &FirewallConfig{Enabled: true, Version: "v0.28.4"},
		},
		SandboxConfig: &SandboxConfig{
			Agent: &AgentSandboxConfig{
				Type:    SandboxTypeAWF,
				Version: "v0.28.4",
				Images:  images,
			},
		},
	}
}

func fullTestImageManifest() map[string]string {
	return map[string]string{
		awfImageRoleSquid:    testPinnedSquid,
		awfImageRoleAgent:    testPinnedAgent,
		awfImageRoleAPIProxy: testPinnedAPIProxy,
	}
}

func TestExtractSandboxAgentImages(t *testing.T) {
	compiler := NewCompiler()
	agentConfig := compiler.extractAgentSandboxConfig(map[string]any{
		"id": "awf",
		"images": map[string]any{
			"squid":    testPinnedSquid,
			"agent":    testPinnedAgent,
			"apiProxy": testPinnedAPIProxy,
		},
	})

	require.NotNil(t, agentConfig)
	assert.Equal(t, fullTestImageManifest(), agentConfig.Images)
}

func TestValidateSandboxAgentImages(t *testing.T) {
	t.Run("accepts a complete digest-pinned manifest", func(t *testing.T) {
		assert.NoError(t, validateSandboxAgentImages(newImagesWorkflowData(fullTestImageManifest())))
	})

	t.Run("no manifest is valid", func(t *testing.T) {
		assert.NoError(t, validateSandboxAgentImages(newImagesWorkflowData(nil)))
	})

	t.Run("rejects unknown roles", func(t *testing.T) {
		images := fullTestImageManifest()
		images["notARole"] = testPinnedSquid
		err := validateSandboxAgentImages(newImagesWorkflowData(images))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "unknown AWF image role")
	})

	t.Run("rejects references without a digest", func(t *testing.T) {
		images := fullTestImageManifest()
		images[awfImageRoleSquid] = "registry.example.com/approved/squid:v0.28.4"
		err := validateSandboxAgentImages(newImagesWorkflowData(images))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "both a tag and a sha256 digest")
	})

	t.Run("rejects references without a tag", func(t *testing.T) {
		images := fullTestImageManifest()
		images[awfImageRoleSquid] = "registry.example.com/approved/squid@sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
		err := validateSandboxAgentImages(newImagesWorkflowData(images))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "both a tag and a sha256 digest")
	})

	t.Run("rejects references that are not registry-qualified", func(t *testing.T) {
		images := fullTestImageManifest()
		images[awfImageRoleSquid] = "squid:v0.28.4@sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
		err := validateSandboxAgentImages(newImagesWorkflowData(images))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "registry-qualified")
	})

	t.Run("matches AWF canonical OCI reference grammar", func(t *testing.T) {
		valid := []string{
			"registry.example.com/approved/squid:v0.28.4@sha256:" + strings.Repeat("a", 64),
			"localhost/approved/squid:tag_1@sha256:" + strings.Repeat("b", 64),
			"registry:5000/team/image__name:v1@sha256:" + strings.Repeat("c", 64),
		}
		for _, value := range valid {
			require.NoError(t, validateAWFPinnedImageReference("sandbox.agent.images.squid", value), value)
		}

		invalid := []string{
			"approved/squid:v1@sha256:" + strings.Repeat("a", 64),
			"registry.example.com/Approved/squid:v1@sha256:" + strings.Repeat("a", 64),
			"registry.example.com/approved/squid:.v1@sha256:" + strings.Repeat("a", 64),
			"registry.example.com/approved/squid:-v1@sha256:" + strings.Repeat("a", 64),
			"registry.example.com/approved/squid:" + strings.Repeat("a", 129) + "@sha256:" + strings.Repeat("a", 64),
		}
		for _, value := range invalid {
			assert.Error(t, validateAWFPinnedImageReference("sandbox.agent.images.squid", value), value)
		}
	})

	t.Run("rejects expressions and interpolation", func(t *testing.T) {
		for _, value := range []string{
			"${{ inputs.image }}",
			"registry.example.com/approved/squid:${{ inputs.tag }}@sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
			"${REGISTRY}/approved/squid:v0.28.4@sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
		} {
			images := fullTestImageManifest()
			images[awfImageRoleSquid] = value
			err := validateSandboxAgentImages(newImagesWorkflowData(images))
			require.Error(t, err, "value %q must be rejected", value)
			assert.Contains(t, err.Error(), "literal value")
		}
	})

	t.Run("rejects an incomplete manifest", func(t *testing.T) {
		images := fullTestImageManifest()
		delete(images, awfImageRoleAPIProxy)
		err := validateSandboxAgentImages(newImagesWorkflowData(images))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "missing required role(s) apiProxy")
	})

	t.Run("requires buildTools on arc-dind topology", func(t *testing.T) {
		data := newImagesWorkflowData(fullTestImageManifest())
		data.RunnerConfig = &RunnerConfig{Topology: RunnerTopologyArcDind}
		err := validateSandboxAgentImages(data)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "missing required role(s) buildTools")

		data.SandboxConfig.Agent.Images[awfImageRoleBuildTools] = testPinnedSquid
		assert.NoError(t, validateSandboxAgentImages(data))
	})

	t.Run("requires cliProxy when raw args enable the DIFC proxy", func(t *testing.T) {
		data := newImagesWorkflowData(fullTestImageManifest())
		data.SandboxConfig.Agent.Args = []string{"--difc-proxy-host=awmg-mcpg:18443"}
		err := validateSandboxAgentImages(data)
		require.Error(t, err)
		assert.Contains(t, err.Error(), awfImageRoleCliProxy)

		data.SandboxConfig.Agent.Images[awfImageRoleCliProxy] = testPinnedSquid
		assert.NoError(t, validateSandboxAgentImages(data))
	})

	t.Run("requires the shared MCP server and selected enclave executors", func(t *testing.T) {
		data := newImagesWorkflowData(fullTestImageManifest())
		data.Enclaves = EnclavesConfig{
			&EnclaveConfig{Script: &ScriptEnclaveConfig{}},
			&EnclaveConfig{Agent: &AgentEnclaveConfig{}},
		}
		err := validateSandboxAgentImages(data)
		require.Error(t, err)
		assert.Contains(t, err.Error(), awfImageRoleEnclaveScript)
		assert.Contains(t, err.Error(), awfImageRoleEnclaveAgent)
		assert.Contains(t, err.Error(), awfImageRoleEnclaveMcpServer)

		data.SandboxConfig.Agent.Images[awfImageRoleEnclaveScript] = testPinnedSquid
		data.SandboxConfig.Agent.Images[awfImageRoleEnclaveAgent] = testPinnedAgent
		data.SandboxConfig.Agent.Images[awfImageRoleEnclaveMcpServer] = testPinnedAPIProxy
		assert.NoError(t, validateSandboxAgentImages(data))
	})

	t.Run("requires dohProxy when DNS-over-HTTPS is enabled by raw args", func(t *testing.T) {
		data := newImagesWorkflowData(fullTestImageManifest())
		data.SandboxConfig.Agent.Runtime = AgentRuntimeDockerSudoIptables
		data.SandboxConfig.Agent.Args = []string{"--dns-over-https=https://dns.google/dns-query"}
		err := validateSandboxAgentImages(data)
		require.Error(t, err)
		assert.Contains(t, err.Error(), awfImageRoleDohProxy)

		data.SandboxConfig.Agent.Images[awfImageRoleDohProxy] = testPinnedSquid
		assert.NoError(t, validateSandboxAgentImages(data))
	})

	t.Run("does not require dohProxy when strict security disables DNS-over-HTTPS", func(t *testing.T) {
		data := newImagesWorkflowData(fullTestImageManifest())
		data.SandboxConfig.Agent.Args = []string{"--dns-over-https=https://dns.google/dns-query"}
		assert.NoError(t, validateSandboxAgentImages(data))
	})

	t.Run("requires dindStaging when DinD pre-staging is enabled by raw args", func(t *testing.T) {
		for _, arg := range []string{
			"--dind-pre-stage-dirs",
			"--dind-stage-engine-binary-path=/usr/local/bin/copilot",
			"--dind-stage-engine-binary-target-path=/tmp/gh-aw/bin/copilot",
		} {
			data := newImagesWorkflowData(fullTestImageManifest())
			data.NetworkPermissions.Firewall.Args = []string{arg}
			err := validateSandboxAgentImages(data)
			require.Error(t, err, arg)
			assert.Contains(t, err.Error(), awfImageRoleDindStaging, arg)

			data.SandboxConfig.Agent.Images[awfImageRoleDindStaging] = testPinnedAgent
			assert.NoError(t, validateSandboxAgentImages(data), arg)
		}
	})

	t.Run("requires AWF v0.28.4 or newer", func(t *testing.T) {
		data := newImagesWorkflowData(fullTestImageManifest())
		data.SandboxConfig.Agent.Version = "v0.28.3"
		data.NetworkPermissions.Firewall.Version = "v0.28.3"
		err := validateSandboxAgentImages(data)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "requires AWF v0.28.4 or newer")
	})

	t.Run("accepts the router role on AWF v0.28.21 or newer", func(t *testing.T) {
		images := fullTestImageManifest()
		images[awfImageRoleRouter] = testPinnedAgent
		data := newImagesWorkflowData(images)
		data.SandboxConfig.Agent.Version = "v0.28.21"
		data.NetworkPermissions.Firewall.Version = "v0.28.21"
		assert.NoError(t, validateSandboxAgentImages(data))
	})

	t.Run("requires AWF v0.28.21 or newer for the router role", func(t *testing.T) {
		images := fullTestImageManifest()
		images[awfImageRoleRouter] = testPinnedAgent
		data := newImagesWorkflowData(images)
		data.SandboxConfig.Agent.Version = "v0.28.20"
		data.NetworkPermissions.Firewall.Version = "v0.28.20"
		err := validateSandboxAgentImages(data)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "sandbox.agent.images.router requires AWF v0.28.21 or newer")
	})

	t.Run("rejects conflicting SSL bump", func(t *testing.T) {
		data := newImagesWorkflowData(fullTestImageManifest())
		data.NetworkPermissions.Firewall.SSLBump = true
		err := validateSandboxAgentImages(data)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "cannot be combined with SSL bump")
	})

	t.Run("rejects conflicting legacy image arguments", func(t *testing.T) {
		data := newImagesWorkflowData(fullTestImageManifest())
		data.SandboxConfig.Agent.Args = []string{"--image-tag=0.28.4"}
		err := validateSandboxAgentImages(data)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "--image-tag")
	})
}

func TestBuildAWFConfigJSONWithSandboxAgentImages(t *testing.T) {
	t.Run("emits container.images and suppresses imageTag", func(t *testing.T) {
		jsonStr, err := BuildAWFConfigJSON(AWFCommandConfig{
			EngineName:     "copilot",
			AllowedDomains: "github.com",
			WorkflowData:   newImagesWorkflowData(fullTestImageManifest()),
		})
		require.NoError(t, err)

		var parsed struct {
			Container struct {
				ImageTag string            `json:"imageTag"`
				Images   map[string]string `json:"images"`
			} `json:"container"`
		}
		require.NoError(t, json.Unmarshal([]byte(jsonStr), &parsed))
		assert.Equal(t, fullTestImageManifest(), parsed.Container.Images)
		assert.Empty(t, parsed.Container.ImageTag, "imageTag conflicts with the custom image manifest")
	})

	t.Run("preserves imageTag when no manifest is configured", func(t *testing.T) {
		jsonStr, err := BuildAWFConfigJSON(AWFCommandConfig{
			EngineName:     "copilot",
			AllowedDomains: "github.com",
			WorkflowData:   newImagesWorkflowData(nil),
		})
		require.NoError(t, err)
		assert.Contains(t, jsonStr, `"imageTag"`)
		assert.NotContains(t, jsonStr, `"images"`)
	})
}
