//go:build !integration

package workflow

import (
	"encoding/json"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCustomJobStagingWithoutAuthorization(t *testing.T) {
	for _, test := range []struct {
		name      string
		global    any
		local     any
		condition string
	}{
		{name: "local literal", local: true, condition: "false"},
		{name: "global literal", global: true, condition: "false"},
		{name: "local expression", local: "${{ inputs.preview }}", condition: "format('{0}', inputs.preview) != 'true'"},
		{name: "global expression", global: "${{ inputs.preview }}", condition: "format('{0}', inputs.preview) != 'true'"},
		{name: "both expressions", global: "${{ inputs.preview }}", local: "${{ inputs.local_preview }}",
			condition: "format('{0}', inputs.preview) != 'true' && format('{0}', inputs.local_preview) != 'true'"},
	} {
		t.Run(test.name, func(t *testing.T) {
			definition := map[string]any{
				"permissions": map[string]any{"contents": "write"},
				"steps": []any{
					map[string]any{"name": "Author setup", "run": "echo setup", "if": "${{ always() }}"},
					map[string]any{"name": "Author mutation", "run": "echo mutate"},
				},
			}
			if test.local != nil {
				definition["staged"] = test.local
			}
			raw := map[string]any{"jobs": map[string]any{"notify": definition}}
			if test.global != nil {
				raw["staged"] = test.global
			}
			config := authorizationTestConfig(t, raw)
			compiler := NewCompiler()
			_, err := compiler.buildSafeJobs(&WorkflowData{SafeOutputs: config}, false)
			require.NoError(t, err)
			job, exists := compiler.jobManager.GetJob("notify")
			require.True(t, exists)
			steps := strings.Join(job.Steps, "")
			require.Equal(t, 2, strings.Count(steps, test.condition))
			require.Contains(t, steps, test.condition+" && (always())")
			require.NotContains(t, steps, "safe_output_authorization_gate.cjs")
			require.NotContains(t, job.Permissions, "id-token:")
		})
	}
}

func TestCustomStagingMetadataWithoutAuthorization(t *testing.T) {
	for _, staged := range []any{true, "${{ inputs.preview }}"} {
		config := authorizationTestConfig(t, map[string]any{
			"scripts": map[string]any{"send-mail": map[string]any{"script": "return () => {}", "staged": staged}},
			"actions": map[string]any{"notify": map[string]any{"uses": "./action", "staged": staged}},
		})
		for _, agentFacing := range []bool{false, true} {
			target := make(map[string]any)
			if agentFacing {
				addSafeScriptsConfig(target, config.Scripts)
				require.NoError(t, addSafeActionsConfig(target, config.Actions))
			}
			addSafeOutputAuthorizationRuntimeConfig(target, config)
			for _, name := range []string{"send_mail", "notify"} {
				entry := target[name].(map[string]any)
				if staged == true {
					require.Equal(t, true, entry["staged"])
				} else {
					require.Equal(t, "${{ format('{0}', inputs.preview) == 'true' }}", entry["staged"])
				}
				require.NotContains(t, entry, "authorization")
			}
		}
		var managerEnv []string
		NewCompiler().addHandlerManagerConfigEnvVar(&managerEnv, &WorkflowData{SafeOutputs: config})
		encoded := strings.TrimPrefix(strings.TrimSpace(strings.Join(managerEnv, "")), "GH_AW_SAFE_OUTPUTS_HANDLER_CONFIG: ")
		decoded, err := strconv.Unquote(encoded)
		require.NoError(t, err)
		var managerConfig map[string]any
		require.NoError(t, json.Unmarshal([]byte(decoded), &managerConfig))
		for _, name := range []string{"send_mail", "notify"} {
			require.NotNil(t, managerConfig[name].(map[string]any)["staged"])
		}
	}
}

func TestCustomJobAuthorizationMetadataPreservesToolDefinition(t *testing.T) {
	config := authorizationTestConfig(t, map[string]any{
		"authorization": authorizationTestAuthority("https://authority.example/a"),
		"jobs": map[string]any{"send-notification": map[string]any{
			"description": "Send a notification", "max": 7,
			"inputs": map[string]any{"body": map[string]any{"type": "string", "required": true}},
			"steps":  []any{map[string]any{"run": "echo send"}},
		}},
	})
	target := make(map[string]any)
	addSafeJobsConfig(target, config.Jobs)
	addSafeOutputAuthorizationRuntimeConfig(target, config)
	require.NotContains(t, target, "send_notification")
	entry := target["send-notification"].(map[string]any)
	require.Equal(t, "Send a notification", entry["description"])
	require.NotEmpty(t, entry["inputs"])
	require.NotEmpty(t, entry["max"])
	require.Equal(t, config.AuthorizationRequirements["send_notification"], entry["authorization"])
}

func TestGlobalAuthorizationIsNotAnAgentTool(t *testing.T) {
	config := authorizationTestConfig(t, map[string]any{
		"authorization": authorizationTestAuthority("https://authority.example/a"),
		"add-comment":   map[string]any{},
	})
	generated, err := generateSafeOutputsConfig(&WorkflowData{SafeOutputs: config})
	require.NoError(t, err)
	var agentConfig map[string]any
	require.NoError(t, json.Unmarshal([]byte(generated), &agentConfig))
	require.NotContains(t, agentConfig, "authorization")
	require.NotNil(t, agentConfig["add_comment"].(map[string]any)["authorization"])
	managerConfig := make(map[string]any)
	addSafeOutputAuthorizationRuntimeConfig(managerConfig, config)
	require.Equal(t, config.Authorization, managerConfig["authorization"])
}

func TestImportedCustomAuthorizationDoesNotEnableImplementations(t *testing.T) {
	for _, authorized := range []bool{false, true} {
		t.Run(map[bool]string{false: "unprotected", true: "protected"}[authorized], func(t *testing.T) {
			c := NewCompiler()
			script := map[string]any{"script": "return () => ({imported:true})"}
			action := map[string]any{"uses": "./imported-action"}
			dispatch := map[string]any{"event_type": "imported", "repository": "owner/imported"}
			if authorized {
				for _, definition := range []map[string]any{script, action, dispatch} {
					definition["authorization"] = authorizationTestAuthority("https://authority.example/a")
				}
			}
			imported, err := json.Marshal(map[string]any{
				"scripts":             map[string]any{"notify": script, "unused-script": script},
				"actions":             map[string]any{"publish": action, "unused-action": action},
				"dispatch-repository": map[string]any{"send": dispatch, "unused-dispatch": dispatch},
			})
			require.NoError(t, err)
			raw := map[string]any{
				"scripts":             map[string]any{"notify": map[string]any{"script": "return () => ({local:true})"}},
				"actions":             map[string]any{"publish": map[string]any{"uses": "./local-action"}},
				"dispatch-repository": map[string]any{"send": map[string]any{"event_type": "local", "repository": "owner/local"}},
			}
			top := c.extractSafeOutputsConfig(map[string]any{"safe-outputs": raw})
			merged, err := c.MergeSafeOutputs(top, []string{string(imported)}, raw)
			require.NoError(t, err)
			require.NoError(t, resolveSafeOutputAuthorizations(merged))
			require.Len(t, merged.Scripts, 1)
			require.Equal(t, "return () => ({local:true})", merged.Scripts["notify"].Script)
			require.Len(t, merged.Actions, 1)
			require.Equal(t, "./local-action", merged.Actions["publish"].Uses)
			require.Len(t, merged.DispatchRepository.Tools, 1)
			require.Equal(t, "local", merged.DispatchRepository.Tools["send"].EventType)
			for _, name := range []string{"notify", "publish", "send"} {
				require.Equal(t, authorized, merged.AuthorizationRequirements[name] != nil, name)
			}
			for _, name := range []string{"unused_script", "unused_action", "unused_dispatch"} {
				require.NotContains(t, merged.AuthorizationRequirements, name)
			}
			if authorized {
				managed := &SafeOutputAuthorization{FromEnvironment: true}
				policy := &SafeOutputsConfig{Scripts: map[string]*SafeScriptConfig{"notify": {Authorization: managed}}}
				selected := &SafeOutputsConfig{Scripts: map[string]*SafeScriptConfig{"notify": {Script: "return () => {}"}}}
				require.NoError(t, mergeSafeOutputAuthorizations(selected, policy))
				require.NoError(t, resolveSafeOutputAuthorizations(selected))
				require.True(t, selected.AuthorizationRequirements["notify"].FromEnvironment)
			}

			onlyImports, err := c.MergeSafeOutputs(nil, []string{string(imported)}, nil)
			require.NoError(t, err)
			require.NoError(t, resolveSafeOutputAuthorizations(onlyImports))
			require.Empty(t, onlyImports.Scripts)
			require.Empty(t, onlyImports.Actions)
		})
	}
}

func TestImportedCustomAuthorizationRejectsConflictingAuthorities(t *testing.T) {
	for _, kind := range []string{"scripts", "actions", "dispatch-repository"} {
		t.Run(kind, func(t *testing.T) {
			definition := func(endpoint string) map[string]any {
				return map[string]any{
					"script": "return () => {}", "uses": "./action", "event_type": "notify",
					"authorization": authorizationTestAuthority(endpoint),
				}
			}

			c := NewCompiler()
			raw := map[string]any{kind: map[string]any{"notify": definition("https://authority.example/a")}}
			top := c.extractSafeOutputsConfig(map[string]any{"safe-outputs": raw})
			imported, err := json.Marshal(map[string]any{kind: map[string]any{"notify": definition("https://different.example/a")}})
			require.NoError(t, err)
			merged, err := c.MergeSafeOutputs(top, []string{string(imported)}, raw)
			if err == nil {
				err = resolveSafeOutputAuthorizations(merged)
			}
			require.ErrorContains(t, err, "conflicting")
		})
	}
}

func TestImportedCustomAuthorizationFollowsNormalizedToolIdentity(t *testing.T) {
	for _, test := range []struct {
		name, importedName, selectedName, localEndpoint string
		conflict                                        bool
	}{
		{name: "script replaced by action", importedName: "secure", selectedName: "secure"},
		{name: "normalized executor replacement", importedName: "send-notification", selectedName: "send_notification"},
		{name: "matching local authority", importedName: "secure", selectedName: "secure", localEndpoint: "https://authority.example/a"},
		{name: "conflicting local authority", importedName: "secure", selectedName: "secure", localEndpoint: "https://different.example/a", conflict: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			compiler := NewCompiler()
			action := map[string]any{"uses": "./selected-action"}
			if test.localEndpoint != "" {
				action["authorization"] = authorizationTestAuthority(test.localEndpoint)
			}
			raw := map[string]any{"actions": map[string]any{test.selectedName: action}}
			top := compiler.extractSafeOutputsConfig(map[string]any{"safe-outputs": raw})
			imported, err := json.Marshal(map[string]any{"scripts": map[string]any{
				test.importedName: map[string]any{
					"script": "return () => {}", "authorization": authorizationTestAuthority("https://authority.example/a"),
				},
			}})
			require.NoError(t, err)
			merged, err := compiler.MergeSafeOutputs(top, []string{string(imported)}, raw)
			require.NoError(t, err)
			err = resolveSafeOutputAuthorizations(merged)
			if test.conflict {
				require.ErrorContains(t, err, "conflicting")
				return
			}
			require.NoError(t, err)
			require.Empty(t, merged.Scripts, "inheriting authority must not activate the imported executor")
			require.Equal(t, "./selected-action", merged.Actions[test.selectedName].Uses)
			require.NotNil(t, merged.AuthorizationRequirements[test.selectedName])
			require.Equal(t, "https://authority.example/a", merged.AuthorizationRequirements[test.selectedName].Endpoint)
			require.Equal(t, "action", merged.AuthorizationCoverage[test.selectedName])
		})
	}
}
