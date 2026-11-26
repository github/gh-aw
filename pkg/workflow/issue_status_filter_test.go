//go:build !integration

package workflow

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/parser"
	"github.com/github/gh-aw/pkg/testutil"
)

func TestIssueStatusFilterValidation(t *testing.T) {
	tests := []struct {
		name   string
		fields map[string]any
		valid  bool
	}{
		{"ids", map[string]any{"types": []any{"status_changed"}, "status-ids": []any{4, 8}}, true},
		{"categories", map[string]any{"types": []any{"status_changed"}, "status-categories": []any{"started"}}, true},
		{"mixed", map[string]any{"types": []any{"opened", "status_changed"}, "status-ids": []any{8}, "status-categories": []any{"started", "planned"}}, true},
		{"no types", map[string]any{"status-ids": []any{8}}, false},
		{"wrong types", map[string]any{"types": []any{"opened"}, "status-ids": []any{8}}, false},
		{"empty ids", map[string]any{"types": []any{"status_changed"}, "status-ids": []any{}}, false},
		{"null ids", map[string]any{"types": []any{"status_changed"}, "status-ids": nil}, false},
		{"zero id", map[string]any{"types": []any{"status_changed"}, "status-ids": []any{0}}, false},
		{"negative id", map[string]any{"types": []any{"status_changed"}, "status-ids": []any{-1}}, false},
		{"fraction id", map[string]any{"types": []any{"status_changed"}, "status-ids": []any{1.5}}, false},
		{"string id", map[string]any{"types": []any{"status_changed"}, "status-ids": []any{"8"}}, false},
		{"unsafe id", map[string]any{"types": []any{"status_changed"}, "status-ids": []any{int64(9007199254740992)}}, false},
		{"unknown category", map[string]any{"types": []any{"status_changed"}, "status-categories": []any{"in_progress"}}, false},
		{"empty categories", map[string]any{"types": []any{"status_changed"}, "status-categories": []any{}}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			frontmatter := map[string]any{"on": map[string]any{"issues": tt.fields}}
			_, err := extractIssueStatusFilter(frontmatter)
			if (err == nil) != tt.valid {
				t.Fatalf("extractIssueStatusFilter() error = %v, valid = %v", err, tt.valid)
			}
			err = parser.ValidateMainWorkflowFrontmatterWithSchemaAndLocation(frontmatter, "status.md")
			if (err == nil) != tt.valid {
				t.Fatalf("schema error = %v, valid = %v", err, tt.valid)
			}
		})
	}
}

func TestIssueStatusFilterAbsentSettings(t *testing.T) {
	for _, frontmatter := range []map[string]any{
		nil,
		{},
		{"on": "workflow_dispatch"},
		{"on": map[string]any{}},
		{"on": map[string]any{"issues": nil}},
		{"on": map[string]any{"issues": map[string]any{"types": []any{"opened"}}}},
	} {
		filter, err := extractIssueStatusFilter(frontmatter)
		if err != nil || filter != nil {
			t.Fatalf("extractIssueStatusFilter(%v) = %v, %v; want nil, nil", frontmatter, filter, err)
		}
	}
}

func TestIssueStatusFilterCompilation(t *testing.T) {
	dir := testutil.TempDir(t, "issue-status-filter")
	path := filepath.Join(dir, "status.md")
	content := `---
on:
  issues:
    types: [opened, status_changed]
    status-ids:
      - 4
      - 8
    status-categories:
      - started
  workflow_dispatch:
  roles: all
engine: claude
---
Process the captured issue transition.
`
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
	compiler := NewCompiler()
	if err := compiler.CompileWorkflow(path); err != nil {
		t.Fatal(err)
	}
	compiled, err := os.ReadFile(filepath.Join(dir, "status.lock.yml"))
	if err != nil {
		t.Fatal(err)
	}
	lock := string(compiled)
	for _, expected := range []string{
		"pre_activation:", "id: check_issue_status", "check_issue_status.cjs",
		"GH_AW_ISSUE_STATUS_FILTER:", "steps.check_issue_status.outputs.status_match == 'true'",
		"needs.pre_activation.outputs.activated == 'true'",
		"# status-ids:", "# status-categories:", "# - 4", "# - started",
	} {
		if !strings.Contains(lock, expected) {
			t.Errorf("compiled workflow missing %q", expected)
		}
	}
	for line := range strings.SplitSeq(lock, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "status-ids:") || strings.HasPrefix(strings.TrimSpace(line), "status-categories:") {
			t.Errorf("compiler-only status filter leaked into native trigger: %s", line)
		}
	}
}
