package workflow

import (
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/github/gh-aw/pkg/connectors"
	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/sliceutil"
)

const connectorContextPath = "/tmp/gh-aw/connector-context.jsonl"

func connectorMap(parent map[string]any, key string) map[string]any {
	value, ok := parent[key].(map[string]any)
	if !ok {
		value = map[string]any{}
		parent[key] = value
	}
	return value
}

func connectorEnv(plan *connectors.Plan) (map[string]any, error) {
	encoded, err := plan.Encode()
	if err != nil {
		return nil, err
	}
	env := map[string]any{"GH_AW_CONNECTOR_PLAN": encoded}
	for _, secret := range plan.Secrets() {
		env["GH_AW_CONNECTOR_SECRET_"+secret] = "${{ secrets." + secret + " }}"
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
			name := "connector-" + alias
			if _, exists := tools[name]; exists {
				return fmt.Errorf("mcp-servers.%s conflicts with generated connector MCP server", name)
			}
			env, err := connectorEnv(config.Agent)
			if err != nil {
				return err
			}
			tools[name] = map[string]any{
				"type": "stdio", "container": constants.DefaultAlpineImage,
				"entrypoint": GhAwBinaryPath, "entrypointArgs": []string{"connectors", "serve"},
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
	// The alias length makes normalized a-b/c and a/b-c unambiguous.
	return "connector-" + strconv.Itoa(len(alias)) + "-" + alias + "-" + operation
}

func (c *Compiler) expandConnectorSafeOutputs(frontmatter map[string]any, alias string, config *WorkflowConnector) error {
	safeOutputs := connectorMap(frontmatter, "safe-outputs")
	scripts := connectorMap(safeOutputs, "scripts")
	env := connectorMap(safeOutputs, "env")
	prefix := "GH_AW_CONNECTOR_" + strconv.Itoa(len(alias)) + "_" + strings.ToUpper(strings.ReplaceAll(alias, "-", "_")) + "_SECRET_"
	for _, secret := range config.SafeOutputs.Secrets() {
		name := prefix + secret
		if _, exists := env[name]; exists {
			return fmt.Errorf("safe-outputs.env.%s conflicts with a connector credential", name)
		}
		env[name] = "${{ secrets." + secret + " }}"
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
			"script":      connectorSafeOutputScript(encoded, operation, prefix, config.SafeOutputs.Secrets(), c.dryRun || c.trialMode || c.forceStaged),
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
		}
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

func connectorSafeOutputScript(encoded, operation, prefix string, secrets []string, staged bool) string {
	var script strings.Builder
	script.WriteString("const { spawnSync } = require('node:child_process');\n")
	script.WriteString("const { join } = require('node:path');\n")
	script.WriteString("const { isStagedMode } = require('./safe_output_helpers.cjs');\n")
	script.WriteString("const { connectorEffectBindings } = require('./connector_effect_bindings.cjs');\n")
	fmt.Fprintf(&script, "const staged = %t || isStagedMode();\n", staged) //nolint:fprintferrorunchecked // strings.Builder cannot fail.
	script.WriteString("const args = { ...item }; delete args.type;\n")
	fmt.Fprintf(&script, "const env = { GH_AW_CONNECTOR_PLAN: %q };\n", encoded) //nolint:fprintferrorunchecked // strings.Builder cannot fail.
	script.WriteString("const bindings = connectorEffectBindings(temporaryIdMap, env.GH_AW_CONNECTOR_PLAN);\n")
	script.WriteString("if (!staged) Object.assign(env, bindings.env);\n")
	for _, secret := range secrets {
		fmt.Fprintf(&script, "if (!staged) env[%q] = process.env[%q];\n", "GH_AW_CONNECTOR_SECRET_"+secret, prefix+secret) //nolint:fprintferrorunchecked // strings.Builder cannot fail.
	}
	fmt.Fprintf(&script, "const result = spawnSync(join(process.env.RUNNER_TEMP, 'gh-aw', 'gh-aw'), ['connectors', 'invoke', 'safe-output', %q, staged ? '--stage' : '--allow-writes'], { input: JSON.stringify(args), encoding: 'utf8', env, timeout: 25000, maxBuffer: 2097152 });\n", operation) //nolint:fprintferrorunchecked // strings.Builder cannot fail.
	script.WriteString("if (result.error || result.status !== 0) throw new Error('Connector effect failed; no retry performed: ' + (result.error?.message || result.stderr));\n")
	script.WriteString("const output = JSON.parse(result.stdout);\n")
	script.WriteString("if (!staged) bindings.record(output);\n")
	script.WriteString("return output;\n")
	return script.String()
}

func (c *Compiler) generateConnectorSetupSteps(yaml *strings.Builder, data *WorkflowData) error {
	if len(data.Connectors) == 0 {
		return nil
	}
	if !c.actionMode.IsDev() {
		generateAgenticWorkflowsInstallStep(c, yaml, true, data)
		return nil
	}
	ref := versionToGitRef(c.version)
	if ref == "" {
		return errors.New("development connector runtime requires a compiler version with a published git reference")
	}
	// Never build credentialed hosts from an agent-controlled workspace or artifact.
	yaml.WriteString("      - name: Checkout trusted connector runtime\n")
	fmt.Fprintf(yaml, "        uses: %s\n", c.getActionPin("actions/checkout")) //nolint:fprintferrorunchecked // strings.Builder cannot fail.
	yaml.WriteString("        with:\n          repository: github/gh-aw\n")
	fmt.Fprintf(yaml, "          ref: %q\n", ref) //nolint:fprintferrorunchecked // strings.Builder cannot fail.
	yaml.WriteString("          path: .gh-aw-connector-runtime\n          fetch-depth: 1\n          persist-credentials: false\n")
	yaml.WriteString("      - name: Build trusted connector runtime\n")
	yaml.WriteString("        working-directory: ${{ github.workspace }}/.gh-aw-connector-runtime\n")
	yaml.WriteString("        run: |\n          CGO_ENABLED=0 make build\n          mkdir -p \"${RUNNER_TEMP}/gh-aw\"\n          install -m 755 gh-aw \"${RUNNER_TEMP}/gh-aw/gh-aw\"\n")
	return nil
}

func (c *Compiler) generateAgentConnectorSteps(yaml *strings.Builder, data *WorkflowData) error {
	if err := c.generateConnectorSetupSteps(yaml, data); err != nil {
		return err
	}
	return generateConnectorContextSteps(yaml, data)
}

func generateConnectorContextSteps(yaml *strings.Builder, data *WorkflowData) error {
	hasContext := false
	for _, config := range data.Connectors {
		hasContext = hasContext || config.Context != nil
	}
	if !hasContext {
		return nil
	}
	yaml.WriteString("      - name: Initialize untrusted connector context\n")
	yaml.WriteString("        run: install -m 600 /dev/null " + connectorContextPath + "\n")
	for _, alias := range sliceutil.SortedKeys(data.Connectors) {
		config := data.Connectors[alias]
		if config.Context == nil {
			continue
		}
		env, err := connectorEnv(config.Context)
		if err != nil {
			return err
		}
		for _, name := range sliceutil.SortedKeys(config.ContextArgs) {
			args, err := json.Marshal(config.ContextArgs[name])
			if err != nil {
				return err
			}
			// Arguments, including expression-looking text, are encoded data.
			// They are never evaluated as Actions expressions or shell input.
			if strings.Contains(string(args), "${{") {
				return errors.New("connector context inputs must be literal values, not Actions expressions")
			}
			fmt.Fprintf(yaml, "      - name: Fetch connector context %s/%s\n", alias, name) //nolint:fprintferrorunchecked // strings.Builder cannot fail.
			yaml.WriteString("        env:\n")
			for _, key := range sliceutil.SortedKeys(env) {
				fmt.Fprintf(yaml, "          %s: %q\n", key, env[key]) //nolint:fprintferrorunchecked // strings.Builder cannot fail.
			}
			fmt.Fprintf(yaml, "          GH_AW_CONNECTOR_INPUTS: %q\n", string(args)) //nolint:fprintferrorunchecked // strings.Builder cannot fail.
			fmt.Fprintf(yaml, "          GH_AW_CONNECTOR_OPERATION: %q\n", name)      //nolint:fprintferrorunchecked // strings.Builder cannot fail.
			yaml.WriteString("        run: |\n")
			yaml.WriteString("          set -euo pipefail\n")
			yaml.WriteString("          printf '%s' \"$GH_AW_CONNECTOR_INPUTS\" | \"${RUNNER_TEMP}/gh-aw/gh-aw\" connectors invoke context \"$GH_AW_CONNECTOR_OPERATION\" >> " + connectorContextPath + "\n")
		}
	}
	return nil
}
