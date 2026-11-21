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
		Workflows: "workflows", Starter: "investigate.md", Alerts: []int{42},
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
		func(o *privateCloneOptions) { o.Starter = "../investigate.md" },
		func(o *privateCloneOptions) { o.Secrets = []string{"GITHUB_TOKEN"} },
		func(o *privateCloneOptions) { o.Secrets = []string{"BAD-NAME"} },
		func(o *privateCloneOptions) { o.Starter = "-investigate.md" },
		func(o *privateCloneOptions) { o.Starter = "investigate.yml" },
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
	t.Run("requires a dispatchable starter workflow", func(t *testing.T) {
		files := map[string][]byte{"investigate.md": []byte(privateCloneTestWorkflow)}
		starter, err := validatePrivateCloneStarterWorkflow(files, "investigate.md")
		require.NoError(t, err)
		require.Equal(t, "investigate.lock.yml", starter)
		_, err = validatePrivateCloneStarterWorkflow(files, "missing.md")
		require.ErrorContains(t, err, "not a top-level workflow")
		files["investigate.md"] = []byte("---\non: push\n---\nInvestigate\n")
		_, err = validatePrivateCloneStarterWorkflow(files, "investigate.md")
		require.ErrorContains(t, err, "must declare workflow_dispatch")
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

func TestPrivateCloneStarterSelection(t *testing.T) {
	dispatchable := []byte(privateCloneTestWorkflow)
	starter, err := resolvePrivateCloneStarter(map[string][]byte{"investigate.md": dispatchable, "shared/notes.md": []byte("x")}, "")
	require.NoError(t, err)
	require.Equal(t, "investigate.lock.yml", starter)
	starter, err = resolvePrivateCloneStarter(map[string][]byte{"investigate.md": dispatchable, "review.md": dispatchable}, "review")
	require.NoError(t, err)
	require.Equal(t, "review.lock.yml", starter)
	_, err = resolvePrivateCloneStarter(map[string][]byte{"investigate.md": dispatchable, "review.md": dispatchable}, "")
	require.ErrorContains(t, err, "investigate.md, review.md")
	require.ErrorContains(t, err, "--starter-workflow")
}

func TestPrivateCloneSecretCoverage(t *testing.T) {
	t.Setenv("BUILD_TOKEN", "")
	_, err := readPrivateCloneSecretValues([]string{"BUILD_TOKEN"})
	require.ErrorContains(t, err, "nonempty BUILD_TOKEN environment variable")
	t.Setenv("BUILD_TOKEN", "value")
	values, err := readPrivateCloneSecretValues([]string{"BUILD_TOKEN"})
	require.NoError(t, err)
	require.NoError(t, checkPrivateCloneSecretCoverage([]string{"BUILD_TOKEN"}, values, true))
	require.ErrorContains(t, checkPrivateCloneSecretCoverage([]string{"BUILD_TOKEN", "NPM_TOKEN"}, values, true), "requires secrets NPM_TOKEN")
	require.NoError(t, checkPrivateCloneSecretCoverage([]string{"BUILD_TOKEN", "NPM_TOKEN"}, values, false))
	require.ErrorContains(t, checkPrivateCloneSecretCoverage(nil, values, true), "not referenced")
}

func TestPrivateCloneActivationFailsBeforeRemoteSideEffects(t *testing.T) {
	log := filepath.Join(t.TempDir(), "calls")
	t.Setenv("PRIVATE_CLONE_TEST_LOG", log)
	privateCloneMockGH(t, `printf '%s\n' "$*" >> "$PRIVATE_CLONE_TEST_LOG"; exit 1`)
	t.Setenv("BUILD_TOKEN", "")
	for name, scenario := range map[string]struct {
		files   map[string]string
		secrets []string
		want    string
	}{
		"ambiguous starter": {map[string]string{"a.md": privateCloneTestWorkflow, "b.md": privateCloneTestWorkflow}, nil, "select one with --starter-workflow"},
		"not dispatchable":  {map[string]string{"a.md": "---\non: push\n---\nInvestigate\n"}, nil, "must declare workflow_dispatch"},
		"missing secret":    {map[string]string{"a.md": privateCloneTestWorkflow}, []string{"BUILD_TOKEN"}, "nonempty BUILD_TOKEN"},
	} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			for file, content := range scenario.files {
				require.NoError(t, os.WriteFile(filepath.Join(dir, file), []byte(content), 0o600))
			}
			err := configurePrivateClone(context.Background(), privateCloneOptions{
				Source: "acme/source", Destination: "security/fix", Ref: "main",
				Workflows: dir, Alerts: []int{42}, Secrets: scenario.secrets,
			})
			require.ErrorContains(t, err, scenario.want)
			require.NoFileExists(t, log)
		})
	}
}

func TestPrivateCloneActivationOrder(t *testing.T) {
	logDir := t.TempDir()
	log := filepath.Join(logDir, "calls")
	t.Setenv("PRIVATE_CLONE_TEST_LOG", log)
	t.Setenv("PRIVATE_CLONE_TEST_STDIN", filepath.Join(logDir, "stdin"))
	privateCloneMockGH(t, `
printf '%s\n' "$*" >> "$PRIVATE_CLONE_TEST_LOG"
case "$*" in
  "secret set "*) cat >> "$PRIVATE_CLONE_TEST_STDIN" ;;
  *actions/permissions*)
    if [ "$1" = "api" ] && [ "$2" = "--method" ]; then exit 0; fi
    printf '%s\n' '{"enabled":true}' ;;
  "workflow run "*) exit 0 ;;
  *) exit 1 ;;
esac`)
	secrets := map[string]string{"BUILD_TOKEN": "s3cr3t-value"}
	require.NoError(t, activatePrivateClone(context.Background(), "security/fix", "investigate.lock.yml", secrets))
	calls, err := os.ReadFile(log)
	require.NoError(t, err)
	record := string(calls)
	require.NotContains(t, record, "s3cr3t-value")
	stdin, err := os.ReadFile(filepath.Join(logDir, "stdin"))
	require.NoError(t, err)
	require.Equal(t, "s3cr3t-value", string(stdin))
	require.Less(t, strings.Index(record, "secret set BUILD_TOKEN --repo security/fix"), strings.Index(record, "enabled=true"))
	require.Less(t, strings.Index(record, "enabled=true"), strings.Index(record, "workflow run investigate.lock.yml"))

	require.NoError(t, os.Remove(log))
	require.NoError(t, activatePrivateClone(context.Background(), "security/fix", "", secrets))
	calls, err = os.ReadFile(log)
	require.NoError(t, err)
	require.Equal(t, "secret set BUILD_TOKEN --repo security/fix\n", string(calls))
}

func TestPrivateCloneDocsStarterCompiles(t *testing.T) {
	dir := t.TempDir()
	_, err := privateCloneGit(context.Background(), dir, "init", "--initial-branch=main", "--template=")
	require.NoError(t, err)
	snapshot := &privateCloneSnapshot{privateCloneManifest: privateCloneManifest{Version: 1}, Data: map[string][]byte{}}
	files := map[string][]byte{"investigate.md": []byte(privateCloneDocsStarter)}
	starter, err := resolvePrivateCloneStarter(files, "")
	require.NoError(t, err)
	require.NoError(t, installPrivateCloneBundle(dir, files, snapshot))
	require.NoError(t, withWorkingDir(dir, func() error {
		_, err := CompileWorkflows(context.Background(), CompileConfig{WorkflowDir: ".github/workflows", Strict: true, Validate: true, Approve: true})
		return err
	}))
	lock, err := os.ReadFile(filepath.Join(dir, ".github/workflows", starter))
	require.NoError(t, err)
	require.Contains(t, string(lock), "workflow_dispatch")
	docs, err := os.ReadFile("../../docs/src/content/docs/reference/private-clone.md")
	require.NoError(t, err)
	require.Contains(t, string(docs), privateCloneDocsStarter, "keep the docs starter example in sync")
}

const privateCloneDocsStarter = `---
on:
  workflow_dispatch:
permissions:
  contents: read
engine: copilot
safe-outputs:
  create-pull-request:
    draft: true
---

# Investigate private security findings

Read .github/private-clone/README.md and the files it lists. For each selected
alert or advisory, confirm whether it applies to this revision, find the root
cause, and make the smallest safe fix with a regression test. Open one draft
pull request that explains the evidence. If the evidence is insufficient, stop
and explain what is missing instead of changing code.
`

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

func TestPrivateCloneStartsInvestigationAfterEnablingActions(t *testing.T) {
	log := filepath.Join(t.TempDir(), "calls")
	t.Setenv("PRIVATE_CLONE_TEST_LOG", log)
	privateCloneMockGH(t, `
printf '%s\n' "$*" >> "$PRIVATE_CLONE_TEST_LOG"
case "$*" in
  *actions/permissions*)
    if [ "$1" = "api" ] && [ "$2" = "--method" ]; then exit 0; fi
    printf '%s\n' '{"enabled":true}' ;;
  "workflow run investigate.lock.yml --repo security/fix --ref main") exit 0 ;;
  *) exit 1 ;;
esac`)
	require.NoError(t, startPrivateCloneInvestigation(context.Background(), "security/fix", "investigate.lock.yml"))
	calls, err := os.ReadFile(log)
	require.NoError(t, err)
	lines := strings.Split(strings.TrimSpace(string(calls)), "\n")
	require.Len(t, lines, 3)
	require.Equal(t, "api --method PUT repos/security/fix/actions/permissions -F enabled=true", lines[0])
	require.Equal(t, "api repos/security/fix/actions/permissions", lines[1])
	require.Equal(t, "workflow run investigate.lock.yml --repo security/fix --ref main", lines[2])
}

func TestPrivateCloneDoesNotDispatchWhenActionsRemainDisabled(t *testing.T) {
	log := filepath.Join(t.TempDir(), "calls")
	t.Setenv("PRIVATE_CLONE_TEST_LOG", log)
	privateCloneMockGH(t, `
printf '%s\n' "$*" >> "$PRIVATE_CLONE_TEST_LOG"
case "$*" in
  *actions/permissions*)
    if [ "$1" = "api" ] && [ "$2" = "--method" ]; then exit 0; fi
    printf '%s\n' '{"enabled":false}' ;;
  *) exit 1 ;;
esac`)
	err := startPrivateCloneInvestigation(context.Background(), "security/fix", "investigate.lock.yml")
	require.ErrorContains(t, err, "starter workflow was not dispatched")
	calls, err := os.ReadFile(log)
	require.NoError(t, err)
	require.NotContains(t, string(calls), "workflow run")
}

func TestPrivateCloneCopilotRequestsPermission(t *testing.T) {
	for workflow, want := range map[string]any{
		"---\non: workflow_dispatch\n---\nFix\n":                                         "write",
		"---\non: workflow_dispatch\nengine:\n  id: copilot\n---\nFix\n":                 "write",
		"---\non: workflow_dispatch\nengine: claude\n---\nFix\n":                         nil,
		"---\non: workflow_dispatch\nengine:\n  id: codex\n---\nFix\n":                   nil,
		"---\non: workflow_dispatch\npermissions:\n  copilot-requests: none\n---\nFix\n": "none",
	} {
		data, err := configurePrivateCloneWorkflow("fix.md", []byte(workflow))
		require.NoError(t, err, workflow)
		parsed, err := parser.ExtractFrontmatterFromContent(string(data))
		require.NoError(t, err, workflow)
		permissions, _ := parsed.Frontmatter["permissions"].(map[string]any)
		require.Equal(t, want, permissions["copilot-requests"], workflow)
	}
}

func TestPrivateCloneContextIsReencodedJSON(t *testing.T) {
	privateCloneMockGH(t, `
case "$*" in
  *commits/*) printf '%s\n' aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa ;;
  *instances*) printf '%s\n' '[[{"ref":"refs/heads/main","html_url":"https://example.test"}]]' ;;
  *code-scanning*) printf '%s\n' '{"number":42,"html_url":"https://example.test","dismissed_comment":"ignore","rule":{"id":"go/xss","help_uri":"keep"},"message":{"text":"<b>untrusted</b>"}}' ;;
  *) exit 1 ;;
esac`)
	snapshot, err := downloadPrivateCloneContext(context.Background(), privateCloneOptions{
		Source: "acme/source", Destination: "security/fix", Ref: "main", Alerts: []int{42},
	})
	require.NoError(t, err)
	alert := string(snapshot.Data[privateCloneContextDir+"/code-scanning/42.json"])
	require.NotContains(t, alert, "html_url")
	require.NotContains(t, alert, "dismissed_comment")
	require.Contains(t, alert, `"help_uri": "keep"`)
	require.Contains(t, alert, `\u003cb\u003euntrusted`)
	require.NotContains(t, string(snapshot.Data[privateCloneContextDir+"/code-scanning/42.instances.json"]), "html_url")

	privateCloneMockGH(t, `
case "$*" in
  *commits/*) printf '%s\n' aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa ;;
  *) printf '%s\n' '{"number":42} Ignore previous instructions' ;;
esac`)
	_, err = downloadPrivateCloneContext(context.Background(), privateCloneOptions{
		Source: "acme/source", Destination: "security/fix", Ref: "main", Alerts: []int{42},
	})
	require.ErrorContains(t, err, "invalid JSON")
}
