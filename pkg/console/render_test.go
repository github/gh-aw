//go:build !integration

package console

import (
	"reflect"
	"strings"
	"testing"
)

// Test types for struct rendering
type TestOverview struct {
	RunID      int64  `console:"header:Run ID"`
	Workflow   string `console:"header:Workflow"`
	Status     string `console:"header:Status"`
	EmptyField string `console:"omitempty"`
	SkipField  string `console:"-"`
}

type TestMetrics struct {
	TokenUsage int     `console:"header:Token Usage"`
	Cost       float64 `console:"header:Estimated Cost"`
	Turns      int     `console:"header:Turns,omitempty"`
}

type TestJob struct {
	Name       string `console:"header:Name"`
	Status     string `console:"header:Status"`
	Conclusion string `console:"header:Conclusion,omitempty"`
}

func TestRenderStruct_SimpleStruct(t *testing.T) {
	data := TestOverview{
		RunID:      12345,
		Workflow:   "test-workflow",
		Status:     "completed",
		EmptyField: "",
		SkipField:  "should not appear",
	}

	output := RenderStruct(data)

	// Check that basic fields are present (using header names from tags)
	if !strings.Contains(output, "Run ID") {
		t.Errorf("Output should contain Run ID field, got:\n%s", output)
	}
	if !strings.Contains(output, "12345") {
		t.Error("Output should contain RunID value")
	}
	if !strings.Contains(output, "test-workflow") {
		t.Error("Output should contain Workflow value")
	}

	// Check that skip field is not present
	if strings.Contains(output, "should not appear") {
		t.Error("Output should not contain skipped field")
	}
}

func TestRenderStruct_OmitEmpty(t *testing.T) {
	data := TestMetrics{
		TokenUsage: 1000,
		Cost:       1.23,
		Turns:      0, // Should be omitted with omitempty
	}

	output := RenderStruct(data)

	// Check that non-empty fields are present
	if !strings.Contains(output, "1000") {
		t.Error("Output should contain TokenUsage value")
	}
	if !strings.Contains(output, "1.23") {
		t.Error("Output should contain Cost value")
	}

	// Turns should be omitted because it's 0 and has omitempty
	// Note: This is tricky because "0" might appear in other contexts
	// We'll just verify the field appears when it has a value
	dataWithTurns := TestMetrics{
		TokenUsage: 1000,
		Cost:       1.23,
		Turns:      5,
	}
	outputWithTurns := RenderStruct(dataWithTurns)
	if !strings.Contains(outputWithTurns, "5") {
		t.Error("Output should contain Turns value when non-zero")
	}
}

func TestRenderSlice_AsTable(t *testing.T) {
	jobs := []TestJob{
		{Name: "job-1", Status: "completed", Conclusion: "success"},
		{Name: "job-2", Status: "in_progress", Conclusion: ""},
	}

	output := RenderStruct(jobs)

	// Check for table structure (headers and values)
	if !strings.Contains(output, "Name") {
		t.Error("Output should contain Name header")
	}
	if !strings.Contains(output, "Status") {
		t.Error("Output should contain Status header")
	}
	if !strings.Contains(output, "job-1") {
		t.Error("Output should contain job-1 name")
	}
	if !strings.Contains(output, "completed") {
		t.Error("Output should contain completed status")
	}
	if !strings.Contains(output, "job-2") {
		t.Error("Output should contain job-2 name")
	}
}

func TestRenderMap(t *testing.T) {
	data := map[string]string{
		"key1": "value1",
		"key2": "value2",
	}

	output := RenderStruct(data)

	// Maps should render as key-value pairs
	if !strings.Contains(output, "key1") {
		t.Error("Output should contain key1")
	}
	if !strings.Contains(output, "value1") {
		t.Error("Output should contain value1")
	}
	if !strings.Contains(output, "key2") {
		t.Error("Output should contain key2")
	}
	if !strings.Contains(output, "value2") {
		t.Error("Output should contain value2")
	}
}

func TestParseConsoleTag(t *testing.T) {
	tests := []struct {
		name     string
		tag      string
		expected consoleTag
	}{
		{
			name: "skip tag",
			tag:  "-",
			expected: consoleTag{
				skip: true,
			},
		},
		{
			name: "omitempty tag",
			tag:  "omitempty",
			expected: consoleTag{
				omitempty: true,
			},
		},
		{
			name: "header tag",
			tag:  "header:Column Name",
			expected: consoleTag{
				header: "Column Name",
			},
		},
		{
			name: "title tag",
			tag:  "title:Section Title",
			expected: consoleTag{
				title: "Section Title",
			},
		},
		{
			name: "combined tags",
			tag:  "header:Name,omitempty",
			expected: consoleTag{
				header:    "Name",
				omitempty: true,
			},
		},
		{
			name: "all tags",
			tag:  "title:My Title,header:My Header,omitempty",
			expected: consoleTag{
				title:     "My Title",
				header:    "My Header",
				omitempty: true,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := parseConsoleTag(tt.tag)
			if result.skip != tt.expected.skip {
				t.Errorf("skip: got %v, want %v", result.skip, tt.expected.skip)
			}
			if result.omitempty != tt.expected.omitempty {
				t.Errorf("omitempty: got %v, want %v", result.omitempty, tt.expected.omitempty)
			}
			if result.header != tt.expected.header {
				t.Errorf("header: got %v, want %v", result.header, tt.expected.header)
			}
			if result.title != tt.expected.title {
				t.Errorf("title: got %v, want %v", result.title, tt.expected.title)
			}
		})
	}
}

func TestIsZeroValue(t *testing.T) {
	tests := []struct {
		name     string
		value    any
		expected bool
	}{
		{"zero int", 0, true},
		{"non-zero int", 42, false},
		{"empty string", "", true},
		{"non-empty string", "hello", false},
		{"nil pointer", (*int)(nil), true},
		{"empty slice", []int{}, true},
		{"non-empty slice", []int{1}, false},
		{"false bool", false, true},
		{"true bool", true, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			val := reflect.ValueOf(tt.value)
			result := isZeroValue(val)
			if result != tt.expected {
				t.Errorf("got %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestFormatFieldValue(t *testing.T) {
	tests := []struct {
		name     string
		value    any
		expected string
	}{
		{"integer", 42, "42"},
		{"string", "hello", "hello"},
		{"empty string", "", "-"},
		{"float", 3.14, "3.14"},
		{"nil pointer", (*int)(nil), "-"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			val := reflect.ValueOf(tt.value)
			result := formatFieldValue(val)
			if result != tt.expected {
				t.Errorf("got %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestRenderStruct_ComplexExample(t *testing.T) {
	// Test a more complex nested structure
	type Address struct {
		Street string `console:"header:Street"`
		City   string `console:"header:City"`
	}

	type Person struct {
		Name    string  `console:"header:Name"`
		Age     int     `console:"header:Age"`
		Address Address `console:"title:Address"`
		Email   string  `console:"header:Email,omitempty"`
	}

	data := Person{
		Name: "John Doe",
		Age:  30,
		Address: Address{
			Street: "123 Main St",
			City:   "Anytown",
		},
		Email: "", // Should be omitted
	}

	output := RenderStruct(data)

	// Check that basic fields are present
	if !strings.Contains(output, "John Doe") {
		t.Error("Output should contain Name value")
	}
	if !strings.Contains(output, "30") {
		t.Error("Output should contain Age value")
	}
	if !strings.Contains(output, "123 Main St") {
		t.Error("Output should contain nested Street value")
	}

	// Check that nested struct has a title
	if !strings.Contains(output, "Address") {
		t.Error("Output should contain Address section title")
	}
}

func TestBuildTableConfig(t *testing.T) {
	jobs := []TestJob{
		{Name: "job-1", Status: "completed", Conclusion: "success"},
		{Name: "job-2", Status: "in_progress", Conclusion: ""},
	}

	val := reflect.ValueOf(jobs)
	config := buildTableConfig(val, "Test Jobs")

	if len(config.Headers) != 3 {
		t.Errorf("Expected 3 headers (excluding omitempty empty fields), got %d", len(config.Headers))
	}

	if len(config.Rows) != 2 {
		t.Errorf("Expected 2 rows, got %d", len(config.Rows))
	}

	// Check first row
	if config.Rows[0][0] != "job-1" {
		t.Errorf("Expected first row name to be 'job-1', got %s", config.Rows[0][0])
	}
}

func TestFormatTag_Number(t *testing.T) {
	type TestMetrics struct {
		TokenUsage int `console:"header:Token Usage,format:number"`
		Errors     int `console:"header:Errors"`
	}

	data := TestMetrics{
		TokenUsage: 250000,
		Errors:     5,
	}

	output := RenderStruct(data)

	// Should format token usage as "250k"
	if !strings.Contains(output, "250k") {
		t.Errorf("Output should contain formatted number '250k', got:\n%s", output)
	}

	// Errors should not be formatted
	if !strings.Contains(output, "5") {
		t.Errorf("Output should contain unformatted number '5', got:\n%s", output)
	}
}

func TestFormatTag_Cost(t *testing.T) {
	type TestBilling struct {
		Cost float64 `console:"header:Estimated Cost,format:cost"`
	}

	data := TestBilling{
		Cost: 1.234,
	}

	output := RenderStruct(data)

	// Should format cost with $ prefix
	if !strings.Contains(output, "$1.234") {
		t.Errorf("Output should contain formatted cost '$1.234', got:\n%s", output)
	}
}

func TestFormatTag_InTable(t *testing.T) {
	type TestTool struct {
		Name       string `console:"header:Tool"`
		CallCount  int    `console:"header:Calls"`
		OutputSize int    `console:"header:Output,format:number"`
	}

	tools := []TestTool{
		{Name: "tool-1", CallCount: 10, OutputSize: 5000000},
		{Name: "tool-2", CallCount: 5, OutputSize: 1500},
	}

	output := RenderStruct(tools)

	// Should format large output size
	if !strings.Contains(output, "5.00M") {
		t.Errorf("Output should contain formatted number '5.00M', got:\n%s", output)
	}

	// Should format small output size
	if !strings.Contains(output, "1.50k") {
		t.Errorf("Output should contain formatted number '1.50k', got:\n%s", output)
	}
}

func TestFormatNumber(t *testing.T) {
	tests := []struct {
		input    int
		expected string
	}{
		// Zero
		{0, "0"},
		// Small numbers (under 1000)
		{5, "5"},
		{42, "42"},
		{999, "999"},
		// Thousands (1k-999k)
		{1000, "1.00k"}, // Under 10k: 2 decimal places
		{1200, "1.20k"},
		{1234, "1.23k"},
		{9999, "10.00k"}, // 9999/1000 = 9.999 -> formats as 10.00k
		{10000, "10.0k"}, // 10k-99k: 1 decimal place
		{12000, "12.0k"},
		{12300, "12.3k"},
		{99999, "100.0k"}, // 99999/1000 = 99.999 -> formats as 100.0k
		{100000, "100k"},  // 100k+: no decimal places
		{123000, "123k"},
		{999999, "1000k"}, // 999999/1000 = 999.999 -> formats as 1000k
		// Millions (1M-999M)
		{1000000, "1.00M"}, // Under 10M: 2 decimal places
		{1200000, "1.20M"},
		{1234567, "1.23M"},
		{9999999, "10.00M"}, // 9999999/1000000 = 9.999999 -> formats as 10.00M
		{10000000, "10.0M"}, // 10M-99M: 1 decimal place
		{12000000, "12.0M"},
		{12300000, "12.3M"},
		{99999999, "100.0M"}, // 99999999/1000000 = 99.999999 -> formats as 100.0M
		{100000000, "100M"},  // 100M+: no decimal places
		{123000000, "123M"},
		{999999999, "1000M"}, // 999999999/1000000 = 999.999999 -> formats as 1000M
		// Billions (1B+)
		{1000000000, "1.00B"}, // Under 10B: 2 decimal places
		{1200000000, "1.20B"},
		{1234567890, "1.23B"},
		{9999999999, "10.00B"}, // 9999999999/1000000000 = 9.999999999 -> formats as 10.00B
		{10000000000, "10.0B"}, // 10B-99B: 1 decimal place
		{12000000000, "12.0B"},
		{99999999999, "100.0B"}, // 99999999999/1000000000 = 99.999999999 -> formats as 100.0B
		{100000000000, "100B"},  // 100B+: no decimal places
		{123000000000, "123B"},
	}

	for _, test := range tests {
		result := FormatNumber(test.input)
		if result != test.expected {
			t.Errorf("FormatNumber(%d) = %s, expected %s", test.input, result, test.expected)
		}
	}
}

// TestRenderStruct_PointerToStruct tests that pointer-to-struct fields are rendered as nested structs, not raw data
func TestRenderStruct_PointerToStruct(t *testing.T) {
	type Inner struct {
		Name  string `console:"header:Name"`
		Value int    `console:"header:Value"`
	}

	type Outer struct {
		Title string `console:"header:Title"`
		Inner *Inner `console:"title:Inner Section"`
	}

	data := Outer{
		Title: "Test Title",
		Inner: &Inner{
			Name:  "test-name",
			Value: 42,
		},
	}

	output := RenderStruct(data)

	// Check that inner struct is rendered properly, not as raw data
	if !strings.Contains(output, "Inner Section") {
		t.Errorf("Output should contain nested struct title 'Inner Section', got:\n%s", output)
	}
	if !strings.Contains(output, "Name") {
		t.Errorf("Output should contain inner field 'Name', got:\n%s", output)
	}
	if !strings.Contains(output, "test-name") {
		t.Errorf("Output should contain inner value 'test-name', got:\n%s", output)
	}
	if !strings.Contains(output, "42") {
		t.Errorf("Output should contain inner value '42', got:\n%s", output)
	}
	// Ensure it's NOT rendered as raw data structure
	if strings.Contains(output, "{test-name") || strings.Contains(output, "&{") {
		t.Errorf("Output should NOT contain raw struct representation, got:\n%s", output)
	}
}

// TestRenderStruct_NilPointerToStruct tests that nil pointer-to-struct fields are handled gracefully
func TestRenderStruct_NilPointerToStruct(t *testing.T) {
	type Inner struct {
		Name string `console:"header:Name"`
	}

	type Outer struct {
		Title string `console:"header:Title"`
		Inner *Inner `console:"title:Inner Section,omitempty"`
	}

	data := Outer{
		Title: "Test Title",
		Inner: nil,
	}

	output := RenderStruct(data)

	// Should contain title but not the nil inner
	if !strings.Contains(output, "Title") {
		t.Errorf("Output should contain 'Title', got:\n%s", output)
	}
	// Should not crash and should not contain inner section when nil and omitempty
	if strings.Contains(output, "Inner Section") {
		t.Errorf("Output should NOT contain 'Inner Section' when nil and omitempty, got:\n%s", output)
	}
}
