package workflow

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/github/gh-aw/pkg/connectors"
	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/sliceutil"
)

func connectorMap(parent map[string]any, key string) map[string]any {
	value, ok := parent[key].(map[string]any)
	if !ok {
		value = map[string]any{}
		parent[key] = value
	}
	return value
}

func connectorEnv(plan *connectors.Plan, config *WorkflowConnector) (map[string]any, error) {
	encoded, err := plan.Encode()
	if err != nil {
		return nil, err
	}
	env := map[string]any{"GH_AW_CONNECTOR_PLAN": encoded}
	for _, secret := range plan.Secrets() {
		env["GH_AW_CONNECTOR_SECRET_"+secret] = connectorSecretExpression(config, secret)
	}
	return env, nil
}

func (c *Compiler) expandWorkflowConnectors(frontmatter map[string]any, configs map[string]*WorkflowConnector) error {
	// Consume only connector grammar. Authored source remains in SourceDocument
	// and frontmatter lines; downstream built-in processing sees native configs.
	delete(frontmatter, "connectors")
	for _, alias := range sliceutil.SortedKeys(configs) {
		config := configs[alias]
		if config.Agent != nil {
			tools := connectorMap(frontmatter, "mcp-servers")
			name := alias
			if _, exists := tools[name]; exists {
				return fmt.Errorf("mcp-servers.%s conflicts with generated connector MCP server", name)
			}
			env, err := connectorEnv(config.Agent, config)
			if err != nil {
				return err
			}
			tools[name] = map[string]any{
				"type": "stdio", "container": constants.DefaultNodeAlpineLTSImage,
				"entrypoint": "node", "entrypointArgs": []string{constants.GhAwRootDirShell + "/actions/connector_runtime.cjs", "serve"},
				"mounts": []string{constants.DefaultGhAwMount},
				"env":    env, "allowed": config.Agent.ToolNames(),
			}
		}
		if config.SafeOutputs != nil {
			if err := c.expandConnectorSafeOutputs(frontmatter, alias, config); err != nil {
				return err
			}
		}
	}
	return nil
}

func connectorSafeOutputName(alias, operation string) string {
	return alias + "-" + operation
}

func connectorCredentialEnvPrefix(alias string) string {
	return "GH_AW_CONNECTOR_" + strconv.Itoa(len(alias)) + "_" + strings.ToUpper(strings.ReplaceAll(alias, "-", "_")) + "_SECRET_"
}

func (c *Compiler) expandConnectorSafeOutputs(frontmatter map[string]any, alias string, config *WorkflowConnector) error {
	safeOutputs := connectorMap(frontmatter, "safe-outputs")
	scripts := connectorMap(safeOutputs, "scripts")
	env := connectorMap(safeOutputs, "env")
	prefix := connectorCredentialEnvPrefix(alias)
	for _, secret := range config.SafeOutputs.Secrets() {
		name := prefix + secret
		if _, exists := env[name]; exists {
			return fmt.Errorf("safe-outputs.env.%s conflicts with a connector credential", name)
		}
		liveCondition, enabled, err := connectorCredentialLiveCondition(config, secret, connectorStagedValue(safeOutputs["staged"]), c.dryRun || c.trialMode || c.forceStaged)
		if err != nil {
			return err
		}
		if !enabled {
			continue
		}
		env[name] = connectorSecretExpression(config, secret)
		if liveCondition != "" {
			env[name] = "${{ " + liveCondition + " && (" +
				stripExpressionWrapper(connectorSecretExpression(config, secret)) + ") || '' }}"
		}
	}
	encoded, err := config.SafeOutputs.Encode()
	if err != nil {
		return err
	}
	for _, operation := range config.SafeOutputs.SafeOutputNames() {
		name := connectorSafeOutputName(alias, operation)
		if len(name) > 128 {
			return fmt.Errorf("connector effect tool name %q exceeds 128 bytes; use a shorter service alias", name)
		}
		if _, exists := scripts[name]; exists {
			return fmt.Errorf("safe-outputs.scripts.%s conflicts with a connector effect", name)
		}
		inputs, err := config.SafeOutputs.Inputs("safe-output", operation)
		if err != nil {
			return err
		}
		scripts[name] = map[string]any{
			"description": fmt.Sprintf("Request %s/%s; validated and executed outside the agent credential boundary.", alias, operation),
			"inputs":      connectorSafeOutputInputs(inputs),
			"script":      connectorSafeOutputScript(encoded, operation, prefix, config.SafeOutputs.Secrets(), c.dryRun || c.trialMode || c.forceStaged, config.SafeOutputs.UsesStagedFile(operation)),
		}
	}
	return nil
}

func attachConnectorInputSchemas(data *WorkflowData) error {
	for alias, config := range data.Connectors {
		if config.SafeOutputs == nil {
			continue
		}
		for _, operation := range config.SafeOutputs.SafeOutputNames() {
			name := connectorSafeOutputName(alias, operation)
			if data.SafeOutputs == nil || data.SafeOutputs.Scripts[name] == nil {
				return fmt.Errorf("connector effect %q was lost during safe-output configuration merging", name)
			}
			schema, err := config.SafeOutputs.InputSchema("safe-output", operation)
			if err != nil {
				return err
			}
			data.SafeOutputs.Scripts[name].connectorInputSchema = schema
			options := config.operationOptions(operation)
			data.SafeOutputs.Scripts[name].Max = options.Max
			data.SafeOutputs.Scripts[name].connectorOptions = &options
			contentConfig := map[string]any{}
			if data.SafeOutputs.Footer != nil {
				contentConfig["footer"] = *data.SafeOutputs.Footer
			}
			if data.SafeOutputs.BodyFooter != "" {
				contentConfig["body_footer"] = data.SafeOutputs.BodyFooter
			}
			encodedConfig, err := json.Marshal(contentConfig)
			if err != nil {
				return fmt.Errorf("encode connector content configuration: %w", err)
			}
			data.SafeOutputs.Scripts[name].Script = "const connectorContentDefaults = JSON.parse(Buffer.from(" +
				strconv.Quote(base64.StdEncoding.EncodeToString(encodedConfig)) + ", 'base64').toString('utf8'));\n" +
				data.SafeOutputs.Scripts[name].Script
		}
		// Imports may contribute the final global staged value after expansion.
		// Narrow existing compiler-owned env entries again after that merge.
		for _, secret := range config.SafeOutputs.Secrets() {
			name := connectorCredentialEnvPrefix(alias) + secret
			if _, exists := data.SafeOutputs.Env[name]; !exists {
				continue
			}
			live, enabled, err := connectorCredentialLiveCondition(config, secret, data.SafeOutputs.Staged, false)
			if err != nil {
				return err
			}
			if !enabled {
				delete(data.SafeOutputs.Env, name)
			} else if live != "" {
				data.SafeOutputs.Env[name] = "${{ " + live + " && (" + stripExpressionWrapper(connectorSecretExpression(config, secret)) + ") || '' }}"
			}
		}
	}
	if data.SafeOutputs != nil && data.SafeOutputs.ReportIncomplete != nil && data.SafeOutputs.ReportIncomplete.Implicit {
		value := defaultReportIncompleteCreateIssue(data.SafeOutputs)
		data.SafeOutputs.ReportIncomplete.CreateIssue = &value
	}
	return nil
}

func connectorSafeOutputInputs(inputs map[string]connectors.Input) map[string]any {
	result := map[string]any{}
	for name, input := range inputs {
		inputType := input.Type
		if inputType == "integer" {
			inputType = "number"
		}
		field := map[string]any{"type": inputType, "required": input.Required}
		if input.MaxLength > 0 {
			field["max-length"] = input.MaxLength
		}
		if len(input.Enum) > 0 {
			options := make([]any, 0, len(input.Enum))
			for _, value := range input.Enum {
				options = append(options, value)
			}
			field["type"], field["options"] = "choice", options
		}
		result[name] = field
	}
	return result
}

func connectorSafeOutputScript(encoded, operation, prefix string, secrets []string, staged, binary bool) string {
	var script strings.Builder
	script.WriteString("const { spawnSync } = require('node:child_process');\n")
	script.WriteString("const { join } = require('node:path');\n")
	script.WriteString("const { isStagedMode } = require('./safe_output_helpers.cjs');\n")
	script.WriteString("const { connectorEffectBindings } = require('./connector_effect_bindings.cjs');\n")
	script.WriteString("const { decodePlan } = require('./connector_runtime.cjs');\n")
	script.WriteString("const { formatConnectorArguments } = require('./connector_content.cjs');\n")
	fmt.Fprintf(&script, "const staged = %t || isStagedMode(typeof config === 'undefined' ? undefined : config);\n", staged) //nolint:fprintferrorunchecked // strings.Builder cannot fail.
	script.WriteString("const input = { ...item }; delete input.type;\n")
	fmt.Fprintf(&script, "const env = { GH_AW_CONNECTOR_PLAN: %q };\n", encoded) //nolint:fprintferrorunchecked // strings.Builder cannot fail.
	script.WriteString("const contentConfig = { ...(typeof connectorContentDefaults === 'undefined' ? {} : connectorContentDefaults), ...(typeof config === 'undefined' ? {} : config) };\n")
	if binary {
		script.WriteString("if (!staged) env.GH_AW_CONNECTOR_FILE_ROOT = join(process.env.RUNNER_TEMP, 'gh-aw', 'safeoutputs', 'upload-artifacts');\n")
	}
	fmt.Fprintf(&script, "const args = formatConnectorArguments(decodePlan(env.GH_AW_CONNECTOR_PLAN), %q, input, { config: contentConfig });\n", operation) //nolint:fprintferrorunchecked // strings.Builder cannot fail.
	script.WriteString("const bindings = staged ? null : connectorEffectBindings(temporaryIdMap, env.GH_AW_CONNECTOR_PLAN);\n")
	script.WriteString("if (!staged) Object.assign(env, bindings.env);\n")
	for _, secret := range secrets {
		fmt.Fprintf(&script, "if (!staged) env[%q] = process.env[%q];\n", "GH_AW_CONNECTOR_SECRET_"+secret, prefix+secret) //nolint:fprintferrorunchecked // strings.Builder cannot fail.
	}
	fmt.Fprintf(&script, "const result = spawnSync(process.execPath, [join(process.env.RUNNER_TEMP, 'gh-aw', 'actions', 'connector_runtime.cjs'), 'invoke', 'safe-output', %q, staged ? '--stage' : '--allow-writes'], { input: JSON.stringify(args), encoding: 'utf8', env, timeout: 25000, maxBuffer: 2097152 });\n", operation) //nolint:fprintferrorunchecked // strings.Builder cannot fail.
	script.WriteString("if (result.error || result.status !== 0) throw new Error('Connector effect failed; no retry performed: ' + (result.error?.message || result.stderr));\n")
	script.WriteString("const output = JSON.parse(result.stdout);\n")
	script.WriteString("if (!staged) bindings.record(output);\n")
	script.WriteString("return output;\n")
	return script.String()
}

func (c *Compiler) generateAgentConnectorSteps(yaml *strings.Builder, data *WorkflowData) error {
	if err := validateConnectorContextInputs(data); err != nil {
		return err
	}
	yaml.WriteString(strings.Join(c.connectorGitHubAppSteps(data, "agent"), ""))
	return nil
}

// Preserve validation of legacy context selections without fetching their data.
func validateConnectorContextInputs(data *WorkflowData) error {
	for _, alias := range sliceutil.SortedKeys(data.Connectors) {
		config := data.Connectors[alias]
		if config.Context == nil {
			continue
		}
		for _, name := range sliceutil.SortedKeys(config.ContextArgs) {
			args, err := json.Marshal(config.ContextArgs[name])
			if err != nil {
				return err
			}
			if strings.Contains(string(args), "${{") {
				return errors.New("connector context inputs must be literal values, not Actions expressions")
			}
		}
	}
	return nil
}
