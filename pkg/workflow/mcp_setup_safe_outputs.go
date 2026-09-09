package workflow

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/sliceutil"
	"github.com/github/gh-aw/pkg/typeutil"
)

var safeOutputsSetupLog = logger.New("workflow:mcp_setup_safe_outputs")

// safeOutputsSecretEnvPrefix is prepended to secret names when generating step env var names for
// safe-outputs config placeholders. The prefix avoids accidental collisions between a workflow
// secret name and a pre-existing step env var (e.g. a secret named DEBUG or
// GH_AW_SAFE_OUTPUTS_CONFIG_PATH would silently override those step vars without the prefix).
// The prefixed env vars are written into the step env: block and resolved in memory at runtime
// by the JavaScript safe-outputs loader (resolveEnvPlaceholders in safe_outputs_config.cjs).
const safeOutputsSecretEnvPrefix = "GH_AW_SECRET_"

type fileRenderItem struct {
	Path       string `json:"path"`
	ContentEnv string `json:"content_env"`
}

type fileRenderConfig struct {
	Files []fileRenderItem `json:"files"`
}

func generateSafeOutputsSetup(c *Compiler, yaml *strings.Builder, safeOutputConfig string, workflowData *WorkflowData) {
	if !HasSafeOutputsEnabled(workflowData.SafeOutputs) {
		return
	}
	safeOutputsSetupLog.Printf("Generating safe outputs setup: configLen=%d", len(safeOutputConfig))
	sanitizedConfig, envKeys, envValues := buildSafeOutputsConfigRuntimeData(safeOutputConfig)
	yaml.WriteString("      - name: Prepare Safe Outputs Directories\n")
	yaml.WriteString("        run: |\n")
	yaml.WriteString("          mkdir -p \"${RUNNER_TEMP}/gh-aw/safeoutputs\"\n")
	yaml.WriteString("          mkdir -p /tmp/gh-aw/safeoutputs\n")
	yaml.WriteString("          mkdir -p /tmp/gh-aw/mcp-logs/safeoutputs\n")
	if usesSafeOutputsArtifactStaging(workflowData.SafeOutputs) {
		yaml.WriteString("          mkdir -p \"${RUNNER_TEMP}/gh-aw/safeoutputs/upload-artifacts\"\n")
	}
	if workflowData.SafeOutputs != nil && workflowData.SafeOutputs.UploadCodeCoverage != nil {
		yaml.WriteString("          mkdir -p \"${RUNNER_TEMP}/gh-aw/safeoutputs/upload-code-coverage\"\n")
	}
	if workflowData.SafeOutputs != nil && workflowData.SafeOutputs.UploadAssets != nil {
		yaml.WriteString("          mkdir -p \"${RUNNER_TEMP}/gh-aw/safeoutputs/assets\"\n")
	}

	if safeOutputConfig != "" {
		safeOutputsSetupLog.Printf("Safe outputs config: envVars=%d", len(envKeys))
		fileConfigJSON, err := json.Marshal(fileRenderConfig{
			Files: []fileRenderItem{{
				Path:       "safeoutputs/config.json",
				ContentEnv: "GH_AW_SAFE_OUTPUTS_CONFIG",
			}},
		})
		if err != nil {
			// Build-time invariant: fileRenderConfig above is a fixed struct literal of
			// strings, which json.Marshal always serialises successfully; this branch is
			// unreachable in practice.
			panic(fmt.Sprintf("BUG: failed to marshal generated file render config: %v", err))
		}
		yaml.WriteString("      - name: Generate Safe Outputs Config\n")
		fmt.Fprintf(yaml, "        uses: %s\n", getCachedActionPin("actions/github-script", workflowData))
		yaml.WriteString("        env:\n")
		writeYAMLEnv(yaml, "          ", "GH_AW_FILE_ROOT", constants.GhAwRootDir)
		writeYAMLEnv(yaml, "          ", "GH_AW_FILE_CONFIG", string(fileConfigJSON))
		writeYAMLEnv(yaml, "          ", "GH_AW_SAFE_OUTPUTS_CONFIG", sanitizedConfig)
		writeStepEnvVars(yaml, envKeys, envValues)
		yaml.WriteString("        with:\n")
		yaml.WriteString("          script: |\n")
		yaml.WriteString(generateGitHubScriptWithRequire("create_files.cjs"))
	}

	toolsMetaJSON, err := generateToolsMetaJSON(workflowData, c.markdownPath)
	if err != nil {
		mcpSetupGeneratorLog.Printf("Error generating tools meta JSON: %v", err)
		toolsMetaJSON = `{"description_suffixes":{},"repo_params":{},"dynamic_tools":[]}`
	}
	sanitizedToolsMetaJSON, toolsMetaEnvKeys, toolsMetaEnvValues := buildToolsMetaRuntimeData(toolsMetaJSON)

	var enabledTypes []string
	if safeOutputConfig != "" {
		var configMap map[string]any
		if err := json.Unmarshal([]byte(safeOutputConfig), &configMap); err == nil {
			for typeName := range configMap {
				enabledTypes = append(enabledTypes, typeName)
			}
		}
	}
	// Propagate mentions config to the collection pass so that allowed @-mentions
	// (e.g. "@copilot") are not backtick-escaped before publish-side handlers run.
	var mentionsBlock map[string]any
	if workflowData.SafeOutputs != nil && workflowData.SafeOutputs.Mentions != nil {
		mentionsBlock = buildMentionsHandlerConfig(workflowData.SafeOutputs.Mentions)
	}
	var normalizedDataSchema map[string]any
	dataEnabled := false
	if workflowData.SafeOutputs != nil {
		normalizedDataSchema = workflowData.SafeOutputs.NormalizedDataSchema
		dataEnabled = workflowData.SafeOutputs.DataEnabled
	}
	validationConfigJSON, err := GetValidationConfigJSONWithDataSchema(enabledTypes, mentionsBlock, dataEnabled, normalizedDataSchema)
	if err != nil {
		mcpSetupGeneratorLog.Printf("CRITICAL: Error generating validation config JSON: %v - validation will not work correctly", err)
		validationConfigJSON = "{}"
	}

	yaml.WriteString("      - name: Generate Safe Outputs Tools\n")
	yaml.WriteString("        env:\n")
	yaml.WriteString("          GH_AW_TOOLS_META_JSON: |\n")
	for line := range strings.SplitSeq(sanitizedToolsMetaJSON, "\n") {
		yaml.WriteString("            " + line + "\n")
	}
	yaml.WriteString("          GH_AW_VALIDATION_JSON: |\n")
	for line := range strings.SplitSeq(validationConfigJSON, "\n") {
		yaml.WriteString("            " + line + "\n")
	}
	writeStepEnvVars(yaml, toolsMetaEnvKeys, toolsMetaEnvValues)
	fmt.Fprintf(yaml, "        uses: %s\n", getCachedActionPin("actions/github-script", workflowData))
	yaml.WriteString("        with:\n")
	yaml.WriteString("          script: |\n")
	yaml.WriteString(generateGitHubScriptWithRequire("generate_safe_outputs_tools.cjs"))
}

func buildSafeOutputsConfigRuntimeEnvVars(safeOutputConfig string) ([]string, map[string]string) {
	configSecrets := ExtractSecretsFromValue(safeOutputConfig)
	configContextVars := ExtractGitHubContextExpressionsFromValue(safeOutputConfig)
	configWorkflowInputs := ExtractWorkflowInputExpressionsFromValue(safeOutputConfig)
	envValues := make(map[string]string, typeutil.SafeAllocationCapacity(len(configSecrets), len(configContextVars), len(configWorkflowInputs)))
	addEnvValue := func(key, value string) {
		envValues[key] = value
	}
	for k, v := range configWorkflowInputs {
		addEnvValue(k, v)
	}
	for k, v := range configContextVars {
		addEnvValue(k, v)
	}
	for k, v := range configSecrets {
		// Prefix secret env vars to avoid colliding with reserved/known step env var names.
		addEnvValue(safeOutputsSecretEnvPrefix+k, v)
	}
	return sliceutil.SortedKeys(envValues), envValues
}

func buildSafeOutputsConfigRuntimeData(safeOutputConfig string) (string, []string, map[string]string) {
	sanitizedConfig := safeOutputConfig
	envKeys, envValues := buildSafeOutputsConfigRuntimeEnvVars(safeOutputConfig)
	safeOutputsSetupLog.Printf("Building safe outputs config runtime data: envKeys=%d", len(envKeys))
	for _, varName := range envKeys {
		value := envValues[varName]
		sanitizedConfig = strings.ReplaceAll(sanitizedConfig, value, "${"+varName+"}")
	}
	return sanitizedConfig, envKeys, envValues
}

func buildToolsMetaRuntimeData(toolsMetaJSON string) (string, []string, map[string]string) {
	if toolsMetaJSON == "" {
		return toolsMetaJSON, nil, nil
	}

	extractor := NewExpressionExtractor()
	expressionEnvVars := make(map[string]string)
	expressions := ExpressionPatternDotAll.FindAllStringSubmatch(toolsMetaJSON, -1)
	for _, match := range expressions {
		if len(match) < 2 {
			continue
		}
		expr := match[0]
		content := strings.TrimSpace(match[1])
		if content == "" {
			continue
		}
		if _, exists := expressionEnvVars[expr]; !exists {
			expressionEnvVars[expr] = extractor.generateEnvVarName(content)
		}
	}

	if len(expressionEnvVars) == 0 {
		return toolsMetaJSON, nil, nil
	}

	envValues := make(map[string]string, len(expressionEnvVars))
	sanitizedToolsMeta := toolsMetaJSON
	for _, expr := range sliceutil.SortedKeys(expressionEnvVars) {
		envName := expressionEnvVars[expr]
		envValues[envName] = decodeJSONStringFragment(expr)
		sanitizedToolsMeta = strings.ReplaceAll(sanitizedToolsMeta, expr, "${"+envName+"}")
	}

	return sanitizedToolsMeta, sliceutil.SortedKeys(envValues), envValues
}

// decodeJSONStringFragment decodes a fragment extracted from a larger JSON-encoded string
// (e.g. an expression matched inside a JSON string value). encoding/json escapes characters
// such as <, >, & as \u003c, \u003e, \u0026 and also escapes quotes/backslashes/control
// characters; this reverses that encoding so the fragment can be used verbatim as a step env
// value. If the fragment cannot be decoded as JSON string content, it is returned unchanged.
//
// Callers must only pass fragments that are themselves valid JSON string interior content
// (i.e. extracted from inside a JSON string value, with balanced escape sequences and no raw
// unescaped quotes). Since toolsMetaJSON is produced by encoding/json and the expression regex
// only matches within an already-encoded string value, this invariant holds for the caller in
// this file.
func decodeJSONStringFragment(fragment string) string {
	var decoded string
	if err := json.Unmarshal([]byte(`"`+fragment+`"`), &decoded); err != nil {
		return fragment
	}
	return decoded
}

func writeStepEnvVars(yaml *strings.Builder, envKeys []string, envValues map[string]string) {
	for _, varName := range envKeys {
		yaml.WriteString("          " + varName + ": " + envValues[varName] + "\n")
	}
}
