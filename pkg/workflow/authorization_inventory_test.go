//go:build !integration

package workflow

import (
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"strings"
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

func TestAuthorizationInventoryReportsEffectAdapterBinding(t *testing.T) {
	authority := authorizationTestAuthority("https://authority.example/a")
	raw := map[string]any{
		"authorization":      authority,
		"merge-pull-request": map[string]any{"allowed-base-branches": []any{"agent-work/*"}},
		"remove-labels":      map[string]any{},
		"add-comment":        map[string]any{},
		"scripts":            map[string]any{"custom": map[string]any{"script": "return async () => ({})"}},
	}
	routes := map[string]AuthorizationRoute{}
	for _, route := range InspectEffectiveAuthorization(&WorkflowData{SafeOutputs: authorizationTestConfig(t, raw)}) {
		routes[route.Name] = route
	}
	require.Equal(t, "gh-aw.merge-pull-request@1", routes["merge_pull_request"].EffectAdapter)
	require.Equal(t, "resolved_effect", routes["merge_pull_request"].ScopeBinding)
	require.Equal(t, "manager", routes["merge_pull_request"].Coverage, "coverage strings are unchanged")
	require.Equal(t, "gh-aw.remove-labels@1", routes["remove_labels"].EffectAdapter)
	require.Equal(t, "resolved_effect", routes["remove_labels"].ScopeBinding)
	for _, name := range []string{"add_comment", "custom"} {
		require.Empty(t, routes[name].EffectAdapter, name)
		require.Equal(t, "invocation", routes[name].ScopeBinding, name)
	}
	require.Empty(t, routes["noop"].ScopeBinding, "exempt diagnostics request no authority")

	raw["staged"] = true
	for _, route := range InspectEffectiveAuthorization(&WorkflowData{SafeOutputs: authorizationTestConfig(t, raw)}) {
		require.Empty(t, route.ScopeBinding, route.Name)
	}

	// A prepared Claim adapter replaces the native route, so it cannot claim exact-effect coverage.
	delete(raw, "staged")
	raw["claim-adapters"] = map[string]any{"remove-labels": map[string]any{"mode": "prepared", "effect-type": "remove_labels", "target-repo": "owner/repo"}}
	data := &WorkflowData{SafeOutputs: authorizationTestConfig(t, raw), Tools: map[string]any{"work-queue": map[string]any{"worker": true}}}
	require.NoError(t, resolveWorkflowSafeOutputAuthorizations(data))
	for _, route := range InspectEffectiveAuthorization(data) {
		if route.Name == "remove_labels" || route.Name == "remove_labels:prepare_claim_adapter" {
			require.Empty(t, route.EffectAdapter, route.Name)
			require.Equal(t, "invocation", route.ScopeBinding, route.Name)
		}
	}
}

func TestSafeOutputEffectAdaptersMirrorRuntimeRegistry(t *testing.T) {
	source, err := os.ReadFile(filepath.Join("..", "..", "actions", "setup", "js", "safe_output_effect_plan.cjs"))
	require.NoError(t, err)
	version := regexp.MustCompile(`function adapterContract\(id, maxEffects, operations\) \{\n  return Object\.freeze\(\{ id, version: ([0-9]+),`).FindStringSubmatch(string(source))
	require.Len(t, version, 2)
	runtime := regexp.MustCompile(`(?m)^  ([a-z_]+): adapterContract\("([^"]+)",`).FindAllStringSubmatch(string(source), -1)
	mirrored := map[string]string{}
	for _, match := range runtime {
		mirrored[match[1]] = match[2] + "@" + version[1]
	}
	require.Equal(t, safeOutputEffectAdapters, mirrored)
}

func TestEffectAdapterConfigurationRejectsUnplannableEffects(t *testing.T) {
	authority := authorizationTestAuthority("https://authority.example/a")
	cases := map[string]map[string]any{
		"create-issue.close-older-issues":                      {"create-issue": map[string]any{"close-older-issues": true}},
		"create-issue.group-by-day":                            {"create-issue": map[string]any{"group-by-day": true}},
		"add-labels.create-if-missing":                         {"add-labels": map[string]any{"create-if-missing": true}},
		"create-pull-request.fallback-as-issue":                {"create-pull-request": map[string]any{"fallback-as-issue": true}},
		"push-to-pull-request-branch.protected-files":          {"push-to-pull-request-branch": map[string]any{"protected-files": "fallback-to-issue"}},
		"push-to-pull-request-branch.fallback-as-pull-request": {"push-to-pull-request-branch": map[string]any{"fallback-as-pull-request": true}},
	}
	for name, raw := range cases {
		protected := map[string]any{"authorization": authority}
		maps.Copy(protected, raw)
		config := NewCompiler().extractSafeOutputsConfig(map[string]any{"safe-outputs": protected})
		require.ErrorContains(t, resolveSafeOutputAuthorizations(config), strings.Split(name, ".")[0], name)

		// Unprotected and statically staged outputs keep their existing behavior.
		require.NoError(t, resolveSafeOutputAuthorizations(NewCompiler().extractSafeOutputsConfig(map[string]any{"safe-outputs": raw})), name)
		staged := map[string]any{"staged": true}
		maps.Copy(staged, protected)
		require.NoError(t, resolveSafeOutputAuthorizations(NewCompiler().extractSafeOutputsConfig(map[string]any{"safe-outputs": staged})), name)
	}
}
