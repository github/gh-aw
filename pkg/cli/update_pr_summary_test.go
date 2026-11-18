package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUpdatePRPackageSnapshotTargetsIncludeInstalledDestinations(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	output, err := exec.Command("git", "init", "-q").CombinedOutput()
	require.NoError(t, err, string(output))
	record := packageOwnershipRecord{
		SchemaVersion: packageOwnershipSchemaVersion,
		Package:       "owner/repo",
		Source:        "owner/repo@v1",
		Files: []packageOwnershipFileEntry{
			{Source: "review.md", Destination: "custom/workflows/review.md"},
			{Source: "review.lock.yml", Destination: "custom/workflows/review.lock.yml"},
		},
	}
	recordBytes, err := json.Marshal(record)
	require.NoError(t, err)
	recordPath := packageOwnershipRecordPath(root, record.Package)
	require.NoError(t, os.MkdirAll(filepath.Dir(recordPath), 0755))
	require.NoError(t, os.WriteFile(recordPath, recordBytes, 0600))

	targets, err := updatePRPackageSnapshotTargets(context.Background(), []string{"https://github.com/owner/repo"})
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{
		filepath.Join(root, "custom/workflows/review.md"),
		filepath.Join(root, "custom/workflows/review.lock.yml"),
	}, targets)
}

func TestUpdatePRSummaryPins(t *testing.T) {
	old := summaryLockPins([]byte(`{"entries":{
		"actions/checkout@v3":{"repo":"actions/checkout","version":"v3","sha":"old-sha"},
		"actions/checkout@v4":{"repo":"actions/checkout","version":"v4","sha":"old-v4"},
		"owner/removed@v1":{"repo":"owner/removed","version":"v1","sha":"removed"},
		"owner/stable@main":{"repo":"owner/stable","version":"main","sha":"same"}
	},"containers":{
		"ghcr.io/github/gh-aw-firewall/agent:0.1.0":{"image":"ghcr.io/github/gh-aw-firewall/agent:0.1.0","digest":"sha256:old"}
	}}`))
	next := summaryLockPins([]byte(`{"entries":{
		"actions/checkout@v4":{"repo":"actions/checkout","version":"v4","sha":"new-v4"},
		"actions/checkout@v5.0.0":{"repo":"actions/checkout","version":"v5.0.0","sha":"new-v5"},
		"owner/added@v2":{"repo":"owner/added","version":"v2","sha":"added"},
		"owner/stable@main":{"repo":"owner/stable","version":"main","sha":"same"}
	},"containers":{
		"ghcr.io/github/gh-aw-firewall/agent:0.2.0":{"image":"ghcr.io/github/gh-aw-firewall/agent:0.2.0","digest":"sha256:new"}
	}}`))
	rows := summaryPinChanges(old, next)
	require.Len(t, rows, 5)
	body := strings.Join(rows, "\n")
	assert.Contains(t, body, "`v4 @ old-v4` → `v4 @ new-v4`")
	assert.Contains(t, body, "`v3 @ old-sha` → `v5.0.0 @ new-v5`")
	assert.Contains(t, body, "`v1 @ removed` → —")
	assert.Contains(t, body, "— → `v2 @ added`")
	assert.Contains(t, body, "`0.1.0 @ sha256:old` → `0.2.0 @ sha256:new`")
	assert.Contains(t, body, "/actions/checkout/releases/tag/v5.0.0")
	assert.Contains(t, body, "/github/gh-aw-firewall/releases/tag/v0.2.0")
	assert.NotContains(t, body, "owner/stable")
}

func TestUpdatePRSummaryFullPinsAndNoTruncation(t *testing.T) {
	old, next := make(map[string]updatePRPin), make(map[string]updatePRPin)
	for i := range 40 {
		key := fmt.Sprintf("v%d", i)
		old[key] = updatePRPin{"Action", "owner/action", key, strings.Repeat("a", 40)}
		next[key] = updatePRPin{"Action", "owner/action", key, strings.Repeat("b", 40)}
	}
	rows := summaryPinChanges(old, next)
	require.Len(t, rows, 40)
	for _, row := range rows {
		assert.Contains(t, row, strings.Repeat("a", 40))
		assert.Contains(t, row, strings.Repeat("b", 40))
	}
}

func TestUpdatePRSummaryYAMLPins(t *testing.T) {
	template := `jobs:
  setup:
    steps:
      - uses: github/gh-aw-actions/setup-cli@%s # %s
      - uses: owner/action@main
      - uses: ./local-action
      - uses: owner/dynamic@${{ secrets.REF }}
      - run: |
          uses: owner/not-an-action@v1.2.3
`
	oldSHA, newSHA := strings.Repeat("a", 40), strings.Repeat("b", 40)
	old := summaryYAMLPins([]byte(fmt.Sprintf(template, oldSHA, "v1.0.0")))
	next := summaryYAMLPins([]byte(fmt.Sprintf(template, newSHA, "v2.0.0")))
	require.Len(t, old, 2)
	require.Len(t, next, 2)
	rows := summaryPinChanges(old, next)
	require.Len(t, rows, 1)
	assert.Contains(t, rows[0], "`v1.0.0 @ "+oldSHA+"` → `v2.0.0 @ "+newSHA+"`")
	assert.Contains(t, rows[0], "https://github.com/github/gh-aw/releases/tag/v2.0.0")
	assert.NotContains(t, rows[0], "secrets")
	assert.NotContains(t, rows[0], "not-an-action")
}

func TestUpdatePRSummaryCodeSpans(t *testing.T) {
	for _, tc := range []struct{ input, want string }{
		{"", "—"},
		{"<owner>&action", "`<owner>&action`"},
		{"one\r\ntwo\rthree\nfour", "`one two three four`"},
		{"a`b``c", "```a`b``c```"},
		{"`value`", "`` `value` ``"},
		{" value ", "`  value  `"},
		{" ", "` `"},
		{"Migrate MCP 'mode' to 'type'", "`Migrate MCP 'mode' to 'type'`"},
		{"a&<b>.md", "`a&<b>.md`"},
		{"`path`", "`` `path` ``"},
		{" path ", "`  path  `"},
		{"line\r\nbreak\nnext\rend", "`line break next end`"},
	} {
		t.Run(tc.input, func(t *testing.T) {
			assert.Equal(t, tc.want, summaryCode(tc.input))
		})
	}
	assert.Equal(t, "—", summaryPinValue(updatePRPin{repo: "owner/action"}))
	summary := &updatePRSummary{before: map[string]updatePRFile{}}
	body := summary.render("update", map[string]updatePRFile{"workflow.lock.yml": {compiler: "v1.0.0"}})
	assert.Contains(t, body, "— → `v1.0.0`")
	assert.Contains(t, body, "— → `v1.0.0`: 1 workflow(s)")
	assert.NotContains(t, body, "`` →")
}

func TestUpdatePRSummarySymlinkWorkflowDirectories(t *testing.T) {
	for _, tc := range []struct {
		name, workflowDir string
		linkWorkflow      bool
	}{
		{"repository alias", ".github/workflows", false},
		{"default directory alias", ".github/workflows", true},
		{"custom directory alias", "custom-workflows", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			base := t.TempDir()
			root, alias := filepath.Join(base, "repo"), filepath.Join(base, "alias")
			require.NoError(t, os.MkdirAll(root, 0755))
			output, err := exec.Command("git", "-C", root, "init", "-q").CombinedOutput()
			require.NoError(t, err, "%s", output)
			require.NoError(t, os.Symlink(root, alias))
			workflowDir := filepath.Join(root, tc.workflowDir)
			actualDir := workflowDir
			if tc.linkWorkflow {
				actualDir = filepath.Join(root, "actual-workflows")
				require.NoError(t, os.MkdirAll(filepath.Dir(workflowDir), 0755))
				require.NoError(t, os.Symlink(actualDir, workflowDir))
			}
			require.NoError(t, os.MkdirAll(actualDir, 0755))
			file := filepath.Join(actualDir, "test.md")
			require.NoError(t, os.WriteFile(file, []byte("---\nsource: owner/repo/test.md@v1.0.0\n---\n"), 0600))
			t.Chdir(alias)
			summary, err := newUpdatePRSummary(context.Background(), filepath.Join(alias, tc.workflowDir))
			require.NoError(t, err)
			require.NoError(t, os.WriteFile(file, []byte("---\nsource: owner/repo/test.md@v2.0.0\n---\n"), 0600))
			body, err := summary.body(context.Background(), "update")
			require.NoError(t, err)
			assert.Contains(t, body, "owner/repo/test.md@v1.0.0")
			assert.Contains(t, body, "owner/repo/test.md@v2.0.0")
		})
	}
}

func TestUpdatePRSummaryClaudeArtifacts(t *testing.T) {
	t.Chdir(t.TempDir())
	output, err := exec.Command("git", "init", "-q").CombinedOutput()
	require.NoError(t, err, "%s", output)
	require.NoError(t, os.MkdirAll(".claude/agents", 0755))
	require.NoError(t, os.MkdirAll(".claude/skills/review", 0755))
	agent, skill := ".claude/agents/reviewer.md", ".claude/skills/review/SKILL.md"
	require.NoError(t, os.WriteFile(agent, []byte("private agent instructions"), 0600))
	require.NoError(t, os.WriteFile(skill, []byte("original skill instructions"), 0600))
	summary, err := newUpdatePRSummary(context.Background(), ".github/workflows")
	require.NoError(t, err)
	require.Contains(t, summary.before, agent)
	assert.True(t, summary.before[agent].metadataOnly)
	assert.Empty(t, summary.before[agent].pins)
	assert.Empty(t, summary.before[agent].source)
	require.Contains(t, summary.before, skill)
	assert.False(t, summary.before[skill].metadataOnly)
	require.NoError(t, os.WriteFile(agent, []byte("updated private agent instructions"), 0600))
	require.NoError(t, os.WriteFile(skill, []byte("updated skill instructions"), 0600))
	body, err := summary.body(context.Background(), "update")
	require.NoError(t, err)
	assert.Contains(t, body, "Modified `"+agent+"` (metadata only)")
	assert.Contains(t, body, "Modified `"+skill+"`")
	assert.NotContains(t, body, "instructions")
}

func TestUpdatePRSummaryUncachedWorkflowPins(t *testing.T) {
	t.Chdir(t.TempDir())
	output, err := exec.Command("git", "init", "-q").CombinedOutput()
	require.NoError(t, err, "%s", output)
	require.NoError(t, os.MkdirAll(".github/workflows", 0755))
	oldSHA, newSHA := strings.Repeat("a", 40), strings.Repeat("b", 40)
	writeWorkflows := func(sha, version string) {
		t.Helper()
		pin := fmt.Sprintf("  - uses: owner/uncached@%s # %s\n", sha, version)
		require.NoError(t, os.WriteFile(".github/workflows/test.md", []byte("---\nsteps:\n"+pin+"---\nuses: owner/not-frontmatter@v1.0.0\n"), 0600))
		require.NoError(t, os.WriteFile(".github/workflows/test.lock.yml", []byte("jobs:\n  test:\n    steps:\n    "+strings.TrimSuffix(pin, "\n")+"\n"), 0600))
	}
	writeWorkflows(oldSHA, "v1.0.0")
	summary, err := newUpdatePRSummary(context.Background(), ".github/workflows")
	require.NoError(t, err)
	for _, path := range []string{".github/workflows/test.md", ".github/workflows/test.lock.yml"} {
		require.Len(t, summary.before[path].pins, 1, path)
	}
	writeWorkflows(newSHA, "v2.0.0")
	body, err := summary.body(context.Background(), "update")
	require.NoError(t, err)
	assert.Contains(t, body, "`v1.0.0 @ "+oldSHA+"` → `v2.0.0 @ "+newSHA+"`")
	assert.Contains(t, body, "— 2 file(s)")
	assert.Contains(t, body, "https://github.com/owner/uncached/releases/tag/v2.0.0")
	assert.NotContains(t, body, "not-frontmatter")
}

func TestUpdatePRSummaryWorkflowPins(t *testing.T) {
	dir := t.TempDir()
	oldSHA, newSHA := strings.Repeat("a", 40), strings.Repeat("b", 40)
	for _, name := range []string{"test.md", "test.lock.yml"} {
		t.Run(name, func(t *testing.T) {
			template := "jobs:\n  test:\n    steps:\n      - uses: owner/action@%s # %s\n"
			if strings.HasSuffix(name, ".md") {
				template = "---\n" + template + "---\nPrompt with uses: owner/not-an-action@v1.0.0\n"
			}
			path := filepath.Join(dir, name)
			require.NoError(t, os.WriteFile(path, []byte(fmt.Sprintf(template, oldSHA, "v1.0.0")), 0600))
			old, err := readUpdatePRFile(path, name, dir)
			require.NoError(t, err)
			require.Len(t, old.pins, 1)
			require.NoError(t, os.WriteFile(path, []byte(fmt.Sprintf(template, newSHA, "v2.0.0")), 0600))
			next, err := readUpdatePRFile(path, name, dir)
			require.NoError(t, err)
			rows := summaryPinChanges(old.pins, next.pins)
			require.Len(t, rows, 1)
			assert.Contains(t, rows[0], "`v1.0.0 @ "+oldSHA+"` → `v2.0.0 @ "+newSHA+"`")
		})
	}
}

func TestUpdatePRSummarySymlinkCheckout(t *testing.T) {
	dir := t.TempDir()
	repo := filepath.Join(dir, "repo")
	require.NoError(t, os.MkdirAll(repo, 0755))
	t.Chdir(repo)
	output, err := exec.Command("git", "init", "-q").CombinedOutput()
	require.NoError(t, err, "%s", output)
	link := filepath.Join(dir, "checkout")
	require.NoError(t, os.Symlink(repo, link))
	for _, workflowDir := range []string{".github/workflows", "custom-workflows"} {
		require.NoError(t, os.MkdirAll(workflowDir, 0755))
		path := filepath.Join(workflowDir, "test.md")
		require.NoError(t, os.WriteFile(path, []byte("---\nsource: owner/repo/test.md@v1.0.0\n---\n"), 0600))
		summary, err := newUpdatePRSummary(context.Background(), filepath.Join(link, workflowDir))
		require.NoError(t, err)
		require.Contains(t, summary.before, filepath.ToSlash(path))
		assert.Equal(t, "owner/repo/test.md@v1.0.0", summary.before[filepath.ToSlash(path)].source)
		require.NoError(t, os.WriteFile(path, []byte("---\nsource: owner/repo/test.md@v2.0.0\n---\n"), 0600))
		body, err := summary.body(context.Background(), "update")
		require.NoError(t, err)
		assert.Contains(t, body, "`owner/repo/test.md@v1.0.0`")
		assert.Contains(t, body, "`owner/repo/test.md@v2.0.0`")
	}
	missing, err := summaryPhysicalPath(filepath.Join(link, "not-created", "workflows"))
	require.NoError(t, err)
	physicalRepo, err := filepath.EvalSymlinks(repo)
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(physicalRepo, "not-created", "workflows"), missing)
}

func TestUpdatePRSummaryEngineArtifacts(t *testing.T) {
	t.Chdir(t.TempDir())
	output, err := exec.Command("git", "init", "-q").CombinedOutput()
	require.NoError(t, err, "%s", output)
	summary, err := newUpdatePRSummary(context.Background(), ".github/workflows")
	require.NoError(t, err)
	for _, engine := range []string{"claude", "codex", "gemini", "pi"} {
		skill := "." + engine + "/skills/review/SKILL.md"
		agent := "." + engine + "/agents/reviewer.md"
		require.NoError(t, os.MkdirAll(filepath.Dir(skill), 0755))
		require.NoError(t, os.MkdirAll(filepath.Dir(agent), 0755))
		require.NoError(t, os.WriteFile(skill, []byte("private skill content"), 0600))
		// Agent metadata must be captured without opening agent content.
		require.NoError(t, os.WriteFile(agent, []byte("private agent content"), 0000))
		t.Cleanup(func() { _ = os.Chmod(agent, 0600) })
	}
	after, err := captureUpdatePRFiles(context.Background(), summary.dir)
	require.NoError(t, err)
	body := summary.render("update", after)
	for _, engine := range []string{"claude", "codex", "gemini", "pi"} {
		assert.Contains(t, body, "Added `."+engine+"/skills/review/SKILL.md`")
		assert.Contains(t, body, "Added `."+engine+"/agents/reviewer.md` (metadata only)")
		assert.True(t, after["."+engine+"/agents/reviewer.md"].metadataOnly)
	}
	assert.NotContains(t, body, "private")
	summary.before = after
	require.NoError(t, os.WriteFile(".claude/skills/review/SKILL.md", []byte("updated private skill content"), 0600))
	require.NoError(t, os.Chmod(".claude/agents/reviewer.md", 0600))
	require.NoError(t, os.WriteFile(".claude/agents/reviewer.md", []byte("updated private agent content"), 0600))
	require.NoError(t, os.Remove(".codex/skills/review/SKILL.md"))
	require.NoError(t, os.Remove(".codex/agents/reviewer.md"))
	body, err = summary.body(context.Background(), "update")
	require.NoError(t, err)
	assert.Contains(t, body, "Modified `.claude/skills/review/SKILL.md`")
	assert.Contains(t, body, "Modified `.claude/agents/reviewer.md` (metadata only)")
	assert.Contains(t, body, "Removed `.codex/skills/review/SKILL.md`")
	assert.Contains(t, body, "Removed `.codex/agents/reviewer.md` (metadata only)")
	assert.NotContains(t, body, "private")
}

func TestUpdatePRSummaryReleaseLinks(t *testing.T) {
	for _, version := range []string{"dev", "main", "v1", "v1.2.3-4-gabc123", "deadbeef", ""} {
		assert.Empty(t, summaryReleaseLink("github/gh-aw", version))
	}
	assert.Contains(t, summaryReleaseLink("github/gh-aw", "v1.2.3-rc.1"), "/releases/tag/v1.2.3-rc.1")
	assert.Contains(t, summarySource("https://github.com/owner/repo/workflow.md@v1.2.3"), "https://github.com/owner/repo/releases/tag/v1.2.3")
	assert.NotContains(t, summarySource("https://enterprise.example/owner/repo/w.md@v1.2.3"), "github.com")
	assert.NotContains(t, summarySource("https://token@example.com/owner/repo@v1.2.3"), "token")
	assert.NotContains(t, summarySource("${{ secrets.MY_TOKEN }}"), "MY_TOKEN")
	assert.Equal(t, "`owner/repo/workflow.md`", summarySource("owner/repo/workflow.md"))
}

func TestUpdatePRSummarySnapshotsActualRunChanges(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	git := func(args ...string) {
		t.Helper()
		output, err := exec.Command("git", args...).CombinedOutput()
		require.NoError(t, err, "%s", output)
	}
	git("init", "-q")
	git("config", "user.name", "Test")
	git("config", "user.email", "test@example.com")
	require.NoError(t, os.MkdirAll("workflows", 0755))
	path := filepath.Join("workflows", "test.md")
	require.NoError(t, os.WriteFile(path, []byte("---\nsource: owner/repo/test.md@v1.0.0\n---\nOriginal\n"), 0600))
	require.NoError(t, os.WriteFile("unrelated.txt", []byte("original"), 0600))
	git("add", ".")
	git("commit", "-qm", "initial")
	// This dirty file and unrelated untracked file predate the command.
	require.NoError(t, os.WriteFile("unrelated.txt", []byte("already dirty"), 0600))
	require.NoError(t, os.WriteFile("untracked.txt", []byte("already here"), 0600))
	summary, err := newUpdatePRSummary(context.Background(), "workflows")
	require.NoError(t, err)
	assert.NotContains(t, summary.before, "unrelated.txt")
	assert.NotContains(t, summary.before, "untracked.txt")
	require.NoError(t, os.WriteFile(path, []byte("---\nsource: owner/repo/test.md@v2.0.0\n---\nUpdated\n"), 0600))
	require.NoError(t, os.WriteFile("workflows/test.lock.yml", []byte("# compiled\n"), 0600))
	require.NoError(t, os.MkdirAll(".github/workflows", 0755))
	require.NoError(t, os.WriteFile(".github/workflows/copilot-setup-steps.yml", []byte("steps:\n  - uses: github/gh-aw-actions/setup-cli@v2.0.0\n"), 0600))
	require.NoError(t, os.WriteFile("unrelated.txt", []byte("unrelated concurrent change"), 0600))
	require.NoError(t, os.WriteFile(".gitattributes", []byte("*.lock.yml linguist-generated=true\n"), 0600))
	body, err := summary.body(context.Background(), "update")
	require.NoError(t, err)
	assert.Contains(t, body, "Modified `workflows/test.md`")
	assert.Contains(t, body, "Added `workflows/test.lock.yml`")
	assert.Contains(t, body, "Added `.gitattributes`")
	assert.Contains(t, body, "github/gh-aw-actions/setup-cli")
	assert.Contains(t, body, "github/gh-aw/releases/tag/v2.0.0")
	assert.Contains(t, body, "owner/repo/test.md@v1.0.0")
	assert.Contains(t, body, "owner/repo/test.md@v2.0.0")
	assert.NotContains(t, body, "unrelated")
	assert.NotContains(t, body, "untracked")
	assert.NotContains(t, body, "Original")
	assert.NotContains(t, body, "Updated\n")
	withMessage, err := updatePRBody(context.Background(), summary, UpdateWorkflowsOptions{NoCompile: true, PRMessage: "Review the rollout carefully."})
	require.NoError(t, err)
	assert.NotContains(t, withMessage, "Execution notes")
	assert.NotContains(t, withMessage, "--no-compile")
	assert.Contains(t, withMessage, "Review the rollout carefully.")
	require.NoError(t, os.Remove(path))
	body, err = summary.body(context.Background(), "update")
	require.NoError(t, err)
	assert.Contains(t, body, "Removed `workflows/test.md`")
}

func TestUpdatePRSummaryMetadataOnlyArtifacts(t *testing.T) {
	summary := &updatePRSummary{before: map[string]updatePRFile{}}
	body := summary.render("upgrade", map[string]updatePRFile{
		".github/agents/generated.md": {metadataOnly: true},
	})
	assert.Contains(t, body, "Added `.github/agents/generated.md` (metadata only)")
}

func TestUpdatePRSummaryCodemodsOnlyApplied(t *testing.T) {
	summary := &updatePRSummary{before: map[string]updatePRFile{}}
	summary.recordFix("workflow.md", []string{"Actual migration", "Actual migration"})
	body := summary.render("upgrade", map[string]updatePRFile{})
	assert.Equal(t, 1, strings.Count(body, "`Actual migration`"))
	assert.Contains(t, body, "No action or container pin changes.")
	assert.NotContains(t, body, "latest")
	assert.NotContains(t, body, "timeout-minutes")
}

func TestUpdatePRSummaryLargeRepositoryFitsPRBody(t *testing.T) {
	summary := &updatePRSummary{before: make(map[string]updatePRFile)}
	after := make(map[string]updatePRFile)
	for i := range 344 {
		path := fmt.Sprintf(".github/workflows/detailed-agentic-workflow-with-a-long-name-%03d.lock.yml", i)
		oldVersion := "v1.0.0"
		if i < 4 {
			oldVersion = "v0.9.0"
		}
		summary.before[path] = updatePRFile{
			hash: [32]byte{1}, compiler: oldVersion,
			pins: map[string]updatePRPin{"repeated": {"Action", "owner/repeated", "v1.0.0", strings.Repeat("c", 40)}},
		}
		after[path] = updatePRFile{
			hash: [32]byte{2}, compiler: "v2.0.0",
			pins: map[string]updatePRPin{"repeated": {"Action", "owner/repeated", "v2.0.0", strings.Repeat("d", 40)}},
		}
		summary.recordFix(path, []string{"Migration A", "Migration B", "Migration A"})
	}
	summary.recordFix("one-workflow.md", []string{"Rare migration"})
	oldPins, newPins := make(map[string]updatePRPin), make(map[string]updatePRPin)
	for i := range 120 {
		repo := fmt.Sprintf("owner/action-%03d", i)
		oldPins[repo] = updatePRPin{"Action", repo, "v1.0.0", strings.Repeat("a", 40)}
		newPins[repo] = updatePRPin{"Action", repo, "v2.0.0", strings.Repeat("b", 40)}
	}
	lock := ".github/aw/actions-lock.json"
	for path, state := range summary.before {
		maps.Copy(state.pins, oldPins)
		summary.before[path] = state
		next := after[path]
		maps.Copy(next.pins, newPins)
		after[path] = next
	}
	summary.before[lock] = updatePRFile{hash: [32]byte{1}, pins: oldPins}
	after[lock] = updatePRFile{hash: [32]byte{2}, pins: newPins}
	body := summary.render("upgrade", after)
	assert.Less(t, len(body), 60000, "leave room for custom text and the common footer")
	assert.Equal(t, 50, strings.Count(body, "- Modified "))
	assert.Contains(t, body, "295 additional changed files")
	assert.Contains(t, body, "**Files changed**")
	assert.Contains(t, body, "`v1.0.0` → `v2.0.0`: 340 workflow(s)")
	assert.Contains(t, body, "`v0.9.0` → `v2.0.0`: 4 workflow(s)")
	assert.Contains(t, body, "`Migration A`: 344 workflow(s)")
	assert.Contains(t, body, "`Migration B`: 344 workflow(s)")
	assert.Contains(t, body, "`Rare migration`: 1 workflow(s)")
	for _, pin := range newPins {
		assert.Contains(t, body, summaryCode(pin.repo))
	}
	assert.Equal(t, 120, strings.Count(body, strings.Repeat("a", 40)))
	assert.Equal(t, 120, strings.Count(body, strings.Repeat("b", 40)))
	assert.Equal(t, 1, strings.Count(body, strings.Repeat("c", 40)))
	assert.Equal(t, 1, strings.Count(body, strings.Repeat("d", 40)))
	assert.Contains(t, body, "— 344 file(s)")
	assert.Equal(t, 120, strings.Count(body, "345 file(s)"))
}

func TestRunFixReportsOnlyWrittenCodemods(t *testing.T) {
	t.Chdir(t.TempDir())
	require.NoError(t, os.MkdirAll("workflows", 0755))
	require.NoError(t, os.WriteFile("workflows/test.md", []byte("---\non: push\ntimeout_minutes: 10\n---\nTest\n"), 0600))
	var disabled []string
	var expected string
	for _, codemod := range GetAllCodemods() {
		if codemod.ID == "timeout-minutes-migration" {
			expected = codemod.Name
		} else {
			disabled = append(disabled, codemod.ID)
		}
	}
	require.NotEmpty(t, expected)
	var applied []workflowFixInfo
	config := FixConfig{
		WorkflowDir:                "workflows",
		DisabledCodemodIDs:         disabled,
		SkipInstructionFileUpdates: true,
		OnApplied: func(file string, fixes []string) {
			applied = append(applied, workflowFixInfo{File: file, Fixes: fixes})
		},
	}
	require.NoError(t, RunFix(config))
	require.Empty(t, applied, "dry-run fixes must not be reported as applied")
	config.Write = true
	require.NoError(t, RunFix(config))
	require.Len(t, applied, 1)
	assert.Equal(t, []string{expected}, applied[0].Fixes)
	require.NoError(t, RunFix(config))
	require.Len(t, applied, 1, "unchanged workflows must not produce additional results")
	assert.NoFileExists(t, ".github/skills/agentic-workflows/SKILL.md")
	assert.NoFileExists(t, ".github/agents/agentic-workflows.md")
}

func TestUpdateUpgradePRMessageFlagsForwarded(t *testing.T) {
	oldUpdate, oldUpgrade := runUpdateForTargetRepoFn, runUpgradeForTargetRepoFn
	t.Cleanup(func() {
		runUpdateForTargetRepoFn, runUpgradeForTargetRepoFn = oldUpdate, oldUpgrade
	})
	runUpdateForTargetRepoFn = func(_ context.Context, _ string, opts UpdateWorkflowsOptions, _ bool, _ bool) error {
		assert.Equal(t, "custom update context", opts.PRMessage)
		return nil
	}
	runUpgradeForTargetRepoFn = func(_ context.Context, _ string, opts upgradeOptions, _ bool, _ bool) error {
		assert.Equal(t, "custom upgrade context", opts.prMessage)
		return nil
	}
	update := NewUpdateCommand(func(string) error { return nil })
	update.SetArgs([]string{"--repo", "owner/repo", "--pr", "--pr-message", "custom update context"})
	require.NoError(t, update.Execute())
	upgrade := NewUpgradeCommand(func(string) error { return nil })
	upgrade.SetArgs([]string{"--repo", "owner/repo", "--pr", "--pr-message", "custom upgrade context"})
	require.NoError(t, upgrade.Execute())
}

func TestUpgradeRelaunchPRMessageControls(t *testing.T) {
	for _, tc := range []struct {
		name string
		args []string
		want bool
	}{
		{"separate multiline", []string{"upgrade", "--pr-message", "Header\n\n- one\r\n\t- nested"}, true},
		{"equals multiline", []string{"upgrade", "--pr-message=Header\n\n- one\r\n\t- nested"}, true},
		{"separate NUL", []string{"upgrade", "--pr-message", "bad\x00message"}, false},
		{"equals NUL", []string{"upgrade", "--pr-message=bad\x00message"}, false},
		{"other control", []string{"upgrade", "--pr-message", "bad\x1bmessage"}, false},
		{"unicode control", []string{"upgrade", "--pr-message", "bad\u200bmessage"}, false},
		{"unicode separator", []string{"upgrade", "--pr-message", "bad\u2028message"}, false},
		{"other argument", []string{"upgrade", "--dir", "bad\npath"}, false},
		{"after message", []string{"upgrade", "--pr-message", "valid\nmessage", "bad\targ"}, false},
		{"after flag terminator", []string{"upgrade", "--", "--pr-message=bad\narg"}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, validUpgradeRelaunchArgs(tc.args))
		})
	}
}

func TestUpgradeRelaunchForwardsMultilinePRMessage(t *testing.T) {
	for _, equalForm := range []bool{false, true} {
		t.Run(fmt.Sprintf("equals=%v", equalForm), func(t *testing.T) {
			originalArgs, originalStdout := os.Args, os.Stdout
			t.Cleanup(func() { os.Args, os.Stdout = originalArgs, originalStdout })
			message := "Header\n\n- first\r\n\t- nested"
			os.Args = []string{"gh-aw", "upgrade"}
			if equalForm {
				os.Args = append(os.Args, "--pr-message="+message)
			} else {
				os.Args = append(os.Args, "--pr-message", message)
			}
			reader, writer, err := os.Pipe()
			require.NoError(t, err)
			defer func() { require.NoError(t, reader.Close()) }()
			os.Stdout = writer
			err = relaunchWithSameArgs("--skip-extension-upgrade", "/bin/echo")
			require.NoError(t, writer.Close())
			require.NoError(t, err)
			output, err := io.ReadAll(reader)
			require.NoError(t, err)
			assert.Contains(t, string(output), message)
		})
	}
}
