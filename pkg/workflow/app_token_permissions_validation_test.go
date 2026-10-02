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
		name         string
		strict       string
		repositories string
		inputs       string
		uses         string
		wantError    bool
		wantWarn     bool
	}{
		{"strict unscoped", "", "          repositories: ${{ github.repository }}\n", "", "", true, false},
		{"case variant strict unscoped", "", "          repositories: ${{ github.repository }}\n", "", "Actions/create-github-app-token@v3.2.0", true, false},
		{"non-strict unscoped", "strict: false\n", "          repositories: ${{ github.repository }}\n", "", "", false, true},
		{"strict empty permission", "", "          repositories: ${{ github.repository }}\n", "          permission-contents: ''\n", "", true, false},
		{"strict invalid permission", "", "          repositories: ${{ github.repository }}\n", "          permission-contents: invalid\n", "", true, false},
		{"strict none permission", "", "          repositories: ${{ github.repository }}\n", "          Permission-contents: none\n", "", false, false},
		{"strict scoped", "", "          repositories: ${{ github.repository }}\n", "          permission-contents: read\n", "", false, false},
		{"non-strict scoped", "strict: false\n", "          repositories: ${{ github.repository }}\n", "          permission-issues: write\n", "", false, false},
		{"strict missing repositories", "", "", "          permission-contents: read\n", "", true, false},
		{"non-strict missing repositories", "strict: false\n", "", "          permission-contents: read\n", "", false, true},
		{"strict empty repositories", "", "          repositories: ''\n", "          permission-contents: read\n", "", true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "app-token.md")
			uses := tc.uses
			if uses == "" {
				uses = "actions/create-github-app-token@v3.2.0"
			}
			content := "---\nname: App token test\non: workflow_dispatch\n" + tc.strict +
				"network:\n  allowed: [defaults]\njobs:\n  activation:\n    steps:\n" +
				"      - name: Mint app token\n        uses: " + uses + "\n" +
				"        with:\n          app-id: ${{ vars.APP_ID }}\n          private-key: ${{ secrets.APP_KEY }}\n" +
				tc.repositories + tc.inputs + "---\n\n# Test\n"
			if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
				t.Fatal(err)
			}
			compiler := NewCompiler()
			err := compiler.CompileWorkflow(path)
			if tc.wantError {
				if err == nil || !strings.Contains(err.Error(), "strict mode: actions/create-github-app-token") {
					t.Fatalf("expected strict app token scope error, got %v", err)
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
	err := compiler.validateAppTokenPermissions(workflow, true)
	if err == nil || !strings.Contains(err.Error(), `job "custom"`) {
		t.Fatalf("expected custom job permission error, got %v", err)
	}
	if !strings.Contains(err.Error(), "repositories: ${{ github.repository }}") {
		t.Fatalf("expected missing-repositories error to recommend the trusted repository context, got %v", err)
	}
}

func TestAppTokenPermissionsGeneratedPreActivationStep(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "app-token.md")
	content := `---
name: App token skip-if test
on:
  workflow_dispatch:
  skip-if-match: "is:issue is:open label:test"
  github-app:
    client-id: ${{ vars.APP_ID }}
    private-key: ${{ secrets.APP_KEY }}
network:
  allowed: [defaults]
---

# Test
`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	compiler := NewCompiler()
	if err := compiler.CompileWorkflow(path); err != nil {
		t.Fatalf("expected generated pre-activation token to compile: %v", err)
	}
	lockContent, err := os.ReadFile(filepath.Join(dir, "app-token.lock.yml"))
	if err != nil {
		t.Fatalf("expected compiled lock file: %v", err)
	}
	for _, permission := range []string{"permission-issues: read", "permission-pull-requests: read"} {
		if !strings.Contains(string(lockContent), permission) {
			t.Errorf("expected generated token permissions to contain %q", permission)
		}
		if !strings.Contains(string(lockContent), "repositories: ${{ github.event.repository.name }}") {
			t.Fatal("expected generated token to explicitly scope repositories to the current repository")
		}
	}
}

func TestAppTokenPermissionsGeneratedTokenDefaults(t *testing.T) {
	compiler := NewCompiler()
	app := &GitHubAppConfig{AppID: "app-id", PrivateKey: "private-key"}

	steps := strings.Join(compiler.buildGitHubAppTokenMintStepWithMeta(
		app, nil, "", "", "Mint token", "mint-token",
	), "")
	if !strings.Contains(steps, "permission-contents: read") {
		t.Fatalf("expected nil permission config to default to contents: read, got:\n%s", steps)
	}

	app.Permissions = map[string]string{"contents": "none"}
	steps = strings.Join(compiler.buildGitHubAppTokenMintStepWithMeta(
		app, nil, "", "", "Mint token", "mint-token",
	), "")
	if !strings.Contains(steps, "permission-contents: none") {
		t.Fatalf("expected app permission override to be emitted with nil permissions, got:\n%s", steps)
	}

	steps = strings.Join(compiler.buildGitHubAppTokenMintStepWithMeta(
		app, NewPermissions(), "", "", "Mint token", "mint-token",
	), "")
	if !strings.Contains(steps, "permission-contents: none") {
		t.Fatalf("expected empty permissions to emit an explicit none scope, got:\n%s", steps)
	}
}

func TestAppTokenPermissionsGeneratedOTLPAndSideRepoSteps(t *testing.T) {
	compiler := NewCompiler()
	data := &WorkflowData{
		RawFrontmatter: map[string]any{
			"observability": map[string]any{
				"otlp": map[string]any{
					"github-app": map[string]any{
						"client-id":   "app-id",
						"private-key": "private-key",
					},
				},
			},
		},
	}
	otlpSteps := strings.Join(compiler.generateOTLPOIDCMintStep(data), "")
	if !strings.Contains(otlpSteps, "permission-contents: read") {
		t.Fatalf("expected OTLP token to explicitly scope repository content access, got:\n%s", otlpSteps)
	}

	sideRepoSteps := sideRepoAppTokenMintStepYAML(
		&GitHubAppConfig{AppID: "app-id", PrivateKey: "private-key"},
		"owner/repo",
	)
	for _, permission := range []string{
		"permission-actions: read",
		"permission-contents: write",
		"permission-discussions: write",
		"permission-issues: write",
		"permission-pull-requests: write",
	} {
		if !strings.Contains(sideRepoSteps, permission) {
			t.Errorf("expected side-repository token permission %q", permission)
		}
	}
}
