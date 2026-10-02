//go:build !integration

package workflow

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAppTokenPermissionsAtCompileTime(t *testing.T) {
	for _, tc := range []struct {
		name      string
		strict    string
		inputs    string
		wantError bool
		wantWarn  bool
	}{
		{"strict unscoped", "", "", true, false},
		{"non-strict unscoped", "strict: false\n", "", false, true},
		{"strict empty permission", "", "          permission-contents: ''\n", true, false},
		{"strict scoped", "", "          permission-contents: read\n", false, false},
		{"non-strict scoped", "strict: false\n", "          permission-issues: write\n", false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "app-token.md")
			content := "---\nname: App token test\non: workflow_dispatch\n" + tc.strict +
				"network:\n  allowed: [defaults]\njobs:\n  activation:\n    steps:\n" +
				"      - name: Mint app token\n        uses: actions/create-github-app-token@v3.2.0\n" +
				"        with:\n          app-id: ${{ vars.APP_ID }}\n          private-key: ${{ secrets.APP_KEY }}\n" +
				tc.inputs + "---\n\n# Test\n"
			if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
				t.Fatal(err)
			}
			compiler := NewCompiler()
			err := compiler.CompileWorkflow(path)
			if tc.wantError {
				if err == nil || !strings.Contains(err.Error(), "strict mode: actions/create-github-app-token") {
					t.Fatalf("expected strict app token permission error, got %v", err)
				}
			} else if err != nil {
				t.Fatalf("unexpected compile error: %v", err)
			}
			if tc.wantWarn && compiler.warningCount == 0 {
				t.Fatal("expected a warning for unscoped app token")
			}
			if !tc.wantError {
				if _, err := os.Stat(filepath.Join(dir, "app-token.lock.yml")); err != nil {
					t.Fatalf("expected compiled lock file: %v", err)
				}
			}
		})
	}
}

func TestAppTokenPermissionsCheckAllCompiledJobs(t *testing.T) {
	compiler := NewCompiler()
	workflow := map[string]any{"jobs": map[string]any{
		"custom": map[string]any{"steps": []any{
			map[string]any{"uses": "actions/create-github-app-token@sha", "with": map[string]any{"private-key": "secret"}},
		}},
	}}
	if err := compiler.validateAppTokenPermissions(workflow, true); err == nil || !strings.Contains(err.Error(), `job "custom"`) {
		t.Fatalf("expected custom job permission error, got %v", err)
	}
}
