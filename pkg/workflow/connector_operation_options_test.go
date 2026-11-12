//go:build !integration

package workflow

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/parser"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.yaml.in/yaml/v3"
)

func TestConnectorOperationOptionsParsing(t *testing.T) {
	for _, test := range []struct {
		name  string
		value any
		max   int
		stage string
		error string
	}{
		{"null", nil, 1, "", ""},
		{"empty", map[string]any{}, 1, "", ""},
		{"configured", map[string]any{"max": 3, "staged": true}, 3, "true", ""},
		{"false", map[string]any{"staged": false}, 1, "false", ""},
		{"maximum", map[string]any{"max": 100}, 100, "", ""},
		{"integral-float", map[string]any{"max": float64(3)}, 3, "", ""},
		{"expression", map[string]any{"staged": "${{ inputs.stage }}"}, 1, "${{ inputs.stage }}", ""},
		{"fraction", map[string]any{"max": 1.5}, 0, "", "max must be an integer"},
		{"zero", map[string]any{"max": 0}, 0, "", "max must be an integer"},
		{"negative", map[string]any{"max": -1}, 0, "", "max must be an integer"},
		{"unbounded", map[string]any{"max": 101}, 0, "", "max must be an integer"},
		{"string-max", map[string]any{"max": "3"}, 0, "", "max must be an integer"},
		{"boolean-max", map[string]any{"max": true}, 0, "", "max must be an integer"},
		{"null-max", map[string]any{"max": nil}, 0, "", "max must be an integer"},
		{"expression-max", map[string]any{"max": "${{ inputs.max }}"}, 0, "", "max must be an integer"},
		{"staged-number", map[string]any{"staged": 1}, 0, "", "staged must be a boolean"},
		{"staged-string", map[string]any{"staged": "true"}, 0, "", "staged must be a boolean"},
		{"staged-null", map[string]any{"staged": nil}, 0, "", "staged must be a boolean"},
		{"unknown", map[string]any{"token": "injected"}, 0, "", "unknown connector operation option"},
		{"scalar", true, 0, "", "must be null or an object"},
		{"array", []any{3}, 0, "", "must be null or an object"},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, err := parseConnectorOperationOptions(test.value)
			if test.error != "" {
				require.ErrorContains(t, err, test.error)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, test.max, got.Max)
			if test.stage == "" {
				assert.Nil(t, got.Staged)
			} else {
				require.NotNil(t, got.Staged)
				assert.Equal(t, test.stage, got.Staged.String())
			}
		})
	}
}

func connectorOptionsSource(t *testing.T, route string, shared bool) string {
	t.Helper()
	doc, err := parser.ExtractFrontmatterFromContent("---\n" + awConnectorSource(t) + "---\n")
	require.NoError(t, err)
	config := doc.Frontmatter["connectors"].(map[string]any)["tracker"].(map[string]any)
	credentials := config["credentials"].(map[string]any)
	writer := credentials["writer"].(map[string]any)
	configure := func(value map[string]any, name string) {
		delete(value, "secret")
		switch route {
		case "github-app":
			value["github-app"] = map[string]any{
				"client-id": "${{ vars.CONNECTOR_APP_ID }}", "private-key": "${{ secrets." + name + "_KEY }}",
				"owner": "example", "repositories": []string{"issues"},
				"permissions": map[string]string{"issues": "write"},
			}
		case "github-oidc":
			value["github-oidc"] = map[string]any{"audience": "https://connector.example/" + name}
		default:
			value["secret"] = "EXAMPLE_" + name + "_TOKEN"
		}
	}
	configure(writer, "WRITE")
	operations := config["safe-outputs"].(map[string]any)
	clone := func(value any) map[string]any {
		data, err := json.Marshal(value)
		require.NoError(t, err)
		var result map[string]any
		require.NoError(t, json.Unmarshal(data, &result))
		return result
	}
	second := clone(operations["add-comment"])
	if !shared {
		second["credential"] = "writer-two"
		writerTwo := clone(writer)
		delete(writerTwo, "github-app")
		delete(writerTwo, "github-oidc")
		configure(writerTwo, "SECOND_WRITE")
		credentials["writer-two"] = writerTwo
	}
	operations["update-issue"] = second
	encoded, err := yaml.Marshal(doc.Frontmatter)
	require.NoError(t, err)
	return string(encoded)
}

func TestConnectorOperationOptionsTrustedConfigurations(t *testing.T) {
	c, root := awConnectorCompiler(t)
	source := awConnectorHeader + connectorOptionsSource(t, "secret", true) + `safe-outputs:
  tracker-add-comment:
    max: 3
    staged: true
  tracker-update-issue:
    max: 2
    staged: false
---
Review.
`
	data, err := c.ParseWorkflowString(source, filepath.Join(root, "proof.md"))
	require.NoError(t, err)
	for _, test := range []struct {
		operation string
		max       int
		staged    bool
	}{{"add-comment", 3, true}, {"update-issue", 2, false}} {
		script := data.SafeOutputs.Scripts["tracker-"+test.operation]
		require.NotNil(t, script)
		assert.Equal(t, test.max, script.Max)
		tool := generateCustomScriptToolDefinition("tracker-"+test.operation, script)
		properties := tool["inputSchema"].(map[string]any)["properties"].(map[string]any)
		assert.NotContains(t, properties, "max")
		assert.NotContains(t, properties, "staged")
	}
	mcpConfig, err := generateSafeOutputsConfig(data)
	require.NoError(t, err)
	var mcp map[string]any
	require.NoError(t, json.Unmarshal([]byte(mcpConfig), &mcp))
	var lines []string
	c.addHandlerManagerConfigEnvVar(&lines, data)
	_, quoted, ok := strings.Cut(strings.TrimSpace(strings.Join(lines, "")), "GH_AW_SAFE_OUTPUTS_HANDLER_CONFIG: ")
	require.True(t, ok)
	handlerJSON, err := strconv.Unquote(strings.TrimSpace(quoted))
	require.NoError(t, err)
	var runtime map[string]any
	require.NoError(t, json.Unmarshal([]byte(handlerJSON), &runtime))
	for _, config := range []map[string]any{mcp, runtime} {
		for _, test := range []struct {
			name   string
			max    float64
			staged bool
		}{{"tracker_add_comment", 3, true}, {"tracker_update_issue", 2, false}} {
			handler := config[test.name].(map[string]any)
			assert.InDelta(t, test.max, handler["max"], 0)
			assert.Equal(t, test.staged, handler["staged"])
			assert.Equal(t, true, handler["connector_operation"])
			assert.Equal(t, "abort", handler["threat_warning_policy"])
		}
	}
}

func TestConnectorOperationOptionsCredentialStaging(t *testing.T) {
	for _, route := range []string{"secret", "github-app", "github-oidc"} {
		for _, shared := range []bool{false, true} {
			for _, test := range []struct {
				name, global, first, second string
				firstLive, secondLive       bool
				forced                      bool
			}{
				{"all-staged", "", "true", "true", false, false, false},
				{"mixed", "", "true", "false", false, true, false},
				{"first-live", "", "false", "true", true, false, false},
				{"global-staged", "true", "false", "false", false, false, false},
				{"forced-staged", "", "false", "false", false, false, true},
				{"operation-expression", "", "${{ inputs.first_stage }}", "true", true, false, false},
				{"two-expressions", "", "${{ inputs.first_stage }}", "${{ inputs.second_stage }}", true, true, false},
				{"global-expression", "${{ inputs.global_stage }}", "false", "true", true, false, false},
			} {
				t.Run(fmt.Sprintf("%s/shared-%t/%s", route, shared, test.name), func(t *testing.T) {
					c, root := awConnectorCompiler(t)
					c.forceStaged = test.forced
					global := ""
					if test.global != "" {
						global = "  staged: " + test.global + "\n"
					}
					selection := "safe-outputs:\n" + global + "  tracker-add-comment:\n    max: 3\n    staged: " + test.first + "\n  tracker-update-issue:\n    max: 2\n    staged: " + test.second + "\n"
					data, err := c.ParseWorkflowString(awConnectorHeader+connectorOptionsSource(t, route, shared)+selection+"---\nReview.\n", filepath.Join(root, "proof.md"))
					require.NoError(t, err)
					operationSecrets, err := connectorOperationSecrets(data.Connectors["tracker"].SafeOutputs)
					require.NoError(t, err)
					firstSecret := operationSecrets["add-comment"][0]
					secondSecret := operationSecrets["update-issue"][0]
					firstEnabled := test.firstLive || (shared && test.secondLive)
					secondEnabled := test.secondLive || (shared && test.firstLive)
					prefix := connectorCredentialEnvPrefix("tracker")
					_, firstPresent := data.SafeOutputs.Env[prefix+firstSecret]
					_, secondPresent := data.SafeOutputs.Env[prefix+secondSecret]
					assert.Equal(t, firstEnabled, firstPresent)
					assert.Equal(t, secondEnabled, secondPresent)
					if test.name == "operation-expression" || test.name == "two-expressions" {
						assert.Contains(t, data.SafeOutputs.Env[prefix+firstSecret], "inputs.first_stage")
						assert.Contains(t, data.SafeOutputs.Env[prefix+firstSecret], "!= 'true'")
					}
					if test.name == "two-expressions" {
						assert.Contains(t, data.SafeOutputs.Env[prefix+secondSecret], "inputs.second_stage")
					}
					if test.name == "global-expression" {
						assert.Contains(t, data.SafeOutputs.Env[prefix+firstSecret], "inputs.global_stage")
					}
					steps := strings.Join(c.connectorGitHubAppSteps(data, "safe-output"), "")
					if route == "secret" {
						assert.Empty(t, steps)
						return
					}
					assert.Equal(t, firstEnabled, strings.Contains(steps, "id: "+connectorAppStepID(firstSecret)+"\n"))
					assert.Equal(t, secondEnabled, strings.Contains(steps, "id: "+connectorAppStepID(secondSecret)+"\n"))
					if strings.Contains(test.name, "expression") {
						assert.Contains(t, steps, "if: ${{ env.GH_AW_CONNECTOR_STAGED != 'true' }}")
						assert.Contains(t, steps, "inputs.")
					}
				})
			}
		}
	}
}

func TestConnectorOperationOptionsPublicValidation(t *testing.T) {
	for _, options := range []string{
		"true", "[]", "{max: 0}", "{max: -1}", "{max: 101}", "{max: 1.2}", "{max: '3'}", "{max: true}", "{max: null}",
		"{staged: 1}", "{staged: 'false'}", "{staged: null}", "{staged: []}", "{token: injected}", "{reviewable: true}",
	} {
		t.Run(options, func(t *testing.T) {
			c, root := awConnectorCompiler(t)
			_, err := c.ParseWorkflowString(awConnectorHeader+awConnectorSource(t)+"safe-outputs:\n  tracker-add-comment: "+options+"\n---\nReview.\n", filepath.Join(root, "proof.md"))
			require.ErrorContains(t, err, "safe-outputs.tracker-add-comment")
		})
	}
	for _, options := range []string{"", "{}", "{max: 3, staged: true}"} {
		t.Run("compatible-"+options, func(t *testing.T) {
			c, root := awConnectorCompiler(t)
			data, err := c.ParseWorkflowString(awConnectorHeader+awConnectorSource(t)+"safe-outputs:\n  tracker-add-comment: "+options+"\n---\nReview.\n", filepath.Join(root, "proof.md"))
			require.NoError(t, err)
			require.NotNil(t, data.SafeOutputs.Scripts["tracker-add-comment"])
			assert.GreaterOrEqual(t, data.SafeOutputs.Scripts["tracker-add-comment"].Max, 1)
		})
	}
}

func TestConnectorOperationOptionsImportedFinalStaging(t *testing.T) {
	c, root := awConnectorCompiler(t)
	require.NoError(t, os.WriteFile(filepath.Join(root, "connector.md"), []byte("---\n"+connectorOptionsSource(t, "github-app", false)+"safe-outputs:\n  staged: true\n---\n"), 0600))
	content := awConnectorHeader + `imports: [connector.md]
safe-outputs:
  tracker-add-comment:
    max: 3
    staged: false
  tracker-update-issue: {}
---
Review.
`
	data, err := c.ParseWorkflowString(content, filepath.Join(root, "proof.md"))
	require.NoError(t, err)
	require.True(t, templatableBoolIsTrue(data.SafeOutputs.Staged))
	assert.Empty(t, data.SafeOutputs.Env)
	assert.Empty(t, c.connectorGitHubAppSteps(data, "safe-output"))
	assert.Equal(t, 3, data.SafeOutputs.Scripts["tracker-add-comment"].Max)
	assert.Equal(t, 1, data.SafeOutputs.Scripts["tracker-update-issue"].Max)
}

func TestConnectorOperationOptionsStagedYAMLPreservesAgentCredentials(t *testing.T) {
	for _, route := range []string{"secret", "github-app", "github-oidc"} {
		t.Run(route, func(t *testing.T) {
			c, root := awConnectorCompiler(t)
			content := awConnectorHeader + connectorOptionsSource(t, route, false) + `tools:
  tracker: [get-issue]
safe-outputs:
  tracker-add-comment: {staged: true}
  tracker-update-issue: {staged: true}
---
Review.
`
			path := filepath.Join(root, "proof.md")
			data, err := c.ParseWorkflowString(content, path)
			require.NoError(t, err)
			output, err := c.CompileToYAML(data, path)
			require.NoError(t, err)
			assert.Contains(t, output, "${{ secrets.EXAMPLE_READ_TOKEN }}")
			assert.NotContains(t, output, "${{ secrets.EXAMPLE_WRITE_TOKEN }}")
			assert.NotContains(t, output, "${{ secrets.EXAMPLE_SECOND_WRITE_TOKEN }}")
			assert.NotContains(t, output, "${{ secrets.WRITE_KEY }}")
			assert.NotContains(t, output, "${{ secrets.SECOND_WRITE_KEY }}")
			assert.NotContains(t, output, "Mint connector GitHub")
			assert.NotContains(t, output, "id-token: write")
		})
	}
}

func TestConnectorOperationOptionsTrustedInputSecretStaging(t *testing.T) {
	c, root := awConnectorCompiler(t)
	doc, err := parser.ExtractFrontmatterFromContent("---\n" + connectorOptionsSource(t, "secret", true) + "---\n")
	require.NoError(t, err)
	config := doc.Frontmatter["connectors"].(map[string]any)["tracker"].(map[string]any)
	operations := config["safe-outputs"].(map[string]any)
	for operation, secret := range map[string]string{"add-comment": "TRUSTED_FIRST_ISSUE", "update-issue": "TRUSTED_SECOND_ISSUE"} {
		operations[operation].(map[string]any)["trustedInputs"] = map[string]any{"issue": map[string]any{"secret": secret}}
	}
	source, err := yaml.Marshal(doc.Frontmatter)
	require.NoError(t, err)
	data, err := c.ParseWorkflowString(awConnectorHeader+string(source)+`safe-outputs:
  tracker-add-comment: {staged: true}
  tracker-update-issue: {}
---
Review.
`, filepath.Join(root, "proof.md"))
	require.NoError(t, err)
	prefix := connectorCredentialEnvPrefix("tracker")
	assert.NotContains(t, data.SafeOutputs.Env, prefix+"TRUSTED_FIRST_ISSUE")
	assert.Contains(t, data.SafeOutputs.Env, prefix+"TRUSTED_SECOND_ISSUE")
	assert.Contains(t, data.SafeOutputs.Env, prefix+"EXAMPLE_WRITE_TOKEN")
}

func TestConnectorOperationOptionsExpressionTruthTable(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node is required to exercise credential gating expressions")
	}
	for _, shared := range []bool{false, true} {
		t.Run(fmt.Sprintf("shared-%t", shared), func(t *testing.T) {
			c, root := awConnectorCompiler(t)
			content := awConnectorHeader + connectorOptionsSource(t, "github-app", shared) + `safe-outputs:
  staged: ${{ inputs.global_stage }}
  tracker-add-comment:
    staged: ${{ inputs.first_stage }}
  tracker-update-issue:
    staged: ${{ inputs.second_stage }}
---
Review.
`
			data, err := c.ParseWorkflowString(content, filepath.Join(root, "proof.md"))
			require.NoError(t, err)
			config := data.Connectors["tracker"]
			secrets, err := connectorOperationSecrets(config.SafeOutputs)
			require.NoError(t, err)
			live, enabled, err := connectorCredentialLiveCondition(config, secrets["add-comment"][0], data.SafeOutputs.Staged, false)
			require.NoError(t, err)
			require.True(t, enabled)
			_, staged, enabled := connectorCredentialMintCondition(c, data, config, secrets["add-comment"][0], "safe-output")
			require.True(t, enabled)
			driverConfig, err := json.Marshal(map[string]any{
				"live": live, "staged": stripExpressionWrapper(staged), "shared": shared,
			})
			require.NoError(t, err)
			driver := fmt.Sprintf(`
const assert = require("node:assert/strict");
const config = %s;
const live = new Function("inputs", "format", "return " + config.live);
const staged = new Function("inputs", "format", "return " + config.staged);
for (const global_stage of [true,false,"true","false",null,""]) {
  for (const first_stage of [true,false,"true","false",null,""]) {
    for (const second_stage of [true,false,"true","false",null,""]) {
      const inputs = {global_stage,first_stage,second_stage};
      const isTrue = value => value === true || value === "true";
      const expected = !isTrue(global_stage) && (!isTrue(first_stage) || (config.shared && !isTrue(second_stage)));
      const format = (_, value) => String(value ?? "");
      assert.equal(live(inputs, format), expected, JSON.stringify(inputs));
      assert.equal(staged(inputs, format), !expected, JSON.stringify(inputs));
    }
  }

}
`, driverConfig)
			output, err := exec.CommandContext(t.Context(), node, "-e", driver).CombinedOutput()
			require.NoError(t, err, "%s", output)
		})
	}
}

func TestConnectorOperationOptionsAgentCannotOverrideStaging(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node is required to exercise trusted connector staging")
	}
	helpers, err := filepath.Abs("../../actions/setup/js/safe_output_helpers.cjs")
	require.NoError(t, err)
	source := generateSafeOutputScriptContent("tracker-add-comment", &SafeScriptConfig{
		Script: connectorSafeOutputScript("PLAN", "add-comment", "PREFIX_", []string{"WRITER"}, false, false),
	})
	for _, global := range []bool{false, true} {
		t.Run(fmt.Sprintf("global-%t", global), func(t *testing.T) {
			config, err := json.Marshal(map[string]any{"helpers": helpers, "source": source, "global": global})
			require.NoError(t, err)
			driver := fmt.Sprintf(`
const assert = require("node:assert/strict");
const options = %s;
process.env = {GH_AW_SAFE_OUTPUTS_STAGED: String(options.global)};
const env = {RUNNER_TEMP: "runner", PLAN:"frozen"};
Object.defineProperty(env, "PREFIX_WRITER", {get() { throw new Error("staging resolved writer credentials"); }});
let invoked = 0;
const adaptedRequire = name => {
  if (name === "./safe_output_helpers.cjs") return require(options.helpers);
  if (name === "./connector_runtime.cjs") return {decodePlan: () => ({})};
  if (name === "./connector_content.cjs") return {formatConnectorArguments: (_, __, args) => args};
  if (name === "./connector_effect_bindings.cjs") return {connectorEffectBindings() {throw new Error("staging created receipt authority");}};
  if (name === "node:child_process") return {spawnSync(_, args, config) {
    invoked++;
    assert.equal(args.at(-1), "--stage");
    assert.deepEqual(Object.keys(config.env), ["GH_AW_CONNECTOR_PLAN"]);
    return {status:0, stdout:'{"staged":true}'};
  }};
  return name.startsWith("./") ? require(require("node:path").join(require("node:path").dirname(options.helpers), name)) : require(name);
};
const module_ = {exports:{}};
new Function("require", "module", "process", options.source)(adaptedRequire, module_, {env,execPath:process.execPath});
(async () => {
  const handler = await module_.exports.main({staged: !options.global, max:1});
  const result = await handler({type:"tracker_add_comment",staged:false,max:100,config:{staged:false,max:100}}, {}, new Map());
  assert.equal(result.staged, true);
  assert.equal(invoked, 1);
})().catch(error => {console.error(error);process.exitCode=1;});
`, config)
			output, err := exec.CommandContext(t.Context(), node, "-e", driver).CombinedOutput()
			require.NoError(t, err, "%s", output)
		})
	}
}

func TestConnectorOperationOptionsExpressionCannotInjectPolicy(t *testing.T) {
	c, root := awConnectorCompiler(t)
	data, err := c.ParseWorkflowString(awConnectorHeader+awConnectorSource(t)+`safe-outputs:
  tracker-add-comment:
    max: 2
    staged: ${{ inputs.stage }}
---
Review.
`, filepath.Join(root, "proof.md"))
	require.NoError(t, err)
	config := map[string]any{}
	addConnectorThreatWarningPolicies(config, data.SafeOutputs.Scripts)
	handler := config["tracker_add_comment"].(map[string]any)
	assert.Equal(t, 2, handler["max"])
	assert.Equal(t, true, handler["connector_operation"])
	assert.Equal(t, "abort", handler["threat_warning_policy"])
	assert.Equal(t, "${{ format('{0}', inputs.stage) == 'true' }}", handler["staged"])
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node is required to exercise hostile expression results")
	}
	expression := stripExpressionWrapper(handler["staged"].(string))
	encoded, err := json.Marshal(expression)
	require.NoError(t, err)
	driver := fmt.Sprintf(`
const assert = require("node:assert/strict");
const staged = new Function("inputs", "format", "return " + %s);
for (const stage of ['false","max":100,"connector_operation":false,"staged":"false', '"\n}',{},null,true,false,"true","false"]) {
  const resolved = staged({stage}, (_,value) => String(value ?? ""));
  assert.equal(typeof resolved, "boolean");
  assert.equal(resolved, stage === true || stage === "true");
}
`, encoded)
	output, err := exec.CommandContext(t.Context(), node, "-e", driver).CombinedOutput()
	require.NoError(t, err, "%s", output)
}
