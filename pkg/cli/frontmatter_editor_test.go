//go:build !integration

package cli

import (
	"strings"
	"testing"
)

func TestRemoveFieldFromOnTrigger(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		content     string
		fieldName   string
		shouldMatch string // We'll check if the result contains this string instead of exact match
		expectError bool
	}{
		{
			name: "remove stop-after from on trigger",
			content: `---
on:
  issues:
    types: [opened]
  stop-after: "+48h"
permissions:
  contents: read
---

# Test Workflow

This is a test workflow.`,
			fieldName:   "stop-after",
			shouldMatch: "# Test Workflow",
			expectError: false,
		},
		{
			name: "no stop-after field to remove",
			content: `---
on:
  issues:
    types: [opened]
permissions:
  contents: read
---

# Test Workflow`,
			fieldName:   "stop-after",
			shouldMatch: "# Test Workflow",
			expectError: false,
		},
		{
			name: "on field is a string not a map",
			content: `---
on: push
permissions:
  contents: read
---

# Test Workflow`,
			fieldName:   "stop-after",
			shouldMatch: "on: push",
			expectError: false,
		},
		{
			name: "no on field at all",
			content: `---
permissions:
  contents: read
---

# Test Workflow`,
			fieldName:   "stop-after",
			shouldMatch: "# Test Workflow",
			expectError: false,
		},
		{
			name: "remove reaction field from on trigger",
			content: `---
on:
  issues:
    types: [opened]
  reaction: "+1"
  stop-after: "+48h"
permissions:
  contents: read
---

# Test Workflow`,
			fieldName:   "stop-after",
			shouldMatch: "reaction:",
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result, err := RemoveFieldFromOnTrigger(tt.content, tt.fieldName)

			if tt.expectError && err == nil {
				t.Errorf("Expected error but got none")
			}
			if !tt.expectError && err != nil {
				t.Errorf("Unexpected error: %v", err)
			}

			// Check that result doesn't contain the field to be removed
			if strings.Contains(result, tt.fieldName+":") && tt.fieldName == "stop-after" {
				t.Errorf("Result still contains '%s' field:\n%s", tt.fieldName, result)
			}

			// Check that expected content is present
			if !strings.Contains(result, tt.shouldMatch) {
				t.Errorf("Result doesn't contain expected string '%s':\n%s", tt.shouldMatch, result)
			}
		})
	}
}

func TestSetFieldInOnTrigger(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		content     string
		fieldName   string
		fieldValue  string
		shouldMatch string
		expectError bool
	}{
		{
			name: "set stop-after in existing on trigger",
			content: `---
on:
  issues:
    types: [opened]
permissions:
  contents: read
---

# Test Workflow`,
			fieldName:   "stop-after",
			fieldValue:  "+48h",
			shouldMatch: "stop-after:",
			expectError: false,
		},
		{
			name: "set stop-after with no on field",
			content: `---
permissions:
  contents: read
---

# Test Workflow`,
			fieldName:   "stop-after",
			fieldValue:  "+72h",
			shouldMatch: "stop-after:",
			expectError: false,
		},
		{
			name: "override existing stop-after value",
			content: `---
on:
  issues:
    types: [opened]
  stop-after: "+24h"
permissions:
  contents: read
---

# Test Workflow`,
			fieldName:   "stop-after",
			fieldValue:  "+96h",
			shouldMatch: "stop-after:",
			expectError: false,
		},
		{
			name: "on field is a string not a map - error case",
			content: `---
on: push
permissions:
  contents: read
---

# Test Workflow`,
			fieldName:   "stop-after",
			fieldValue:  "+48h",
			shouldMatch: "",
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result, err := SetFieldInOnTrigger(tt.content, tt.fieldName, tt.fieldValue)

			if tt.expectError && err == nil {
				t.Errorf("Expected error but got none")
			}
			if !tt.expectError && err != nil {
				t.Errorf("Unexpected error: %v", err)
			}

			if !tt.expectError {
				// Check that field is present
				if !strings.Contains(result, tt.shouldMatch) {
					t.Errorf("Result doesn't contain expected string '%s':\n%s", tt.shouldMatch, result)
				}

				// Check that field has the value we set (approximately)
				if !strings.Contains(result, tt.fieldValue) {
					t.Errorf("Result doesn't contain expected value '%s':\n%s", tt.fieldValue, result)
				}
			}
		})
	}
}

func TestRemoveTopLevelFieldFromFrontmatter(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		content   string
		fieldName string
		want      string
		wantErr   bool
	}{
		{
			name: "removes existing top-level field",
			content: `---
name: my-workflow
source: owner/repo/workflow.md@abc123
features:
  - foo
---

# Body`,
			fieldName: "source",
			want: `---
name: my-workflow
features:
  - foo
---

# Body`,
		},
		{
			name: "field absent returns content unchanged",
			content: `---
name: my-workflow
features:
  - foo
---

# Body`,
			fieldName: "source",
			want: `---
name: my-workflow
features:
  - foo
---

# Body`,
		},
		{
			name: "field name prefix does not match other fields",
			content: `---
source: owner/repo@abc
source-extra: other
---

# Body`,
			fieldName: "source",
			want: `---
source-extra: other
---

# Body`,
		},
		{
			name: "no frontmatter returns content unchanged",
			content: `# Just a heading

Some text.`,
			fieldName: "source",
			want: `# Just a heading

Some text.`,
		},
		{
			name: "removes field with child lines",
			content: `---
name: wf
source:
  repo: owner/repo
  ref: abc123
evals:
  - test
---

# Body`,
			fieldName: "source",
			want: `---
name: wf
evals:
  - test
---

# Body`,
		},
		{
			name: "preserves nested fields with same name",
			content: `---
steps:
  - name: test
    with:
      source: nested-input
source: owner/repo/workflow.md@abc123
---

# Body`,
			fieldName: "source",
			want: `---
steps:
  - name: test
    with:
      source: nested-input
---

# Body`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := RemoveTopLevelFieldFromFrontmatter(tt.content, tt.fieldName)
			if (err != nil) != tt.wantErr {
				t.Fatalf("RemoveTopLevelFieldFromFrontmatter() error = %v, wantErr %v", err, tt.wantErr)
			}
			if got != tt.want {
				t.Errorf("RemoveTopLevelFieldFromFrontmatter() mismatch\ngot:\n%s\nwant:\n%s", got, tt.want)
			}
		})
	}
}
