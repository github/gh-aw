package workflow

import (
	"encoding/json"
	"fmt"
	"strings"
)

func (c *Compiler) generateStructuredOutputSetup(yaml *strings.Builder, data *WorkflowData) error {
	if data.StructuredOutput == nil {
		return nil
	}
	schema, err := json.Marshal(data.StructuredOutput.Schema)
	if err != nil {
		return fmt.Errorf("structured-output.schema could not be encoded: %w", err)
	}
	yaml.WriteString("      - name: Install structured output validator\n")
	yaml.WriteString("        run: |\n")
	yaml.WriteString("          npm install --prefix \"$RUNNER_TEMP/gh-aw/actions/structured-output-validator\" --ignore-scripts --no-audit --no-fund --package-lock=false ajv@8.17.1 ajv-formats@3.0.1\n")
	yaml.WriteString("      - name: Prepare structured output schema\n")
	yaml.WriteString("        uses: " + getCachedActionPin("actions/github-script", data) + "\n")
	yaml.WriteString("        env:\n")
	yaml.WriteString("          GH_AW_STRUCTURED_OUTPUT_SCHEMA: " + quoteYAMLEnvValue(string(schema)) + "\n")
	yaml.WriteString("        with:\n")
	yaml.WriteString("          script: |\n")
	yaml.WriteString("            const fs = require('fs');\n")
	yaml.WriteString("            fs.writeFileSync('" + StructuredOutputSchemaPath + "', process.env.GH_AW_STRUCTURED_OUTPUT_SCHEMA);\n")
	yaml.WriteString("            fs.rmSync('" + StructuredOutputFilePath + "', { force: true });\n")
	return nil
}

func (c *Compiler) generateStructuredOutputCollection(yaml *strings.Builder, data *WorkflowData) error {
	if data.StructuredOutput == nil {
		return nil
	}
	schema, err := json.Marshal(data.StructuredOutput.Schema)
	if err != nil {
		return fmt.Errorf("structured-output.schema could not be encoded: %w", err)
	}
	yaml.WriteString("      - name: Validate structured output\n")
	yaml.WriteString("        id: structured_output\n")
	yaml.WriteString("        if: \"!cancelled()\"\n")
	yaml.WriteString("        uses: " + getCachedActionPin("actions/github-script", data) + "\n")
	yaml.WriteString("        env:\n")
	yaml.WriteString("          GH_AW_STRUCTURED_OUTPUT_EXECUTION: ${{ steps.agentic_execution.outcome }}\n")
	yaml.WriteString("          GH_AW_STRUCTURED_OUTPUT_REDACTION: ${{ steps.redact_secrets.outcome }}\n")
	yaml.WriteString("          GH_AW_STRUCTURED_OUTPUT_SCHEMA: " + quoteYAMLEnvValue(string(schema)) + "\n")
	yaml.WriteString("          GH_AW_STRUCTURED_OUTPUT_FILE: " + StructuredOutputFilePath + "\n")
	yaml.WriteString("        with:\n")
	yaml.WriteString("          script: |\n")
	yaml.WriteString("            if (process.env.GH_AW_STRUCTURED_OUTPUT_EXECUTION !== 'success' || process.env.GH_AW_STRUCTURED_OUTPUT_REDACTION !== 'success') {\n")
	yaml.WriteString("              throw new Error('Structured output requires successful agent execution and secret redaction');\n")
	yaml.WriteString("            }\n")
	yaml.WriteString("            const { publishStructuredOutput } = require('" + SetupActionDestination + "/structured_output.cjs');\n")
	yaml.WriteString("            publishStructuredOutput(core);\n")
	return nil
}
