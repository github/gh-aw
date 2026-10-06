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

func TestAppTokenPermissionsGeneratedWildcardRepositories(t *testing.T) {
	for _, tc := range []struct {
		name   string
		config string
		stepID string
	}{
		{
			name:   "github tool",
			config: "tools:\n  github:\n    toolsets: [repos]\n    github-app:\n      app-id: ${{ vars.APP_ID }}\n      private-key: ${{ secrets.APP_KEY }}\n      repositories: [\"*\"]\n",
			stepID: "github-mcp-app-token",
		},
		{
			name:   "safe outputs",
			config: "safe-outputs:\n  github-app:\n    app-id: ${{ vars.APP_ID }}\n    private-key: ${{ secrets.APP_KEY }}\n    repositories: [\"*\"]\n  create-issue:\n",
			stepID: "safe-outputs-app-token",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "wildcard.md")
			content := "---\non: workflow_dispatch\nstrict: true\nengine: copilot\npermissions:\n  contents: read\nnetwork:\n  allowed: [defaults]\n" +
				tc.config + "---\n\nTest wildcard token.\n"
			if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
				t.Fatal(err)
			}
			compiler := NewCompiler()
			compiler.approve = true
			if err := compiler.CompileWorkflow(path); err != nil {
				t.Fatalf("explicit wildcard should compile in strict mode: %v", err)
			}
			if compiler.warningCount != 0 {
				t.Fatalf("explicit wildcard should not produce scope warnings, got %d", compiler.warningCount)
			}
			lock, err := os.ReadFile(filepath.Join(dir, "wildcard.lock.yml"))
			if err != nil {
				t.Fatal(err)
			}
			step := strings.SplitN(string(lock), "id: "+tc.stepID, 2)
			if len(step) != 2 {
				t.Fatalf("missing generated token step %s", tc.stepID)
			}
			tokenInputs := strings.SplitN(step[1], "\n      - name:", 2)[0]
			if strings.Contains(tokenInputs, "repositories:") || !strings.Contains(tokenInputs, "permission-") {
				t.Fatalf("wildcard token must omit repositories and retain explicit permissions:\n%s", tokenInputs)
			}
		})
	}
}

func TestAppTokenPermissionsWildcardStillChecksPermissionsAndOtherSteps(t *testing.T) {
	compiler := NewCompiler()
	compiler.buildGitHubAppTokenMintStepWithMeta(
		&GitHubAppConfig{AppID: "app-id", PrivateKey: "private-key", Repositories: []string{"*"}},
		nil, "", "", "Mint token", "generated-token",
	)
	generated := map[string]any{
		"id": "generated-token", "uses": "actions/create-github-app-token@sha",
		"with": map[string]any{"client-id": "app-id", "private-key": "private-key"},
	}
	workflow := map[string]any{"jobs": map[string]any{"agent": map[string]any{"steps": []any{generated}}}}
	if err := compiler.validateAppTokenPermissions(workflow, true); err == nil || !strings.Contains(err.Error(), "permission-*") {
		t.Fatalf("wildcard must not bypass permission checks, got %v", err)
	}
	generated["with"].(map[string]any)["permission-contents"] = "read"
	if err := compiler.validateAppTokenPermissions(workflow, true); err != nil {
		t.Fatalf("generated wildcard should pass with explicit permissions: %v", err)
	}
	workflow["jobs"].(map[string]any)["custom"] = map[string]any{"steps": []any{
		map[string]any{"uses": "actions/create-github-app-token@sha", "with": map[string]any{
			"client-id": "other-app", "private-key": "private-key", "permission-contents": "read",
		}},
	}}
	if err := compiler.validateAppTokenPermissions(workflow, true); err == nil || !strings.Contains(err.Error(), "repositories input") {
		t.Fatalf("other steps must still require repository scoping, got %v", err)
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
