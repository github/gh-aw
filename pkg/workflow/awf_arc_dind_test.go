//go:build !integration

package workflow

import (
	"fmt"
	"os/exec"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestArcDindDockerHostDetection(t *testing.T) {
	tests := []struct {
		name            string
		dockerHost      string
		wantDockerHost  bool
		wantDockerHostV string
	}{
		{"tcp://localhost:2375", "tcp://localhost:2375", true, "tcp://localhost:2375"},
		{"tcp://127.0.0.1:2375", "tcp://127.0.0.1:2375", true, "tcp://127.0.0.1:2375"},
		{"tcp://dind:2375 (K8s service name)", "tcp://dind:2375", true, "tcp://dind:2375"},
		{"tcp://172.30.0.5:2375 (pod IP)", "tcp://172.30.0.5:2375", true, "tcp://172.30.0.5:2375"},
		{"tcp://dind-sidecar.default.svc:2376", "tcp://dind-sidecar.default.svc:2376", true, "tcp://dind-sidecar.default.svc:2376"},
		{"unix socket (not tcp)", "unix:///var/run/docker.sock", false, ""},
		{"bare path", "/var/run/docker.sock", false, ""},
		{"empty (unset)", "", false, ""},
	}

	// Build the shell snippet from the constant (same code the compiler emits).
	scriptTemplate := fmt.Sprintf(`#!/bin/bash
export DOCKER_HOST="%%s"
GH_AW_DOCKER_HOST=""
if [[ "${DOCKER_HOST:-}" =~ %s ]]; then
  GH_AW_DOCKER_HOST="${DOCKER_HOST}"
fi
printf 'docker-host=%%%%s\n' "$GH_AW_DOCKER_HOST"
`, awfArcDindDockerHostRegex)

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			script := fmt.Sprintf(scriptTemplate, tt.dockerHost)
			cmd := exec.Command("bash", "-c", script)
			out, err := cmd.CombinedOutput()
			require.NoError(t, err, "bash script should succeed, output: %s", string(out))

			gotDockerHost := strings.TrimPrefix(strings.TrimSpace(string(out)), "docker-host=")
			if tt.wantDockerHost {
				assert.Equal(t, tt.wantDockerHostV, gotDockerHost,
					"expected docker host passthrough value to be set for DOCKER_HOST=%s", tt.dockerHost)
			} else {
				assert.Empty(t, gotDockerHost,
					"expected docker host passthrough value to NOT be set for DOCKER_HOST=%s", tt.dockerHost)
			}
		})
	}
}

func TestBuildAWFCommand_IncludesChrootInjectScript(t *testing.T) {
	t.Run("chroot inject script present when AWF version supports it", func(t *testing.T) {
		config := AWFCommandConfig{
			EngineName:    "copilot",
			EngineCommand: "copilot --prompt-file /tmp/prompt.txt",
			LogFile:       "/tmp/gh-aw/agent-stdio.log",
			WorkflowData: &WorkflowData{
				EngineConfig: &EngineConfig{ID: "copilot"},
				NetworkPermissions: &NetworkPermissions{
					Firewall: &FirewallConfig{
						Enabled: true,
						Version: string(constants.AWFChrootConfigMinVersion),
					},
				},
			},
		}

		command := BuildAWFCommand(config)
		assert.Contains(t, command, awfArcDindChrootBinariesSourcePath,
			"command should include the expected binariesSourcePath constant")
		assert.Contains(t, command, awfArcDindChrootIdentityHome,
			"command should include the expected identity.home constant")
		assert.Contains(t, command, `node "${RUNNER_TEMP}/gh-aw/actions/patch_awf_chroot_config.cjs"`,
			"command should invoke the repository JavaScript helper for chroot config patching")
		assert.NotContains(t, command, "python3 - <<'PY'",
			"command should not inject an inline Python heredoc")
		assert.Contains(t, command, awfArcDindDockerHostRegex,
			"chroot inject script should reuse the DinD Docker host regex")
		// Structural: the chroot injection must appear *after* the DOCKER_HOST guard,
		// confirming it is nested inside the if-block and not emitted at top level.
		dockerhostIdx := strings.Index(command, awfArcDindDockerHostRegex)
		helperIdx := strings.Index(command, "patch_awf_chroot_config.cjs")
		assert.Greater(t, helperIdx, dockerhostIdx,
			"chroot injection must appear after the DOCKER_HOST guard in the generated script")
	})

	t.Run("chroot inject script absent when AWF version too old", func(t *testing.T) {
		config := AWFCommandConfig{
			EngineName:    "copilot",
			EngineCommand: "copilot --prompt-file /tmp/prompt.txt",
			LogFile:       "/tmp/gh-aw/agent-stdio.log",
			WorkflowData: &WorkflowData{
				EngineConfig: &EngineConfig{ID: "copilot"},
				NetworkPermissions: &NetworkPermissions{
					Firewall: &FirewallConfig{
						Enabled: true,
						Version: "v0.27.0",
					},
				},
			},
		}
		command := BuildAWFCommand(config)
		assert.NotContains(t, command, "binariesSourcePath",
			"command should NOT include chroot inject script for old AWF version")
	})
}

func TestBuildArcDindChrootConfigPatchBodyBashWritesConfigOnce(t *testing.T) {
	body := buildArcDindChrootConfigPatchBodyBash()

	assert.Equal(t, 1, strings.Count(body, `> "${RUNNER_TEMP}/gh-aw/awf-config.json"`))
}

func TestRewriteArcDindPath(t *testing.T) {
	t.Run("rewrites tmp gh-aw prefix", func(t *testing.T) {
		assert.Equal(t, "${RUNNER_TEMP}/gh-aw/aw-prompts/prompt.txt", rewriteArcDindPath("/tmp/gh-aw/aw-prompts/prompt.txt"))
	})

	t.Run("rewrites multiple occurrences", func(t *testing.T) {
		input := "/tmp/gh-aw/a /tmp/gh-aw/b"
		expected := "${RUNNER_TEMP}/gh-aw/a ${RUNNER_TEMP}/gh-aw/b"
		assert.Equal(t, expected, rewriteArcDindPath(input))
	})

	t.Run("leaves unrelated paths unchanged", func(t *testing.T) {
		assert.Equal(t, "/tmp/not-gh-aw/file.txt", rewriteArcDindPath("/tmp/not-gh-aw/file.txt"))
	})
}

func TestRewriteArcDindEngineCommand(t *testing.T) {
	command := "copilot --prompt-file /tmp/gh-aw/aw-prompts/prompt.txt"
	rewritten := rewriteArcDindEngineCommand(command)

	assert.Contains(t, rewritten, "export HOME=${RUNNER_TEMP}/gh-aw/home")
	assert.Contains(t, rewritten, "copilot --prompt-file ${RUNNER_TEMP}/gh-aw/aw-prompts/prompt.txt")
}

func TestBuildAWFImageTagWithDigests(t *testing.T) {
	t.Run("includes digest metadata for known firewall images", func(t *testing.T) {
		imageTag := strings.TrimPrefix(string(constants.DefaultFirewallVersion), "v")
		tag := buildAWFImageTagWithDigests(imageTag, nil)

		assert.Contains(t, tag, imageTag, "should keep original AWF tag")
		assert.Contains(t, tag, "squid=sha256:", "should include squid digest metadata")
		assert.Contains(t, tag, "agent=sha256:", "should include agent digest metadata")
		assert.Contains(t, tag, "api-proxy=sha256:", "should include api-proxy digest metadata")
		assert.Contains(t, tag, "cli-proxy=sha256:", "should include cli-proxy digest metadata")
	})

	t.Run("leaves tag unchanged when digests are unavailable", func(t *testing.T) {
		tag := buildAWFImageTagWithDigests("0.0.1", nil)
		assert.Equal(t, "0.0.1", tag, "should not append digest metadata when no pins are available")
	})

	t.Run("includes build-tools digest for arc-dind topology", func(t *testing.T) {
		imageTag := strings.TrimPrefix(string(constants.DefaultFirewallVersion), "v")
		buildToolsImage := constants.DefaultFirewallRegistry + "/build-tools:" + imageTag
		cache := &ActionCache{ContainerPins: make(map[string]ContainerPin)}
		cache.SetContainerPin(
			buildToolsImage,
			"sha256:1111111111111111111111111111111111111111111111111111111111111111",
			buildToolsImage+"@sha256:1111111111111111111111111111111111111111111111111111111111111111",
		)
		workflowData := &WorkflowData{
			RunnerConfig: &RunnerConfig{Topology: RunnerTopologyArcDind},
			ActionCache:  cache,
		}
		tag := buildAWFImageTagWithDigests(imageTag, workflowData)

		assert.Contains(t, tag, "build-tools=sha256:", "should include build-tools digest metadata for arc-dind topology")
	})

	t.Run("excludes build-tools digest without arc-dind topology", func(t *testing.T) {
		imageTag := strings.TrimPrefix(string(constants.DefaultFirewallVersion), "v")
		tag := buildAWFImageTagWithDigests(imageTag, nil)

		assert.NotContains(t, tag, "build-tools=", "should not include build-tools digest metadata without arc-dind topology")
	})
}

func TestBuildAWFArgs_ImageTagIncludesDigests(t *testing.T) {
	// Use the default firewall version so this test tracks pin/version updates.
	config := AWFCommandConfig{
		EngineName:     "copilot",
		AllowedDomains: "github.com",
		WorkflowData: &WorkflowData{
			EngineConfig: &EngineConfig{ID: "copilot"},
			NetworkPermissions: &NetworkPermissions{
				Firewall: &FirewallConfig{Enabled: true, Version: string(constants.DefaultFirewallVersion)},
			},
		},
	}

	// When the AWF version supports --config (default), --image-tag moves to the JSON config file.
	// Verify the config file JSON contains the image tag with digest metadata.
	awfConfigJSON, err := BuildAWFConfigJSON(config)
	require.NoError(t, err, "BuildAWFConfigJSON should not error")
	assert.Contains(t, awfConfigJSON, "imageTag", "expected imageTag in AWF config JSON")
	assert.Contains(t, awfConfigJSON, "squid=sha256:", "expected squid digest metadata in AWF config JSON")
	assert.Contains(t, awfConfigJSON, "agent=sha256:", "expected agent digest metadata in AWF config JSON")
	assert.Contains(t, awfConfigJSON, "api-proxy=sha256:", "expected api-proxy digest metadata in AWF config JSON")

	// --image-tag should NOT appear in the CLI args (it's in the config file).
	args := BuildAWFArgs(config)
	argsStr := strings.Join(args, " ")
	assert.NotContains(t, argsStr, "--image-tag", "expected --image-tag to be absent from CLI args when config file is used")
}

func TestBuildAWFCommand_ArcDindPreCreatesMountDirs(t *testing.T) {
	config := AWFCommandConfig{
		EngineName:    "copilot",
		EngineCommand: "copilot run",
		LogFile:       "/tmp/log.txt",
		PathSetup:     "export PATH=/usr/bin:$PATH",
		WorkflowData: &WorkflowData{
			Name:            "Test",
			AI:              "copilot",
			MarkdownContent: "test",
			RunnerConfig:    &RunnerConfig{Topology: RunnerTopologyArcDind},
			SandboxConfig: &SandboxConfig{
				Agent: &AgentSandboxConfig{ID: "awf"},
			},
		},
	}

	command := BuildAWFCommand(config)

	// Verify mount source directories are pre-created before AWF invocation
	assert.Contains(t, command, `mkdir -p "${RUNNER_TEMP}/gh-aw/home" "${RUNNER_TEMP}/gh-aw/sandbox/agent"`,
		"should pre-create rw mount source directories for arc-dind")

	// Verify the mounts themselves are present
	assert.Contains(t, command, `--mount "${RUNNER_TEMP}/gh-aw/home:${RUNNER_TEMP}/gh-aw/home:rw"`)
	assert.Contains(t, command, `--mount "${RUNNER_TEMP}/gh-aw/sandbox/agent:${RUNNER_TEMP}/gh-aw/sandbox/agent:rw"`)
	assert.Contains(t, command, `export GH_AW_HARNESS_TELEMETRY_DIR="${RUNNER_TEMP}/gh-aw/sandbox/agent"`)
}
