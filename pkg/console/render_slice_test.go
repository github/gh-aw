//go:build !integration

package console

import (
	"reflect"
	"strings"
	"testing"
)

// Test types for slice rendering
type SliceTestStruct struct {
	ID   int    `console:"header:ID"`
	Name string `console:"header:Name"`
}

func TestRenderSlice_EmptySlice(t *testing.T) {
	var output strings.Builder
	emptySlice := []int{}
	val := reflect.ValueOf(emptySlice)

	renderSlice(val, "Empty List", &output, 0)

	// Empty slice should produce no output
	if output.String() != "" {
		t.Errorf("Empty slice should produce no output, got: %q", output.String())
	}
}

func TestRenderSlice_NoTitle(t *testing.T) {
	var output strings.Builder
	data := []int{1, 2, 3}
	val := reflect.ValueOf(data)

	renderSlice(val, "", &output, 0)

	result := output.String()
	// Should not have title header
	if strings.Contains(result, "#") {
		t.Errorf("Output with no title should not contain header markers, got: %q", result)
	}
	// Should have list items
	if !strings.Contains(result, "•") {
		t.Error("Output should contain list bullet points")
	}
}

func TestRenderSlice_WithTitleDepth0(t *testing.T) {
	var output strings.Builder
	data := []string{"a", "b", "c"}
	val := reflect.ValueOf(data)

	renderSlice(val, "My List", &output, 0)

	result := output.String()
	// Depth 0 should use single # header
	if !strings.Contains(result, "# My List") {
		t.Errorf("Output should contain '# My List' header, got: %q", result)
	}
	// Should not have ## or ###
	if strings.Contains(result, "##") {
		t.Errorf("Depth 0 should only use single #, got: %q", result)
	}
}

func TestRenderSlice_WithTitleDepth1(t *testing.T) {
	var output strings.Builder
	data := []string{"x", "y"}
	val := reflect.ValueOf(data)

	renderSlice(val, "Nested List", &output, 1)

	result := output.String()
	// Depth 1 should use ## header (depth + 1 = 2 hashes)
	if !strings.Contains(result, "## Nested List") {
		t.Errorf("Output should contain '## Nested List' header, got: %q", result)
	}
}

func TestRenderSlice_WithTitleDepth2(t *testing.T) {
	var output strings.Builder
	data := []int{10, 20}
	val := reflect.ValueOf(data)

	renderSlice(val, "Deep List", &output, 2)

	result := output.String()
	// Depth 2 should use ### header (depth + 1 = 3 hashes)
	if !strings.Contains(result, "### Deep List") {
		t.Errorf("Output should contain '### Deep List' header, got: %q", result)
	}
}

func TestRenderSlice_SimpleTypesAsList(t *testing.T) {
	tests := []struct {
		name     string
		data     any
		expected []string
	}{
		{
			name:     "integers",
			data:     []int{1, 2, 3},
			expected: []string{"• 1", "• 2", "• 3"},
		},
		{
			name:     "strings",
			data:     []string{"apple", "banana", "cherry"},
			expected: []string{"• apple", "• banana", "• cherry"},
		},
		{
			name:     "floats",
			data:     []float64{1.5, 2.7, 3.9},
			expected: []string{"• 1.5", "• 2.7", "• 3.9"},
		},
		{
			name:     "booleans",
			data:     []bool{true, true, true}, // Note: false renders as "false"
			expected: []string{"• true", "• true", "• true"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var output strings.Builder
			val := reflect.ValueOf(tt.data)

			renderSlice(val, "", &output, 0)

			result := output.String()
			for _, expected := range tt.expected {
				if !strings.Contains(result, expected) {
					t.Errorf("Output should contain %q, got: %q", expected, result)
				}
			}
		})
	}
}

func TestRenderSlice_StructsAsTable(t *testing.T) {
	var output strings.Builder
	data := []SliceTestStruct{
		{ID: 1, Name: "first"},
		{ID: 2, Name: "second"},
	}
	val := reflect.ValueOf(data)

	renderSlice(val, "Test Table", &output, 0)

	result := output.String()

	// Should have title
	if !strings.Contains(result, "# Test Table") {
		t.Errorf("Output should contain title, got: %q", result)
	}

	// Should render as table (check for table headers from console tags)
	if !strings.Contains(result, "ID") {
		t.Error("Output should contain ID column header")
	}
	if !strings.Contains(result, "Name") {
		t.Error("Output should contain Name column header")
	}
	if !strings.Contains(result, "first") {
		t.Error("Output should contain data from first row")
	}
	if !strings.Contains(result, "second") {
		t.Error("Output should contain data from second row")
	}
}

func TestRenderSlice_PointerToStructsAsTable(t *testing.T) {
	var output strings.Builder
	data := []*SliceTestStruct{
		{ID: 10, Name: "alpha"},
		{ID: 20, Name: "beta"},
	}
	val := reflect.ValueOf(data)

	renderSlice(val, "Pointer Table", &output, 0)

	result := output.String()

	// Should still render as table even though elements are pointers
	if !strings.Contains(result, "# Pointer Table") {
		t.Errorf("Output should contain title, got: %q", result)
	}
	if !strings.Contains(result, "ID") {
		t.Error("Output should contain ID column header")
	}
	if !strings.Contains(result, "alpha") {
		t.Error("Output should contain data from pointer elements")
	}
}

func TestRenderSlice_DoublePointerToStructsAsTable(t *testing.T) {
	var output strings.Builder

	// Create double pointer elements
	s1 := SliceTestStruct{ID: 100, Name: "double"}
	ps1 := &s1
	pps1 := &ps1
	s2 := SliceTestStruct{ID: 200, Name: "pointer"}
	ps2 := &s2
	pps2 := &ps2

	// Create slice with reflect
	sliceType := reflect.TypeOf((**SliceTestStruct)(nil))
	val := reflect.MakeSlice(reflect.SliceOf(sliceType), 0, 2)
	val = reflect.Append(val, reflect.ValueOf(pps1))
	val = reflect.Append(val, reflect.ValueOf(pps2))

	renderSlice(val, "", &output, 0)

	result := output.String()

	// Should still detect as struct and render as table
	if !strings.Contains(result, "ID") {
		t.Error("Output should contain ID column header")
	}
	if !strings.Contains(result, "double") {
		t.Error("Output should contain data from double pointer elements")
	}
}

func TestRenderSlice_EmptyStructSliceAsTable(t *testing.T) {
	var output strings.Builder
	data := []SliceTestStruct{}
	val := reflect.ValueOf(data)

	renderSlice(val, "Empty Structs", &output, 0)

	result := output.String()

	// Empty slice should produce no output (early return)
	if result != "" {
		t.Errorf("Empty struct slice should produce no output, got: %q", result)
	}
}

func TestRenderSlice_SingleElementList(t *testing.T) {
	var output strings.Builder
	data := []string{"only one"}
	val := reflect.ValueOf(data)

	renderSlice(val, "Single Item", &output, 0)

	result := output.String()

	if !strings.Contains(result, "# Single Item") {
		t.Error("Output should contain title")
	}
	if !strings.Contains(result, "• only one") {
		t.Error("Output should contain single list item")
	}
}

func TestRenderSlice_MixedContentInList(t *testing.T) {
	var output strings.Builder
	// Test with various characters that might need formatting
	data := []string{
		"simple",
		"with spaces",
		"with\nnewline",
		"", // empty string
		"special-chars!@#",
	}
	val := reflect.ValueOf(data)

	renderSlice(val, "", &output, 0)

	result := output.String()

	// All items should be in the output
	if !strings.Contains(result, "• simple") {
		t.Error("Output should contain first item")
	}
	if !strings.Contains(result, "• with spaces") {
		t.Error("Output should contain item with spaces")
	}
	if !strings.Contains(result, "special-chars!@#") {
		t.Error("Output should contain item with special characters")
	}
}
