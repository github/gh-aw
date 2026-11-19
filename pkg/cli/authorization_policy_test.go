package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/workflow"
	"github.com/stretchr/testify/require"
)

const authorizationPolicyFixture = `{
  "version": 1,
  "policy_revision": "baseline-001",
  "repository_id": "123456789",
  "authority": {"endpoint":"https://authority.example.org/authorize","audience":"safe-outputs"},
  "required_workflows": [".github/workflows/triage.md"]
}`

const authorizationSourceFixture = `---
on: workflow_dispatch
permissions:
  contents: read
safe-outputs:
  authorization:
    endpoint: https://authority.example.org/authorize
    audience: safe-outputs
  add-comment:
---
# Triage
Do nothing.
`

func TestAuthorizationPolicySchema(t *testing.T) {
	require.NotNil(t, mustParseAuthorizationPolicy(t))
	for _, test := range []struct{ name, input string }{
		{"version", strings.Replace(authorizationPolicyFixture, `"version": 1`, `"version": 2`, 1)},
		{"unknown", strings.Replace(authorizationPolicyFixture, `"version": 1`, `"disable": true, "version": 1`, 1)},
		{"case sensitive fields", strings.Replace(authorizationPolicyFixture, `"version": 1`, `"Version": 1`, 1)},
		{"duplicate", strings.Replace(authorizationPolicyFixture, `"version": 1`, `"version": 2, "version": 1`, 1)},
		{"nested duplicate", strings.Replace(authorizationPolicyFixture, `"audience":"safe-outputs"`, `"audience":"other","audience":"safe-outputs"`, 1)},
		{"metadata", strings.Replace(authorizationPolicyFixture, `"audience":"safe-outputs"`, `"audience":"safe-outputs","source":"global"`, 1)},
		{"mutable", strings.Replace(authorizationPolicyFixture, `"audience":"safe-outputs"`, `"audience":"safe-outputs","from_environment":true`, 1)},
		{"traversal", strings.Replace(authorizationPolicyFixture, ".github/workflows/triage.md", ".github/workflows/../triage.md", 1)},
		{"nested path", strings.Replace(authorizationPolicyFixture, ".github/workflows/triage.md", ".github/workflows/shared/triage.md", 1)},
		{"duplicate path", strings.Replace(authorizationPolicyFixture, `[".github/workflows/triage.md"]`, `[".github/workflows/triage.md",".github/workflows/triage.md"]`, 1)},
		{"empty scope", strings.Replace(authorizationPolicyFixture, `[".github/workflows/triage.md"]`, `[]`, 1)},
		{"http", strings.Replace(authorizationPolicyFixture, "https://", "http://", 1)},
		{"identity", strings.Replace(authorizationPolicyFixture, `"123456789"`, `""`, 1)},
		{"trailing", authorizationPolicyFixture + `{}`},
		{"null", `null`},
		{"oversized", strings.Repeat(" ", maxAuthorizationPolicySize+1)},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, err := parseAuthorizationPolicy([]byte(test.input))
			require.Error(t, err)
			require.NotContains(t, err.Error(), "https://")
		})
	}
}

func mustParseAuthorizationPolicy(t *testing.T) *AuthorizationPolicy {
	t.Helper()
	policy, err := parseAuthorizationPolicy([]byte(authorizationPolicyFixture))
	require.NoError(t, err)
	return policy
}

func authorizationTestGit(t *testing.T, args ...string) string {
	t.Helper()
	command := exec.Command("git", append([]string{"-c", "commit.gpgsign=false"}, args...)...)
	command.Env = append(os.Environ(), "GIT_AUTHOR_NAME=Test", "GIT_AUTHOR_EMAIL=test@example.org", "GIT_COMMITTER_NAME=Test", "GIT_COMMITTER_EMAIL=test@example.org")
	output, err := command.CombinedOutput()
	require.NoError(t, err, "%s", output)
	return strings.TrimSpace(string(output))
}

func writeAuthorizationTestFile(t *testing.T, path, content string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o700))
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
}

func compileAuthorizationTestSource(t *testing.T, file string) {
	t.Helper()
	compiler := workflow.NewCompiler()
	compiler.SetQuiet(true)
	compiler.SetApprove(true)
	compiler.SetReadOnly(true)
	require.NoError(t, compiler.CompileWorkflow(file))
}

func TestAuthorizationPolicySnapshot(t *testing.T) {
	for _, test := range []struct {
		name        string
		source      string
		extra       map[string]string
		mutate      func(*testing.T)
		afterCommit func(*testing.T)
		code        string
		status      string
		approved    []string
	}{
		{name: "compliant"},
		{name: "enrollment removed", source: strings.Replace(authorizationSourceFixture, "  authorization:\n    endpoint: https://authority.example.org/authorize\n    audience: safe-outputs\n", "", 1), code: "authorization_required"},
		{name: "authority redirected", source: strings.ReplaceAll(authorizationSourceFixture, "authority.example.org", "other.example.org"), code: "authority_mismatch"},
		{name: "environment binding", source: strings.Replace(authorizationSourceFixture, "  authorization:\n    endpoint: https://authority.example.org/authorize\n    audience: safe-outputs\n", "", 1), extra: map[string]string{workflow.RepoConfigFileName: `{"safe_output_authorization":true}`}, code: "mutable_authority_binding"},
		{name: "global staged", source: strings.Replace(authorizationSourceFixture, "safe-outputs:", "safe-outputs:\n  staged: true", 1)},
		{name: "local staged", source: strings.Replace(authorizationSourceFixture, "  add-comment:", "  add-comment:\n    staged: true", 1)},
		{name: "explicit diagnostic mismatch", source: "---\non: workflow_dispatch\npermissions:\n  contents: read\nsafe-outputs:\n  create-issue: false\n  noop:\n    authorization:\n      endpoint: https://other.example.org/authorize\n      audience: safe-outputs\n---\n# Test\nDo nothing.\n", code: "authority_mismatch"},
		{name: "expression staged", source: strings.Replace(authorizationSourceFixture, "safe-outputs:", "safe-outputs:\n  staged: \"${{ github.event_name == 'workflow_dispatch' }}\"", 1)},
		{name: "staged without requirement", source: strings.Replace(authorizationSourceFixture, "  authorization:\n    endpoint: https://authority.example.org/authorize\n    audience: safe-outputs\n", "  staged: true\n", 1), code: "authorization_required"},
		{name: "diagnostics only", source: strings.Replace(authorizationSourceFixture, "  add-comment:", "  noop:\n  create-issue: false", 1)},
		{name: "diagnostics without enrollment", source: "---\non: workflow_dispatch\npermissions:\n  contents: read\nsafe-outputs:\n  noop:\n  create-issue: false\n---\n# Test\nDo nothing.\n"},
		{name: "no outputs", source: "---\non: workflow_dispatch\npermissions:\n  contents: read\nsafe-outputs:\n  create-issue: false\n---\n# Test\nDo nothing.\n"},
		{name: "auto-injected eligible output", source: "---\non: workflow_dispatch\npermissions:\n  contents: read\n---\n# Test\nDo nothing.\n", code: "authorization_required"},
		{name: "custom script", source: strings.Replace(authorizationSourceFixture, "  add-comment:", "  scripts:\n    notify:\n      description: Notify\n      script: |\n        throw new Error('SCRIPT_MUST_NOT_EXECUTE');", 1)},
		{name: "shared authorization", source: strings.Replace(authorizationSourceFixture, "safe-outputs:\n  authorization:\n    endpoint: https://authority.example.org/authorize\n    audience: safe-outputs\n", "imports:\n  - shared/policy.md\nsafe-outputs:\n", 1), extra: map[string]string{".github/workflows/shared/policy.md": "---\nsafe-outputs:\n  authorization:\n    endpoint: https://authority.example.org/authorize\n    audience: safe-outputs\n---\n"}},
		{name: "new workflow joins inventory", mutate: func(t *testing.T) {
			writeAuthorizationTestFile(t, ".github/workflows/new.md", strings.Replace(authorizationSourceFixture, "  authorization:\n    endpoint: https://authority.example.org/authorize\n    audience: safe-outputs\n", "", 1))
			compileAuthorizationTestSource(t, ".github/workflows/new.md")
		}, code: "authorization_required"},
		{name: "working tree cannot redirect snapshot", afterCommit: func(t *testing.T) {
			writeAuthorizationTestFile(t, ".github/workflows/triage.md", strings.ReplaceAll(authorizationSourceFixture, "authority.example.org", "uncommitted.example.org"))
		}},
		{name: "candidate policy ignored", extra: map[string]string{"baseline.json": strings.ReplaceAll(authorizationPolicyFixture, "authority.example.org", "unapproved.example.org")}},
		{name: "export attributes cannot omit source", extra: map[string]string{".gitattributes": ".github/workflows/* export-ignore\n"}},
		{name: "deleted source", mutate: func(t *testing.T) { require.NoError(t, os.Remove(".github/workflows/triage.md")) }, code: "required_workflow_missing"},
		{name: "renamed source", mutate: func(t *testing.T) {
			require.NoError(t, os.Rename(".github/workflows/triage.md", ".github/workflows/renamed.md"))
		}, code: "required_workflow_missing"},
		{name: "missing lock", mutate: func(t *testing.T) { require.NoError(t, os.Remove(".github/workflows/triage.lock.yml")) }, code: "generated_workflow_drift"},
		{name: "forged lock", mutate: func(t *testing.T) {
			data, err := os.ReadFile(".github/workflows/triage.lock.yml")
			require.NoError(t, err)
			require.Contains(t, string(data), "id-token: write")
			writeAuthorizationTestFile(t, ".github/workflows/triage.lock.yml", strings.Replace(string(data), "id-token: write", "id-token: none", 1))
		}, code: "generated_workflow_drift"},
		{name: "extra job", mutate: func(t *testing.T) {
			file, err := os.OpenFile(".github/workflows/triage.lock.yml", os.O_APPEND|os.O_WRONLY, 0)
			require.NoError(t, err)
			_, err = file.WriteString("\n  extra:\n    runs-on: ubuntu-latest\n    steps:\n      - run: echo unsafe\n")
			require.NoError(t, err)
			require.NoError(t, file.Close())
		}, code: "generated_workflow_drift"},
		{name: "unexpected executable", extra: map[string]string{".github/workflows/other.yaml": "on: push\njobs: {}\n"}, code: "inventory_unresolved", status: "incomplete"},
		{name: "approved handwritten executable", extra: map[string]string{".github/workflows/other.yaml": "on: push\njobs: {}\n"}, approved: []string{".github/workflows/other.yaml"}},
		{name: "orphan", extra: map[string]string{".github/workflows/orphan.lock.yml": "on: push\njobs: {}\n"}, code: "generated_workflow_drift"},
		{name: "conflicting local authority", mutate: func(t *testing.T) {
			writeAuthorizationTestFile(t, ".github/workflows/triage.md", strings.Replace(authorizationSourceFixture, "  add-comment:", "  add-comment:\n    authorization:\n      endpoint: https://other.example.org/authorize\n      audience: safe-outputs", 1))
		}, code: "verification_incomplete", status: "incomplete"},
		{name: "missing import", mutate: func(t *testing.T) {
			writeAuthorizationTestFile(t, ".github/workflows/triage.md", strings.Replace(authorizationSourceFixture, "on: workflow_dispatch", "on: workflow_dispatch\nimports:\n  - shared/missing.md", 1))
		}, code: "verification_incomplete", status: "incomplete"},
		{name: "remote import", mutate: func(t *testing.T) {
			writeAuthorizationTestFile(t, ".github/workflows/triage.md", strings.Replace(authorizationSourceFixture, "on: workflow_dispatch", "on: workflow_dispatch\nimports:\n  - owner/repo/policy.md@0123456789012345678901234567890123456789", 1))
		}, code: "verification_incomplete", status: "incomplete"},
		{name: "symbolic link", mutate: func(t *testing.T) { require.NoError(t, os.Symlink("triage.md", ".github/workflows/link.md")) }, code: "verification_incomplete", status: "incomplete"},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			t.Chdir(root)
			authorizationTestGit(t, "init", "-q")
			source := test.source
			if source == "" {
				source = authorizationSourceFixture
			}
			marker := filepath.Join(root, "execution-marker")
			if test.name == "custom script" {
				source = strings.Replace(source, "throw new Error('SCRIPT_MUST_NOT_EXECUTE');",
					"require('node:fs').writeFileSync("+strconv.Quote(marker)+", 'executed');\n        throw new Error('SCRIPT_MUST_NOT_EXECUTE');", 1)
			}
			writeAuthorizationTestFile(t, ".github/workflows/triage.md", source)
			for file, content := range test.extra {
				writeAuthorizationTestFile(t, file, content)
			}
			compileAuthorizationTestSource(t, ".github/workflows/triage.md")
			require.NoFileExists(t, marker)
			if test.mutate != nil {
				test.mutate(t)
			}
			authorizationTestGit(t, "add", ".")
			authorizationTestGit(t, "-c", "core.hooksPath=/dev/null", "commit", "-qm", "fixture")
			if test.afterCommit != nil {
				test.afterCommit(t)
			}
			beforeStatus := authorizationTestGit(t, "status", "--porcelain")
			policyPath := filepath.Join(t.TempDir(), "baseline.json")
			writeAuthorizationTestFile(t, policyPath, authorizationPolicyFixture)
			var output bytes.Buffer
			cmd := NewValidateCommand(func(string) error { t.Fatal("ordinary validation must not run"); return nil })
			cmd.SetOut(&output)
			args := []string{"--authorization-policy", policyPath, "--authorization-repository-id", "123456789", "--json"}
			for _, approved := range test.approved {
				args = append(args, "--authorization-actions-workflow", approved)
			}
			cmd.SetArgs(args)
			err := cmd.Execute()
			var result AuthorizationPolicyResult
			require.NoError(t, json.Unmarshal(output.Bytes(), &result), "%s", output.String())
			require.NotContains(t, output.String(), "authority.example.org")
			if test.code == "" {
				require.NoError(t, err, "%+v", result.Findings)
				require.Equal(t, "compliant", result.Status)
				require.Len(t, result.Workflows, 1)
				if strings.HasPrefix(test.name, "diagnostics") || test.name == "no outputs" {
					require.Zero(t, result.Eligible)
				}
				require.NotEmpty(t, result.CompilerDigest)
				require.NotEmpty(t, result.SnapshotDigest)
				require.NotEmpty(t, result.CandidateCommit)
				require.Equal(t, authorizationDigest([]byte(authorizationPolicyFixture)), result.PolicyDigest)
			} else {
				require.Error(t, err)
				status := test.status
				if status == "" {
					status = "violations"
				}
				require.Equal(t, status, result.Status, "%+v", result.Findings)
				require.True(t, authorizationHasFinding(result, test.code), "%+v", result.Findings)
			}
			require.Equal(t, beforeStatus, authorizationTestGit(t, "status", "--porcelain"), "verification must not modify candidate files")
			require.NoFileExists(t, marker)
		})
	}
}

func TestAuthorizationPolicyCommandFailsClosed(t *testing.T) {
	policyPath := filepath.Join(t.TempDir(), "baseline.json")
	writeAuthorizationTestFile(t, policyPath, authorizationPolicyFixture)
	for _, test := range []struct {
		name string
		args []string
		code string
	}{
		{"no identity", nil, "verification_incomplete"},
		{"wrong identity", []string{"--authorization-repository-id", "999"}, "verification_incomplete"},
		{"wrong digest", []string{"--authorization-policy-sha256", strings.Repeat("0", 64)}, "policy_invalid"},
		{"subset", []string{"triage"}, "verification_incomplete"},
		{"directory", []string{"--dir", ".github/workflows"}, "verification_incomplete"},
		{"engine override", []string{"--engine", "copilot"}, "verification_incomplete"},
		{"missing policy", []string{"--authorization-policy", policyPath + ".missing"}, "policy_invalid"},
	} {
		t.Run(test.name, func(t *testing.T) {
			var output bytes.Buffer
			cmd := NewValidateCommand(func(string) error { t.Fatal("ordinary validation ran"); return nil })
			cmd.SetOut(&output)
			cmd.SetErr(&bytes.Buffer{})
			cmd.SetArgs(append([]string{"--authorization-policy", policyPath, "--json"}, test.args...))
			require.Error(t, cmd.Execute())
			var result AuthorizationPolicyResult
			require.NoError(t, json.Unmarshal(output.Bytes(), &result))
			require.Equal(t, "incomplete", result.Status)
			require.True(t, authorizationHasFinding(result, test.code))
		})
	}
}

func authorizationHasFinding(result AuthorizationPolicyResult, code string) bool {
	for _, finding := range result.Findings {
		if finding.Code == code {
			return true
		}
	}
	return false
}
