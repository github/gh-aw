//go:build !integration

package workflow

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDetectionJobAzureOIDCAWFVersion(t *testing.T) {
	tests := []struct {
		name           string
		version        string
		sandboxVersion string
		inherited      bool
		envOnly        bool
		wantErr        bool
	}{
		{name: "explicit Azure auth rejects old AWF", version: "v0.25.2", wantErr: true},
		{name: "inherited Azure auth rejects old AWF", version: "v0.25.2", inherited: true, wantErr: true},
		{name: "explicit Azure auth accepts minimum AWF", version: "v0.25.3"},
		{name: "inherited Azure auth accepts minimum AWF", version: "v0.25.3", inherited: true},
		{name: "default AWF supports credential exclusion"},
		{name: "unversioned AWF cannot establish credential exclusion", version: "custom", wantErr: true},
		{name: "old sandbox version overrides supported network version", version: "v0.25.3", sandboxVersion: "v0.25.2", wantErr: true},
		{name: "supported sandbox version overrides old network version", version: "v0.25.2", sandboxVersion: "v0.25.3"},
		{name: "environment alone does not grant OIDC permission", version: "v0.25.2", envOnly: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			auth := &EngineAuthConfig{
				Type: "github-oidc", Provider: "azure",
				AzureTenantID: "tenant", AzureClientID: "client",
			}
			data := &WorkflowData{
				AI:           "codex",
				EngineConfig: &EngineConfig{ID: "codex"},
				NetworkPermissions: &NetworkPermissions{
					Firewall: &FirewallConfig{Enabled: true, Version: tt.version},
				},
				SafeOutputs: &SafeOutputsConfig{
					ThreatDetection: &ThreatDetectionConfig{
						EngineConfig: &EngineConfig{ID: "codex", Auth: auth},
					},
				},
			}
			if tt.sandboxVersion != "" {
				data.SandboxConfig = &SandboxConfig{Agent: &AgentSandboxConfig{Version: tt.sandboxVersion}}
			}
			if tt.inherited {
				data.EngineConfig.Auth = auth
				data.SafeOutputs.ThreatDetection.EngineConfig = nil
			}
			if tt.envOnly {
				data.SafeOutputs.ThreatDetection.EngineConfig = &EngineConfig{
					ID: "codex",
					Env: map[string]string{
						"AWF_AUTH_TYPE":     "github-oidc",
						"AWF_AUTH_PROVIDER": "azure",
					},
				}
			}

			job, err := NewCompiler().buildDetectionJob(data)
			if tt.wantErr {
				require.ErrorContains(t, err, "github-oidc requires AWF v0.25.3 or newer")
				assert.Contains(t, err.Error(), "safe-outputs.threat-detection.engine.auth.type")
				assert.Contains(t, err.Error(), "keep Actions OIDC credentials out of the agent container")
				assert.Nil(t, job)
				return
			}
			require.NoError(t, err)
			require.NotNil(t, job)
			if tt.envOnly {
				assert.NotContains(t, job.Permissions, "id-token: write")
				return
			}
			assert.Contains(t, job.Permissions, "id-token: write")
			steps := strings.Join(job.Steps, "")
			assert.Contains(t, steps, "--exclude-env ACTIONS_ID_TOKEN_REQUEST_TOKEN")
			assert.Contains(t, steps, "--exclude-env ACTIONS_ID_TOKEN_REQUEST_URL")
		})
	}
}

func TestDetectionAzureOIDCWorkflowRejectsOldAWF(t *testing.T) {
	dir := testutil.TempDir(t, "detection-azure-oidc-*")
	workflows := filepath.Join(dir, constants.GetWorkflowDir())
	require.NoError(t, os.MkdirAll(workflows, 0o755))
	for _, version := range []string{"v0.25.2", "v0.25.3"} {
		t.Run(version, func(t *testing.T) {
			file := filepath.Join(workflows, "azure-detector.md")
			require.NoError(t, os.WriteFile(file, []byte(fmt.Sprintf(`---
on: workflow_dispatch
permissions:
  contents: read
engine: copilot
network:
  allowed: [defaults, login.microsoftonline.com]
sandbox:
  agent:
    version: %s
safe-outputs:
  add-comment:
  threat-detection:
    engine:
      id: codex
      auth:
        type: github-oidc
        provider: azure
        azure-tenant-id: tenant
        azure-client-id: client
---
Inspect the repository.
`, version)), 0o644))
			err := NewCompiler().CompileWorkflow(file)
			if version == "v0.25.2" {
				require.ErrorContains(t, err, "github-oidc requires AWF v0.25.3 or newer")
				_, err := os.Stat(filepath.Join(workflows, "azure-detector.lock.yml"))
				assert.True(t, os.IsNotExist(err), "rejected detector must not produce a lock file")
				return
			}
			require.NoError(t, err)
			lock, err := os.ReadFile(filepath.Join(workflows, "azure-detector.lock.yml"))
			require.NoError(t, err)
			detection := extractJobSection(string(lock), "detection")
			require.NotEmpty(t, detection)
			assert.Contains(t, detection, "id-token: write")
			assert.Contains(t, detection, "--exclude-env ACTIONS_ID_TOKEN_REQUEST_TOKEN")
			assert.Contains(t, detection, "--exclude-env ACTIONS_ID_TOKEN_REQUEST_URL")
		})
	}
}
