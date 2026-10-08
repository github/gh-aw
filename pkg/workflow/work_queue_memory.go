package workflow

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"path"
	"slices"
	"strings"

	"github.com/github/gh-aw/pkg/stringutil"
)

// WorkQueueMemoryConfig declares data preparation; installed Claim scope remains authoritative.
type WorkQueueMemoryConfig struct {
	Name         string         `json:"name"`
	Path         string         `json:"path"`
	TargetRepo   string         `json:"target-repo"`
	BaseRevision string         `json:"base-revision"`
	BranchPrefix string         `json:"branch-prefix"`
	MaxBytes     int            `json:"max-bytes"`
	Schema       map[string]any `json:"schema"`
}

func configureWorkQueueMemory(data *WorkflowData) error {
	tool, ok := data.Tools["work-queue"].(map[string]any)
	if !ok {
		return nil
	}
	raw, exists := tool["memory"]
	if !exists {
		return nil
	}
	if !isWorkQueueWorker(data) {
		return errors.New("tools.work-queue.memory requires a declared work-queue worker")
	}
	config, err := parseWorkQueueMemory(raw)
	if err != nil {
		return err
	}
	if data.SafeOutputs == nil {
		data.SafeOutputs = &SafeOutputsConfig{WorkQueueEnabled: true}
	}
	outputs := data.SafeOutputs
	for _, name := range []string{"noop", "constructor", "prototype", "work_queue_read", "work_queue_explain", "work_queue_submit", "work_queue_dispatch_next", "work_queue_claim_finish"} {
		if config.Name == name {
			return fmt.Errorf("tools.work-queue.memory.name: %q is reserved", name)
		}
	}
	if _, builtin := handlerRegistry[config.Name]; builtin {
		return fmt.Errorf("tools.work-queue.memory.name: %q conflicts with a built-in output", config.Name)
	}
	script, adapter, err := buildWorkQueueMemoryPreparation(config)
	if err != nil {
		return err
	}
	if err := validateWorkQueueMemoryConflicts(outputs, config.Name, script, adapter); err != nil {
		return err
	}
	if outputs.Scripts == nil {
		outputs.Scripts = map[string]*SafeScriptConfig{}
	}
	if outputs.ClaimAdapters == nil {
		outputs.ClaimAdapters = map[string]*WorkQueueClaimAdapter{}
	}
	outputs.Scripts[config.Name] = script
	outputs.ClaimAdapters[config.Name] = adapter
	return nil
}

func buildWorkQueueMemoryPreparation(config *WorkQueueMemoryConfig) (*SafeScriptConfig, *WorkQueueClaimAdapter, error) {
	encoded, err := json.Marshal(config)
	if err != nil {
		return nil, nil, fmt.Errorf("tools.work-queue.memory: %w", err)
	}
	preparation, err := json.Marshal(map[string]any{"path": config.Path, "max_bytes": config.MaxBytes, "schema": config.Schema})
	if err != nil {
		return nil, nil, fmt.Errorf("tools.work-queue.memory: %w", err)
	}
	literal, err := json.Marshal(string(preparation))
	if err != nil {
		return nil, nil, fmt.Errorf("tools.work-queue.memory: %w", err)
	}
	script := &SafeScriptConfig{
		Description:      "Prepare one schema-validated immutable memory snapshot for this Claim",
		Inputs:           map[string]*InputDefinition{"memory": {Type: "object", Required: true, Description: "Structured memory data conforming to the configured schema"}},
		Script:           `return require("./work_queue_memory.cjs").prepareMemorySnapshot(item, JSON.parse(` + string(literal) + `));`,
		MemorySchema:     config.Schema,
		MemoryDefinition: string(encoded),
		Max:              1,
	}
	adapter := &WorkQueueClaimAdapter{
		Mode: "script", EffectType: "git_tree", TargetRepo: config.TargetRepo,
		FieldMap: map[string]string{"files": "files"},
		GitTree:  &WorkQueueGitTree{BaseRevision: config.BaseRevision, BranchPrefix: config.BranchPrefix},
	}
	return script, adapter, nil
}

func validateWorkQueueMemoryConflicts(outputs *SafeOutputsConfig, name string, script *SafeScriptConfig, adapter *WorkQueueClaimAdapter) error {
	generated := outputs.Scripts[name] != nil && outputs.Scripts[name].MemoryDefinition == script.MemoryDefinition
	if generated {
		for _, pair := range [][2]any{{outputs.Scripts[name], script}, {outputs.ClaimAdapters[name], adapter}} {
			equal, err := workQueueMemoryDefinitionsEqual(pair[0], pair[1])
			if err != nil {
				return err
			}
			if !equal {
				return fmt.Errorf("tools.work-queue.memory.name: %q generated preparation or delivery configuration was modified", name)
			}
		}
	}
	for index, names := range []map[string]int{
		memoryConfiguredNames(outputs.Scripts), memoryConfiguredNames(outputs.ClaimAdapters),
		memoryConfiguredNames(outputs.Jobs), memoryConfiguredNames(outputs.Actions),
	} {
		allowed := 0
		if generated && index < 2 {
			allowed = 1
		}
		if names[name] > allowed {
			return fmt.Errorf("tools.work-queue.memory.name: %q conflicts with a configured output or adapter", name)
		}
	}
	return nil
}

func workQueueMemoryDefinitionsEqual(actual, expected any) (bool, error) {
	actualJSON, err := json.Marshal(actual)
	if err != nil {
		return false, fmt.Errorf("tools.work-queue.memory: invalid generated configuration: %w", err)
	}
	expectedJSON, err := json.Marshal(expected)
	if err != nil {
		return false, fmt.Errorf("tools.work-queue.memory: invalid expected configuration: %w", err)
	}
	return bytes.Equal(actualJSON, expectedJSON), nil
}

func memoryConfiguredNames[T any](values map[string]T) map[string]int {
	result := make(map[string]int, len(values))
	for name := range values {
		result[stringutil.NormalizeSafeOutputIdentifier(name)]++
	}
	return result
}

func parseWorkQueueMemory(raw any) (*WorkQueueMemoryConfig, error) {
	fields, ok := raw.(map[string]any)
	if !ok {
		return nil, errors.New("tools.work-queue.memory requires an object")
	}
	encoded, err := json.Marshal(fields)
	if err != nil {
		return nil, fmt.Errorf("tools.work-queue.memory: %w", err)
	}
	config := &WorkQueueMemoryConfig{Name: "persist_work_queue_memory", MaxBytes: 262144}
	decoder := json.NewDecoder(strings.NewReader(string(encoded)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(config); err != nil {
		return nil, fmt.Errorf("tools.work-queue.memory: %w", err)
	}
	for _, field := range []string{"name", "max-bytes"} {
		if value, exists := fields[field]; exists && value == nil {
			return nil, fmt.Errorf("tools.work-queue.memory.%s cannot be null; omit the field to use its default", field)
		}
	}
	if strings.Contains(string(encoded), "${{") {
		return nil, errors.New("tools.work-queue.memory requires literal configuration without GitHub expressions")
	}
	if !workQueueAdapterNamePattern.MatchString(config.Name) || len(config.Name) > 64 {
		return nil, errors.New("tools.work-queue.memory.name requires a bounded tool identifier")
	}
	config.Name = stringutil.NormalizeSafeOutputIdentifier(config.Name)
	if err := validateWorkQueueMemoryTarget(config); err != nil {
		return nil, err
	}
	if err := validateWorkQueueMemorySnapshot(config); err != nil {
		return nil, err
	}
	return config, nil
}

func validateWorkQueueMemoryTarget(config *WorkQueueMemoryConfig) error {
	if config.Path == "" || path.IsAbs(config.Path) || path.Clean(config.Path) != config.Path || config.Path == "." || strings.Contains(config.Path, "\\") || strings.HasPrefix(config.Path, "../") || !strings.HasSuffix(config.Path, ".json") || len(config.Path) > 256 {
		return errors.New("tools.work-queue.memory.path requires a fixed relative .json path without traversal")
	}
	for component := range strings.SplitSeq(config.Path, "/") {
		if strings.EqualFold(component, ".git") {
			return errors.New("tools.work-queue.memory.path cannot address Git metadata")
		}
	}
	for _, char := range config.Path {
		if char < 32 || char == 127 {
			return errors.New("tools.work-queue.memory.path cannot contain control characters")
		}
	}
	adapter := &WorkQueueClaimAdapter{Mode: "script", EffectType: "git_tree", TargetRepo: config.TargetRepo, FieldMap: map[string]string{"files": "files"}, GitTree: &WorkQueueGitTree{BaseRevision: config.BaseRevision, BranchPrefix: config.BranchPrefix}}
	if !repoSlugPattern.MatchString(config.TargetRepo) || strings.Contains(config.TargetRepo, "${{") {
		return errors.New("tools.work-queue.memory.target-repo requires a fixed repository")
	}
	if err := validateWorkQueueGitTreeAdapter(config.Name, adapter); err != nil {
		return fmt.Errorf("tools.work-queue.memory: %w", err)
	}
	return nil
}

func validateWorkQueueMemorySnapshot(config *WorkQueueMemoryConfig) error {
	if config.MaxBytes < 1 || config.MaxBytes > 262144 {
		return errors.New("tools.work-queue.memory.max-bytes must be between 1 and 262144")
	}
	schemaBytes, err := json.Marshal(config.Schema)
	rootType, typed := config.Schema["type"].(string)
	if err != nil || len(schemaBytes) > 16384 || !typed || rootType != "object" {
		return errors.New("tools.work-queue.memory.schema requires a bounded object schema")
	}
	if err := validateWorkQueueMemorySchema(config.Schema, 0); err != nil {
		return err
	}
	if _, err := compileSchema(string(schemaBytes), "inmem://work-queue-memory.json"); err != nil {
		return fmt.Errorf("tools.work-queue.memory.schema: %w", err)
	}
	return nil
}

func validateWorkQueueMemorySchema(schema map[string]any, depth int) error {
	if depth > 16 {
		return errors.New("tools.work-queue.memory.schema exceeds 16 nested schemas")
	}
	allowed := []string{"type", "description", "properties", "required", "additionalProperties", "items", "enum", "minimum", "maximum", "minLength", "maxLength", "minItems", "maxItems"}
	for key := range schema {
		if !slices.Contains(allowed, key) {
			return fmt.Errorf("tools.work-queue.memory.schema: unsupported keyword %q; references and executable validators are unavailable", key)
		}
	}
	kind, typed := schema["type"].(string)
	if !typed || !slices.Contains([]string{"object", "array", "string", "number", "integer", "boolean", "null"}, kind) {
		return errors.New("tools.work-queue.memory.schema: every schema requires one supported type")
	}
	if additional, exists := schema["additionalProperties"]; exists {
		if _, ok := additional.(bool); !ok {
			return errors.New("tools.work-queue.memory.schema.additionalProperties must be boolean")
		}
	}
	if properties, exists := schema["properties"]; exists {
		fields, ok := properties.(map[string]any)
		if !ok {
			return errors.New("tools.work-queue.memory.schema.properties must be an object")
		}
		for _, raw := range fields {
			child, ok := raw.(map[string]any)
			if !ok {
				return errors.New("tools.work-queue.memory.schema.properties entries must be typed schemas")
			}
			if err := validateWorkQueueMemorySchema(child, depth+1); err != nil {
				return err
			}
		}
	}
	if raw, exists := schema["items"]; exists {
		child, ok := raw.(map[string]any)
		if !ok {
			return errors.New("tools.work-queue.memory.schema.items requires one typed schema")
		}
		if err := validateWorkQueueMemorySchema(child, depth+1); err != nil {
			return err
		}
	}
	if choices, exists := schema["enum"].([]any); exists {
		for _, choice := range choices {
			switch choice.(type) {
			case map[string]any, []any:
				return errors.New("tools.work-queue.memory.schema.enum supports scalar values only")
			}
		}
	}
	return nil
}
