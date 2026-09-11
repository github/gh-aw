package cli

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/github/gh-aw/pkg/logger"
	"github.com/google/jsonschema-go/jsonschema"
)

var mcpSchemaLog = logger.New("cli:mcp_schema")

// GenerateSchema generates a JSON schema from a Go struct type.
// This is used for both MCP tool input parameters (InputSchema) and output data types.
// The schema conforms to JSON Schema draft 2020-12 and draft-07.
//
// Schema generation rules:
//   - json tags define property names
//   - jsonschema tags define descriptions
//   - omitempty/omitzero mark optional fields
//   - Pointer types include null in their type array
//   - Slices allow null values (jsonschema-go v0.4.0+)
//   - PropertyOrder maintains deterministic field ordering (v0.4.0+)
//
// MCP Requirements:
//   - Tool input/output schemas must be objects (not arrays or primitives)
//   - All properties should have descriptions for better LLM understanding
//   - Required vs optional fields must be correctly specified
//
// Example:
//
//	type MyArgs struct {
//	    Name string `json:"name" jsonschema:"Name of the user"`
//	    Age  int    `json:"age,omitempty" jsonschema:"Age in years"`
//	}
//	schema, err := GenerateSchema[MyArgs]()
func GenerateSchema[T any]() (*jsonschema.Schema, error) {
	return jsonschema.For[T](nil)
}

// GenerateOutputSchema generates a JSON schema for a structured command output type.
func GenerateOutputSchema[T any]() (*jsonschema.Schema, error) {
	return GenerateSchema[T]()
}

// MarshalOutputSchema serializes a generated output schema using the CLI's
// standard indented JSON format.
func MarshalOutputSchema(schema *jsonschema.Schema) ([]byte, error) {
	data, err := json.MarshalIndent(schema, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("failed to marshal output schema to JSON: %w", err)
	}
	return append(data, '\n'), nil
}

// GenerateNamedOutputSchema generates and serializes a known CLI output schema.
func GenerateNamedOutputSchema(name string) ([]byte, error) {
	var (
		schema *jsonschema.Schema
		err    error
	)
	switch name {
	case "audit":
		schema, err = generateAuditOutputSchema()
	case "logs":
		schema, err = generateLogsOutputSchema()
	case "logs-jsonl":
		schema, err = generateLogsJSONLItemSchema()
	default:
		return nil, fmt.Errorf("unsupported schema: %s", name)
	}

	if err != nil {
		return nil, fmt.Errorf("failed to generate %s schema: %w", name, err)
	}
	return MarshalOutputSchema(schema)
}

type cachedLogsJSONLRunItemSchema struct {
	SchemaVersion int                    `json:"schema_version"`
	Kind          string                 `json:"kind"`
	Run           cachedLogsJSONLRunData `json:"run"`
}

type cachedWorkflowRunListItemSchema struct {
	DatabaseID   int64     `json:"databaseId"`
	Number       int       `json:"number"`
	URL          string    `json:"url"`
	Status       string    `json:"status"`
	Conclusion   string    `json:"conclusion"`
	WorkflowName string    `json:"workflowName"`
	CreatedAt    time.Time `json:"createdAt"`
	StartedAt    time.Time `json:"startedAt"`
	UpdatedAt    time.Time `json:"updatedAt"`
	Event        string    `json:"event"`
	HeadBranch   string    `json:"headBranch"`
	HeadSha      string    `json:"headSha"`
	DisplayTitle string    `json:"displayTitle"`
	Attempt      int       `json:"attempt"`
}

type cachedLogsJSONLWorkflowRunsItemSchema struct {
	SchemaVersion int                               `json:"schema_version"`
	Kind          string                            `json:"kind"`
	Request       cachedWorkflowRunsRequest         `json:"request"`
	Payload       []cachedWorkflowRunListItemSchema `json:"payload"`
}

type cachedLogsJSONLRateLimitItemSchema struct {
	SchemaVersion int                      `json:"schema_version"`
	Kind          string                   `json:"kind"`
	RateLimit     GitHubAPIRateLimitReport `json:"rate_limit"`
}

func generateLogsJSONLItemSchema() (*jsonschema.Schema, error) {
	run, err := GenerateOutputSchema[cachedLogsJSONLRunItemSchema]()
	if err != nil {
		return nil, err
	}
	run.Properties["schema_version"].Enum = []any{cachedLogsJSONLSchemaVersion}
	run.Properties["kind"].Enum = []any{cachedLogsJSONLKindRun}
	auditData, err := generateAuditDataOutputSchema()
	if err != nil {
		return nil, err
	}
	if err := replacePropertySchema(run, []string{"run", "audit"}, auditData); err != nil {
		return nil, err
	}
	workflowRuns, err := GenerateOutputSchema[cachedLogsJSONLWorkflowRunsItemSchema]()
	if err != nil {
		return nil, err
	}
	workflowRuns.Properties["schema_version"].Enum = []any{cachedLogsJSONLSchemaVersion}
	workflowRuns.Properties["kind"].Enum = []any{cachedLogsJSONLKindWorkflowRuns}
	workflowRuns.Properties["payload"].Types = nil
	workflowRuns.Properties["payload"].Type = "array"
	workflowRuns.Properties["payload"].Items.AdditionalProperties = &jsonschema.Schema{}
	rateLimit, err := GenerateOutputSchema[cachedLogsJSONLRateLimitItemSchema]()
	if err != nil {
		return nil, err
	}
	rateLimit.Properties["schema_version"].Enum = []any{cachedLogsJSONLSchemaVersion}
	rateLimit.Properties["kind"].Enum = []any{cachedLogsJSONLKindRateLimit}
	return &jsonschema.Schema{OneOf: []*jsonschema.Schema{run, workflowRuns, rateLimit}}, nil
}

func generateAuditOutputSchema() (*jsonschema.Schema, error) {
	auditData, err := generateAuditDataOutputSchema()
	if err != nil {
		return nil, err
	}
	auditDiff, err := GenerateOutputSchema[AuditDiff]()
	if err != nil {
		return nil, err
	}
	auditDiffs, err := GenerateOutputSchema[[]AuditDiff]()
	if err != nil {
		return nil, err
	}
	return &jsonschema.Schema{OneOf: []*jsonschema.Schema{auditData, auditDiff, auditDiffs}}, nil
}

func generateLogsOutputSchema() (*jsonschema.Schema, error) {
	logsData, err := generateLogsDataOutputSchema()
	if err != nil {
		return nil, err
	}
	crossRun, err := generateCrossRunAuditOutputSchema()
	if err != nil {
		return nil, err
	}
	return &jsonschema.Schema{OneOf: []*jsonschema.Schema{logsData, crossRun}}, nil
}

// These wire types mirror custom MarshalJSON output, which reflection cannot infer.
type toolUsageSummaryWire struct {
	Name          string `json:"name"`
	TotalCalls    int    `json:"total_calls"`
	Runs          int    `json:"runs"`
	MaxOutputSize int    `json:"max_output_size,omitempty"`
	MaxDuration   string `json:"max_duration,omitempty"`
}

type mcpServerHealthDetailWire struct {
	ServerName   string  `json:"server_name"`
	RequestCount int     `json:"request_count"`
	ToolCalls    int     `json:"tool_calls"`
	ErrorCount   int     `json:"error_count"`
	ErrorRate    float64 `json:"error_rate"`
	ErrorRateStr string  `json:"error_rate_str"`
	AvgLatency   string  `json:"avg_latency"`
	Status       string  `json:"status"`
}

type mcpServerCrossRunHealthWire struct {
	ServerName    string  `json:"server_name"`
	RunsConnected int     `json:"runs_connected"`
	TotalRuns     int     `json:"total_runs"`
	TotalCalls    int     `json:"total_calls"`
	TotalErrors   int     `json:"total_errors"`
	ErrorRate     float64 `json:"error_rate"`
	Unreliable    bool    `json:"unreliable"`
}

type mcpFailureSummaryWire struct {
	ServerName string   `json:"server_name"`
	Count      int      `json:"count"`
	Workflows  []string `json:"workflows"`
	RunIDs     []int64  `json:"run_ids"`
}

type domainAnalysisWireSchema struct {
	TotalRequests  int      `json:"total_requests"`
	AllowedCount   int      `json:"allowed_count"`
	BlockedCount   int      `json:"blocked_count"`
	AllowedDomains []string `json:"allowed_domains,omitempty"`
	BlockedDomains []string `json:"blocked_domains,omitempty"`
}

func generateAuditDataOutputSchema() (*jsonschema.Schema, error) {
	schema, err := GenerateOutputSchema[AuditData]()
	if err != nil {
		return nil, err
	}
	wire, err := GenerateOutputSchema[mcpServerHealthDetailWire]()
	if err != nil {
		return nil, err
	}
	if err := replaceArrayItemSchema(schema, []string{"mcp_server_health", "servers"}, wire); err != nil {
		return nil, err
	}
	return schema, nil
}

func generateLogsDataOutputSchema() (*jsonschema.Schema, error) {
	schema, err := GenerateOutputSchema[LogsData]()
	if err != nil {
		return nil, err
	}
	wire, err := GenerateOutputSchema[toolUsageSummaryWire]()
	if err != nil {
		return nil, err
	}
	if err := replaceArrayItemSchema(schema, []string{"tool_usage"}, wire); err != nil {
		return nil, err
	}
	mcpFailures, err := GenerateOutputSchema[mcpFailureSummaryWire]()
	if err != nil {
		return nil, err
	}
	if err := replaceArrayItemSchema(schema, []string{"mcp_failures"}, mcpFailures); err != nil {
		return nil, err
	}
	accessAnalysis, err := GenerateOutputSchema[domainAnalysisWireSchema]()
	if err != nil {
		return nil, err
	}
	if err := replaceMapValueSchema(schema, []string{"access_log", "by_workflow"}, accessAnalysis); err != nil {
		return nil, err
	}
	return schema, nil
}

func generateCrossRunAuditOutputSchema() (*jsonschema.Schema, error) {
	schema, err := GenerateOutputSchema[CrossRunAuditReport]()
	if err != nil {
		return nil, err
	}
	wire, err := GenerateOutputSchema[mcpServerCrossRunHealthWire]()
	if err != nil {
		return nil, err
	}
	if err := replaceArrayItemSchema(schema, []string{"mcp_health"}, wire); err != nil {
		return nil, err
	}
	return schema, nil
}

func replaceArrayItemSchema(schema *jsonschema.Schema, path []string, replacement *jsonschema.Schema) error {
	target := schema
	for _, property := range path {
		var ok bool
		target, ok = target.Properties[property]
		if !ok {
			return fmt.Errorf("schema property %q not found", property)
		}
	}
	if target.Items == nil {
		return fmt.Errorf("schema property %q is not an array", path[len(path)-1])
	}
	target.Items = replacement
	return nil
}

func replacePropertySchema(schema *jsonschema.Schema, path []string, replacement *jsonschema.Schema) error {
	target := schema
	for _, property := range path[:len(path)-1] {
		var ok bool
		target, ok = target.Properties[property]
		if !ok {
			return fmt.Errorf("schema property %q not found", property)
		}
	}
	property := path[len(path)-1]
	if _, ok := target.Properties[property]; !ok {
		return fmt.Errorf("schema property %q not found", property)
	}
	target.Properties[property] = replacement
	return nil
}

func replaceMapValueSchema(schema *jsonschema.Schema, path []string, replacement *jsonschema.Schema) error {
	target := schema
	for _, property := range path {
		var ok bool
		target, ok = target.Properties[property]
		if !ok {
			return fmt.Errorf("schema property %q not found", property)
		}
	}
	if target.AdditionalProperties == nil {
		return fmt.Errorf("schema property %q is not a map", path[len(path)-1])
	}
	target.AdditionalProperties = replacement
	return nil
}

func generateSchemaWithDefaults[T any](defaults map[string]any) (*jsonschema.Schema, error) {
	schema, err := GenerateSchema[T]()
	if err != nil {
		return nil, err
	}

	for propertyName, value := range defaults {
		if err := AddSchemaDefault(schema, propertyName, value); err != nil {
			mcpSchemaLog.Printf("Failed to add default for %s: %v", propertyName, err)
		}
	}

	return schema, nil
}

// AddSchemaDefault adds a default value to a property in a JSON schema.
// This is useful for elicitation defaults (SEP-1024) that improve UX by
// suggesting sensible starting values to MCP clients.
//
// The value must be JSON-marshallable and appropriate for the property type.
//
// Example:
//
//	schema, err := GenerateSchema[MyArgs]()
//	AddSchemaDefault(schema, "count", 100)          // number default
//	AddSchemaDefault(schema, "enabled", true)       // boolean default
//	AddSchemaDefault(schema, "name", "default")     // string default
func AddSchemaDefault(schema *jsonschema.Schema, propertyName string, value any) error {
	if schema == nil || schema.Properties == nil {
		// Defaults are meaningful only for object schemas with generated properties.
		return nil
	}

	prop, ok := schema.Properties[propertyName]
	if !ok {
		return fmt.Errorf("schema property %q not found", propertyName)
	}

	// Marshal the value to JSON
	defaultBytes, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("failed to marshal default value for %s: %w", propertyName, err)
	}

	mcpSchemaLog.Printf("Setting default value for schema property: %s", propertyName)
	prop.Default = json.RawMessage(defaultBytes)
	return nil
}
