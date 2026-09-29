//go:build !integration

package cli

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetSandboxFieldsCodemod(t *testing.T) {
	t.Parallel()
	codemod := getSandboxFieldsCodemod()

	assert.Equal(t, "sandbox-fields-kebab-case", codemod.ID)
	assert.Equal(t, "Normalize sandbox fields to kebab-case", codemod.Name)
	assert.NotEmpty(t, codemod.Description)
	assert.Equal(t, "0.86.0", codemod.IntroducedIn)
	require.NotNil(t, codemod.Apply)
}

func TestSandboxFieldsCodemod(t *testing.T) {
	t.Parallel()
	codemod := getSandboxFieldsCodemod()
	content := `---
on: workflow_dispatch
sandbox:
  agent:
    images:
      apiProxy: registry.example.com/proxy:v1@sha256:abc # keep image comment
    config:
      filesystem:
        denyRead: [/secret]
        allowWrite: [/workspace] # keep write comment
        denyWrite: [/root]
      ignoreViolations:
        "git *": [/tmp]
      enableWeakerNestedSandbox: true
    targets:
      openai:
        authHeader: api-key
      copilot:
        extraHeaders:
          x-customHeader: value
        extraBodyFields:
          camelCasePayloadField: value
        sessionId: run-1
  mcp:
    entrypointArgs: [serve]
allowWrite: unrelated
---

# Body
Keep allowWrite and apiProxy here.
`
	frontmatter := map[string]any{
		"sandbox": map[string]any{
			"agent": map[string]any{
				"images": map[string]any{"apiProxy": "image"},
				"config": map[string]any{
					"filesystem":                map[string]any{"denyRead": []any{"/secret"}, "allowWrite": []any{"/workspace"}, "denyWrite": []any{"/root"}},
					"ignoreViolations":          map[string]any{"git *": []any{"/tmp"}},
					"enableWeakerNestedSandbox": true,
				},
				"targets": map[string]any{
					"openai":  map[string]any{"authHeader": "api-key"},
					"copilot": map[string]any{"extraHeaders": map[string]any{"x-customHeader": "value"}, "extraBodyFields": map[string]any{"camelCasePayloadField": "value"}, "sessionId": "run-1"},
				},
			},
			"mcp": map[string]any{"entrypointArgs": []any{"serve"}},
		},
		"allowWrite": "unrelated",
	}

	result, applied, err := codemod.Apply(content, frontmatter)

	require.NoError(t, err)
	assert.True(t, applied)
	assert.Contains(t, result, "      api-proxy: registry.example.com/proxy:v1@sha256:abc # keep image comment")
	assert.Contains(t, result, "        deny-read: [/secret]")
	assert.Contains(t, result, "        allow-write: [/workspace] # keep write comment")
	assert.Contains(t, result, "        deny-write: [/root]")
	assert.Contains(t, result, "      ignore-violations:")
	assert.Contains(t, result, "      enable-weaker-nested-sandbox: true")
	assert.Contains(t, result, "        auth-header: api-key")
	assert.Contains(t, result, "        extra-headers:")
	assert.Contains(t, result, "        extra-body-fields:")
	assert.Contains(t, result, "        session-id: run-1")
	assert.Contains(t, result, "    entrypoint-args: [serve]")
	assert.Contains(t, result, "          x-customHeader: value")
	assert.Contains(t, result, "          camelCasePayloadField: value")
	assert.Contains(t, result, "allowWrite: unrelated")
	assert.Contains(t, result, "# Body\nKeep allowWrite and apiProxy here.")
}

func TestSandboxFieldsCodemodLegacyConfigAndNetwork(t *testing.T) {
	t.Parallel()
	codemod := getSandboxFieldsCodemod()
	content := `---
sandbox:
  config:
    network:
      allowedDomains: [example.com]
      blockedDomains: [blocked.example.com]
      allowUnixSockets: [/var/run/test.sock]
      allowLocalBinding: true
      allowAllUnixSockets: false
    filesystem:
      allowWrite: [/tmp]
---
`
	frontmatter := map[string]any{
		"sandbox": map[string]any{
			"config": map[string]any{
				"network": map[string]any{
					"allowedDomains":      []any{"example.com"},
					"blockedDomains":      []any{"blocked.example.com"},
					"allowUnixSockets":    []any{"/var/run/test.sock"},
					"allowLocalBinding":   true,
					"allowAllUnixSockets": false,
				},
				"filesystem": map[string]any{"allowWrite": []any{"/tmp"}},
			},
		},
	}

	result, applied, err := codemod.Apply(content, frontmatter)

	require.NoError(t, err)
	assert.True(t, applied)
	assert.Contains(t, result, "allowed-domains:")
	assert.Contains(t, result, "blocked-domains:")
	assert.Contains(t, result, "allow-unix-sockets:")
	assert.Contains(t, result, "allow-local-binding:")
	assert.Contains(t, result, "allow-all-unix-sockets:")
	assert.Contains(t, result, "allow-write:")
}

func TestSandboxFieldsCodemodNoOpAndIdempotent(t *testing.T) {
	t.Parallel()
	codemod := getSandboxFieldsCodemod()
	content := `---
sandbox:
  agent:
    config:
      filesystem:
        allow-write: [/workspace]
---
`
	frontmatter := map[string]any{
		"sandbox": map[string]any{
			"agent": map[string]any{
				"config": map[string]any{
					"filesystem": map[string]any{"allow-write": []any{"/workspace"}},
				},
			},
		},
	}

	result, applied, err := codemod.Apply(content, frontmatter)

	require.NoError(t, err)
	assert.False(t, applied)
	assert.Equal(t, content, result)
}

func TestSandboxFieldsCodemodInlineMappings(t *testing.T) {
	t.Parallel()
	codemod := getSandboxFieldsCodemod()
	content := `---
sandbox:
  agent:
    config: {filesystem: {"allowWrite": [/workspace], denyWrite: [/root]}}
    targets: {copilot: {authHeader: api-key}}
---
`
	frontmatter := map[string]any{
		"sandbox": map[string]any{
			"agent": map[string]any{
				"config":  map[string]any{"filesystem": map[string]any{"allowWrite": []any{"/workspace"}, "denyWrite": []any{"/root"}}},
				"targets": map[string]any{"copilot": map[string]any{"authHeader": "api-key"}},
			},
		},
	}

	result, applied, err := codemod.Apply(content, frontmatter)

	require.NoError(t, err)
	assert.True(t, applied)
	assert.Contains(t, result, `filesystem: {"allow-write": [/workspace], deny-write: [/root]}`)
	assert.Contains(t, result, "copilot: {auth-header: api-key}")
}

func TestSandboxFieldsCodemodRejectsCanonicalKeyCollision(t *testing.T) {
	t.Parallel()
	codemod := getSandboxFieldsCodemod()
	content := `---
sandbox:
  agent:
    config:
      filesystem:
        allowWrite: [/workspace]
        allow-write: [/other]
---
`
	frontmatter := map[string]any{
		"sandbox": map[string]any{
			"agent": map[string]any{
				"config": map[string]any{
					"filesystem": map[string]any{"allowWrite": []any{"/workspace"}, "allow-write": []any{"/other"}},
				},
			},
		},
	}

	result, applied, err := codemod.Apply(content, frontmatter)

	require.ErrorContains(t, err, "both spellings are present")
	assert.False(t, applied)
	assert.Equal(t, content, result)
}

func TestSandboxFieldsCodemodRejectsInlineCanonicalKeyCollision(t *testing.T) {
	t.Parallel()
	codemod := getSandboxFieldsCodemod()
	content := `---
sandbox:
  agent:
    config:
      filesystem: {allowWrite: [/workspace], allow-write: [/other]}
---
`
	frontmatter := map[string]any{
		"sandbox": map[string]any{
			"agent": map[string]any{
				"config": map[string]any{
					"filesystem": map[string]any{"allowWrite": []any{"/workspace"}, "allow-write": []any{"/other"}},
				},
			},
		},
	}

	result, applied, err := codemod.Apply(content, frontmatter)

	require.ErrorContains(t, err, "both spellings are present")
	assert.False(t, applied)
	assert.Equal(t, content, result)
}
