//go:build !integration

package cli

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/parser"
	"github.com/stretchr/testify/require"
)

const privateCloneTestWorkflow = `---
on: workflow_dispatch
engine: copilot
permissions:
  contents: read
tools:
  github:
    toolsets: [repos]
safe-outputs:
  create-pull-request:
    draft: true
    fallback-as-issue: false
---
Fix the selected findings using the local security context.
`

func TestPrivateCloneOptions(t *testing.T) {
	valid := privateCloneOptions{
		Source: "acme/source", Destination: "security/fix", Ref: "main",
		Workflows: "workflows", Alerts: []int{42},
	}
	require.NoError(t, validatePrivateCloneOptions(valid))
	for _, change := range []func(*privateCloneOptions){
		func(o *privateCloneOptions) { o.Destination = "ACME/SOURCE" },
		func(o *privateCloneOptions) { o.Source = "../source" },
		func(o *privateCloneOptions) { o.Source = "-org/source" },
		func(o *privateCloneOptions) { o.Alerts = nil },
		func(o *privateCloneOptions) { o.Alerts = []int{-1} },
		func(o *privateCloneOptions) { o.Advisories = []string{"../../outside"} },
		func(o *privateCloneOptions) { o.Workflows = "" },
		func(o *privateCloneOptions) { o.Ref = "--upload-pack=bad" },
	} {
		opts := valid
		change(&opts)
		require.Error(t, validatePrivateCloneOptions(opts), "%+v", opts)
	}
}

func TestPrivateCloneCommandRequiresOperatorWorkflows(t *testing.T) {
	cmd := NewPrivateCloneCommand()
	cmd.SetArgs([]string{"acme/source", "security/fix", "--alert", "42"})
	require.ErrorContains(t, cmd.Execute(), `"workflows" not set`)
}

func TestPrivateCloneWorkflowBundle(t *testing.T) {
	t.Run("requires bundled imports", func(t *testing.T) {
		dir := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(dir, "fix.md"),
			[]byte(strings.Replace(privateCloneTestWorkflow, "engine: copilot", "imports: [shared/context.md]\nengine: copilot", 1)), 0o600))
		_, err := readPrivateCloneWorkflows(dir)
		require.ErrorContains(t, err, "not bundled")
		require.NoError(t, os.Mkdir(filepath.Join(dir, "shared"), 0o700))
		require.NoError(t, os.WriteFile(filepath.Join(dir, "shared/context.md"), []byte("Local context"), 0o600))
		_, err = readPrivateCloneWorkflows(dir)
		require.NoError(t, err)
	})
	t.Run("rejects links and precompiled workflows", func(t *testing.T) {
		for _, name := range []string{"fix.lock.yml", "other.yml", "linked.md"} {
			dir := t.TempDir()
			require.NoError(t, os.WriteFile(filepath.Join(dir, "fix.md"), []byte(privateCloneTestWorkflow), 0o600))
			if name == "linked.md" {
				require.NoError(t, os.Symlink(filepath.Join(dir, "fix.md"), filepath.Join(dir, name)))
			} else {
				require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte("name: unexpected"), 0o600))
			}
			_, err := readPrivateCloneWorkflows(dir)
			require.Error(t, err)
		}
	})
	t.Run("rejects upstream authority", func(t *testing.T) {
		for _, field := range []string{
			"checkout:\n  repository: acme/source",
			"safe-outputs:\n  create-pull-request:\n    target-repo: acme/source",
			"tools:\n  github:\n    github-token: ${{ secrets.UPSTREAM_TOKEN }}",
			"tools:\n  github:\n    allowed-repos: all",
			"source: acme/upstream@main",
			"imports: [acme/upstream/fix.md@main]",
		} {
			dir := t.TempDir()
			require.NoError(t, os.WriteFile(filepath.Join(dir, "fix.md"), []byte("---\non: workflow_dispatch\n"+field+"\n---\nFix\n"), 0o600))
			_, err := readPrivateCloneWorkflows(dir)
			require.Error(t, err, field)
		}
	})
}

func TestPrivateCloneBootstrap(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, ".github/workflows"), 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".github/workflows/upstream.yml"), []byte("on: push\n"), 0o600))
	snapshot := &privateCloneSnapshot{
		privateCloneManifest: privateCloneManifest{Version: 1, SourceSHA: strings.Repeat("a", 40)},
		Data:                 map[string][]byte{privateCloneContextDir + "/code-scanning/42.json": []byte(`{"number":42}`)},
	}
	files := map[string][]byte{
		"fix.md":           []byte(privateCloneTestWorkflow),
		"shared/notes.md":  []byte("Use the local snapshot."),
		"shared/setup.md":  []byte("${{ secrets.BUILD_TOKEN }}"),
		"shared/github.md": []byte("---\ntools:\n  github:\n    toolsets: [repos]\n---\n"),
	}
	require.NoError(t, installPrivateCloneBundle(dir, files, snapshot))
	require.NoFileExists(t, filepath.Join(dir, ".github/workflows/upstream.yml"))
	data, err := os.ReadFile(filepath.Join(dir, ".github/workflows/fix.md"))
	require.NoError(t, err)
	parsed, err := parser.ExtractFrontmatterFromContent(string(data))
	require.NoError(t, err)
	require.Equal(t, "write", parsed.Frontmatter["permissions"].(map[string]any)["copilot-requests"])
	github := parsed.Frontmatter["tools"].(map[string]any)["github"].(map[string]any)
	require.Equal(t, "${{ github.repository }}", github["allowed-repos"])
	require.Contains(t, parsed.Markdown, ".github/private-clone/README.md")
	require.NotContains(t, parsed.Markdown, `"number":42`)
	require.Equal(t, []string{"BUILD_TOKEN"}, snapshot.Secrets)
	require.FileExists(t, filepath.Join(dir, privateCloneContextDir, "code-scanning/42.json"))
	imported, err := os.ReadFile(filepath.Join(dir, ".github/workflows/shared/github.md"))
	require.NoError(t, err)
	importedParsed, err := parser.ExtractFrontmatterFromContent(string(imported))
	require.NoError(t, err)
	require.Equal(t, "${{ github.repository }}", importedParsed.Frontmatter["tools"].(map[string]any)["github"].(map[string]any)["allowed-repos"])

	run := func(args ...string) string {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL="+os.DevNull, "GIT_CONFIG_NOSYSTEM=1")
		out, err := cmd.CombinedOutput()
		require.NoError(t, err, "%s", out)
		return strings.TrimSpace(string(out))
	}
	run("init", "--initial-branch=main", "--template=")
	run("add", "--all")
	run("-c", "user.name=Test", "-c", "user.email=test@example.com", "-c", "commit.gpgsign=false", "commit", "-m", "Bootstrap")
	run("tag", "private-clone/bootstrap")
	require.NoError(t, os.WriteFile(filepath.Join(dir, "source.go"), []byte("package fixed\n"), 0o600))
	run("add", "source.go")
	run("-c", "user.name=Test", "-c", "user.email=test@example.com", "-c", "commit.gpgsign=false", "commit", "-m", "Fix")
	require.Equal(t, "source.go", run("diff", "--name-only", "private-clone/bootstrap", "HEAD"))
}

func TestPrivateCloneRejectsUpstreamSymlink(t *testing.T) {
	dir := t.TempDir()
	outside := t.TempDir()
	require.NoError(t, os.Symlink(outside, filepath.Join(dir, ".github")))
	err := installPrivateCloneBundle(dir, nil, &privateCloneSnapshot{})
	require.ErrorContains(t, err, ".github must be a real directory")
}

func TestPrivateCloneCompilation(t *testing.T) {
	dir := t.TempDir()
	_, err := privateCloneGit(context.Background(), dir, "init", "--initial-branch=main", "--template=")
	require.NoError(t, err)
	snapshot := &privateCloneSnapshot{
		privateCloneManifest: privateCloneManifest{Version: 1, SourceSHA: strings.Repeat("a", 40)},
		Data:                 map[string][]byte{},
	}
	require.NoError(t, installPrivateCloneBundle(dir, map[string][]byte{"fix.md": []byte(privateCloneTestWorkflow)}, snapshot))
	require.NoError(t, withWorkingDir(dir, func() error {
		_, err := CompileWorkflows(context.Background(), CompileConfig{
			WorkflowDir: ".github/workflows",
			Strict:      true,
			Validate:    true,
			Approve:     true,
		})
		return err
	}))
	require.FileExists(t, filepath.Join(dir, ".github/workflows/fix.lock.yml"))
}

func privateCloneMockGH(t *testing.T, body string) {
	t.Helper()
	bin := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(bin, "gh"), []byte("#!/bin/sh\nset -eu\n"+body+"\n"), 0o700))
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
}

func TestPrivateCloneDownloads(t *testing.T) {
	log := filepath.Join(t.TempDir(), "calls")
	t.Setenv("PRIVATE_CLONE_TEST_LOG", log)
	privateCloneMockGH(t, `
printf '%s\n' "$*" >> "$PRIVATE_CLONE_TEST_LOG"
case "$*" in
  *commits/*) printf '%s\n' aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa ;;
  *instances*) printf '%s\n' '[[{"ref":"refs/heads/main"}],[{"ref":"refs/heads/release"}]]' ;;
  *code-scanning*) printf '%s\n' '{"number":42,"message":{"text":"untrusted instruction"}}' ;;
  *security-advisories*) printf '%s\n' '{"ghsa_id":"GHSA-2345-6789-cfgh","state":"draft"}' ;;
  *) exit 1 ;;
esac`)
	snapshot, err := downloadPrivateCloneContext(context.Background(), privateCloneOptions{
		Source: "acme/source", Destination: "security/fix", Ref: "main",
		Alerts: []int{42, 42}, Advisories: []string{"GHSA-2345-6789-cfgh"},
	})
	require.NoError(t, err)
	require.Len(t, snapshot.Files, 3)
	var instances []json.RawMessage
	require.NoError(t, json.Unmarshal(snapshot.Data[privateCloneContextDir+"/code-scanning/42.instances.json"], &instances))
	require.Len(t, instances, 2)
	calls, err := os.ReadFile(log)
	require.NoError(t, err)
	require.Equal(t, 1, strings.Count(string(calls), "/42/instances"))
	require.NotContains(t, string(calls), "repo create")
}

func TestPrivateCloneDownloadFailureDoesNotLeakContext(t *testing.T) {
	privateCloneMockGH(t, `printf '%s\n' 'CONFIDENTIAL RESPONSE' >&2; exit 1`)
	_, err := downloadPrivateCloneContext(context.Background(), privateCloneOptions{Source: "acme/source", Ref: "main"})
	require.Error(t, err)
	require.NotContains(t, err.Error(), "CONFIDENTIAL RESPONSE")
}

func TestPrivateCloneDestinationPolicy(t *testing.T) {
	for _, scenario := range []struct {
		name        string
		repo        string
		permissions string
		ok          bool
	}{
		{"private standalone disabled", `{"private":true,"fork":false,"full_name":"security/fix"}`, `{"enabled":false}`, true},
		{"public", `{"private":false,"fork":false,"full_name":"security/fix"}`, `{"enabled":false}`, false},
		{"fork", `{"private":true,"fork":true,"full_name":"security/fix"}`, `{"enabled":false}`, false},
		{"wrong repository", `{"private":true,"fork":false,"full_name":"security/other"}`, `{"enabled":false}`, false},
		{"missing fork classification", `{"private":true,"full_name":"security/fix"}`, `{"enabled":false}`, false},
		{"Actions enabled", `{"private":true,"fork":false,"full_name":"security/fix"}`, `{"enabled":true}`, false},
		{"missing policy", `{"private":true,"fork":false,"full_name":"security/fix"}`, `{}`, false},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			t.Setenv("PRIVATE_CLONE_TEST_REPO", scenario.repo)
			t.Setenv("PRIVATE_CLONE_TEST_PERMISSIONS", scenario.permissions)
			privateCloneMockGH(t, `
case "$*" in
  *actions/permissions*) printf '%s\n' "$PRIVATE_CLONE_TEST_PERMISSIONS" ;;
  *) printf '%s\n' "$PRIVATE_CLONE_TEST_REPO" ;;
esac`)
			err := verifyPrivateCloneDestination(context.Background(), "security/fix")
			if scenario.ok {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
			}
		})
	}
}

func TestPrivateClonePublicationChecksBeforePush(t *testing.T) {
	log := filepath.Join(t.TempDir(), "calls")
	t.Setenv("PRIVATE_CLONE_TEST_LOG", log)
	privateCloneMockGH(t, `
printf 'gh %s\n' "$*" >> "$PRIVATE_CLONE_TEST_LOG"
case "$*" in
  "repo create "*) exit 0 ;;
  *"--method PUT"*) exit 0 ;;
  *actions/permissions*) printf '%s\n' '{"enabled":false}' ;;
  "api repos/security/fix") printf '%s\n' '{"private":true,"fork":false,"full_name":"security/fix"}' ;;
  *) exit 1 ;;
esac`)
	gitBin := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(gitBin, "git"), []byte(`#!/bin/sh
set -eu
printf 'git %s\n' "$*" >> "$PRIVATE_CLONE_TEST_LOG"
`), 0o700))
	t.Setenv("PATH", gitBin+string(os.PathListSeparator)+os.Getenv("PATH"))
	require.NoError(t, publishPrivateClone(context.Background(), t.TempDir(), "security/fix"))
	calls, err := os.ReadFile(log)
	require.NoError(t, err)
	record := string(calls)
	for _, step := range []string{"enabled=false", "remote add origin", "api repos/security/fix/actions/permissions\n", "push origin"} {
		require.Contains(t, record, step)
	}
	require.Less(t, strings.Index(record, "enabled=false"), strings.Index(record, "remote add origin"))
	require.Less(t, strings.Index(record, "api repos/security/fix/actions/permissions\n"), strings.Index(record, "push origin"))
	require.Contains(t, record, "HEAD:refs/heads/main refs/tags/private-clone/bootstrap")
	require.NotContains(t, record, "--mirror")
	require.NotContains(t, record, "workflow run")
}
