//go:build !integration

package workflow

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestAWFSupportsExcludeEnv(t *testing.T) {
	tests := []struct {
		name           string
		firewallConfig *FirewallConfig
		want           bool
	}{
		{
			name:           "nil firewall config (default version) supports --exclude-env",
			firewallConfig: nil,
			want:           true,
		},
		{
			name:           "empty version (default) supports --exclude-env",
			firewallConfig: &FirewallConfig{},
			want:           true,
		},
		{
			name:           "v0.25.3 supports --exclude-env",
			firewallConfig: &FirewallConfig{Version: "v0.25.3"},
			want:           true,
		},
		{
			name:           "v0.26.0 supports --exclude-env",
			firewallConfig: &FirewallConfig{Version: "v0.26.0"},
			want:           true,
		},
		{
			name:           "v0.27.0 supports --exclude-env",
			firewallConfig: &FirewallConfig{Version: "v0.27.0"},
			want:           true,
		},
		{
			name:           "latest supports --exclude-env",
			firewallConfig: &FirewallConfig{Version: "latest"},
			want:           true,
		},
		{
			name:           "v0.25.0 does not support --exclude-env",
			firewallConfig: &FirewallConfig{Version: "v0.25.0"},
			want:           false,
		},
		{
			name:           "v0.1.0 does not support --exclude-env",
			firewallConfig: &FirewallConfig{Version: "v0.1.0"},
			want:           false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := awfSupportsExcludeEnv(tt.firewallConfig)
			assert.Equal(t, tt.want, got, "awfSupportsExcludeEnv result")
		})
	}
}

func TestAWFSupportsCliProxy(t *testing.T) {
	tests := []struct {
		name           string
		firewallConfig *FirewallConfig
		want           bool
	}{
		{
			name:           "nil firewall config returns true (uses default version)",
			firewallConfig: nil,
			want:           true,
		},
		{
			name:           "empty version returns true (uses default version)",
			firewallConfig: &FirewallConfig{},
			want:           true,
		},
		{
			name:           "latest returns true",
			firewallConfig: &FirewallConfig{Version: "latest"},
			want:           true,
		},
		{
			name:           "v0.25.17 supports CLI proxy flags (exact minimum version)",
			firewallConfig: &FirewallConfig{Version: "v0.25.17"},
			want:           true,
		},
		{
			name:           "v0.26.0 supports CLI proxy flags",
			firewallConfig: &FirewallConfig{Version: "v0.26.0"},
			want:           true,
		},
		{
			name:           "v0.27.0 supports CLI proxy flags",
			firewallConfig: &FirewallConfig{Version: "v0.27.0"},
			want:           true,
		},
		{
			name:           "v0.25.16 does not support CLI proxy flags",
			firewallConfig: &FirewallConfig{Version: "v0.25.16"},
			want:           false,
		},
		{
			name:           "v0.25.14 does not support CLI proxy flags",
			firewallConfig: &FirewallConfig{Version: "v0.25.14"},
			want:           false,
		},
		{
			name:           "v0.1.0 does not support CLI proxy flags",
			firewallConfig: &FirewallConfig{Version: "v0.1.0"},
			want:           false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := awfSupportsCliProxy(tt.firewallConfig)
			assert.Equal(t, tt.want, got, "awfSupportsCliProxy result")
		})
	}
}

// TestAWFSupportsAllowHostPorts tests the awfSupportsAllowHostPorts version gate function.
func TestAWFSupportsAllowHostPorts(t *testing.T) {
	tests := []struct {
		name           string
		firewallConfig *FirewallConfig
		want           bool
	}{
		{
			name:           "nil firewall config returns true (uses default version)",
			firewallConfig: nil,
			want:           true,
		},
		{
			name:           "empty version returns true (uses default version)",
			firewallConfig: &FirewallConfig{},
			want:           true,
		},
		{
			name:           "latest returns true",
			firewallConfig: &FirewallConfig{Version: "latest"},
			want:           true,
		},
		{
			name:           "v0.25.24 supports --allow-host-ports (exact minimum version)",
			firewallConfig: &FirewallConfig{Version: "v0.25.24"},
			want:           true,
		},
		{
			name:           "v0.26.0 supports --allow-host-ports",
			firewallConfig: &FirewallConfig{Version: "v0.26.0"},
			want:           true,
		},
		{
			name:           "v0.25.23 does not support --allow-host-ports",
			firewallConfig: &FirewallConfig{Version: "v0.25.23"},
			want:           false,
		},
		{
			name:           "v0.1.0 does not support --allow-host-ports",
			firewallConfig: &FirewallConfig{Version: "v0.1.0"},
			want:           false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := awfSupportsAllowHostPorts(tt.firewallConfig)
			assert.Equal(t, tt.want, got, "awfSupportsAllowHostPorts result")
		})
	}
}

// TestAWFSupportsDockerHostPathPrefix tests the awfSupportsDockerHostPathPrefix version gate.
func TestAWFSupportsDockerHostPathPrefix(t *testing.T) {
	tests := []struct {
		name           string
		firewallConfig *FirewallConfig
		want           bool
	}{
		{
			name:           "nil firewall config returns true (uses default version)",
			firewallConfig: nil,
			want:           true,
		},
		{
			name:           "empty version returns true (uses default version)",
			firewallConfig: &FirewallConfig{},
			want:           true,
		},
		{
			name:           "latest returns true",
			firewallConfig: &FirewallConfig{Version: "latest"},
			want:           true,
		},
		{
			name:           "v0.25.43 supports --docker-host-path-prefix (exact minimum version)",
			firewallConfig: &FirewallConfig{Version: "v0.25.43"},
			want:           true,
		},
		{
			name:           "v0.25.42 does not support --docker-host-path-prefix",
			firewallConfig: &FirewallConfig{Version: "v0.25.42"},
			want:           false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := awfSupportsDockerHostPathPrefix(tt.firewallConfig)
			assert.Equal(t, tt.want, got, "awfSupportsDockerHostPathPrefix result")
		})
	}
}

func TestAWFSupportsTokenSteering(t *testing.T) {
	tests := []struct {
		name           string
		firewallConfig *FirewallConfig
		want           bool
	}{
		{
			name:           "nil firewall config returns true (uses default version)",
			firewallConfig: nil,
			want:           true,
		},
		{
			name:           "empty version returns true (uses default version)",
			firewallConfig: &FirewallConfig{},
			want:           true,
		},
		{
			name:           "latest returns true",
			firewallConfig: &FirewallConfig{Version: "latest"},
			want:           true,
		},
		{
			name:           "v0.25.44 supports token steering (exact minimum version)",
			firewallConfig: &FirewallConfig{Version: "v0.25.44"},
			want:           true,
		},
		{
			name:           "v0.25.43 does not support token steering",
			firewallConfig: &FirewallConfig{Version: "v0.25.43"},
			want:           false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := awfSupportsTokenSteering(tt.firewallConfig)
			assert.Equal(t, tt.want, got, "awfSupportsTokenSteering result")
		})
	}
}

// TestAWFSupportsChrootConfig tests the awfSupportsChrootConfig version gate.
func TestAWFSupportsChrootConfig(t *testing.T) {
	tests := []struct {
		name           string
		firewallConfig *FirewallConfig
		want           bool
	}{
		{
			name:           "nil firewall config returns true (uses default version)",
			firewallConfig: nil,
			want:           true,
		},
		{
			name:           "empty version returns true (uses default version)",
			firewallConfig: &FirewallConfig{},
			want:           true,
		},
		{
			name:           "latest returns true",
			firewallConfig: &FirewallConfig{Version: "latest"},
			want:           true,
		},
		{
			name:           "v0.27.1 supports chroot config (exact minimum version)",
			firewallConfig: &FirewallConfig{Version: "v0.27.1"},
			want:           true,
		},
		{
			name:           "v0.27.0 does not support chroot config",
			firewallConfig: &FirewallConfig{Version: "v0.27.0"},
			want:           false,
		},
		{
			name:           "v0.25.44 (old) does not support chroot config",
			firewallConfig: &FirewallConfig{Version: "v0.25.44"},
			want:           false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := awfSupportsChrootConfig(tt.firewallConfig)
			assert.Equal(t, tt.want, got, "awfSupportsChrootConfig result")
		})
	}
}

// TestAWFSupportsAPIProxyProviders tests the awfSupportsAPIProxyProviders version gate.
func TestAWFSupportsAPIProxyProviders(t *testing.T) {
	tests := []struct {
		name           string
		firewallConfig *FirewallConfig
		want           bool
	}{
		{
			name:           "nil firewall config returns true (default version v0.27.43 meets minimum)",
			firewallConfig: nil,
			want:           true,
		},
		{
			name:           "empty version returns true (default version v0.27.43 meets minimum)",
			firewallConfig: &FirewallConfig{},
			want:           true,
		},
		{
			name:           "latest returns true",
			firewallConfig: &FirewallConfig{Version: "latest"},
			want:           true,
		},
		{
			name:           "v0.27.43 supports apiProxy.providers (exact minimum version)",
			firewallConfig: &FirewallConfig{Version: "v0.27.43"},
			want:           true,
		},
		{
			name:           "v0.27.42 does not support apiProxy.providers (schema not present)",
			firewallConfig: &FirewallConfig{Version: "v0.27.42"},
			want:           false,
		},
		{
			name:           "v0.27.41 does not support apiProxy.providers",
			firewallConfig: &FirewallConfig{Version: "v0.27.41"},
			want:           false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := awfSupportsAPIProxyProviders(tt.firewallConfig)
			assert.Equal(t, tt.want, got, "awfSupportsAPIProxyProviders result")
		})
	}
}

func TestAWFEmitsFilesystemAllowWrite(t *testing.T) {
	cloudHypervisor := &WorkflowData{
		SandboxConfig: &SandboxConfig{
			Agent: &AgentSandboxConfig{Runtime: AgentRuntimeCloudHypervisor},
		},
	}
	docker := &WorkflowData{
		SandboxConfig: &SandboxConfig{
			Agent: &AgentSandboxConfig{Runtime: AgentRuntimeDocker},
		},
	}
	gvisor := &WorkflowData{
		SandboxConfig: &SandboxConfig{
			Agent: &AgentSandboxConfig{Runtime: AgentRuntimeGVisor},
		},
	}

	tests := []struct {
		name           string
		workflowData   *WorkflowData
		firewallConfig *FirewallConfig
		want           bool
	}{
		{
			name:         "cloud-hypervisor emits at the default version",
			workflowData: cloudHypervisor,
			want:         true,
		},
		{
			name:           "cloud-hypervisor emits at its exact minimum version",
			workflowData:   cloudHypervisor,
			firewallConfig: &FirewallConfig{Version: "v0.28.6"},
			want:           true,
		},
		{
			name:           "cloud-hypervisor does not emit below its minimum version",
			workflowData:   cloudHypervisor,
			firewallConfig: &FirewallConfig{Version: "v0.28.5"},
			want:           false,
		},
		{
			name:         "docker never emits",
			workflowData: docker,
			want:         false,
		},
		{
			name:         "gvisor never emits",
			workflowData: gvisor,
			want:         false,
		},
		{
			name:         "default runtime never emits",
			workflowData: &WorkflowData{},
			want:         false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, awfEmitsFilesystemAllowWrite(tt.workflowData, tt.firewallConfig))
		})
	}
}

func TestAWFSupportsCloudHypervisorFilesystemAllowWrite(t *testing.T) {
	tests := []struct {
		name           string
		firewallConfig *FirewallConfig
		want           bool
	}{
		{
			name: "default version supports Cloud Hypervisor filesystem.allowWrite",
			want: true,
		},
		{
			name:           "exact minimum version supports Cloud Hypervisor filesystem.allowWrite",
			firewallConfig: &FirewallConfig{Version: "v0.28.6"},
			want:           true,
		},
		{
			name:           "v0.28.5 does not support Cloud Hypervisor filesystem.allowWrite",
			firewallConfig: &FirewallConfig{Version: "v0.28.5"},
			want:           false,
		},
		{
			name:           "prior version does not support Cloud Hypervisor filesystem.allowWrite",
			firewallConfig: &FirewallConfig{Version: "v0.28.4"},
			want:           false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, awfSupportsCloudHypervisorFilesystemAllowWrite(tt.firewallConfig))
		})
	}
}

// TestAWFSupportsAPIProxyCACert tests the awfSupportsAPIProxyCACert version gate.
func TestAWFSupportsAPIProxyCACert(t *testing.T) {
	tests := []struct {
		name           string
		firewallConfig *FirewallConfig
		want           bool
	}{
		{
			name:           "nil firewall config returns true (default version v0.28.10 meets minimum)",
			firewallConfig: nil,
			want:           true,
		},
		{
			name:           "empty version returns true (default version v0.28.10 meets minimum)",
			firewallConfig: &FirewallConfig{},
			want:           true,
		},
		{
			name:           "latest returns true",
			firewallConfig: &FirewallConfig{Version: "latest"},
			want:           true,
		},
		{
			name:           "v0.28.10 supports apiProxy.caCert (exact minimum version)",
			firewallConfig: &FirewallConfig{Version: "v0.28.10"},
			want:           true,
		},
		{
			name:           "v0.28.9 does not support apiProxy.caCert (schema not present)",
			firewallConfig: &FirewallConfig{Version: "v0.28.9"},
			want:           false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := awfSupportsAPIProxyCACert(tt.firewallConfig)
			assert.Equal(t, tt.want, got, "awfSupportsAPIProxyCACert result")
		})
	}
}

func TestAWFSupportsVerifySbxEgress(t *testing.T) {
	tests := []struct {
		name           string
		firewallConfig *FirewallConfig
		want           bool
	}{
		{name: "default version supports verify-sbx-egress", want: true},
		{name: "exact minimum version supports verify-sbx-egress", firewallConfig: &FirewallConfig{Version: "v0.28.13"}, want: true},
		{name: "older version does not support verify-sbx-egress", firewallConfig: &FirewallConfig{Version: "v0.28.12"}, want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, awfSupportsVerifySbxEgress(tt.firewallConfig))
		})
	}
}
