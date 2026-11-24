//go:build !integration

package workflow

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSafeOutputAuthorizationSerializationFailsClosed(t *testing.T) {
	config := authorizationTestConfig(t, map[string]any{
		"create-issue": map[string]any{"authorization": authorizationTestAuthority("https://authority.example/a")},
	})
	_, err := NewCompiler().buildAuthorizationGateStep(&WorkflowData{SafeOutputs: config}, "create_issue", "job",
		map[string]any{"unsupported": make(chan int)}, nil, authorizationGateOptions{})
	require.ErrorContains(t, err, "cannot serialize")
	local := parseLocalSafeOutputAuthorization(map[string]any{"endpoint": "http://insecure.example"})
	_, err = effectiveSafeOutputAuthorization(nil, local, false)
	require.Error(t, err, "invalid local authorization must survive extraction as a compilation error")
}

func TestSafeOutputAuthorizationFingerprintDoesNotExposeConfiguration(t *testing.T) {
	config := authorizationTestConfig(t, map[string]any{
		"create-issue": map[string]any{"authorization": authorizationTestAuthority("https://authority.example/a")},
	})
	implementation := map[string]any{"credential": "${{ secrets.EXAMPLE_TOKEN }}"}
	encoded, err := authorizationGateConfig(config, "create_issue", "job", implementation, nil, "")
	require.NoError(t, err)
	require.NotContains(t, encoded, "${{ secrets.EXAMPLE_TOKEN }}")

	var gate map[string]any
	require.NoError(t, json.Unmarshal([]byte(encoded), &gate))
	require.Equal(t, "188dec00605d5d6ef26e62137bd6085b659e2d011baa7439d377632f2764ee69", gate["implementation"])
	require.Regexp(t, `^[a-f0-9]{64}$`, gate["implementation"])
}

func TestSafeOutputAuthorizationWorkflowLaunchBoundary(t *testing.T) {
	for _, staged := range []bool{false, true} {
		config := authorizationTestConfig(t, map[string]any{
			"call-workflow": map[string]any{
				"workflows": []any{"worker"}, "staged": staged,
				"authorization": authorizationTestAuthority("https://authority.example/a"),
			},
		})
		data := &WorkflowData{SafeOutputs: config}
		require.NoError(t, resolveWorkflowSafeOutputAuthorizations(data))
		compiler := NewCompiler()
		names, err := compiler.buildCallWorkflowJobs(data, "")
		require.NoError(t, err)
		require.Len(t, names, 2)
		launch, exists := compiler.jobManager.GetJob("call-worker")
		require.True(t, exists)
		gate, exists := compiler.jobManager.GetJob("authorize-call-worker")
		require.True(t, exists)
		require.Contains(t, launch.Needs, gate.Name)
		require.Contains(t, fmt.Sprint(launch.Actions.If), gate.Name)
		require.Contains(t, fmt.Sprint(launch.Actions.If), "authorized")
		steps := strings.Join(gate.Steps, "")
		require.Contains(t, steps, "call_workflow:launch:worker")
		require.Contains(t, steps, "needs.safe_outputs.outputs.call_workflow_payload")
		require.Contains(t, steps, "safe_output_authorization_gate.cjs")
		require.Equal(t, `"${{ needs.safe_outputs.outputs.call_workflow_record_index }}"`, gate.Env["GH_AW_AUTHORIZATION_SOURCE_RECORD_INDEX"])
		if staged {
			require.Equal(t, "staged", config.AuthorizationCoverage["call_workflow:launch:worker"])
			require.NotContains(t, gate.Permissions, "id-token:")
		} else {
			require.Equal(t, "workflow", config.AuthorizationCoverage["call_workflow:launch:worker"])
			require.Contains(t, gate.Permissions, "id-token: write")
		}
	}
}

func authorizationTestAuthority(endpoint string) map[string]any {
	return map[string]any{"endpoint": endpoint, "audience": "https://authority.example/gh-aw"}
}

func authorizationTestConfig(t *testing.T, raw map[string]any) *SafeOutputsConfig {
	t.Helper()
	config := NewCompiler().extractSafeOutputsConfig(map[string]any{"safe-outputs": raw})
	require.NoError(t, resolveSafeOutputAuthorizations(config))
	return config
}

func TestSafeOutputAuthorizationInheritanceAndDiagnostics(t *testing.T) {
	raw := map[string]any{
		"authorization": authorizationTestAuthority("https://authority.example/authorize"),
		"create-issue":  map[string]any{},
		"noop":          map[string]any{"authorization": authorizationTestAuthority("https://authority.example/authorize")},
		"scripts":       map[string]any{"send-mail": map[string]any{"script": "return async () => ({success:true})"}},
		"actions":       map[string]any{"publish": map[string]any{"uses": "./action"}},
		"jobs":          map[string]any{"notify": map[string]any{"steps": []any{map[string]any{"run": "echo reviewed"}}}},
	}
	config := authorizationTestConfig(t, raw)
	require.Equal(t, "global", config.AuthorizationRequirements["create_issue"].Source)
	require.Equal(t, "local", config.AuthorizationRequirements["noop"].Source)
	for _, diagnostic := range []string{"report_missing_tool", "report_missing_data", "report_incomplete"} {
		require.Nil(t, config.AuthorizationRequirements[diagnostic], diagnostic)
	}
	for _, custom := range []string{"send_mail", "publish", "notify"} {
		require.Equal(t, "global", config.AuthorizationRequirements[custom].Source, custom)
	}
	target := map[string]any{"create_issue": map[string]any{}, "noop": map[string]any{}, "report_missing_tool": map[string]any{}}
	addSafeOutputAuthorizationRuntimeConfig(target, config)
	require.Equal(t, false, target["create_issue"].(map[string]any)["builtin"])
	require.Equal(t, true, target["report_missing_tool"].(map[string]any)["builtin"])
	require.Equal(t, "script", target["send_mail"].(map[string]any)["authorization_executor"])
}

func TestSafeOutputAuthorizationValidation(t *testing.T) {
	for _, endpoint := range []string{
		"http://authority.example/a", "https://user:pass@authority.example/a",
		"https://authority.example/a?secret=x", "https://authority.example/a#fragment",
		"https://authority.example/a?", "https://authority.example/a b", "${{ inputs.endpoint }}",
	} {
		t.Run(endpoint, func(t *testing.T) {
			_, err := parseSafeOutputAuthorization(authorizationTestAuthority(endpoint))
			require.Error(t, err)
		})
	}
	for _, raw := range []any{false, true, nil, map[string]any{"endpoint": "https://authority.example/a"}, map[string]any{
		"endpoint": "https://authority.example/a", "audience": "aud", "ignore": true,
	}} {
		config := NewCompiler().extractSafeOutputsConfig(map[string]any{"safe-outputs": map[string]any{"authorization": raw}})
		require.Error(t, resolveSafeOutputAuthorizations(config))
	}
	config := NewCompiler().extractSafeOutputsConfig(map[string]any{"safe-outputs": map[string]any{
		"authorization": authorizationTestAuthority("https://authority.example/a"),
		"create-issue":  map[string]any{"authorization": authorizationTestAuthority("https://different.example/a")},
	}})
	require.ErrorContains(t, resolveSafeOutputAuthorizations(config), "conflicts")
}

func TestSafeOutputAuthorizationRejectsReservedNamesAndHooks(t *testing.T) {
	for _, scripts := range []map[string]any{
		{"authorization": map[string]any{"script": "return () => {}"}},
		{"missing-tool": map[string]any{"script": "return () => {}"}},
		{"create_issue": map[string]any{"script": "return () => {}"}},
		{"send-mail": map[string]any{}, "send_mail": map[string]any{}},
	} {
		config := NewCompiler().extractSafeOutputsConfig(map[string]any{"safe-outputs": map[string]any{"scripts": scripts}})
		require.Error(t, resolveSafeOutputAuthorizations(config))
	}
	config := NewCompiler().extractSafeOutputsConfig(map[string]any{"safe-outputs": map[string]any{
		"authorization": authorizationTestAuthority("https://authority.example/a"),
		"steps":         []any{map[string]any{"run": "echo hook"}},
	}})
	require.ErrorContains(t, resolveSafeOutputAuthorizations(config), "pre-output hooks")
	config = NewCompiler().extractSafeOutputsConfig(map[string]any{"safe-outputs": map[string]any{
		"authorization":       authorizationTestAuthority("https://authority.example/a"),
		"report-missing-tool": map[string]any{},
		"missing-tool":        map[string]any{},
	}})
	require.ErrorContains(t, resolveSafeOutputAuthorizations(config), "multiple aliases")
}

func TestSafeOutputAuthorizationImports(t *testing.T) {
	c := NewCompiler()
	top := c.extractSafeOutputsConfig(map[string]any{"safe-outputs": map[string]any{
		"authorization": authorizationTestAuthority("https://authority.example/a"),
	}})
	for _, same := range []bool{true, false} {
		endpoint := "https://authority.example/a"
		if !same {
			endpoint = "https://different.example/a"
		}
		encoded, err := json.Marshal(map[string]any{
			"authorization": authorizationTestAuthority(endpoint),
			"add-comment":   map[string]any{},
		})
		require.NoError(t, err)
		merged, err := c.MergeSafeOutputs(top, []string{string(encoded)}, nil)
		if same {
			require.NoError(t, err)
			require.NoError(t, resolveSafeOutputAuthorizations(merged))
			require.Equal(t, "global", merged.AuthorizationRequirements["add_comment"].Source)
		} else {
			require.ErrorContains(t, err, "conflicting")
		}
	}
}

func TestSafeOutputAuthorizationSharedWorkflowImports(t *testing.T) {
	const authority = "    endpoint: https://authority.example/a\n    audience: https://authority.example/gh-aw\n"
	const global = "safe-outputs:\n  authorization:\n" + authority
	const local = "safe-outputs:\n  create-issue:\n    authorization:\n      endpoint: https://authority.example/a\n      audience: https://authority.example/gh-aw\n"
	for _, test := range []struct {
		name, shared, main, source, errorText, tool, additional string
		nested                                                  bool
	}{
		{name: "policy-only", shared: global, main: "safe-outputs:\n  create-issue:\n    max: 1\n", source: "global"},
		{name: "nested-policy", shared: global, main: "safe-outputs:\n  create-issue:\n    max: 1\n", source: "global", nested: true},
		{name: "policy-from-sibling-import", shared: global, additional: "safe-outputs:\n  create-issue:\n    max: 1\n", source: "global"},
		{name: "outputs-and-policy", shared: global + "  create-issue:\n    max: 1\n", source: "global"},
		{name: "identical-authorities", shared: global, main: global + "  create-issue:\n    max: 1\n", source: "global"},
		{name: "local-authority", shared: local, source: "local"},
		{name: "local-authority-with-override", shared: local, main: "safe-outputs:\n  create-issue:\n    max: 2\n", source: "local"},
		{name: "global-conflict", shared: global, main: strings.ReplaceAll(global, "authority.example", "different.example") + "  create-issue:\n    max: 1\n", errorText: "conflicting"},
		{name: "local-conflict", shared: local, main: strings.ReplaceAll(local, "authority.example", "different.example"), errorText: "conflicting"},
		{name: "disabled-import", shared: "safe-outputs:\n  authorization: false\n", main: "safe-outputs:\n  create-issue:\n    max: 1\n", errorText: "authorization"},
		{name: "custom-script", tool: "notify", source: "local", nested: true, main: "safe-outputs:\n  scripts:\n    notify:\n      script: return async () => ({success:true})\n", shared: "safe-outputs:\n  scripts:\n    notify:\n      script: return async () => ({success:true})\n      authorization:\n        endpoint: https://authority.example/a\n        audience: https://authority.example/gh-aw\n"},
		{name: "custom-action", tool: "notify", source: "local", main: "safe-outputs:\n  actions:\n    notify:\n      uses: ./reviewed-action\n", shared: "safe-outputs:\n  actions:\n    notify:\n      uses: ./reviewed-action\n      authorization:\n        endpoint: https://authority.example/a\n        audience: https://authority.example/gh-aw\n"},
		{name: "custom-job", tool: "notify", source: "local", shared: "safe-outputs:\n  jobs:\n    notify:\n      steps:\n        - run: echo reviewed\n      authorization:\n        endpoint: https://authority.example/a\n        audience: https://authority.example/gh-aw\n"},
	} {
		t.Run(test.name, func(t *testing.T) {
			dir := t.TempDir()
			write := func(name, frontmatter string) {
				t.Helper()
				require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte("---\n"+frontmatter+"---\nShared authorization.\n"), 0o600))
			}
			write("policy.md", test.shared)
			importPath := "./policy.md"
			if test.nested {
				write("shared.md", "imports:\n  - ./policy.md\n")
				importPath = "./shared.md"
			}
			imports := "imports:\n  - " + importPath + "\n"
			if test.additional != "" {
				write("outputs.md", test.additional)
				imports += "  - ./outputs.md\n"
			}
			write("main.md", "on: workflow_dispatch\n"+imports+test.main)
			data, err := NewCompiler().ParseWorkflowFile(filepath.Join(dir, "main.md"))
			if test.errorText != "" {
				require.ErrorContains(t, err, test.errorText)
				return
			}
			require.NoError(t, err)
			require.NotNil(t, data.SafeOutputs)
			tool := test.tool
			if tool == "" {
				tool = "create_issue"
			}
			require.NotNil(t, data.SafeOutputs.AuthorizationRequirements[tool])
			require.Equal(t, "https://authority.example/a", data.SafeOutputs.AuthorizationRequirements[tool].Endpoint)
			require.Equal(t, test.source, data.SafeOutputs.AuthorizationRequirements[tool].Source)
			if test.name == "custom-job" {
				compiler := NewCompiler()
				_, err := compiler.buildSafeJobs(data, false)
				require.NoError(t, err)
				job, exists := compiler.jobManager.GetJob(tool)
				require.True(t, exists)
				require.Contains(t, job.Permissions, "id-token: write")
				require.Contains(t, strings.Join(job.Steps, ""), "safe_output_authorization_gate.cjs")
			} else {
				require.Contains(t, ComputePermissionsForSafeOutputs(data.SafeOutputs).RenderToYAML(), "id-token: write")
			}
			generated, err := generateSafeOutputsConfig(data)
			require.NoError(t, err)
			var runtimeConfig map[string]any
			require.NoError(t, json.Unmarshal([]byte(generated), &runtimeConfig))
			require.NotNil(t, runtimeConfig[tool].(map[string]any)["authorization"])
		})
	}
}

func TestSafeOutputAuthorizationStagingAndExecutorOIDC(t *testing.T) {
	for _, staged := range []any{true, "${{ inputs.preview }}"} {
		config := authorizationTestConfig(t, map[string]any{
			"create-issue": map[string]any{
				"authorization": authorizationTestAuthority("https://authority.example/a"),
				"staged":        staged,
			},
		})
		permissions := ComputePermissionsForSafeOutputs(config).RenderToYAML()
		if staged == true {
			require.NotContains(t, permissions, "id-token:")
		} else {
			require.Contains(t, permissions, "id-token: write")
		}
	}
	config := authorizationTestConfig(t, map[string]any{
		"create-issue":         false,
		"upload-code-coverage": map[string]any{"authorization": authorizationTestAuthority("https://authority.example/a")},
	})
	require.NotContains(t, ComputePermissionsForSafeOutputs(config).RenderToYAML(), "id-token:")
	job, err := NewCompiler().buildUploadCodeCoverageJob(&WorkflowData{SafeOutputs: config}, "agent")
	require.NoError(t, err)
	require.Contains(t, job.Permissions, "id-token: write")
	steps := strings.Join(job.Steps, "")
	require.Contains(t, steps, "safe_output_authorization_gate.cjs")
	require.Contains(t, steps, "steps.authorize_upload_code_coverage.outputs.authorized == 'true'")
	require.Less(t, strings.Index(steps, "id: authorize_upload_code_coverage"), strings.Index(steps, "id: upload_code_coverage"))
}

func TestSafeOutputAuthorizationCustomActionGate(t *testing.T) {
	config := authorizationTestConfig(t, map[string]any{
		"actions": map[string]any{"notify": map[string]any{
			"uses":          "./reviewed-action",
			"authorization": authorizationTestAuthority("https://authority.example/a"),
		}},
	})
	actionSteps, err := NewCompiler().buildActionSteps(&WorkflowData{SafeOutputs: config})
	require.NoError(t, err)
	steps := strings.Join(actionSteps, "")
	require.Contains(t, steps, "GH_AW_AUTHORIZATION_PAYLOAD: ${{ steps.process_safe_outputs.outputs.action_notify_payload }}")
	require.Contains(t, steps, "steps.authorize_notify.outputs.authorized == 'true'")
	require.Less(t, strings.Index(steps, "id: authorize_notify"), strings.Index(steps, "id: action_notify"))
}

func TestSafeOutputAuthorizationNativeLocalConfiguration(t *testing.T) {
	for _, handler := range safeOutputHandlers {
		if handler.ToolName == "" || handler.ToolName == "dispatch_repository" {
			continue
		}
		t.Run(handler.Key, func(t *testing.T) {
			config := NewCompiler().extractSafeOutputsConfig(map[string]any{"safe-outputs": map[string]any{
				handler.Key: map[string]any{"authorization": authorizationTestAuthority("https://authority.example/a")},
			}})
			require.NoError(t, resolveSafeOutputAuthorizations(config))
			require.NotNil(t, config.AuthorizationRequirements[handler.ToolName], "local authorization must not be discarded by native extraction")
			require.Equal(t, "local", config.AuthorizationRequirements[handler.ToolName].Source)
			require.NotEmpty(t, config.AuthorizationCoverage[handler.ToolName])
		})
	}
}

func TestSafeOutputAuthorizationWorkflowLaunchBinding(t *testing.T) {
	config := authorizationTestConfig(t, map[string]any{
		"call-workflow": map[string]any{
			"workflows":     []any{"worker"},
			"authorization": authorizationTestAuthority("https://authority.example/a"),
		},
	})
	config.CallWorkflow.WorkflowFiles = map[string]string{"worker": "./.github/workflows/worker.yml"}
	target := map[string]any{"call_workflow": map[string]any{}}
	addSafeOutputAuthorizationRuntimeConfig(target, config)
	bindings := target["call_workflow"].(map[string]any)["workflow-implementations"].(map[string]any)
	require.Equal(t, "./.github/workflows/worker.yml", bindings["worker"].(map[string]any)["path"])
	require.Equal(t, "${{ github.workflow_sha }}", bindings["worker"].(map[string]any)["revision"])
}

func TestSafeOutputAuthorizationNamedRepositoryDispatch(t *testing.T) {
	config := authorizationTestConfig(t, map[string]any{
		"dispatch-repository": map[string]any{
			"authorization": authorizationTestAuthority("https://authority.example/a"),
			"notify":        map[string]any{"event_type": "notify", "repository": "owner/repo"},
		},
	})
	require.Equal(t, "local", config.AuthorizationRequirements["notify"].Source)
	target := map[string]any{"dispatch_repository": workflowHandlerRegistry["dispatch_repository"](config)}
	addSafeOutputAuthorizationRuntimeConfig(target, config)
	tool := target["dispatch_repository"].(map[string]any)["tools"].(map[string]any)["notify"].(map[string]any)
	require.Equal(t, config.AuthorizationRequirements["notify"], tool["authorization"])
	require.Equal(t, "manager", config.AuthorizationCoverage["notify"])
}

func TestSafeOutputAuthorizationImportCannotOverrideAuthority(t *testing.T) {
	for _, conflict := range []bool{false, true} {
		c := NewCompiler()
		raw := map[string]any{"create-issue": map[string]any{"max": 2}}
		if conflict {
			raw["create-issue"].(map[string]any)["authorization"] = authorizationTestAuthority("https://different.example/a")
		}
		top := c.extractSafeOutputsConfig(map[string]any{"safe-outputs": raw})
		imported, err := json.Marshal(map[string]any{
			"create-issue": map[string]any{"authorization": authorizationTestAuthority("https://authority.example/a")},
		})
		require.NoError(t, err)
		merged, err := c.MergeSafeOutputs(top, []string{string(imported)}, raw)
		if conflict {
			require.ErrorContains(t, err, "conflicting")
			continue
		}
		require.NoError(t, err)
		require.NoError(t, resolveSafeOutputAuthorizations(merged))
		require.Equal(t, "https://authority.example/a", merged.AuthorizationRequirements["create_issue"].Endpoint)
		require.Equal(t, 2, templatableIntValue(merged.CreateIssues.Max), "ordinary main-workflow override behavior must be preserved")
	}
}

func TestSafeOutputAuthorizationCustomJobBoundary(t *testing.T) {
	for _, staged := range []bool{false, true} {
		c := NewCompiler()
		config := authorizationTestConfig(t, map[string]any{
			"jobs": map[string]any{"notify": map[string]any{
				"authorization": authorizationTestAuthority("https://authority.example/a"),
				"staged":        staged,
				"steps": []any{
					map[string]any{"name": "Author setup", "run": "echo setup", "if": "always()"},
					map[string]any{"name": "Author executor", "run": "echo effect"},
				},
			}},
		})
		_, err := c.buildSafeJobs(&WorkflowData{SafeOutputs: config}, false)
		require.NoError(t, err)
		job, exists := c.jobManager.GetJob("notify")
		require.True(t, exists)
		steps := strings.Join(job.Steps, "")
		require.Less(t, strings.Index(steps, "id: authorize_notify"), strings.Index(steps, "name: Author setup"))
		require.Equal(t, 2, strings.Count(steps, "steps.authorize_notify.outputs.authorized == 'true'"))
		if staged {
			require.NotContains(t, job.Permissions, "id-token:")
		} else {
			require.Contains(t, job.Permissions, "id-token: write")
		}
	}
	c := NewCompiler()
	config := authorizationTestConfig(t, map[string]any{
		"jobs": map[string]any{"notify": map[string]any{
			"authorization": authorizationTestAuthority("https://authority.example/a"),
			"permissions":   map[string]any{"id-token": "none"},
			"steps":         []any{map[string]any{"run": "echo reviewed"}},
		}},
	})
	_, err := c.buildSafeJobs(&WorkflowData{SafeOutputs: config}, false)
	require.ErrorContains(t, err, "requires id-token: write")
}

func TestSafeOutputAuthorizationPreparedClaimAdapters(t *testing.T) {
	for _, kind := range []string{"jobs", "actions", "scripts"} {
		t.Run(kind, func(t *testing.T) {
			definition := map[string]any{"authorization": authorizationTestAuthority("https://authority.example/a")}
			switch kind {
			case "jobs":
				definition["steps"] = []any{map[string]any{"name": "Author preparation", "run": "echo prepare", "if": "always()"}}
			case "actions":
				definition["uses"] = "./reviewed-action"
			case "scripts":
				definition["script"] = "return async () => ({success:true})"
			}
			config := authorizationTestConfig(t, map[string]any{
				kind: map[string]any{"custom": definition},
				"claim-adapters": map[string]any{"custom": map[string]any{
					"mode": "prepared", "effect-type": "update_issue", "target-repo": "owner/repo",
				}},
			})
			data := &WorkflowData{SafeOutputs: config, Tools: map[string]any{"work-queue": map[string]any{"worker": true}}}
			require.NoError(t, resolveWorkflowSafeOutputAuthorizations(data))
			require.Equal(t, config.AuthorizationCoverage["custom"], config.AuthorizationCoverage["custom:prepare_claim_adapter"])
			configuration, err := json.Marshal(config.ClaimAdapters["custom"])
			require.NoError(t, err)
			job, err := NewCompiler().buildWorkQueuePreparedAdapterJob(data, "custom", configuration, false)
			require.NoError(t, err)
			require.Contains(t, job.Permissions, "id-token: write")
			steps := strings.Join(job.Steps, "")
			require.Contains(t, steps, `custom:prepare_claim_adapter`)
			require.Contains(t, steps, "GH_AW_AUTHORIZATION_CLAIM_HANDLE: ${{ steps.claim_adapter_context.outputs.claim_handle }}")
			require.Contains(t, steps, "steps.claim_adapter_context.outputs.active == 'true' && steps.authorize_custom.outputs.authorized == 'true'")
			staged := TemplatableBool("true")
			config.Staged = &staged
			stagedJob, err := NewCompiler().buildWorkQueuePreparedAdapterJob(data, "custom", configuration, false)
			require.NoError(t, err)
			require.NotContains(t, stagedJob.Permissions, "id-token:")
			require.Equal(t, `"true"`, stagedJob.Env["GH_AW_SAFE_OUTPUTS_STAGED"])
			config.Staged = nil
			switch kind {
			case "jobs":
				config.Jobs["custom"].Staged = &staged
			case "actions":
				config.Actions["custom"].Staged = &staged
			case "scripts":
				config.Scripts["custom"].Staged = &staged
			}
			localStagedJob, err := NewCompiler().buildWorkQueuePreparedAdapterJob(data, "custom", configuration, false)
			require.NoError(t, err)
			require.NotContains(t, localStagedJob.Permissions, "id-token:")
			require.Equal(t, `"true"`, localStagedJob.Env["GH_AW_CLAIM_ADAPTER_STAGED"])
		})
	}
}

func TestSafeOutputAuthorizationInjectedMemoryCoverage(t *testing.T) {
	for _, staged := range []bool{false, true} {
		config := authorizationTestConfig(t, map[string]any{
			"authorization": authorizationTestAuthority("https://authority.example/a"),
			"staged":        staged,
		})
		data := &WorkflowData{
			SafeOutputs: config, CommentMemoryConfig: &CommentMemoryConfig{},
			RepoMemoryConfig: &RepoMemoryConfig{Memories: []RepoMemoryEntry{{ID: "notes", Ledger: &RepoMemoryLedgerConfig{}}}},
			LedgerConfig:     &LedgerToolConfig{Ledgers: []LedgerConfig{{Name: "events", Compaction: &LedgerCompactionConfig{}}}},
		}
		require.NoError(t, resolveWorkflowSafeOutputAuthorizations(data))
		for _, name := range []string{commentMemoryHandlerKey, ledgerMutationHandlerKey, "ledger_append", ledgerRequestCompactionHandlerKey} {
			require.NotNil(t, config.AuthorizationRequirements[name], name)
			if staged {
				require.Equal(t, "staged", config.AuthorizationCoverage[name], name)
			} else {
				require.Equal(t, "manager", config.AuthorizationCoverage[name], name)
			}
		}
		require.Equal(t, !staged, safeOutputsAuthorizationNeedsOIDC(config))
	}
	none := "none"
	config := authorizationTestConfig(t, map[string]any{"authorization": authorizationTestAuthority("https://authority.example/a")})
	config.IDToken = &none
	require.ErrorContains(t, resolveWorkflowSafeOutputAuthorizations(&WorkflowData{
		SafeOutputs: config, CommentMemoryConfig: &CommentMemoryConfig{},
	}), "requires id-token: write")
	staged := TemplatableBool("true")
	require.NoError(t, resolveWorkflowSafeOutputAuthorizations(&WorkflowData{
		SafeOutputs: config, CommentMemoryConfig: &CommentMemoryConfig{BaseSafeOutputConfig: BaseSafeOutputConfig{Staged: &staged}},
	}))
	require.False(t, safeOutputsAuthorizationNeedsOIDC(config))
}
