//go:build !integration

package workflow

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/github/gh-aw/pkg/parser"
	"github.com/stretchr/testify/require"
)

func TestAuthorizationInventoryIncludesUnconfiguredRoutes(t *testing.T) {
	for _, authorized := range []bool{false, true} {
		raw := map[string]any{
			"create-issue":   map[string]any{},
			"upload-asset":   map[string]any{},
			"call-workflow":  map[string]any{"workflows": []any{"worker"}},
			"scripts":        map[string]any{"custom": map[string]any{"script": "throw new Error('must not initialize')"}},
			"actions":        map[string]any{"publish": map[string]any{"uses": "./action"}},
			"jobs":           map[string]any{"notify": map[string]any{"steps": []any{map[string]any{"run": "exit 93"}}}},
			"claim-adapters": map[string]any{"custom": map[string]any{"mode": "prepared", "effect-type": "update_issue", "target-repo": "owner/repo"}},
		}
		if authorized {
			raw["authorization"] = authorizationTestAuthority("https://authority.example/a")
		}
		config := authorizationTestConfig(t, raw)
		data := &WorkflowData{
			SafeOutputs: config, CommentMemoryConfig: &CommentMemoryConfig{},
			RepoMemoryConfig: &RepoMemoryConfig{Memories: []RepoMemoryEntry{{ID: "notes", Ledger: &RepoMemoryLedgerConfig{}}}},
			LedgerConfig:     &LedgerToolConfig{Ledgers: []LedgerConfig{{Name: "events", Compaction: &LedgerCompactionConfig{}}}},
			Tools:            map[string]any{"work-queue": map[string]any{"worker": true}},
		}
		require.NoError(t, resolveWorkflowSafeOutputAuthorizations(data))
		routes := map[string]AuthorizationRoute{}
		for _, route := range InspectEffectiveAuthorization(data) {
			routes[route.Name] = route
		}
		for _, name := range []string{
			"create_issue", "upload_asset", "call_workflow", "call_workflow:launch:worker",
			"custom", "custom:prepare_claim_adapter", "publish", "notify",
			commentMemoryHandlerKey, ledgerMutationHandlerKey, "ledger_append", ledgerRequestCompactionHandlerKey,
		} {
			route, exists := routes[name]
			require.True(t, exists, name)
			require.False(t, route.Exempt, name)
			require.Equal(t, authorized, route.Authorization != nil, name)
		}
		for _, name := range []string{"noop", "report_missing_tool", "report_missing_data", "report_incomplete"} {
			require.True(t, routes[name].Exempt, name)
		}
	}
}

func TestAuthorizationInventoryExplicitDiagnosticAndStaging(t *testing.T) {
	for _, staged := range []any{true, "${{ inputs.preview }}"} {
		config := authorizationTestConfig(t, map[string]any{
			"staged":  staged,
			"noop":    map[string]any{"authorization": authorizationTestAuthority("https://authority.example/a")},
			"scripts": map[string]any{"diagnostic-looking": map[string]any{"script": "return () => ({noop: true})"}},
		})
		routes := InspectEffectiveAuthorization(&WorkflowData{SafeOutputs: config})
		for _, route := range routes {
			require.Equal(t, staged == true, route.Staged)
			if route.Name == "noop" {
				require.NotNil(t, route.Authorization)
				require.False(t, route.Exempt)
			}
			if route.Name == "diagnostic_looking" {
				require.False(t, route.Exempt)
				require.Nil(t, route.Authorization)
			}
		}
	}
}

func TestAuthorizationCompilerRejectsUncontrolledInputs(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "workflow.md")
	cache := parser.NewImportCache(root)
	cache.SetLocalOnlyRoot(root)
	_, err := parser.ResolveIncludePath("owner/repo/policy.md@0123456789012345678901234567890123456789", root, cache)
	require.ErrorContains(t, err, "local dependency")
	_, err = NewAuthorizationVerificationCompiler(root).actionResolver.ResolveSHA(t.Context(), "unknown/action", "v999999")
	require.ErrorContains(t, err, "unpinned action")

	for _, stop := range []string{"+48h", "2030-01-02 03:04:05"} {
		compiler := NewAuthorizationVerificationCompiler(root)
		data := &WorkflowData{StopTime: stop}
		// A forged lock's timestamp must not be used as a compiler input.
		require.NoError(t, os.WriteFile(filepath.Join(root, "workflow.lock.yml"), []byte("# gh-aw-metadata: {\"stop_time\":\"2099-12-31 00:00:00\"}\n"), 0o600))
		err := compiler.resolveAndApplyStopTime(data, path, stop)
		if stop == "+48h" {
			require.NoError(t, os.WriteFile(path, []byte("---\non:\n  workflow_dispatch:\n  stop-after: +48h\npermissions:\n  contents: read\n---\n# Test\nDo nothing.\n"), 0o600))
			_, _, err = compiler.CompileAuthorizationSnapshot(path)
			require.ErrorContains(t, err, "absolute")
		} else {
			require.NoError(t, err)
			require.Equal(t, stop, data.StopTime)
		}
	}
	_, _, err = NewCompiler().CompileAuthorizationSnapshot(path)
	require.ErrorContains(t, err, "read-only")
}
