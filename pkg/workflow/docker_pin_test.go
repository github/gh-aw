//go:build !integration

package workflow

import (
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestApplyContainerPins verifies that applyContainerPins substitutes
// cached digest references while leaving unpinned images unchanged.
func TestApplyContainerPins(t *testing.T) {
	imageTag := strings.TrimPrefix(string(constants.DefaultFirewallVersion), "v")
	defaultFirewallAgentImage := constants.DefaultFirewallRegistry + "/agent:" + imageTag
	defaultFirewallAgentPin, ok := getEmbeddedContainerPin(defaultFirewallAgentImage)
	require.True(t, ok, "embedded pin must exist for %s", defaultFirewallAgentImage)

	nodeLtsAlpinePin, ok := getEmbeddedContainerPin("node:lts-alpine")
	require.True(t, ok, "embedded pin must exist for node:lts-alpine")

	ghAwNodePin, ok := getEmbeddedContainerPin(constants.DefaultGhAwNodeImage)
	require.True(t, ok, "embedded pin must exist for %s", constants.DefaultGhAwNodeImage)

	tests := []struct {
		name            string
		images          []string
		pins            map[string]ContainerPin
		expectedRefs    []string
		expectedDigests []string // expected Digest field in corresponding pin entry
	}{
		{
			name:            "no pins - images returned unchanged",
			images:          []string{"example.com/custom:1.0.0", "alpine:3.20"},
			pins:            nil,
			expectedRefs:    []string{"example.com/custom:1.0.0", "alpine:3.20"},
			expectedDigests: []string{"", ""},
		},
		{
			name:            "embedded pin used when cache is absent",
			images:          []string{"node:lts-alpine"},
			pins:            nil,
			expectedRefs:    []string{nodeLtsAlpinePin.PinnedImage},
			expectedDigests: []string{nodeLtsAlpinePin.Digest},
		},
		{
			name:            "embedded firewall pin used when cache is absent",
			images:          []string{defaultFirewallAgentImage},
			pins:            nil,
			expectedRefs:    []string{defaultFirewallAgentPin.PinnedImage},
			expectedDigests: []string{defaultFirewallAgentPin.Digest},
		},
		{
			name:            "embedded gh-aw-node pin used when cache is absent",
			images:          []string{constants.DefaultGhAwNodeImage},
			pins:            nil,
			expectedRefs:    []string{ghAwNodePin.PinnedImage},
			expectedDigests: []string{ghAwNodePin.Digest},
		},
		{
			name:   "pinned image replaced with digest reference",
			images: []string{"node:lts-alpine"},
			pins: map[string]ContainerPin{
				"node:lts-alpine": {
					Image:       "node:lts-alpine",
					Digest:      "sha256:abc123",
					PinnedImage: "node:lts-alpine@sha256:abc123",
				},
			},
			expectedRefs:    []string{"node:lts-alpine@sha256:abc123"},
			expectedDigests: []string{"sha256:abc123"},
		},
		{
			name:   "only matching image is pinned",
			images: []string{"node:lts-alpine", "busybox:latest"},
			pins: map[string]ContainerPin{
				"node:lts-alpine": {
					Image:       "node:lts-alpine",
					Digest:      "sha256:abc123",
					PinnedImage: "node:lts-alpine@sha256:abc123",
				},
			},
			expectedRefs:    []string{"node:lts-alpine@sha256:abc123", "busybox:latest"},
			expectedDigests: []string{"sha256:abc123", ""},
		},
		{
			name:            "empty images list",
			images:          nil,
			pins:            nil,
			expectedRefs:    []string{},
			expectedDigests: []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var workflowData *WorkflowData
			if tt.pins != nil {
				cache := NewActionCache(t.TempDir())
				for k, v := range tt.pins {
					cache.SetContainerPin(k, v.Digest, v.PinnedImage)
				}
				workflowData = &WorkflowData{ActionCache: cache}
			}

			refs, pinEntries := applyContainerPins(tt.images, workflowData)
			require.Len(t, refs, len(tt.expectedRefs), "refs length")
			require.Len(t, pinEntries, len(tt.expectedDigests), "pin entries length")
			for i, img := range refs {
				assert.Equal(t, tt.expectedRefs[i], img, "ref at index %d", i)
				assert.Equal(t, tt.expectedDigests[i], pinEntries[i].Digest, "digest at index %d", i)
			}
		})
	}
}

// TestApplyContainerPins_DefaultFirewallVersion is a regression test for gh-aw#43307:
// all four gh-aw-firewall images at constants.DefaultFirewallVersion (including cli-proxy,
// which was new in v0.82) must have entries in the embedded pin table so that consumer
// compiles without a local cache still emit digest-pinned references.
// Using constants means the test automatically tracks version bumps.
func TestApplyContainerPins_DefaultFirewallVersion(t *testing.T) {
	imageTag := strings.TrimPrefix(string(constants.DefaultFirewallVersion), "v")
	sidecars := []string{"agent", "api-proxy", "cli-proxy", "squid"}

	for _, sidecar := range sidecars {
		image := constants.DefaultFirewallRegistry + "/" + sidecar + ":" + imageTag
		t.Run(sidecar, func(t *testing.T) {
			pin, ok := getEmbeddedContainerPin(image)
			require.True(t, ok, "embedded pin must exist for %s", image)
			require.NotEmpty(t, pin.Digest, "Digest must be non-empty for %s", image)
			require.NotEmpty(t, pin.PinnedImage, "PinnedImage must be non-empty for %s", image)

			refs, pinEntries := applyContainerPins([]string{image}, nil)
			require.Len(t, refs, 1)
			assert.Equal(t, pin.PinnedImage, refs[0], "resolved ref for %s", image)
			assert.Equal(t, pin.Digest, pinEntries[0].Digest, "digest in manifest entry for %s", image)
		})
	}
}

// TestCollectDockerImages_StoresInWorkflowData verifies that collectDockerImages
// populates workflowData.DockerImages and DockerImagePins with the collected image refs.
func TestCollectDockerImages_StoresInWorkflowData(t *testing.T) {
	workflowData := &WorkflowData{
		SandboxConfig: &SandboxConfig{
			MCP: &MCPGatewayRuntimeConfig{
				Container: constants.DefaultMCPGatewayContainer,
			},
		},
	}

	tools := map[string]any{}

	images := collectDockerImages(tools, workflowData, ActionModeRelease)

	// DockerImages on workflowData should now be populated (MCP gateway from sandbox config).
	require.NotEmpty(t, workflowData.DockerImages, "DockerImages should be populated after collectDockerImages")
	assert.Equal(t, images, workflowData.DockerImages, "DockerImages should match the returned slice")

	// DockerImagePins should also be populated with matching Image fields.
	require.NotEmpty(t, workflowData.DockerImagePins, "DockerImagePins should be populated")
	assert.Len(t, workflowData.DockerImagePins, len(workflowData.DockerImages), "pin count should match image count")
}

// TestCollectDockerImages_SandboxAgentImagesAndProjectMappings verifies that the
// closed AWF manifest is authoritative while project container_pins continue to
// transform non-AWF workflow containers.
func TestCollectDockerImages_SandboxAgentImagesAndProjectMappings(t *testing.T) {
	pinnedSquid := "registry.example.com/approved/squid:v0.28.4@sha256:" + strings.Repeat("a", 64)
	pinnedAgent := "registry.example.com/approved/agent:v0.28.4@sha256:" + strings.Repeat("b", 64)
	pinnedAPIProxy := "registry.example.com/approved/api-proxy:v0.28.4@sha256:" + strings.Repeat("c", 64)
	mappedAlpine := "mirror.example.com/alpine:latest@sha256:" + strings.Repeat("d", 64)
	mappedSquid := "mirror.example.com/squid:0.28.4@sha256:" + strings.Repeat("e", 64)

	workflowData := &WorkflowData{
		AI: "claude",
		NetworkPermissions: &NetworkPermissions{
			Firewall: &FirewallConfig{
				Enabled: true,
				Version: "0.28.4",
			},
		},
		SandboxConfig: &SandboxConfig{
			Agent: &AgentSandboxConfig{
				Images: map[string]string{
					awfImageRoleSquid:    pinnedSquid,
					awfImageRoleAgent:    pinnedAgent,
					awfImageRoleAPIProxy: pinnedAPIProxy,
				},
			},
		},
		ContainerPinMappings: map[string]string{
			constants.DefaultFirewallRegistry + "/squid:0.28.4": mappedSquid,
			constants.DefaultAlpineImage:                        mappedAlpine,
		},
	}

	require.NoError(t, validateSandboxAgentImages(workflowData))
	images := collectDockerImages(map[string]any{"agentic-workflows": map[string]any{}}, workflowData, ActionModeRelease)

	assert.Contains(t, images, pinnedSquid, "sandbox.agent.images squid override should be used for pre-pull")
	assert.Contains(t, images, pinnedAgent, "sandbox.agent.images agent override should be used for pre-pull")
	assert.Contains(t, images, pinnedAPIProxy, "sandbox.agent.images apiProxy override should be used for pre-pull")
	assert.Contains(t, images, mappedAlpine, "non-AWF container should still use the project mapping")
	assert.NotContains(t, images, mappedSquid, "project mapping must not replace an authoritative AWF manifest role")
	assert.NotContains(t, images, constants.DefaultFirewallRegistry+"/squid:0.28.4", "default squid image should not be collected when overridden")
	assert.NotContains(t, images, constants.DefaultFirewallRegistry+"/agent:0.28.4", "default agent image should not be collected when overridden")
	assert.Contains(t, workflowData.DockerImagePins, GHAWManifestContainer{Image: pinnedSquid})
	assert.Contains(t, workflowData.DockerImagePins, GHAWManifestContainer{Image: pinnedAgent})
	assert.Contains(t, workflowData.DockerImagePins, GHAWManifestContainer{Image: pinnedAPIProxy})
	assert.Contains(t, workflowData.DockerImagePins, GHAWManifestContainer{Image: mappedAlpine})
}

func TestCollectDockerImages_ManifestAndMappedContainerReferenceCollision(t *testing.T) {
	pinnedSquid := "registry.example.com/shared/image:v0.28.4@sha256:" + strings.Repeat("a", 64)
	mappedMCP := "mirror.example.com/mcp/image:v0.28.4@sha256:" + strings.Repeat("b", 64)
	workflowData := &WorkflowData{
		AI: "claude",
		NetworkPermissions: &NetworkPermissions{
			Firewall: &FirewallConfig{Enabled: true, Version: "0.28.4"},
		},
		SandboxConfig: &SandboxConfig{
			Agent: &AgentSandboxConfig{
				Images: map[string]string{
					awfImageRoleSquid:    pinnedSquid,
					awfImageRoleAgent:    testPinnedAgent,
					awfImageRoleAPIProxy: testPinnedAPIProxy,
				},
			},
		},
		ContainerPinMappings: map[string]string{pinnedSquid: mappedMCP},
	}
	tools := map[string]any{
		"custom": map[string]any{
			"type":      "stdio",
			"container": pinnedSquid,
		},
	}

	images := collectDockerImages(tools, workflowData, ActionModeRelease)

	assert.Contains(t, images, pinnedSquid, "AWF must predownload its authoritative manifest reference")
	assert.Contains(t, images, mappedMCP, "the colliding non-AWF container must still use its project mapping")
	assert.Contains(t, workflowData.DockerImagePins, GHAWManifestContainer{Image: pinnedSquid})
	assert.Contains(t, workflowData.DockerImagePins, GHAWManifestContainer{Image: mappedMCP})
}

func TestCollectDockerImages_EnabledOptionalAWFRoles(t *testing.T) {
	newData := func() *WorkflowData {
		return &WorkflowData{
			AI: "claude",
			NetworkPermissions: &NetworkPermissions{
				Firewall: &FirewallConfig{Enabled: true, Version: "0.28.4"},
			},
		}
	}

	t.Run("enclaves include the selected executor and shared MCP server", func(t *testing.T) {
		data := newData()
		data.Enclaves = EnclavesConfig{&EnclaveConfig{Script: &ScriptEnclaveConfig{}}}

		images := collectDockerImages(nil, data, ActionModeRelease)

		assert.Contains(t, images, resolveContainerImage(defaultAWFImageForRole(awfImageRoleEnclaveScript, "0.28.4"), data))
		assert.Contains(t, images, resolveContainerImage(defaultAWFImageForRole(awfImageRoleEnclaveMcpServer, "0.28.4"), data))
	})

	t.Run("raw DIFC proxy configuration includes the CLI proxy sidecar", func(t *testing.T) {
		data := newData()
		data.SandboxConfig = &SandboxConfig{
			Agent: &AgentSandboxConfig{Args: []string{"--difc-proxy-host=awmg-mcpg:18443"}},
		}

		images := collectDockerImages(nil, data, ActionModeRelease)

		assert.Contains(t, images, resolveContainerImage(defaultAWFImageForRole(awfImageRoleCliProxy, "0.28.4"), data))
	})

	t.Run("legacy DNS-over-HTTPS includes the DoH sidecar", func(t *testing.T) {
		data := newData()
		data.SandboxConfig = &SandboxConfig{
			Agent: &AgentSandboxConfig{
				Runtime: AgentRuntimeDockerSudoIptables,
				Args:    []string{"--dns-over-https=https://dns.google/dns-query"},
			},
		}

		images := collectDockerImages(nil, data, ActionModeRelease)

		assert.Contains(t, images, resolveContainerImage(defaultAWFImageForRole(awfImageRoleDohProxy, "0.28.4"), data))
	})

	t.Run("DinD pre-staging includes its helper image", func(t *testing.T) {
		data := newData()
		data.NetworkPermissions.Firewall.Args = []string{"--dind-pre-stage-dirs"}

		images := collectDockerImages(nil, data, ActionModeRelease)

		assert.Contains(t, images, resolveContainerImage(defaultAWFImageForRole(awfImageRoleDindStaging, "0.28.4"), data))
	})
}

// TestCollectDockerImages_SafeOutputsAddsGhAwNodeImage verifies that enabling
// safe-outputs adds the published gh-aw-node container to the default Docker pull
// list and manifest data, while not falling back to node:lts-alpine.
func TestCollectDockerImages_SafeOutputsAddsGhAwNodeImage(t *testing.T) {
	workflowData := &WorkflowData{
		SafeOutputs: &SafeOutputsConfig{
			CreateIssues: &CreateIssuesConfig{
				BaseSafeOutputConfig: BaseSafeOutputConfig{},
			},
		},
	}

	images := collectDockerImages(map[string]any{}, workflowData, ActionModeRelease)

	pinnedGhAwNodeImage := resolveContainerImage(constants.DefaultGhAwNodeImage, nil)
	ghAwNodePin, ok := getEmbeddedContainerPin(constants.DefaultGhAwNodeImage)
	require.True(t, ok, "embedded pin must exist for %s", constants.DefaultGhAwNodeImage)
	assert.Contains(t, images, pinnedGhAwNodeImage,
		"safe-outputs should add the gh-aw-node container image to the Docker pull list")
	require.NotEmpty(t, workflowData.DockerImagePins, "DockerImagePins should be populated")
	assert.Contains(t, workflowData.DockerImagePins, GHAWManifestContainer{
		Image:       constants.DefaultGhAwNodeImage,
		Digest:      ghAwNodePin.Digest,
		PinnedImage: ghAwNodePin.PinnedImage,
	}, "safe-outputs should add gh-aw-node to manifest container pins")

	for _, img := range images {
		assert.NotContains(t, img, constants.DefaultNodeAlpineLTSImage,
			"safe-outputs should not add node:lts-alpine (or any digest-pinned form) to the Docker pull list")
	}
}

// TestMergeDockerImages verifies deduplication when merging two slices.
func TestMergeDockerImages(t *testing.T) {
	existing := []string{"image-a", "image-b"}
	newImages := []string{"image-b", "image-c"}

	result := mergeDockerImages(existing, newImages)

	assert.Equal(t, []string{"image-a", "image-b", "image-c"}, result, "deduplicated merge")
}

// TestMergeDockerImagePins verifies deduplication when merging two GHAWManifestContainer slices.
func TestMergeDockerImagePins(t *testing.T) {
	existing := []GHAWManifestContainer{
		{Image: "image-a", Digest: "sha256:aaa"},
		{Image: "image-b"},
	}
	newPins := []GHAWManifestContainer{
		{Image: "image-b", Digest: "sha256:bbb"}, // duplicate — should not replace existing
		{Image: "image-c", Digest: "sha256:ccc"},
	}

	result := mergeDockerImagePins(existing, newPins)

	require.Len(t, result, 3, "deduplicated merge length")
	assert.Equal(t, "image-a", result[0].Image)
	assert.Equal(t, "image-b", result[1].Image)
	assert.Equal(t, "image-c", result[2].Image)
	assert.Equal(t, "sha256:ccc", result[2].Digest)
}

// TestApplyContainerPins_ContainerPinMappings verifies that applyContainerPins
// applies container_pins redirects before digest lookup so that both the
// pre-download list and the manifest entries reference the mapped image.
func TestApplyContainerPins_ContainerPinMappings(t *testing.T) {
	const digest = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	t.Run("digest-pinned mapped image returned as mapped", func(t *testing.T) {
		workflowData := &WorkflowData{
			ContainerPinMappings: map[string]string{
				"ghcr.io/owner/image:v1": "registry.acme.com/image:v1@sha256:" + digest,
			},
		}
		refs, pinEntries := applyContainerPins([]string{"ghcr.io/owner/image:v1"}, workflowData)
		require.Len(t, refs, 1)
		assert.Equal(t, "registry.acme.com/image:v1@sha256:"+digest, refs[0],
			"source image should be redirected to the mapped registry")
		assert.Equal(t, "registry.acme.com/image:v1@sha256:"+digest, pinEntries[0].Image,
			"manifest entry Image should reflect the mapped image")
		assert.Empty(t, pinEntries[0].Digest, "mapped reference already includes its digest")
	})

	t.Run("unmapped image passes through unchanged", func(t *testing.T) {
		workflowData := &WorkflowData{
			ContainerPinMappings: map[string]string{
				"other.registry.io/image:v1": "registry.acme.com/image:v1@sha256:" + digest,
			},
		}
		refs, _ := applyContainerPins([]string{"ghcr.io/owner/image:v1"}, workflowData)
		require.Len(t, refs, 1)
		assert.Equal(t, "ghcr.io/owner/image:v1", refs[0],
			"image not in mappings should be returned unchanged")
	})

	t.Run("digest-pinned non-AWF image is still eligible for mapping", func(t *testing.T) {
		source := "ghcr.io/owner/image:v1@sha256:" + digest
		mapped := "registry.acme.com/image:v1@sha256:" + strings.Repeat("a", 64)
		workflowData := &WorkflowData{
			ContainerPinMappings: map[string]string{source: mapped},
		}
		refs, _ := applyContainerPins([]string{source}, workflowData)
		require.Len(t, refs, 1)
		assert.Equal(t, mapped, refs[0])
	})

	t.Run("nil ContainerPinMappings leaves images unchanged", func(t *testing.T) {
		workflowData := &WorkflowData{}
		refs, _ := applyContainerPins([]string{"ghcr.io/owner/image:v1"}, workflowData)
		require.Len(t, refs, 1)
		assert.Equal(t, "ghcr.io/owner/image:v1", refs[0])
	})
}

// TestCollectDockerImages_DefaultAlpineContainerPinMapping verifies that when
// container_pins maps DefaultAlpineImage, collectDockerImages applies the redirect
// so the pre-download list and manifest use the configured private mirror.
func TestCollectDockerImages_DefaultAlpineContainerPinMapping(t *testing.T) {
	const digest = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	mapped := "registry.acme.com/alpine@sha256:" + digest
	workflowData := &WorkflowData{
		ContainerPinMappings: map[string]string{
			constants.DefaultAlpineImage: mapped,
		},
	}

	tools := map[string]any{
		"agentic-workflows": map[string]any{},
	}

	images := collectDockerImages(tools, workflowData, ActionModeRelease)

	assert.Contains(t, images, mapped,
		"DefaultAlpineImage should be replaced by the container_pins mapped image in the pre-download list")
	for _, img := range images {
		assert.NotEqual(t, constants.DefaultAlpineImage, img,
			"original DefaultAlpineImage should not appear in the pre-download list when mapped")
	}
}

// TestResolveGatewayContainerFromMappings verifies that resolveGatewayContainerFromMappings
// applies the container_pins redirect without discarding the digest.
func TestResolveGatewayContainerFromMappings(t *testing.T) {
	const digest = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

	t.Run("mapped image preserves digest for gateway", func(t *testing.T) {
		mappings := map[string]string{
			"ghcr.io/owner/image:v1": "registry.acme.com/image:v1@sha256:" + digest,
		}
		got := resolveGatewayContainerFromMappings("ghcr.io/owner/image:v1", mappings)
		assert.Equal(t, "registry.acme.com/image:v1@sha256:"+digest, got)
	})

	t.Run("digest-only mapping preserves digest", func(t *testing.T) {
		mapped := "registry.acme.com/image@sha256:" + digest
		mappings := map[string]string{"ghcr.io/owner/image:v1": mapped}
		assert.Equal(t, mapped, resolveGatewayContainerFromMappings("ghcr.io/owner/image:v1", mappings))
	})

	t.Run("unmapped pinned image preserves digest", func(t *testing.T) {
		image := "ghcr.io/owner/image@sha256:" + digest
		mappings := map[string]string{"other.io/image:v1": "registry.acme.com/image:v1@sha256:" + digest}
		assert.Equal(t, image, resolveGatewayContainerFromMappings(image, mappings))
	})

	t.Run("unmapped image passes through unchanged", func(t *testing.T) {
		mappings := map[string]string{
			"other.io/image:v1": "registry.acme.com/image:v1@sha256:" + digest,
		}
		got := resolveGatewayContainerFromMappings("ghcr.io/owner/image:v1", mappings)
		assert.Equal(t, "ghcr.io/owner/image:v1", got)
	})

	t.Run("nil mappings returns image unchanged", func(t *testing.T) {
		got := resolveGatewayContainerFromMappings("ghcr.io/owner/image:v1", nil)
		assert.Equal(t, "ghcr.io/owner/image:v1", got)
	})
}

func TestResolveMCPGatewayContainerImagePreservesDigest(t *testing.T) {
	image := "example.com/server:v1"
	pinned := image + "@sha256:" + strings.Repeat("a", 64)
	t.Run("explicit digest", func(t *testing.T) {
		assert.Equal(t, pinned, resolveMCPGatewayContainerImage(pinned, nil))
	})
	t.Run("cached digest", func(t *testing.T) {
		cache := NewActionCache(t.TempDir())
		cache.SetContainerPin(image, "sha256:"+strings.Repeat("a", 64), pinned)
		assert.Equal(t, pinned, resolveMCPGatewayContainerImage(image, &WorkflowData{ActionCache: cache}))
	})
	t.Run("embedded digest", func(t *testing.T) {
		pin, ok := getEmbeddedContainerPin(constants.DefaultGhAwNodeImage)
		require.True(t, ok)
		require.Contains(t, pin.PinnedImage, "@sha256:")
		assert.Equal(t, pin.PinnedImage, resolveMCPGatewayContainerImage(constants.DefaultGhAwNodeImage, nil))
	})
	t.Run("default gateway reviewed digest", func(t *testing.T) {
		image := string(constants.DefaultMCPGatewayContainer) + ":" + string(constants.DefaultMCPGatewayVersion)
		assert.Equal(t, image+"@sha256:716b41d53a20660e945c351c08140380430d65d98d3263596e45114c29fa7a43",
			resolveMCPGatewayContainerImage(image, nil))
	})
	t.Run("mapped digest", func(t *testing.T) {
		data := &WorkflowData{ContainerPinMappings: map[string]string{image: pinned}}
		assert.Equal(t, pinned, resolveMCPGatewayContainerImage(image, data))
	})
}

// resolveContainerImage redirects the source image through container_pins before
// digest lookup, ensuring the mapped registry is used in the compiled output.
func TestResolveContainerImage_AppliesContainerPinMapping(t *testing.T) {
	const digest = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	t.Run("mapped image returns digest-pinned redirected image", func(t *testing.T) {
		data := &WorkflowData{
			ContainerPinMappings: map[string]string{
				"ghcr.io/owner/img:v1": "registry.acme.com/img:v1@sha256:" + digest,
			},
		}
		got := resolveContainerImage("ghcr.io/owner/img:v1", data)
		assert.Equal(t, "registry.acme.com/img:v1@sha256:"+digest, got,
			"resolveContainerImage should return the mapped replacement image")
	})

	t.Run("mapped image does not inherit embedded pin from source", func(t *testing.T) {
		data := &WorkflowData{
			ContainerPinMappings: map[string]string{
				"node:lts-alpine": "registry.acme.com/node:lts-alpine@sha256:" + digest,
			},
		}
		got := resolveContainerImage("node:lts-alpine", data)
		assert.Equal(t, "registry.acme.com/node:lts-alpine@sha256:"+digest, got,
			"mapped image keeps its configured digest instead of the source image digest")
	})

	t.Run("unmapped image passes through to normal resolution", func(t *testing.T) {
		data := &WorkflowData{
			ContainerPinMappings: map[string]string{
				"other.registry.io/image:v1": "registry.acme.com/image:v1@sha256:" + digest,
			},
		}
		// node:lts-alpine is not in the mappings but has an embedded digest pin.
		got := resolveContainerImage("node:lts-alpine", data)
		assert.Contains(t, got, "sha256:",
			"unmapped image should still be resolved through normal digest-pin path")
	})
}
