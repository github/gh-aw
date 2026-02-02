//go:build !integration

package parser

import (
	"os"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/testutil"
)

func TestParseImportDirective(t *testing.T) {
	tests := []struct {
		name         string
		input        string
		wantMatch    bool
		wantPath     string
		wantOptional bool
		wantLegacy   bool
	}{
		// New syntax tests
		{
			name:         "new syntax - basic import",
			input:        "{{#import: shared/tools.md}}",
			wantMatch:    true,
			wantPath:     "shared/tools.md",
			wantOptional: false,
			wantLegacy:   false,
		},
		{
			name:         "new syntax - optional import",
			input:        "{{#import?: shared/tools.md}}",
			wantMatch:    true,
			wantPath:     "shared/tools.md",
			wantOptional: true,
			wantLegacy:   false,
		},
		{
			name:         "new syntax - with extra spaces",
			input:        "{{#import:   shared/tools.md  }}",
			wantMatch:    true,
			wantPath:     "shared/tools.md",
			wantOptional: false,
			wantLegacy:   false,
		},
		{
			name:         "new syntax - with section",
			input:        "{{#import: shared/tools.md#Security}}",
			wantMatch:    true,
			wantPath:     "shared/tools.md#Security",
			wantOptional: false,
			wantLegacy:   false,
		},
		{
			name:         "new syntax - optional with section",
			input:        "{{#import?: shared/tools.md#Security}}",
			wantMatch:    true,
			wantPath:     "shared/tools.md#Security",
			wantOptional: true,
			wantLegacy:   false,
		},
		// New syntax without colon tests
		{
			name:         "new syntax - basic import without colon",
			input:        "{{#import shared/tools.md}}",
			wantMatch:    true,
			wantPath:     "shared/tools.md",
			wantOptional: false,
			wantLegacy:   false,
		},
		{
			name:         "new syntax - optional import without colon",
			input:        "{{#import? shared/tools.md}}",
			wantMatch:    true,
			wantPath:     "shared/tools.md",
			wantOptional: true,
			wantLegacy:   false,
		},
		{
			name:         "new syntax - with section without colon",
			input:        "{{#import shared/tools.md#Security}}",
			wantMatch:    true,
			wantPath:     "shared/tools.md#Security",
			wantOptional: false,
			wantLegacy:   false,
		},
		// Legacy syntax tests
		{
			name:         "legacy - @include basic",
			input:        "@include shared/tools.md",
			wantMatch:    true,
			wantPath:     "shared/tools.md",
			wantOptional: false,
			wantLegacy:   true,
		},
		{
			name:         "legacy - @include optional",
			input:        "@include? shared/tools.md",
			wantMatch:    true,
			wantPath:     "shared/tools.md",
			wantOptional: true,
			wantLegacy:   true,
		},
		{
			name:         "legacy - @import basic",
			input:        "@import shared/config.md",
			wantMatch:    true,
			wantPath:     "shared/config.md",
			wantOptional: false,
			wantLegacy:   true,
		},
		{
			name:         "legacy - @import optional",
			input:        "@import? shared/config.md",
			wantMatch:    true,
			wantPath:     "shared/config.md",
			wantOptional: true,
			wantLegacy:   true,
		},
		{
			name:         "legacy - with section",
			input:        "@include shared/tools.md#Section",
			wantMatch:    true,
			wantPath:     "shared/tools.md#Section",
			wantOptional: false,
			wantLegacy:   true,
		},
		// Non-matching tests
		{
			name:      "no match - regular text",
			input:     "This is regular text",
			wantMatch: false,
		},
		{
			name:      "no match - legacy without path",
			input:     "@include",
			wantMatch: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ParseImportDirective(tt.input)

			if tt.wantMatch {
				if result == nil {
					t.Errorf("ParseImportDirective() returned nil, want match")
					return
				}

				if result.Path != tt.wantPath {
					t.Errorf("ParseImportDirective() Path = %q, want %q", result.Path, tt.wantPath)
				}

				if result.IsOptional != tt.wantOptional {
					t.Errorf("ParseImportDirective() IsOptional = %v, want %v", result.IsOptional, tt.wantOptional)
				}

				if result.IsLegacy != tt.wantLegacy {
					t.Errorf("ParseImportDirective() IsLegacy = %v, want %v", result.IsLegacy, tt.wantLegacy)
				}

				if result.Original != strings.TrimSpace(tt.input) {
					t.Errorf("ParseImportDirective() Original = %q, want %q", result.Original, strings.TrimSpace(tt.input))
				}
			} else {
				if result != nil {
					t.Errorf("ParseImportDirective() returned %+v, want nil", result)
				}
			}
		})
	}
}

func TestProcessIncludesWithNewSyntax(t *testing.T) {
	// Create temporary test files
	tempDir := testutil.TempDir(t, "test-*")

	// Create test file with markdown content
	testFile := tempDir + "/test.md"
	testContent := `---
tools:
  bash:
    allowed: ["ls", "cat"]
---

# Test Content
This is a test file content.
`
	if err := os.WriteFile(testFile, []byte(testContent), 0644); err != nil {
		t.Fatalf("Failed to write test file: %v", err)
	}

	tests := []struct {
		name     string
		content  string
		expected string
		wantErr  bool
	}{
		{
			name:     "new syntax - basic import with colon",
			content:  "{{#import: test.md}}\n# After import",
			expected: "# Test Content\nThis is a test file content.\n# After import\n",
			wantErr:  false,
		},
		{
			name:     "new syntax - basic import without colon",
			content:  "{{#import test.md}}\n# After import",
			expected: "# Test Content\nThis is a test file content.\n# After import\n",
			wantErr:  false,
		},
		{
			name:     "new syntax - optional import with colon (file exists)",
			content:  "{{#import?: test.md}}\n# After import",
			expected: "# Test Content\nThis is a test file content.\n# After import\n",
			wantErr:  false,
		},
		{
			name:     "new syntax - optional import without colon (file exists)",
			content:  "{{#import? test.md}}\n# After import",
			expected: "# Test Content\nThis is a test file content.\n# After import\n",
			wantErr:  false,
		},
		{
			name:     "new syntax - optional import (file missing)",
			content:  "{{#import?: nonexistent.md}}\n# After import",
			expected: "# After import\n",
			wantErr:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := ProcessIncludes(tt.content, tempDir, false)

			if tt.wantErr {
				if err == nil {
					t.Errorf("ProcessIncludes() expected error, got nil")
				}
				return
			}

			if err != nil {
				t.Errorf("ProcessIncludes() unexpected error = %v", err)
				return
			}

			if result != tt.expected {
				t.Errorf("ProcessIncludes() result = %q, want %q", result, tt.expected)
			}
		})
	}
}
