//go:build !integration

package workflow

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCompilePiDurableRequiresEffectiveFirewall(t *testing.T) {
	for _, test := range []struct {
		name   string
		config string
		want   string
	}{
		{"disabled agent in strict mode", "sandbox:\n  agent: false\n", "strict mode: 'sandbox.agent: false' is not allowed"},
		{"disabled agent", "strict: false\nsandbox:\n  agent: false\n", "pi durable requires the credential-isolated model gateway"},
		{"disabled network firewall", "network:\n  firewall: false\n", "Unknown property: firewall"},
		{"disabled firewall with enabled sandbox", "network:\n  firewall: false\nsandbox:\n  agent:\n    runtime: docker\n", "Unknown property: firewall"},
	} {
		t.Run(test.name, func(t *testing.T) {
			dir := t.TempDir()
			source := filepath.Join(dir, "durable-invalid.md")
			markdown := "---\non: workflow_dispatch\nengine:\n  pi:\n    durable: {}\n" + test.config + "---\nContinue the pending task.\n"
			require.NoError(t, os.WriteFile(source, []byte(markdown), 0o600))
			err := NewCompiler(WithVersion("dev")).CompileWorkflow(source)
			require.ErrorContains(t, err, test.want)
			_, err = os.Stat(filepath.Join(dir, "durable-invalid.lock.yml"))
			require.ErrorIs(t, err, os.ErrNotExist)
		})
	}
}

func TestPiDurablePlanRejectsDisabledEffectiveFirewall(t *testing.T) {
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
		require.Nil(t, plan)
		require.ErrorContains(t, err, "pi durable requires the credential-isolated model gateway")
		require.ErrorContains(t, err, "remove network.firewall: false")
	}
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
			require.Contains(t, string(lock), "GH_AW_PI_DURABLE_PACKAGE_ROOT: /tmp/gh-aw/pi-durable-runtime/node_modules")
			require.Contains(t, string(lock), "npm install --prefix /tmp/gh-aw/pi-durable-runtime")
		})
	}
}
