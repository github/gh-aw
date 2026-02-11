//go:build !integration

package cli

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestCheckLogsOutputSize_SmallOutput(t *testing.T) {
	// Create a small output (less than default token limit)
	smallOutput := `{"summary": {"total_runs": 1}, "runs": []}`

	result, triggered := checkLogsOutputSize(smallOutput, 0)

	if triggered {
		t.Error("Guardrail should not be triggered for small output")
	}

	if result != smallOutput {
		t.Error("Output should be unchanged for small output")
	}
}

func TestCheckLogsOutputSize_LargeOutput(t *testing.T) {
	// Create a large output (more than default token limit)
	// Default is 12000 tokens, which is ~48000 characters
	// Use 50000 to be safely over the limit
	largeOutput := strings.Repeat("x", 50000)

	result, triggered := checkLogsOutputSize(largeOutput, 0)

	if !triggered {
		t.Error("Guardrail should be triggered for large output")
	}

	if result == largeOutput {
		t.Error("Output should be replaced with guardrail response for large output")
	}

	// Verify the result contains a valid JSON guardrail response
	var guardrail MCPLogsGuardrailResponse
	if err := json.Unmarshal([]byte(result), &guardrail); err != nil {
		t.Errorf("Guardrail response should be valid JSON: %v", err)
	}

	// Verify guardrail response structure
	if guardrail.Message == "" {
		t.Error("Guardrail response should have a message")
	}

	expectedTokens := estimateTokens(largeOutput)
	if guardrail.OutputTokens != expectedTokens {
		t.Errorf("Guardrail should report correct output tokens: expected %d, got %d", expectedTokens, guardrail.OutputTokens)
	}

	if guardrail.OutputSizeLimit != DefaultMaxMCPLogsOutputTokens {
		t.Errorf("Guardrail should report correct limit: expected %d, got %d", DefaultMaxMCPLogsOutputTokens, guardrail.OutputSizeLimit)
	}

	if len(guardrail.Schema.Fields) == 0 {
		t.Error("Guardrail response should have schema fields")
	}
}

func TestCheckLogsOutputSize_ExactLimit(t *testing.T) {
	// Create output exactly at the limit
	// 12000 tokens = 48000 characters
	exactOutput := strings.Repeat("x", 48000)

	result, triggered := checkLogsOutputSize(exactOutput, 0)

	if triggered {
		t.Error("Guardrail should not be triggered for output at exact limit")
	}

	if result != exactOutput {
		t.Error("Output should be unchanged for output at exact limit")
	}
}

func TestCheckLogsOutputSize_JustOverLimit(t *testing.T) {
	// Create output just over the limit (12000 tokens + 1 token)
	// 12001 tokens = 48004+ characters
	overOutput := strings.Repeat("x", 48005)

	_, triggered := checkLogsOutputSize(overOutput, 0)

	if !triggered {
		t.Error("Guardrail should be triggered for output just over limit")
	}
}

func TestCheckLogsOutputSize_CustomLimit(t *testing.T) {
	// Test with custom token limit
	customLimit := 100
	// 100 tokens = 400 characters, so use 500 to exceed
	largeOutput := strings.Repeat("x", 500)

	result, triggered := checkLogsOutputSize(largeOutput, customLimit)

	if !triggered {
		t.Error("Guardrail should be triggered when exceeding custom limit")
	}

	var guardrail MCPLogsGuardrailResponse
	if err := json.Unmarshal([]byte(result), &guardrail); err != nil {
		t.Errorf("Guardrail response should be valid JSON: %v", err)
	}

	if guardrail.OutputSizeLimit != customLimit {
		t.Errorf("Guardrail should report custom limit: expected %d, got %d", customLimit, guardrail.OutputSizeLimit)
	}
}

func TestGetLogsDataSchema(t *testing.T) {
	schema := getLogsDataSchema()

	// Verify basic schema structure
	if schema.Type != "object" {
		t.Errorf("Schema type should be 'object', got '%s'", schema.Type)
	}

	if schema.Description == "" {
		t.Error("Schema should have a description")
	}

	// Verify expected fields are present
	expectedFields := []string{
		"summary",
		"runs",
		"tool_usage",
		"errors_and_warnings",
		"missing_tools",
		"mcp_failures",
		"access_log",
		"firewall_log",
		"continuation",
		"logs_location",
	}

	for _, field := range expectedFields {
		if _, ok := schema.Fields[field]; !ok {
			t.Errorf("Schema should have field '%s'", field)
		}
	}

	// Verify each field has type and description
	for fieldName, field := range schema.Fields {
		if field.Type == "" {
			t.Errorf("Field '%s' should have a type", fieldName)
		}
		if field.Description == "" {
			t.Errorf("Field '%s' should have a description", fieldName)
		}
	}
}

func TestFormatGuardrailMessage(t *testing.T) {
	guardrail := MCPLogsGuardrailResponse{
		Message:         "Test message",
		OutputTokens:    15000,
		OutputSizeLimit: DefaultMaxMCPLogsOutputTokens,
		Schema:          getLogsDataSchema(),
	}

	message := formatGuardrailMessage(guardrail)

	// Verify message contains key components
	if !strings.Contains(message, "Test message") {
		t.Error("Formatted message should contain the original message")
	}

	if !strings.Contains(message, "Output Schema") {
		t.Error("Formatted message should contain schema section")
	}

	// Verify it mentions some fields
	if !strings.Contains(message, "summary") {
		t.Error("Formatted message should mention 'summary' field")
	}
}

func TestGuardrailResponseJSON(t *testing.T) {
	// Create a large output to trigger guardrail
	// Default limit is 12000 tokens = 48000 characters
	largeOutput := strings.Repeat("x", 96000)

	result, triggered := checkLogsOutputSize(largeOutput, 0)

	if !triggered {
		t.Fatal("Guardrail should be triggered")
	}

	// Parse the JSON response
	var guardrail MCPLogsGuardrailResponse
	if err := json.Unmarshal([]byte(result), &guardrail); err != nil {
		t.Fatalf("Should return valid JSON: %v", err)
	}

	// Verify the JSON structure is complete and valid
	if guardrail.Message == "" {
		t.Error("JSON should have message field")
	}

	if guardrail.OutputTokens == 0 {
		t.Error("JSON should have output_tokens field")
	}

	if guardrail.OutputSizeLimit == 0 {
		t.Error("JSON should have output_size_limit field")
	}

	if guardrail.Schema.Type == "" {
		t.Error("JSON should have schema.type field")
	}

	if len(guardrail.Schema.Fields) == 0 {
		t.Error("JSON should have schema.fields")
	}
}

func TestDefaultMaxTokensConstant(t *testing.T) {
	// Verify the constant is set to expected value (12000 tokens)
	expected := 12000
	if DefaultMaxMCPLogsOutputTokens != expected {
		t.Errorf("DefaultMaxMCPLogsOutputTokens should be %d tokens, got %d", expected, DefaultMaxMCPLogsOutputTokens)
	}
}

func TestEstimateTokens(t *testing.T) {
	// Test token estimation
	tests := []struct {
		text           string
		expectedTokens int
	}{
		{"", 0},
		{"x", 0},                        // 1 char / 4 = 0
		{"xxxx", 1},                     // 4 chars / 4 = 1
		{"xxxxxxxx", 2},                 // 8 chars / 4 = 2
		{strings.Repeat("x", 400), 100}, // 400 chars / 4 = 100
	}

	for _, tt := range tests {
		got := estimateTokens(tt.text)
		if got != tt.expectedTokens {
			t.Errorf("estimateTokens(%q) = %d, want %d", tt.text, got, tt.expectedTokens)
		}
	}
}
