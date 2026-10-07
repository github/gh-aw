package workflow

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/github/gh-aw/pkg/logger"
)

var structuredOutputLog = logger.New("workflow:structured_output")

const (
	StructuredOutputSchemaPath = "/tmp/gh-aw/structured-output-schema.json"
	StructuredOutputFilePath   = "/tmp/gh-aw/structured-output.json"
)

// StructuredOutputConfig defines the contract for an engine's primary response.
// Schema files are resolved and embedded at compile time, not read from the checkout at runtime.
type StructuredOutputConfig struct {
	Schema map[string]any `json:"schema"`
}

type structuredOutputEngine interface {
	Engine
	CapabilityProvider
}

func parseStructuredOutput(frontmatter map[string]any, workflowPath string, engine structuredOutputEngine, config *EngineConfig) (*StructuredOutputConfig, error) {
	raw, present := frontmatter["structured-output"]
	if !present {
		return nil, nil
	}
	if !engine.GetCapabilities().StructuredOutput {
		return nil, fmt.Errorf("structured-output is not supported by engine %q: select an engine with native JSON Schema output support, for example 'engine: codex'", engine.GetID())
	}
	if validator, ok := engine.(StructuredOutputConfigValidator); ok {
		if err := validator.ValidateStructuredOutputConfig(config); err != nil {
			return nil, err
		}
	}
	settings, ok := raw.(map[string]any)
	if !ok {
		return nil, errors.New("structured-output must be an object with exactly one of 'schema' or 'schema-file'. Example: structured-output: {schema: {type: object}}")
	}
	inline, hasInline := settings["schema"]
	file, hasFile := settings["schema-file"]
	if hasInline == hasFile || len(settings) != 1 {
		return nil, errors.New("structured-output must specify exactly one of 'schema' or 'schema-file'. Example: structured-output: {schema-file: .github/schemas/output.json}")
	}
	var schema map[string]any
	if hasInline {
		schema, ok = inline.(map[string]any)
		if !ok {
			return nil, errors.New("structured-output.schema must be a JSON Schema object. Example: structured-output: {schema: {type: object}}")
		}
	} else {
		name, ok := file.(string)
		if !ok || name == "" || filepath.IsAbs(name) {
			return nil, errors.New("structured-output.schema-file must be a repository-relative file path. Example: structured-output: {schema-file: .github/schemas/output.json}")
		}
		root, err := findStructuredOutputRepositoryRoot(workflowPath)
		if err != nil {
			return nil, err
		}
		resolved, err := filepath.EvalSymlinks(filepath.Join(root, name))
		if err != nil {
			return nil, fmt.Errorf("structured-output.schema-file %q could not be resolved: %w", name, err)
		}
		rel, err := filepath.Rel(root, resolved)
		if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return nil, errors.New("structured-output.schema-file must stay inside the repository. Place the schema at .github/schemas/output.json and reference that path")
		}
		contents, err := os.ReadFile(resolved)
		if err != nil {
			return nil, fmt.Errorf("structured-output.schema-file %q could not be read: %w", name, err)
		}
		if err := json.Unmarshal(contents, &schema); err != nil {
			return nil, fmt.Errorf("structured-output.schema-file %q must contain a JSON Schema object: %w", name, err)
		}
	}
	if err := validateStructuredOutputSchema(schema); err != nil {
		return nil, err
	}
	structuredOutputLog.Printf("Validated primary response schema for engine %s", engine.GetID())
	return &StructuredOutputConfig{Schema: schema}, nil
}

func formatStructuredOutputError(ctx *workflowBuildContext, err error) error {
	message := err.Error() + ". See https://github.github.com/gh-aw/reference/structured-output/"
	line := findFrontmatterFieldLine(ctx.frontmatter.FrontmatterLines, ctx.frontmatter.FrontmatterStart, "structured-output")
	if line > 0 {
		return formatCompilerErrorWithContext(ctx.cleanPath, line, 1, "error", message, err, readSourceContextLines(ctx.content, line))
	}
	return formatCompilerError(ctx.cleanPath, "error", message, err)
}

func findStructuredOutputRepositoryRoot(workflowPath string) (string, error) {
	dir, err := filepath.Abs(filepath.Dir(workflowPath))
	if err != nil {
		return "", fmt.Errorf("structured-output repository path could not be resolved: %w", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
			return filepath.EvalSymlinks(dir)
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", errors.New("structured-output.schema-file requires a workflow inside a repository")
		}
		dir = parent
	}
}

func validateStructuredOutputSchema(schema map[string]any) error {
	if schema == nil || schema["type"] != "object" {
		return errors.New("structured-output.schema must declare 'type: object' at its root. Example: structured-output: {schema: {type: object}}")
	}
	if draft, present := schema["$schema"]; present && !isStructuredOutputDraft(draft) {
		return errors.New("structured-output.schema supports JSON Schema draft-07 or 2020-12. Example: $schema: https://json-schema.org/draft/2020-12/schema")
	}
	if err := validateStructuredOutputReferences(schema); err != nil {
		return err
	}
	encoded, err := json.Marshal(schema)
	if err != nil {
		return fmt.Errorf("structured-output.schema could not be encoded as JSON: %w", err)
	}
	if strings.Contains(string(encoded), "${{") {
		return errors.New("structured-output.schema must be static: GitHub Actions expressions are not supported")
	}
	if len(encoded) > 64*1024 {
		return errors.New("structured-output.schema exceeds the 64 KiB native invocation limit: reduce the schema size")
	}
	if _, err := compileSchema(string(encoded), "https://gh-aw.invalid/structured-output.schema.json"); err != nil {
		return fmt.Errorf("structured-output.schema is not a valid JSON Schema: %w", err)
	}
	return nil
}

func isStructuredOutputDraft(value any) bool {
	draft, ok := value.(string)
	if !ok {
		return false
	}
	switch strings.TrimSuffix(draft, "#") {
	case "http://json-schema.org/draft-07/schema", "https://json-schema.org/draft-07/schema",
		"https://json-schema.org/draft/2020-12/schema":
		return true
	default:
		return false
	}
}

func validateStructuredOutputReferences(value any) error {
	switch node := value.(type) {
	case map[string]any:
		for key, child := range node {
			if key == "$ref" || key == "$dynamicRef" || key == "$recursiveRef" {
				ref, ok := child.(string)
				if !ok || !strings.HasPrefix(ref, "#") {
					return errors.New("structured-output.schema references must be local fragments; external schema references are not supported. Example: $ref: '#/$defs/value'")
				}
			}
			if key == "$schema" && !isStructuredOutputDraft(child) {
				return errors.New("structured-output.schema supports JSON Schema draft-07 or 2020-12. Example: $schema: https://json-schema.org/draft/2020-12/schema")
			}
			switch key {
			case "properties", "patternProperties", "definitions", "$defs", "dependentSchemas", "dependencies":
				if entries, ok := child.(map[string]any); ok {
					for _, subschema := range entries {
						if err := validateStructuredOutputReferences(subschema); err != nil {
							return err
						}
					}
				}
			case "allOf", "anyOf", "oneOf", "prefixItems", "items", "additionalItems",
				"additionalProperties", "unevaluatedProperties", "unevaluatedItems",
				"contains", "propertyNames", "not", "if", "then", "else", "contentSchema":
				if err := validateStructuredOutputReferences(child); err != nil {
					return err
				}
			}
		}
	case []any:
		for _, child := range node {
			if err := validateStructuredOutputReferences(child); err != nil {
				return err
			}
		}
	}
	return nil
}
