//go:build integration

package workflow

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEnclavesCloudHypervisorCompileValidation(t *testing.T) {
	for _, tc := range []struct {
		name  string
		entry string
		want  string
	}{
		{"image override", "image: custom:latest", ".image is incompatible"},
		{"mixed default", `agent:
      model: gpt-5
    repos:
      - repo: octo-org/private-service
        sensitivity: confidential`, "cannot be mixed"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := testutil.TempDir(t, "enclaves-runtime-validation")
			path := filepath.Join(dir, "workflow.md")
			entry := "    " + tc.entry
			if tc.name == "mixed default" {
				entry = "  - " + tc.entry
			}
			content := `---
on: workflow_dispatch
engine: copilot
permissions:
  contents: read
sandbox:
  agent:
    version: v0.28.47
enclaves:
  - script:
    runtime: cloud-hypervisor
    repos:
      - repo: octo-org/private-service
        sensitivity: confidential
` + entry + `
---
Inspect through the enclave.
`
			require.NoError(t, os.WriteFile(path, []byte(content), 0600))
			compiler := NewCompiler()
			compiler.SetStrictMode(false)
			require.ErrorContains(t, compiler.CompileWorkflow(path), tc.want)
		})
	}
}

func TestEnclavesCloudHypervisorCompileAgentRoute(t *testing.T) {
	for _, tc := range []struct {
		name    string
		engine  string
		version string
		want    string
	}{
		{"old AWF", "copilot", "v0.28.46", "requires AWF v0.28.47 or newer"},
		{"Copilot route", "copilot", "v0.28.47", ""},
		{"OpenAI primary lacks Copilot route", "codex", "v0.28.47", "configured Copilot API proxy provider route"},
		{"OpenAI primary filters enclave credential", "\n  id: codex\n  env:\n    COPILOT_PROVIDER_API_KEY: ${{ secrets.ENCLAVE_KEY }}", "v0.28.47", "configured Copilot API proxy provider route"},
		{"GitHub-backed Codex route", "\n  id: codex\n  model-provider: github", "v0.28.47", ""},
		{"GitHub-backed Claude route", "\n  id: claude\n  model-provider: github", "v0.28.47", ""},
		{"BYOK target without credential", "\n  id: copilot\n  env:\n    COPILOT_PROVIDER_BASE_URL: https://provider.example.com", "v0.28.47", "configured Copilot API proxy provider route"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := testutil.TempDir(t, "enclaves-runtime-agent-route")
			path := filepath.Join(dir, "workflow.md")
			content := `---
on: workflow_dispatch
engine: ` + tc.engine + `
strict: false
permissions:
  contents: read
sandbox:
  agent:
    version: ` + tc.version + `
enclaves:
  - agent:
      model: gpt-5
    runtime: cloud-hypervisor
    repos:
      - repo: octo-org/private-service
        sensitivity: confidential
---
Inspect through the enclave.
`
			require.NoError(t, os.WriteFile(path, []byte(content), 0600))
			compiler := NewCompiler()
			compiler.SetStrictMode(false)
			err := compiler.CompileWorkflow(path)
			if tc.want == "" {
				require.NoError(t, err)
			} else {
				require.ErrorContains(t, err, tc.want)
			}
		})
	}
}

func TestEnclavesCloudHypervisorCompileWarning(t *testing.T) {
	for _, runtime := range []string{"cloud-hypervisor", "docker"} {
		t.Run(runtime, func(t *testing.T) {
			dir := testutil.TempDir(t, "enclaves-runtime-warning")
			path := filepath.Join(dir, "workflow.md")
			content := `---
on: workflow_dispatch
engine: copilot
permissions:
  contents: read
sandbox:
  agent:
    version: v0.28.47
enclaves:
  - script:
    runtime: ` + runtime + `
    repos:
      - repo: octo-org/private-service
        sensitivity: confidential
---
Inspect the repository through the enclave.
`
			require.NoError(t, os.WriteFile(path, []byte(content), 0600))
			stderr := os.Stderr
			reader, writer, err := os.Pipe()
			require.NoError(t, err)
			defer reader.Close()
			defer func() { os.Stderr = stderr }()
			os.Stderr = writer
			compiler := NewCompiler()
			compiler.SetStrictMode(false)
			err = compiler.CompileWorkflow(path)
			require.NoError(t, writer.Close())
			os.Stderr = stderr
			var output bytes.Buffer
			_, readErr := io.Copy(&output, reader)
			require.NoError(t, readErr)
			require.NoError(t, err)
			const warning = "Using experimental feature: enclaves cloud-hypervisor runtime"
			assert.Equal(t, runtime == "cloud-hypervisor", strings.Contains(output.String(), warning))
			if runtime == "cloud-hypervisor" {
				assert.Positive(t, compiler.GetWarningCount())
			}
		})
	}
}
