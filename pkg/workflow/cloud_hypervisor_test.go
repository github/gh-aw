//go:build !integration

package workflow

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGenerateCloudHypervisorSetupSteps(t *testing.T) {
	t.Run("KVM access step", func(t *testing.T) {
		step := generateCloudHypervisorKVMAccessStep()
		require.NotEmpty(t, step)
		content := strings.Join(step, "\n")
		assert.Contains(t, content, "Grant runner access to KVM")
		assert.Contains(t, content, "cloud_hypervisor_kvm_access.sh")
	})

	t.Run("host preflight step", func(t *testing.T) {
		step := generateCloudHypervisorHostPreflightStep()
		require.NotEmpty(t, step)
		content := strings.Join(step, "\n")
		assert.Contains(t, content, "Check host eligibility for cloud-hypervisor")
		assert.Contains(t, content, "cloud_hypervisor_host_preflight.sh")
	})

	t.Run("bundle setup step", func(t *testing.T) {
		step := generateCloudHypervisorBundleSetupStep("v0.28.1")
		require.NotEmpty(t, step)
		content := strings.Join(step, "\n")
		assert.Contains(t, content, "Download and verify cloud-hypervisor bundle")
		assert.Contains(t, content, "id: cloud-hypervisor-bundle")
		assert.Contains(t, content, "GH_AW_AWF_VERSION: v0.28.1")
		assert.Contains(t, content, "cloud_hypervisor_setup_bundle.sh")
	})
}

func TestCloudHypervisorInstallStepOrderInBuildNpmEngineInstallStepsWithAWF(t *testing.T) {
	workflowData := &WorkflowData{
		SandboxConfig:      &SandboxConfig{Agent: &AgentSandboxConfig{ID: "awf", Runtime: AgentRuntimeCloudHypervisor}},
		NetworkPermissions: &NetworkPermissions{Firewall: &FirewallConfig{Enabled: true}},
	}

	steps := BuildNpmEngineInstallStepsWithAWF(nil, workflowData)
	require.NotEmpty(t, steps)

	kvmAccessIdx := -1
	preflightIdx := -1
	bundleIdx := -1
	awfIdx := -1
	awfInstallContent := ""
	for i, step := range steps {
		content := strings.Join(step, "\n")
		switch {
		case strings.Contains(content, "Grant runner access to KVM"):
			kvmAccessIdx = i
		case strings.Contains(content, "Check host eligibility for cloud-hypervisor"):
			preflightIdx = i
		case strings.Contains(content, "Download and verify cloud-hypervisor bundle"):
			bundleIdx = i
		case strings.Contains(content, "install_awf_binary.sh"):
			awfIdx = i
			awfInstallContent = content
		}
	}

	require.NotEqual(t, -1, kvmAccessIdx)
	require.NotEqual(t, -1, preflightIdx)
	require.NotEqual(t, -1, bundleIdx)
	require.NotEqual(t, -1, awfIdx)
	assert.Less(t, kvmAccessIdx, preflightIdx)
	assert.Less(t, preflightIdx, bundleIdx)
	assert.Less(t, bundleIdx, awfIdx)
	assert.NotContains(t, awfInstallContent, "--rootless")
}

func TestCloudHypervisorAWFArgs(t *testing.T) {
	config := AWFCommandConfig{
		EngineName: "copilot",
		WorkflowData: &WorkflowData{
			EngineConfig: &EngineConfig{ID: "copilot"},
			NetworkPermissions: &NetworkPermissions{
				Firewall: &FirewallConfig{Enabled: true, Version: string(constants.AWFCloudHypervisorMinVersion)},
			},
			SandboxConfig: &SandboxConfig{Agent: &AgentSandboxConfig{ID: "awf", Runtime: AgentRuntimeCloudHypervisor}},
		},
	}

	args := strings.Join(BuildAWFArgs(config), " ")
	assert.Contains(t, args, "--container-runtime cloud-hypervisor")
	assert.Contains(t, args, "--cloud-hypervisor-preview")
	assert.Contains(t, args, "--cloud-hypervisor-vcpus 2")
	assert.Contains(t, args, "--cloud-hypervisor-memory-mib 4096")
	assert.NotContains(t, args, "${{ steps.cloud-hypervisor-bundle.outputs.")
	assert.NotContains(t, args, "--mount")
}

func TestCloudHypervisorAWFCommandOmitsUnsupportedMountsAndTTY(t *testing.T) {
	config := AWFCommandConfig{
		EngineName: "claude",
		UsesTTY:    true,
		WorkflowData: &WorkflowData{
			EngineConfig: &EngineConfig{ID: "claude"},
			NetworkPermissions: &NetworkPermissions{
				Firewall: &FirewallConfig{Enabled: true, Version: string(constants.AWFCloudHypervisorMinVersion)},
			},
			SandboxConfig: &SandboxConfig{Agent: &AgentSandboxConfig{
				ID:      "awf",
				Runtime: AgentRuntimeCloudHypervisor,
				Mounts:  []string{"/tmp/custom:/tmp/custom"},
			}},
		},
	}

	command := BuildAWFCommand(config)
	assert.Contains(t, command, "sudo --preserve-env awf")
	assert.Contains(t, command, `--cloud-hypervisor-artifact-manifest "${GH_AW_CLOUD_HYPERVISOR_ARTIFACT_MANIFEST}"`)
	assert.Contains(t, command, `--cloud-hypervisor-artifact-manifest-bundle "${GH_AW_CLOUD_HYPERVISOR_ARTIFACT_MANIFEST_BUNDLE}"`)
	assert.Contains(t, command, `--cloud-hypervisor-artifact-release-tag "${GH_AW_CLOUD_HYPERVISOR_ARTIFACT_RELEASE_TAG}"`)
	assert.NotContains(t, command, "--cloud-hypervisor-virtiofsd-sha256")
	assert.NotContains(t, command, "development-allow-unattested-artifacts")
	assert.NotContains(t, command, "--mount")
	assert.NotContains(t, command, "--tty")
	assert.NotContains(t, command, "--legacy-security")
	assert.NotContains(t, command, "--enable-host-access")
}

func TestCloudHypervisorAWFCommandEmitsAwfHomeMkdirBeforeInvocation(t *testing.T) {
	config := AWFCommandConfig{
		EngineName: "copilot",
		WorkflowData: &WorkflowData{
			EngineConfig: &EngineConfig{ID: "copilot"},
			NetworkPermissions: &NetworkPermissions{
				Firewall: &FirewallConfig{Enabled: true, Version: string(constants.AWFCloudHypervisorFilesystemAllowWriteMinVersion)},
			},
			SandboxConfig: &SandboxConfig{Agent: &AgentSandboxConfig{
				ID:      "awf",
				Runtime: AgentRuntimeCloudHypervisor,
				Config: &SandboxRuntimeConfig{
					Filesystem: &SRTFilesystemConfig{
						AllowWrite: []string{"/workspace", "/workspace/.awf-home", "/tmp/gh-aw/agent"},
					},
				},
			}},
		},
	}

	command := BuildAWFCommand(config)
	// /workspace already exists (checked out by actions/checkout) and must not be mkdir'd.
	assert.NotContains(t, command, `mkdir -p "${GITHUB_WORKSPACE}"`+"\n")
	assert.Contains(t, command, `mkdir -p "${GITHUB_WORKSPACE}/.awf-home" "/tmp/gh-aw/agent"`)
	assert.Contains(t, command, `if GH_AW_GIT_EXCLUDE="$(git -C "${GITHUB_WORKSPACE}" rev-parse --path-format=absolute --git-path info/exclude 2>/dev/null)"; then`)
	assert.Contains(t, command, `if ! grep -qxF -- '/.awf-home/' "$GH_AW_GIT_EXCLUDE"`)

	mkdirIdx := strings.Index(command, "mkdir -p")
	excludeIdx := strings.Index(command, `printf '\n%s\n' '/.awf-home/'`)
	configWriteIdx := strings.Index(command, "cp \"")
	require.NotEqual(t, -1, mkdirIdx)
	require.NotEqual(t, -1, excludeIdx)
	require.NotEqual(t, -1, configWriteIdx)
	assert.Less(t, mkdirIdx, excludeIdx)
	assert.Less(t, excludeIdx, configWriteIdx, "HOME exclusion must run before the AWF config file is finalized")
}

func TestCloudHypervisorAWFCommandOmitsAwfHomeExcludeWithoutHomeSetup(t *testing.T) {
	config := AWFCommandConfig{
		EngineName: "copilot",
		WorkflowData: &WorkflowData{
			EngineConfig: &EngineConfig{ID: "copilot"},
			NetworkPermissions: &NetworkPermissions{
				Firewall: &FirewallConfig{Enabled: true, Version: string(constants.AWFFilesystemAllowWriteMinVersion)},
			},
			SandboxConfig: &SandboxConfig{Agent: &AgentSandboxConfig{
				ID:      "awf",
				Runtime: AgentRuntimeCloudHypervisor,
				Config: &SandboxRuntimeConfig{
					Filesystem: &SRTFilesystemConfig{
						AllowWrite: []string{"/workspace", "/workspace/.awf-home", "/tmp/gh-aw/agent"},
					},
				},
			}},
		},
	}

	command := BuildAWFCommand(config)
	assert.NotContains(t, command, "mkdir -p \"${GITHUB_WORKSPACE}/.awf-home\"")
	assert.NotContains(t, command, "GH_AW_GIT_EXCLUDE")

	config.WorkflowData.NetworkPermissions.Firewall.Version = string(constants.AWFCloudHypervisorFilesystemAllowWriteMinVersion)
	config.WorkflowData.SandboxConfig.Agent.Runtime = AgentRuntimeDocker
	command = BuildAWFCommand(config)
	assert.NotContains(t, command, "GH_AW_GIT_EXCLUDE")

	config.WorkflowData.SandboxConfig.Agent.Runtime = AgentRuntimeCloudHypervisor
	config.WorkflowData.SandboxConfig.Agent.Config.Filesystem.AllowWrite = []string{"/workspace", "/tmp/gh-aw/agent"}
	command = BuildAWFCommand(config)
	assert.NotContains(t, command, "GH_AW_GIT_EXCLUDE")
}

func TestCloudHypervisorHomeGitExclude(t *testing.T) {
	workflowData := &WorkflowData{
		NetworkPermissions: &NetworkPermissions{
			Firewall: &FirewallConfig{Enabled: true, Version: string(constants.AWFCloudHypervisorFilesystemAllowWriteMinVersion)},
		},
		SandboxConfig: &SandboxConfig{Agent: &AgentSandboxConfig{
			ID:      "awf",
			Runtime: AgentRuntimeCloudHypervisor,
			Config: &SandboxRuntimeConfig{
				Filesystem: &SRTFilesystemConfig{AllowWrite: []string{cloudHypervisorAwfHomeWritePath}},
			},
		}},
	}
	script := buildCloudHypervisorFilesystemMkdirScript(workflowData)
	require.NotEmpty(t, script)

	t.Run("not a git repository", func(t *testing.T) {
		workspace := t.TempDir()
		cmd := exec.Command("bash", "-e", "-c", script+"\nprintf 'setup complete\\n'")
		cmd.Env = append(os.Environ(), "GITHUB_WORKSPACE="+workspace)
		output, err := cmd.CombinedOutput()
		require.NoError(t, err, "%s", output)
		assert.Equal(t, "setup complete\n", string(output))
		info, err := os.Stat(filepath.Join(workspace, ".awf-home"))
		require.NoError(t, err)
		assert.True(t, info.IsDir())
		assert.NoDirExists(t, filepath.Join(workspace, ".git"))
		assert.NoFileExists(t, filepath.Join(workspace, "info", "exclude"))
	})

	for _, worktree := range []bool{false, true} {
		for _, tc := range []struct {
			name     string
			existing string
			missing  bool
		}{
			{name: "missing", missing: true},
			{name: "empty"},
			{name: "existing entries", existing: "# local excludes\n/local-cache/\n"},
			{name: "no trailing newline", existing: "# local excludes\n/local-cache/"},
			{name: "already excluded", existing: "/.awf-home/\n"},
			{name: "already excluded without newline", existing: "/.awf-home/"},
		} {
			t.Run(fmt.Sprintf("worktree=%t/%s", worktree, tc.name), func(t *testing.T) {
				repoDir := t.TempDir()
				git := func(args ...string) string {
					t.Helper()
					cmd := exec.Command("git", append([]string{"-C", repoDir}, args...)...)
					output, err := cmd.CombinedOutput()
					require.NoError(t, err, "git %v: %s", args, output)
					return string(output)
				}
				git("init")
				require.NoError(t, os.WriteFile(filepath.Join(repoDir, "source.txt"), []byte("before\n"), 0o644))
				git("add", "source.txt")
				git("-c", "user.name=Test", "-c", "user.email=test@example.com", "commit", "-m", "Initial source")
				if worktree {
					worktreeDir := filepath.Join(t.TempDir(), "checkout with spaces")
					git("worktree", "add", "-b", "agent", worktreeDir)
					repoDir = worktreeDir
					info, err := os.Stat(filepath.Join(repoDir, ".git"))
					require.NoError(t, err)
					require.False(t, info.IsDir())
				}
				excludePath := strings.TrimSpace(git("rev-parse", "--path-format=absolute", "--git-path", "info/exclude"))
				if tc.missing {
					require.NoError(t, os.Remove(excludePath))
				} else {
					require.NoError(t, os.WriteFile(excludePath, []byte(tc.existing), 0o644))
				}
				runSetup := func() {
					t.Helper()
					cmd := exec.Command("bash", "-e", "-c", script)
					cmd.Env = append(os.Environ(), "GITHUB_WORKSPACE="+repoDir)
					output, err := cmd.CombinedOutput()
					require.NoError(t, err, "%s", output)
				}
				runSetup()
				exclude, err := os.ReadFile(excludePath)
				require.NoError(t, err)
				assert.True(t, strings.HasPrefix(string(exclude), tc.existing), "existing excludes must be preserved")
				assert.Equal(t, 1, strings.Count(string(exclude), "/.awf-home/"))
				assert.Contains(t, strings.Split(string(exclude), "\n"), "/.awf-home/")
				runSetup()
				repeatedExclude, err := os.ReadFile(excludePath)
				require.NoError(t, err)
				assert.Equal(t, exclude, repeatedExclude, "setup must be idempotent")

				homeCache := filepath.Join(repoDir, ".awf-home", ".cache", "uv")
				require.NoError(t, os.MkdirAll(homeCache, 0o755))
				require.NoError(t, os.WriteFile(filepath.Join(homeCache, "state"), []byte("tool state\n"), 0o644))
				require.NoError(t, os.WriteFile(filepath.Join(repoDir, "source.txt"), []byte("after\n"), 0o644))
				nestedHome := filepath.Join(repoDir, "nested", ".awf-home")
				require.NoError(t, os.MkdirAll(nestedHome, 0o755))
				require.NoError(t, os.WriteFile(filepath.Join(nestedHome, "source.txt"), []byte("intentional source\n"), 0o644))
				assert.Equal(t, "nested/.awf-home/source.txt\n", git("ls-files", "--others", "--exclude-standard"))
				git("add", "-A")
				assert.Equal(t, "nested/.awf-home/source.txt\nsource.txt\n", git("diff", "--cached", "--name-only"))
				git("-c", "user.name=Test", "-c", "user.email=test@example.com", "commit", "-m", "Intentional changes")
				patch := git("format-patch", "-1", "--stdout")
				assert.Contains(t, patch, "+after")
				assert.Contains(t, patch, "+intentional source")
				assert.NotContains(t, patch, "tool state")
				assert.NotContains(t, patch, ".cache/uv")
			})
		}
	}
}

func TestCloudHypervisorFirewallLogsUsePrivilegedMode(t *testing.T) {
	workflowData := &WorkflowData{
		SandboxConfig: &SandboxConfig{Agent: &AgentSandboxConfig{
			ID:      "awf",
			Runtime: AgentRuntimeCloudHypervisor,
		}},
	}

	step := generateFirewallLogParsingStep("cloud-hypervisor", workflowData)
	assert.NotContains(t, strings.Join(step, "\n"), "--rootless")
}

func TestCloudHypervisorAWFConfigJSON(t *testing.T) {
	config := AWFCommandConfig{
		EngineName:     "copilot",
		AllowedDomains: "github.com",
		WorkflowData: &WorkflowData{
			EngineConfig:   &EngineConfig{ID: "copilot"},
			TimeoutMinutes: "timeout-minutes: 60",
			NetworkPermissions: &NetworkPermissions{
				Firewall: &FirewallConfig{Enabled: true},
			},
			Tools: map[string]any{"github": map[string]any{"mode": "gh-proxy"}},
			SandboxConfig: applySandboxDefaults(
				&SandboxConfig{Agent: &AgentSandboxConfig{ID: "awf", Runtime: AgentRuntimeCloudHypervisor}},
				&EngineConfig{ID: "copilot"},
			),
		},
	}

	jsonStr, err := BuildAWFConfigJSON(config)
	require.NoError(t, err)
	assert.NotContains(t, jsonStr, `"containerRuntime"`)
	assert.NotContains(t, jsonStr, "host.docker.internal")
	assert.Contains(t, jsonStr, `"isolation":true`)
	assert.Contains(t, jsonStr, `"topologyAttach":["awmg-mcpg"]`)
	assert.NotContains(t, jsonStr, "awmg-cli-proxy")
	assert.Contains(t, jsonStr, `"agentTimeout":60`)
	assert.Contains(t, jsonStr, `"cloudHypervisor":{"previewEnabled":true,"mountPolicy":"workspace-and-tool-cache","vcpuCount":2,"memoryMib":4096}`)
	assert.Contains(t, jsonStr, `"allowWrite":["/tmp/gh-aw/agent","/tmp/gh-aw/sandbox/agent/logs","/workspace","/workspace/.awf-home"]`)
}

func TestCloudHypervisorValidationArcDindIncompatible(t *testing.T) {
	workflowData := &WorkflowData{
		SandboxConfig: &SandboxConfig{Agent: &AgentSandboxConfig{ID: "awf", Runtime: AgentRuntimeCloudHypervisor}},
		RunnerConfig:  &RunnerConfig{Topology: RunnerTopologyArcDind},
		NetworkPermissions: &NetworkPermissions{
			Firewall: &FirewallConfig{Enabled: true},
		},
		Tools: map[string]any{"github": map[string]any{"mode": "remote"}},
	}

	err := validateSandboxConfig(workflowData)
	require.Error(t, err)
	require.ErrorContains(t, err, "arc-dind")
	require.ErrorContains(t, err, "cloud-hypervisor")
}

func TestCloudHypervisorValidationRequiresPreviewVersion(t *testing.T) {
	workflowData := &WorkflowData{
		SandboxConfig: &SandboxConfig{Agent: &AgentSandboxConfig{
			ID:      "awf",
			Runtime: AgentRuntimeCloudHypervisor,
			Version: string(constants.AWFCloudHypervisorMinVersion),
		}},
		NetworkPermissions: &NetworkPermissions{
			Firewall: &FirewallConfig{Enabled: true},
		},
		Tools: map[string]any{"github": map[string]any{"mode": "remote"}},
	}

	require.NoError(t, validateSandboxConfig(workflowData))

	workflowData.SandboxConfig.Agent.Version = "v0.27.44"
	err := validateSandboxConfig(workflowData)
	require.Error(t, err)
	require.ErrorContains(t, err, string(constants.AWFCloudHypervisorMinVersion))

	workflowData.SandboxConfig.Agent.Version = "v0.28.10"
	err = validateSandboxConfig(workflowData)
	require.Error(t, err)
	require.ErrorContains(t, err, string(constants.AWFCloudHypervisorMinVersion))
}

func TestCloudHypervisorValidationRejectsGHProxy(t *testing.T) {
	workflowData := &WorkflowData{
		SandboxConfig: &SandboxConfig{Agent: &AgentSandboxConfig{
			ID:      "awf",
			Runtime: AgentRuntimeCloudHypervisor,
			Version: string(constants.AWFCloudHypervisorMinVersion),
		}},
		NetworkPermissions: &NetworkPermissions{
			Firewall: &FirewallConfig{Enabled: true, Version: string(constants.AWFCloudHypervisorMinVersion)},
		},
		Tools: map[string]any{"github": map[string]any{"mode": "gh-proxy"}},
	}

	err := validateSandboxConfig(workflowData)
	require.Error(t, err)
	require.ErrorContains(t, err, "gh-proxy")
	require.ErrorContains(t, err, "cloud-hypervisor")
}

func TestCloudHypervisorValidationRejectsAllowHostPorts(t *testing.T) {
	workflowData := &WorkflowData{
		SandboxConfig: &SandboxConfig{Agent: &AgentSandboxConfig{
			ID:             "awf",
			Runtime:        AgentRuntimeCloudHypervisor,
			Version:        string(constants.AWFCloudHypervisorMinVersion),
			AllowHostPorts: []int{8080},
		}},
		NetworkPermissions: &NetworkPermissions{
			Firewall: &FirewallConfig{Enabled: true, Version: string(constants.AWFCloudHypervisorMinVersion)},
		},
		Tools: map[string]any{"github": map[string]any{"mode": "remote"}},
	}

	err := validateSandboxConfig(workflowData)
	require.Error(t, err)
	require.ErrorContains(t, err, "allow-host-ports")
	require.ErrorContains(t, err, "cloud-hypervisor")
}

func TestCloudHypervisorValidationRejectsEnclaves(t *testing.T) {
	workflowData := &WorkflowData{
		SandboxConfig: &SandboxConfig{Agent: &AgentSandboxConfig{
			ID:      "awf",
			Runtime: AgentRuntimeCloudHypervisor,
			Version: string(constants.AWFCloudHypervisorMinVersion),
		}},
		NetworkPermissions: &NetworkPermissions{
			Firewall: &FirewallConfig{Enabled: true, Version: string(constants.AWFCloudHypervisorMinVersion)},
		},
		Tools:    map[string]any{"github": map[string]any{"mode": "remote"}},
		Enclaves: EnclavesConfig{{}},
	}

	err := validateSandboxConfig(workflowData)
	require.Error(t, err)
	require.ErrorContains(t, err, "enclaves")
	require.ErrorContains(t, err, "cloud-hypervisor")
}

func TestCloudHypervisorFrontmatterExtraction(t *testing.T) {
	workflowsDir := t.TempDir()

	markdown := `---
on:
  workflow_dispatch:
engine: copilot
strict: false
timeout-minutes: 60
sandbox:
  agent:
    id: awf
    runtime: cloud-hypervisor
    version: v0.28.11
---

# Test cloud-hypervisor Runtime
`

	testFile := filepath.Join(workflowsDir, "test-cloud-hypervisor.md")
	err := os.WriteFile(testFile, []byte(markdown), 0o644)
	require.NoError(t, err)

	compiler := NewCompiler()
	err = compiler.CompileWorkflow(testFile)
	require.NoError(t, err)

	lockContent, err := os.ReadFile(filepath.Join(workflowsDir, "test-cloud-hypervisor.lock.yml"))
	require.NoError(t, err)
	lockStr := string(lockContent)

	assert.Contains(t, lockStr, "Grant runner access to KVM")
	assert.Contains(t, lockStr, "Check host eligibility for cloud-hypervisor")
	assert.Contains(t, lockStr, "Download and verify cloud-hypervisor bundle")
	assert.Contains(t, lockStr, "GH_AW_AWF_VERSION: v0.28.11")
	assert.Contains(t, lockStr, "sudo --preserve-env awf")
	assert.NotContains(t, lockStr, `install_awf_binary.sh" v0.28.1 --rootless`)
	assert.NotContains(t, lockStr, `print_firewall_logs.sh" --rootless`)
	assert.Contains(t, lockStr, "--container-runtime cloud-hypervisor")
	assert.Contains(t, lockStr, "--cloud-hypervisor-preview")
	assert.Contains(t, lockStr, "--cloud-hypervisor-vcpus 2")
	assert.Contains(t, lockStr, "--cloud-hypervisor-memory-mib 4096")
	assert.Contains(t, lockStr, "--cloud-hypervisor-kernel \"${GH_AW_CLOUD_HYPERVISOR_KERNEL}\"")
	assert.Contains(t, lockStr, "--cloud-hypervisor-artifact-manifest \"${GH_AW_CLOUD_HYPERVISOR_ARTIFACT_MANIFEST}\"")
	assert.Contains(t, lockStr, "--cloud-hypervisor-artifact-manifest-bundle \"${GH_AW_CLOUD_HYPERVISOR_ARTIFACT_MANIFEST_BUNDLE}\"")
	assert.Contains(t, lockStr, "--cloud-hypervisor-artifact-release-tag \"${GH_AW_CLOUD_HYPERVISOR_ARTIFACT_RELEASE_TAG}\"")
	assert.NotContains(t, lockStr, "--cloud-hypervisor-virtiofsd-sha256")
	assert.NotContains(t, lockStr, "development-allow-unattested-artifacts")
	assert.Contains(t, lockStr, `\"agentTimeout\":60`)
	assert.Contains(t, lockStr, `\"topologyAttach\":[\"awmg-mcpg\"]`)
	assert.NotContains(t, lockStr, "--mount")
	assert.NotContains(t, lockStr, "--tty")
	assert.NotContains(t, lockStr, "--legacy-security")
}

func TestCloudHypervisorCacheMemoryAllowWrite(t *testing.T) {
	workflowsDir := t.TempDir()
	markdown := `---
on:
  workflow_dispatch:
engine: claude
strict: false
tools:
  cache-memory: true
sandbox:
  agent:
    id: awf
    runtime: cloud-hypervisor
---

# Test cache-memory write access
`

	testFile := filepath.Join(workflowsDir, "test-cache-memory-allow-write.md")
	require.NoError(t, os.WriteFile(testFile, []byte(markdown), 0o644))
	require.NoError(t, NewCompiler().CompileWorkflow(testFile))

	lockContent, err := os.ReadFile(filepath.Join(workflowsDir, "test-cache-memory-allow-write.lock.yml"))
	require.NoError(t, err)
	lockStr := string(lockContent)

	assert.Contains(t, lockStr, `\"allowWrite\":[\"/tmp/gh-aw/agent\",\"/workspace\",\"/workspace/.awf-home\",\"/tmp/gh-aw/cache-memory\"]`)
	assert.Contains(t, lockStr, `mkdir -p "${GITHUB_WORKSPACE}/.awf-home"`)
	assert.Contains(t, lockStr, `git -C "${GITHUB_WORKSPACE}" rev-parse --path-format=absolute --git-path info/exclude`)
	assert.Contains(t, lockStr, `if ! grep -qxF -- '/.awf-home/' "$GH_AW_GIT_EXCLUDE"`)
	homeMkdirIdx := strings.Index(lockStr, `mkdir -p "${GITHUB_WORKSPACE}/.awf-home"`)
	homeExcludeIdx := strings.Index(lockStr, `printf '\n%s\n' '/.awf-home/'`)
	createDirIdx := strings.Index(lockStr, "Create cache-memory directory")
	awfIdx := strings.Index(lockStr, "sudo --preserve-env awf")
	require.NotEqual(t, -1, homeExcludeIdx)
	require.NotEqual(t, -1, createDirIdx)
	require.NotEqual(t, -1, awfIdx)
	assert.Less(t, homeMkdirIdx, homeExcludeIdx)
	assert.Less(t, homeExcludeIdx, awfIdx, "HOME must be excluded before AWF starts")
	assert.Less(t, createDirIdx, awfIdx, "cache-memory directory must exist before AWF starts")
}

func TestIsCloudHypervisorRuntime(t *testing.T) {
	assert.False(t, isCloudHypervisorRuntime(nil))
	assert.False(t, isCloudHypervisorRuntime(&WorkflowData{}))
	assert.True(t, isCloudHypervisorRuntime(&WorkflowData{SandboxConfig: &SandboxConfig{Agent: &AgentSandboxConfig{Runtime: AgentRuntimeCloudHypervisor}}}))
}

func TestCloudHypervisorShellScriptContent(t *testing.T) {
	wd, err := os.Getwd()
	require.NoError(t, err)
	shDir := filepath.Join(wd, "..", "..", "actions", "setup", "sh")

	tests := []struct {
		script   string
		contains []string
	}{
		{
			script:   "cloud_hypervisor_kvm_access.sh",
			contains: []string{"RUNNER_ENVIRONMENT", "github-hosted", "ImageOS", "setfacl", "u:${runner_uid}:rw", "/dev/kvm", "-c /dev/kvm", "getfacl -ncp /dev/kvm", "-r /dev/kvm", "-w /dev/kvm"},
		},
		{
			script:   "cloud_hypervisor_host_preflight.sh",
			contains: []string{"RUNNER_ENVIRONMENT", "github-hosted", "ImageOS", "/dev/kvm", "test -c /dev/kvm", "cloud-hypervisor preview", "gh rsync docker", "docker info", "/sys/fs/cgroup/cgroup.controllers", "landlock", "/sys/kernel/security/lsm"},
		},
		{
			script:   "cloud_hypervisor_setup_bundle.sh",
			contains: []string{"cloud-hypervisor-test-x86_64.tar.gz", "cloud-hypervisor-test-x86_64.manifest.json", "cloud-hypervisor-test-x86_64.manifest.sigstore.jsonl", "release.tag == $releaseTag", "archive structure validated", "tar --no-same-owner --no-same-permissions", "validate_extracted_file", "vmlinux.bin", "rootfs.ext4", "awf-supervisor", "virtiofsd", "virtiofsd_path=", "manifest_path=", "manifest_bundle_path=", "release_tag="},
		},
	}

	for _, tc := range tests {
		t.Run(tc.script, func(t *testing.T) {
			content, err := os.ReadFile(filepath.Join(shDir, tc.script))
			require.NoError(t, err)
			for _, expected := range tc.contains {
				assert.Contains(t, string(content), expected)
			}
		})
	}
}

const cloudHypervisorFixtureReleaseTag = "v9.9.9"

// cloudHypervisorFixtureManifest returns a manifest JSON document that
// satisfies the jq contract enforced by cloud_hypervisor_setup_bundle.sh,
// for the given release tag.
func cloudHypervisorFixtureManifest(releaseTag string) string {
	sha := strings.Repeat("b", 64)
	return fmt.Sprintf(`{
  "schemaVersion": 1,
  "release": {
    "repository": "github/gh-aw-firewall",
    "workflow": "github/gh-aw-firewall/.github/workflows/release.yml",
    "tag": %q,
    "sourceCommit": "%s"
  },
  "architecture": "x86_64",
  "artifacts": {
    "cloudHypervisor": {"file": "cloud-hypervisor", "sha256": "%s"},
    "virtiofsd": {"file": "virtiofsd", "sha256": "%s"},
    "kernel": {"file": "vmlinux.bin", "sha256": "%s"},
    "rootfs": {"file": "rootfs.ext4", "sha256": "%s"},
    "supervisor": {"file": "awf-supervisor", "sha256": "%s"}
  }
}`, releaseTag, strings.Repeat("a", 40), sha, sha, sha, sha, sha)
}

const cloudHypervisorFixtureBundle = `{"mediaType":"application/vnd.dev.sigstore.bundle.v0.3+json","verificationMaterial":{}}` + "\n"

// buildCloudHypervisorFixtureArchive builds a valid tar.gz guest archive
// containing the five artifact files cloud_hypervisor_setup_bundle.sh
// requires after extraction.
func buildCloudHypervisorFixtureArchive(t *testing.T) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for _, name := range []string{"cloud-hypervisor", "virtiofsd", "vmlinux.bin", "rootfs.ext4", "awf-supervisor"} {
		content := []byte("fixture-content-" + name)
		require.NoError(t, tw.WriteHeader(&tar.Header{
			Name: name,
			Mode: 0o755,
			Size: int64(len(content)),
		}))
		_, err := tw.Write(content)
		require.NoError(t, err)
	}
	require.NoError(t, tw.Close())
	require.NoError(t, gz.Close())
	return buf.Bytes()
}

// writeCloudHypervisorCurlShim installs a fake curl on PATH that copies
// files out of fixtureDir instead of performing a network request, keyed by
// the requested URL's basename. A "<basename>.symlink-target" sidecar file
// makes the shim create a symlink pointing at its contents instead of
// copying a regular file, simulating a substituted/tampered artifact. A
// missing fixture makes the shim fail closed like a real curl 404. Requests
// to the GitHub "latest release" API are answered from
// fixtureDir/latest-release.json (written to stdout, since the real script
// pipes that response into jq instead of passing -o).
func writeCloudHypervisorCurlShim(t *testing.T, fixtureDir string) string {
	t.Helper()
	binDir := t.TempDir()
	script := "#!/usr/bin/env bash\n" +
		"set -euo pipefail\n" +
		"outfile=\"\"\n" +
		"prev=\"\"\n" +
		"for arg in \"$@\"; do\n" +
		"  if [[ \"$prev\" == \"-o\" ]]; then\n" +
		"    outfile=\"$arg\"\n" +
		"  fi\n" +
		"  prev=\"$arg\"\n" +
		"done\n" +
		"url=\"${!#}\"\n" +
		"if [[ \"$url\" == *\"/repos/github/gh-aw-firewall/releases/latest\" ]]; then\n" +
		"  src=\"" + fixtureDir + "/latest-release.json\"\n" +
		"  if [[ ! -e \"$src\" ]]; then\n" +
		"    exit 22\n" +
		"  fi\n" +
		"  cat \"$src\"\n" +
		"  exit 0\n" +
		"fi\n" +
		"name=\"$(basename \"$url\")\"\n" +
		"src=\"" + fixtureDir + "/${name}\"\n" +
		"if [[ -f \"${src}.symlink-target\" ]]; then\n" +
		"  ln -sf \"$(cat \"${src}.symlink-target\")\" \"$outfile\"\n" +
		"  exit 0\n" +
		"fi\n" +
		"if [[ ! -e \"$src\" ]]; then\n" +
		"  exit 22\n" +
		"fi\n" +
		"cp \"$src\" \"$outfile\"\n"
	shimPath := filepath.Join(binDir, "curl")
	require.NoError(t, os.WriteFile(shimPath, []byte(script), 0o755))
	return binDir
}

// runCloudHypervisorSetupBundleScript executes the real
// cloud_hypervisor_setup_bundle.sh script against fixtureDir via the curl
// shim, so the script's own validation logic runs end-to-end.
func runCloudHypervisorSetupBundleScript(t *testing.T, fixtureDir string) (string, error) {
	t.Helper()
	return runCloudHypervisorSetupBundleScriptWithVersion(t, fixtureDir, cloudHypervisorFixtureReleaseTag)
}

// runCloudHypervisorSetupBundleScriptWithVersion is like
// runCloudHypervisorSetupBundleScript but lets the caller override
// GH_AW_AWF_VERSION, e.g. to exercise the "latest" resolution path.
func runCloudHypervisorSetupBundleScriptWithVersion(t *testing.T, fixtureDir, version string) (string, error) {
	t.Helper()
	wd, err := os.Getwd()
	require.NoError(t, err)
	scriptPath, err := filepath.Abs(filepath.Join(wd, "..", "..", "actions", "setup", "sh", "cloud_hypervisor_setup_bundle.sh"))
	require.NoError(t, err)

	binDir := writeCloudHypervisorCurlShim(t, fixtureDir)
	runnerTemp := t.TempDir()

	cmd := exec.Command("bash", scriptPath)
	cmd.Env = append(os.Environ(),
		"RUNNER_TEMP="+runnerTemp,
		"GH_AW_AWF_VERSION="+version,
		"PATH="+binDir+":"+os.Getenv("PATH"),
	)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func TestCloudHypervisorSetupBundleScriptExecutesAgainstFixtures(t *testing.T) {
	validManifest := cloudHypervisorFixtureManifest(cloudHypervisorFixtureReleaseTag)
	validArchive := buildCloudHypervisorFixtureArchive(t)
	archiveName := "cloud-hypervisor-test-x86_64.tar.gz"
	manifestName := "cloud-hypervisor-test-x86_64.manifest.json"
	bundleName := "cloud-hypervisor-test-x86_64.manifest.sigstore.jsonl"

	t.Run("valid fixtures succeed", func(t *testing.T) {
		dir := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(dir, archiveName), validArchive, 0o644))
		require.NoError(t, os.WriteFile(filepath.Join(dir, manifestName), []byte(validManifest), 0o644))
		require.NoError(t, os.WriteFile(filepath.Join(dir, bundleName), []byte(cloudHypervisorFixtureBundle), 0o644))

		out, err := runCloudHypervisorSetupBundleScript(t, dir)
		require.NoError(t, err, out)
		assert.Contains(t, out, "cloud-hypervisor bundle prepared")
	})

	t.Run("missing manifest fails closed", func(t *testing.T) {
		dir := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(dir, archiveName), validArchive, 0o644))
		require.NoError(t, os.WriteFile(filepath.Join(dir, bundleName), []byte(cloudHypervisorFixtureBundle), 0o644))

		out, err := runCloudHypervisorSetupBundleScript(t, dir)
		require.Error(t, err, out)
	})

	t.Run("missing bundle fails closed", func(t *testing.T) {
		dir := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(dir, archiveName), validArchive, 0o644))
		require.NoError(t, os.WriteFile(filepath.Join(dir, manifestName), []byte(validManifest), 0o644))

		out, err := runCloudHypervisorSetupBundleScript(t, dir)
		require.Error(t, err, out)
	})

	t.Run("malformed manifest json rejected", func(t *testing.T) {
		dir := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(dir, archiveName), validArchive, 0o644))
		require.NoError(t, os.WriteFile(filepath.Join(dir, manifestName), []byte(`{"schemaVersion": 1, "not": "a valid manifest"}`), 0o644))
		require.NoError(t, os.WriteFile(filepath.Join(dir, bundleName), []byte(cloudHypervisorFixtureBundle), 0o644))

		out, err := runCloudHypervisorSetupBundleScript(t, dir)
		require.Error(t, err, out)
		assert.Contains(t, out, "does not match the cloud-hypervisor release bundle contract")
	})

	t.Run("release tag mismatch rejected", func(t *testing.T) {
		dir := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(dir, archiveName), validArchive, 0o644))
		require.NoError(t, os.WriteFile(filepath.Join(dir, manifestName), []byte(cloudHypervisorFixtureManifest("v1.0.0")), 0o644))
		require.NoError(t, os.WriteFile(filepath.Join(dir, bundleName), []byte(cloudHypervisorFixtureBundle), 0o644))

		out, err := runCloudHypervisorSetupBundleScript(t, dir)
		require.Error(t, err, out)
		assert.Contains(t, out, "does not match the cloud-hypervisor release bundle contract")
	})

	t.Run("substituted manifest symlink rejected", func(t *testing.T) {
		dir := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(dir, archiveName), validArchive, 0o644))
		require.NoError(t, os.WriteFile(filepath.Join(dir, manifestName), []byte(validManifest), 0o644))
		require.NoError(t, os.WriteFile(filepath.Join(dir, manifestName+".symlink-target"), []byte("/etc/passwd"), 0o644))
		require.NoError(t, os.WriteFile(filepath.Join(dir, bundleName), []byte(cloudHypervisorFixtureBundle), 0o644))

		out, err := runCloudHypervisorSetupBundleScript(t, dir)
		require.Error(t, err, out)
		assert.Contains(t, out, "does not match the cloud-hypervisor release bundle contract")
	})

	t.Run("malformed bundle jsonl rejected", func(t *testing.T) {
		dir := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(dir, archiveName), validArchive, 0o644))
		require.NoError(t, os.WriteFile(filepath.Join(dir, manifestName), []byte(validManifest), 0o644))
		require.NoError(t, os.WriteFile(filepath.Join(dir, bundleName), []byte("not-json\n"), 0o644))

		out, err := runCloudHypervisorSetupBundleScript(t, dir)
		require.Error(t, err, out)
		assert.Contains(t, out, "is missing or malformed")
	})

	t.Run("empty bundle rejected", func(t *testing.T) {
		dir := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(dir, archiveName), validArchive, 0o644))
		require.NoError(t, os.WriteFile(filepath.Join(dir, manifestName), []byte(validManifest), 0o644))
		require.NoError(t, os.WriteFile(filepath.Join(dir, bundleName), []byte(""), 0o644))

		out, err := runCloudHypervisorSetupBundleScript(t, dir)
		require.Error(t, err, out)
		assert.Contains(t, out, "is missing or malformed")
	})

	t.Run("substituted bundle symlink rejected", func(t *testing.T) {
		dir := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(dir, archiveName), validArchive, 0o644))
		require.NoError(t, os.WriteFile(filepath.Join(dir, manifestName), []byte(validManifest), 0o644))
		require.NoError(t, os.WriteFile(filepath.Join(dir, bundleName), []byte(cloudHypervisorFixtureBundle), 0o644))
		require.NoError(t, os.WriteFile(filepath.Join(dir, bundleName+".symlink-target"), []byte("/etc/passwd"), 0o644))

		out, err := runCloudHypervisorSetupBundleScript(t, dir)
		require.Error(t, err, out)
		assert.Contains(t, out, "is missing or malformed")
	})

	t.Run("latest resolves to concrete release tag", func(t *testing.T) {
		dir := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(dir, archiveName), validArchive, 0o644))
		require.NoError(t, os.WriteFile(filepath.Join(dir, manifestName), []byte(validManifest), 0o644))
		require.NoError(t, os.WriteFile(filepath.Join(dir, bundleName), []byte(cloudHypervisorFixtureBundle), 0o644))
		require.NoError(t, os.WriteFile(filepath.Join(dir, "latest-release.json"),
			fmt.Appendf(nil, `{"tag_name": %q}`, cloudHypervisorFixtureReleaseTag), 0o644))

		out, err := runCloudHypervisorSetupBundleScriptWithVersion(t, dir, "latest")
		require.NoError(t, err, out)
		assert.Contains(t, out, "resolved latest release tag: "+cloudHypervisorFixtureReleaseTag)
		assert.Contains(t, out, "Download cloud-hypervisor bundle ("+cloudHypervisorFixtureReleaseTag+")")
		assert.Contains(t, out, "cloud-hypervisor bundle prepared")
	})

	t.Run("latest resolution failure fails closed", func(t *testing.T) {
		dir := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(dir, archiveName), validArchive, 0o644))
		require.NoError(t, os.WriteFile(filepath.Join(dir, manifestName), []byte(validManifest), 0o644))
		require.NoError(t, os.WriteFile(filepath.Join(dir, bundleName), []byte(cloudHypervisorFixtureBundle), 0o644))
		// No latest-release.json fixture: the shim 404s the GitHub API call.

		out, err := runCloudHypervisorSetupBundleScriptWithVersion(t, dir, "latest")
		require.Error(t, err, out)
		assert.Contains(t, out, "failed to resolve the latest gh-aw-firewall release tag: could not reach the GitHub releases API")
	})

	t.Run("latest resolution missing tag_name fails closed", func(t *testing.T) {
		dir := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(dir, archiveName), validArchive, 0o644))
		require.NoError(t, os.WriteFile(filepath.Join(dir, manifestName), []byte(validManifest), 0o644))
		require.NoError(t, os.WriteFile(filepath.Join(dir, bundleName), []byte(cloudHypervisorFixtureBundle), 0o644))
		require.NoError(t, os.WriteFile(filepath.Join(dir, "latest-release.json"), []byte(`{}`), 0o644))

		out, err := runCloudHypervisorSetupBundleScriptWithVersion(t, dir, "latest")
		require.Error(t, err, out)
		assert.Contains(t, out, "failed to resolve the latest gh-aw-firewall release tag: no tag_name in API response")
	})
}
