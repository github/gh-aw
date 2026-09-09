//go:build integration

package workflow

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestImportedEngineWithAnthropicWIFAuthIntegration(t *testing.T) {
	tmpDir := testutil.TempDir(t, "test-imported-wif-auth-*")
	workflowsDir := filepath.Join(tmpDir, constants.GetWorkflowDir())
	sharedDir := filepath.Join(workflowsDir, "shared")
	require.NoError(t, os.MkdirAll(sharedDir, 0755))

	sharedContent := `---
engine:
  id: claude
  auth:
    type: github-oidc
    provider: anthropic
    federation-rule-id: fr_01ABC
    organization-id: org_01XYZ
    service-account-id: sa_01DEF
    workspace-id: ws_01GHI
---

# Shared Anthropic WIF engine config
`
	sharedFile := filepath.Join(sharedDir, "wif-engine.md")
	require.NoError(t, os.WriteFile(sharedFile, []byte(sharedContent), 0644))

	mainContent := `---
name: Test Imported WIF Engine
on:
  workflow_dispatch:
permissions:
  contents: read
  id-token: write
imports:
  - shared/wif-engine.md
---

# Test Workflow
`
	mainFile := filepath.Join(workflowsDir, "test-wif.md")
	require.NoError(t, os.WriteFile(mainFile, []byte(mainContent), 0644))

	compiler := NewCompiler()
	require.NoError(t, compiler.CompileWorkflow(mainFile))

	lockFile := filepath.Join(workflowsDir, "test-wif.lock.yml")
	lockContent, err := os.ReadFile(lockFile)
	require.NoError(t, err)

	lockStr := string(lockContent)
	assert.Contains(t, lockStr, "AWF_AUTH_TYPE: github-oidc")
	assert.Contains(t, lockStr, "AWF_AUTH_PROVIDER: anthropic")
	assert.Contains(t, lockStr, "AWF_AUTH_ANTHROPIC_FEDERATION_RULE_ID: fr_01ABC")
	assert.Contains(t, lockStr, "AWF_AUTH_ANTHROPIC_ORGANIZATION_ID: org_01XYZ")
	assert.Contains(t, lockStr, "AWF_AUTH_ANTHROPIC_SERVICE_ACCOUNT_ID: sa_01DEF")
	assert.Contains(t, lockStr, "AWF_AUTH_ANTHROPIC_WORKSPACE_ID: ws_01GHI")
}

func TestImportedEngineWithMalformedAuthMappingStillFailsIntegration(t *testing.T) {
	tmpDir := testutil.TempDir(t, "test-imported-malformed-auth-*")
	workflowsDir := filepath.Join(tmpDir, constants.GetWorkflowDir())
	sharedDir := filepath.Join(workflowsDir, "shared")
	require.NoError(t, os.MkdirAll(sharedDir, 0755))

	sharedContent := `---
engine:
  id: claude
  auth:
    role: session
    secret: ANTHROPIC_API_KEY
---

# Shared malformed auth config
`
	sharedFile := filepath.Join(sharedDir, "bad-auth-engine.md")
	require.NoError(t, os.WriteFile(sharedFile, []byte(sharedContent), 0644))

	mainContent := `---
name: Test Imported Invalid Engine Auth
on:
  workflow_dispatch:
permissions:
  contents: read
imports:
  - shared/bad-auth-engine.md
---

# Test Workflow
`
	mainFile := filepath.Join(workflowsDir, "test-invalid-auth.md")
	require.NoError(t, os.WriteFile(mainFile, []byte(mainContent), 0644))

	compiler := NewCompiler()
	err := compiler.CompileWorkflow(mainFile)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "Unknown properties: role, secret")
}
