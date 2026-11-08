//go:build !integration

package workflow

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCompilePiDurableNativeModes(t *testing.T) {
	for _, test := range []struct {
		name   string
		config string
	}{
		{"CLI", ""},
		{"SDK", "  driver: pi_agent_core_driver.cjs\n"},
		{"RPC", "  driver: pi_rpc_driver.cjs\n"},
	} {
		t.Run(test.name, func(t *testing.T) {
			dir := t.TempDir()
			source := filepath.Join(dir, "durable.md")
			markdown := "---\non: workflow_dispatch\nengine:\n  id: pi\n  durable: {}\n" + test.config + "---\nContinue the workflow task.\n"
			require.NoError(t, os.WriteFile(source, []byte(markdown), 0o600))
			require.NoError(t, NewCompiler(WithVersion("dev")).CompileWorkflow(source))
			lock, err := os.ReadFile(filepath.Join(dir, "durable.lock.yml"))
			require.NoError(t, err)
			require.Contains(t, string(lock), "/tmp/gh-aw/agent/durable-sessions/pi/session.jsonl")
			require.Contains(t, string(lock), `GH_AW_PI_SUBAGENT_ARGS: '["--print","--mode","json","--no-session","--no-approve"]'`)
			require.NotContains(t, string(lock), "pi_durable_")
			if test.config != "" {
				require.Contains(t, string(lock), test.config[len("  driver: "):len(test.config)-1])
			}
		})
	}
}

func TestPiDurablePlanUsesNativeSessionsWithoutExtraDependencies(t *testing.T) {
	for _, sandbox := range []*SandboxConfig{
		nil,
		{Agent: &AgentSandboxConfig{Type: SandboxTypeAWF, Runtime: AgentRuntimeDocker}},
	} {
		data := &WorkflowData{
			WorkflowID:         "durable",
			EngineConfig:       &EngineConfig{ID: "pi", Durable: &DurableConfig{RetentionDays: 7}},
			SandboxConfig:      sandbox,
			NetworkPermissions: &NetworkPermissions{Firewall: &FirewallConfig{Enabled: false}},
		}
		require.False(t, isFirewallDisabledBySandboxAgent(data))
		require.False(t, isFirewallEnabled(data))
		plan, err := NewPiEngine().GetDurableExecutionPlan(data, "")
		require.NoError(t, err)
		require.Equal(t, []string{"/tmp/gh-aw/agent/durable-sessions/pi/session.jsonl"}, plan.Files)
		require.Empty(t, plan.InstallationSteps)
		data.EngineConfig.Extensions = []string{"npm:@example/workflow"}
		data.EngineConfig.Args = []string{"--thinking", "high"}
		plan, err = NewPiEngine().GetDurableExecutionPlan(data, "")
		require.NoError(t, err)
		require.Empty(t, plan.InstallationSteps)
		require.Contains(t, durableStepsText(plan.ExecutionSteps), "--thinking high")
	}
}

func TestPiDurableRejectsConflictingNativeSessionSelection(t *testing.T) {
	data := &WorkflowData{EngineConfig: &EngineConfig{ID: "pi", Durable: &DurableConfig{RetentionDays: 7}, Config: `{"session":{"enabled":true}}`}}
	_, err := NewPiEngine().GetDurableExecutionPlan(data, "")
	require.ErrorContains(t, err, "remove engine.config.session")
}

func TestCompilePiDurableSandboxRuntimes(t *testing.T) {
	for _, runtime := range []string{"docker", "cloud-hypervisor", "nvx"} {
		t.Run(runtime, func(t *testing.T) {
			dir := t.TempDir()
			source := filepath.Join(dir, "durable.md")
			sandbox := "sandbox:\n  agent:\n    runtime: " + runtime + "\n"
			if runtime == "nvx" {
				sandbox += `    nvx:
      preview: true
      network-isolation: true
      api-proxy: true
      layer-path: /opt/nvx/guest-layer
      openvmm-path: /opt/nvx/openvmm
      kernel-path: /opt/nvx/vmlinux
      initramfs-path: /opt/nvx/initramfs.cpio.gz
      artifact-manifest-path: /opt/nvx/nvx-test-x86_64.manifest.json
      artifact-manifest-bundle-path: /opt/nvx/nvx-test-x86_64.manifest.sigstore.jsonl
      mount-policy: workspace-only
`
			}
			markdown := "---\non: workflow_dispatch\nengine:\n  pi:\n    durable: {}\n" + sandbox + "---\nContinue the pending task.\n"
			require.NoError(t, os.WriteFile(source, []byte(markdown), 0o600))
			require.NoError(t, NewCompiler(WithVersion("dev")).CompileWorkflow(source))
			lock, err := os.ReadFile(filepath.Join(dir, "durable.lock.yml"))
			require.NoError(t, err)
			require.Contains(t, string(lock), "pi_models_json.cjs")
			require.Contains(t, string(lock), "/tmp/gh-aw/agent/durable-sessions/pi/session.jsonl")
			require.NotContains(t, string(lock), "GH_AW_PI_DURABLE")
			require.NotContains(t, string(lock), "Install Pi durable")
		})
	}
}
