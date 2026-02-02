//go:build !integration

package parser

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/github/gh-aw/pkg/testutil"
)

func TestExpandIncludesForEngines(t *testing.T) {
	// Create temporary directory for test files
	tmpDir := testutil.TempDir(t, "test-*")

	// Create include file with engine specification
	includeContent := `---
engine: codex
tools:
  github:
    allowed: ["list_issues"]
---

# Include with Engine
`
	includeFile := filepath.Join(tmpDir, "include-engine.md")
	if err := os.WriteFile(includeFile, []byte(includeContent), 0644); err != nil {
		t.Fatal(err)
	}

	// Create main markdown content with include directive
	mainContent := `# Main Workflow

@include include-engine.md

Some content here.
`

	// Test engine expansion
	engines, err := ExpandIncludesForEngines(mainContent, tmpDir)
	if err != nil {
		t.Fatalf("Expected successful engine expansion, got error: %v", err)
	}

	// Should find one engine
	if len(engines) != 1 {
		t.Fatalf("Expected 1 engine, got %d", len(engines))
	}

	// Should extract "codex" engine
	if engines[0] != `"codex"` {
		t.Errorf("Expected engine 'codex', got %s", engines[0])
	}
}

func TestExpandIncludesForEnginesObjectFormat(t *testing.T) {
	// Create temporary directory for test files
	tmpDir := testutil.TempDir(t, "test-*")

	// Create include file with object-format engine specification
	includeContent := `---
engine:
  id: claude
  model: claude-3-5-sonnet-20241022
  max-turns: 5
tools:
  github:
    allowed: ["list_issues"]
---

# Include with Object Engine
`
	includeFile := filepath.Join(tmpDir, "include-object-engine.md")
	if err := os.WriteFile(includeFile, []byte(includeContent), 0644); err != nil {
		t.Fatal(err)
	}

	// Create main markdown content with include directive
	mainContent := `# Main Workflow

@include include-object-engine.md

Some content here.
`

	// Test engine expansion
	engines, err := ExpandIncludesForEngines(mainContent, tmpDir)
	if err != nil {
		t.Fatalf("Expected successful engine expansion, got error: %v", err)
	}

	// Should find one engine
	if len(engines) != 1 {
		t.Fatalf("Expected 1 engine, got %d", len(engines))
	}

	// Should extract engine object as JSON
	expectedFields := []string{`"id":"claude"`, `"model":"claude-3-5-sonnet-20241022"`, `"max-turns":5`}
	for _, field := range expectedFields {
		if !contains(engines[0], field) {
			t.Errorf("Expected engine JSON to contain %s, got %s", field, engines[0])
		}
	}
}

func TestExpandIncludesForEnginesNoEngine(t *testing.T) {
	// Create temporary directory for test files
	tmpDir := testutil.TempDir(t, "test-*")

	// Create include file without engine specification
	includeContent := `---
tools:
  github:
    allowed: ["list_issues"]
---

# Include without Engine
`
	includeFile := filepath.Join(tmpDir, "include-no-engine.md")
	if err := os.WriteFile(includeFile, []byte(includeContent), 0644); err != nil {
		t.Fatal(err)
	}

	// Create main markdown content with include directive
	mainContent := `# Main Workflow

@include include-no-engine.md

Some content here.
`

	// Test engine expansion
	engines, err := ExpandIncludesForEngines(mainContent, tmpDir)
	if err != nil {
		t.Fatalf("Expected successful engine expansion, got error: %v", err)
	}

	// Should find no engines
	if len(engines) != 0 {
		t.Errorf("Expected 0 engines, got %d: %v", len(engines), engines)
	}
}

func TestExpandIncludesForEnginesMultipleIncludes(t *testing.T) {
	// Create temporary directory for test files
	tmpDir := testutil.TempDir(t, "test-*")

	// Create first include file with engine
	include1Content := `---
engine: claude
tools:
  github:
    allowed: ["list_issues"]
---

# First Include
`
	include1File := filepath.Join(tmpDir, "include1.md")
	if err := os.WriteFile(include1File, []byte(include1Content), 0644); err != nil {
		t.Fatal(err)
	}

	// Create second include file with engine
	include2Content := `---
engine: codex
tools:
  claude:
    allowed: ["Read", "Write"]
---

# Second Include
`
	include2File := filepath.Join(tmpDir, "include2.md")
	if err := os.WriteFile(include2File, []byte(include2Content), 0644); err != nil {
		t.Fatal(err)
	}

	// Create main markdown content with multiple include directives
	mainContent := `# Main Workflow

@include include1.md

Some content here.

@include include2.md

More content.
`

	// Test engine expansion
	engines, err := ExpandIncludesForEngines(mainContent, tmpDir)
	if err != nil {
		t.Fatalf("Expected successful engine expansion, got error: %v", err)
	}

	// Should find two engines
	if len(engines) != 2 {
		t.Fatalf("Expected 2 engines, got %d: %v", len(engines), engines)
	}

	// Should extract both engines
	if engines[0] != `"claude"` {
		t.Errorf("Expected first engine 'claude', got %s", engines[0])
	}
	if engines[1] != `"codex"` {
		t.Errorf("Expected second engine 'codex', got %s", engines[1])
	}
}

func TestExpandIncludesForEnginesOptionalMissing(t *testing.T) {
	// Create temporary directory for test files
	tmpDir := testutil.TempDir(t, "test-*")

	// Create main markdown content with optional include directive to non-existent file
	mainContent := `# Main Workflow

@include? missing-file.md

Some content here.
`

	// Test engine expansion - should not fail for optional includes
	engines, err := ExpandIncludesForEngines(mainContent, tmpDir)
	if err != nil {
		t.Fatalf("Expected successful engine expansion with optional missing include, got error: %v", err)
	}

	// Should find no engines
	if len(engines) != 0 {
		t.Errorf("Expected 0 engines, got %d: %v", len(engines), engines)
	}
}

func TestExtractEngineFromContent(t *testing.T) {
	tests := []struct {
		name     string
		content  string
		expected string
	}{
		{
			name: "string engine",
			content: `---
engine: claude
---
# Test
`,
			expected: `"claude"`,
		},
		{
			name: "object engine",
			content: `---
engine:
  id: codex
  model: gpt-4
---
# Test
`,
			expected: `{"id":"codex","model":"gpt-4"}`,
		},
		{
			name: "no engine",
			content: `---
tools:
  github: {}
---
# Test
`,
			expected: "",
		},
		{
			name: "no frontmatter",
			content: `# Test

Just markdown content.
`,
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := extractEngineFromContent(tt.content)
			if err != nil {
				t.Fatalf("Unexpected error: %v", err)
			}
			if result != tt.expected {
				t.Errorf("Expected %q, got %q", tt.expected, result)
			}
		})
	}
}

func TestExpandIncludesForEnginesWithCommand(t *testing.T) {
	// Create temporary directory for test files
	tmpDir := testutil.TempDir(t, "test-*")

	// Create include file with engine command specification
	includeContent := `---
engine:
  id: copilot
  command: /custom/path/to/copilot
  version: "1.0.0"
tools:
  github:
    allowed: ["list_issues"]
---

# Include with Custom Command
`
	includeFile := filepath.Join(tmpDir, "include-command.md")
	if err := os.WriteFile(includeFile, []byte(includeContent), 0644); err != nil {
		t.Fatal(err)
	}

	// Create main markdown content with include directive
	mainContent := `# Main Workflow

@include include-command.md

Some content here.
`

	// Test engine expansion
	engines, err := ExpandIncludesForEngines(mainContent, tmpDir)
	if err != nil {
		t.Fatalf("Expected successful engine expansion, got error: %v", err)
	}

	// Should find one engine
	if len(engines) != 1 {
		t.Fatalf("Expected 1 engine, got %d", len(engines))
	}

	// Should extract engine object as JSON with command field
	expectedFields := []string{`"id":"copilot"`, `"command":"/custom/path/to/copilot"`, `"version":"1.0.0"`}
	for _, field := range expectedFields {
		if !contains(engines[0], field) {
			t.Errorf("Expected engine JSON to contain %s, got %s", field, engines[0])
		}
	}
}
