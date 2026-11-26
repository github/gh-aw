package lint

import (
	"context"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/workflow/aw"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Requirement IDs refer to specs/file-access-lint-spec.md.

type fileAccessCase struct {
	name     string
	fields   string
	prompt   string
	resolved map[string]any
	// want lists expected finding kinds by message fragment, in order.
	want  []string
	lines []int
}

const (
	findingWrite     = "neither the edit tool"
	findingReadOnly  = "mounts read-only"
	findingDocker    = "hides the Docker socket"
	findingCache     = "cache-memory"
	findingRepo      = "tools.repo-memory is not configured"
	findingProtected = "protected file"
)

func runFileAccess(t *testing.T, tt fileAccessCase) []Diagnostic {
	t.Helper()
	document, err := aw.Parse("---\n" + tt.fields + "---\n" + tt.prompt)
	require.NoError(t, err)
	diagnostics, err := checkFileAccess(context.Background(), Input{Document: document, ResolvedTools: tt.resolved})
	require.NoError(t, err)
	return diagnostics
}

func assertFileAccess(t *testing.T, tt fileAccessCase) {
	t.Helper()
	diagnostics := runFileAccess(t, tt)
	require.Len(t, diagnostics, len(tt.want), "unexpected findings: %+v", diagnostics)
	for index, fragment := range tt.want {
		assert.Equal(t, "prompt", diagnostics[index].Path)
		assert.Contains(t, diagnostics[index].Message, fragment)
		assert.NotEmpty(t, diagnostics[index].Related, "findings must name the responsible configuration")
		if index < len(tt.lines) {
			assert.Equal(t, tt.lines[index], diagnostics[index].Line)
		}
	}
}

func runFileAccessCases(t *testing.T, cases []fileAccessCase) {
	t.Helper()
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			assertFileAccess(t, tt)
		})
	}
}

func TestFileAccessConformance(t *testing.T) {
	createNotes := "Create a file `notes/summary.md`."
	writeReport := "Write the report to `report.md`."
	install := "Install the tool into /usr/local/bin."
	cacheNotes := "Save notes in /tmp/gh-aw/cache-memory/notes.md."
	bumpPackage := "Update `package.json` to bump the version."
	blocked := "safe-outputs:\n  create-pull-request:\n    protected-files: blocked\n"
	runFileAccessCases(t, []fileAccessCase{
		{name: "C1 FAL-R1", fields: "tools:\n  edit: false\n  bash: false\n", prompt: createNotes, want: []string{findingWrite}, lines: []int{1}},
		{name: "C2 FAL-M2 defaults", prompt: createNotes},
		{name: "C3 FAL-M1 disabled sandbox", fields: "sandbox:\n  agent: false\n", prompt: writeReport, want: []string{findingWrite}},
		{name: "C4 FAL-M4 write-capable list", fields: "sandbox:\n  agent: false\ntools:\n  bash: [tee]\n", prompt: writeReport},
		{name: "C5 FAL-X2 negation", fields: "tools:\n  edit: false\n  bash: [cat]\n", prompt: "Do not write to `report.md`."},
		{name: "C6 FAL-R2", prompt: install, want: []string{findingReadOnly}},
		{name: "C7 FAL-R2 disabled sandbox", fields: "sandbox:\n  agent: false\ntools:\n  edit: true\n", prompt: install},
		{name: "C8 FAL-R3", prompt: "Run docker build to package the app.", want: []string{findingDocker}},
		{name: "C9 FAL-R3 shell fence", prompt: "Steps:\n\n```bash\ndocker run alpine\n```\n", want: []string{findingDocker}, lines: []int{4}},
		{name: "C10 FAL-R4", prompt: cacheNotes, want: []string{findingCache}},
		{name: "C11 FAL-M5 configured", fields: "tools:\n  cache-memory: true\n", prompt: cacheNotes},
		{name: "C12 FAL-R4 named cache", fields: "tools:\n  cache-memory:\n    - id: logs\n", prompt: "Use /tmp/gh-aw/cache-memory-other/.", want: []string{"cache-memory-other"}},
		{name: "C13 FAL-R5", fields: blocked, prompt: bumpPackage, want: []string{findingProtected}},
		{name: "C14 FAL-M6 exclude", fields: "safe-outputs:\n  create-pull-request:\n    protected-files:\n      policy: blocked\n      exclude: [package.json]\n", prompt: bumpPackage},
		{name: "C15 FAL-M7 imports pending", fields: "imports: [shared/tools.md]\ntools:\n  edit: false\n", prompt: createNotes},
		{name: "C16 FAL-M7 resolved tools", fields: "tools:\n  edit: false\n", prompt: createNotes, resolved: map[string]any{"edit": false, "bash": []any{"*"}}},
	})
}

func TestFileAccessCapabilityModel(t *testing.T) {
	createNotes := "Create a file `notes/summary.md`."
	runFileAccessCases(t, []fileAccessCase{
		{name: "FAL-M1 sandbox string stays enabled", fields: "sandbox: awf\n", prompt: "Run docker compose up.", want: []string{findingDocker}},
		{name: "FAL-M1 sandbox agent object stays enabled", fields: "sandbox:\n  agent:\n    id: awf\n", prompt: "Run docker compose up.", want: []string{findingDocker}},
		{name: "FAL-M2 explicit edit with disabled sandbox", fields: "sandbox:\n  agent: false\ntools:\n  edit:\n", prompt: createNotes},
		{name: "FAL-M2 pull-request output enables edit", fields: "sandbox:\n  agent: false\nsafe-outputs:\n  create-pull-request:\n", prompt: createNotes},
		{name: "FAL-M3 bash true writes", fields: "tools:\n  edit: false\n  bash: true\n", prompt: createNotes},
		{name: "FAL-M3 bash null keeps safe defaults", fields: "tools:\n  edit: false\n  bash:\n", prompt: createNotes, want: []string{findingWrite}},
		{name: "FAL-M3 sandbox default bash writes", fields: "tools:\n  edit: false\n", prompt: createNotes},
		{name: "FAL-M4 wildcard", fields: "tools:\n  edit: false\n  bash: [\"*\"]\n", prompt: createNotes},
		{name: "FAL-M4 git apply", fields: "tools:\n  edit: false\n  bash: [\"git apply:*\"]\n", prompt: createNotes},
		{name: "FAL-M4 git status cannot write", fields: "tools:\n  edit: false\n  bash: [\"git status\", \"cat\"]\n", prompt: createNotes, want: []string{findingWrite}},
		{name: "FAL-M5 named cache configured", fields: "tools:\n  cache-memory:\n    id: logs\n", prompt: "Read /tmp/gh-aw/cache-memory-logs/state.json first."},
		{name: "FAL-M5 default id maps to default path", fields: "tools:\n  cache-memory:\n    - id: default\n", prompt: "Read /tmp/gh-aw/cache-memory/state.json first."},
		{name: "FAL-M5 repo memory", fields: "tools:\n  repo-memory: true\n", prompt: "Append to /tmp/gh-aw/repo-memory/default/log.md."},
		{name: "FAL-M5 resolved cache memory", prompt: "Read /tmp/gh-aw/cache-memory/state.json.", resolved: map[string]any{"cache-memory": true, "edit": nil}},
		{name: "FAL-M6 any permissive writer allows", fields: "safe-outputs:\n  create-pull-request:\n    protected-files: blocked\n  push-to-pull-request-branch:\n", prompt: "Update `package.json`."},
		{name: "FAL-M6 engine instruction file", fields: "engine: claude\n" + "safe-outputs:\n  create-pull-request:\n    protected-files: blocked\n", prompt: "Update `CLAUDE.md` with the new rules.", want: []string{findingProtected}},
		{name: "FAL-M6 dot folder", fields: "safe-outputs:\n  push-to-pull-request-branch:\n    protected-files: blocked\n", prompt: "Edit `.github/workflows/ci.yml` to add a job.", want: []string{findingProtected}},
		{name: "FAL-M6 nested readme is not protected", fields: "safe-outputs:\n  create-pull-request:\n    protected-files: blocked\n", prompt: "Update `docs/README.md`."},
		{name: "FAL-M7 imports defer memory", fields: "imports: [shared/memory.md]\n", prompt: "Read /tmp/gh-aw/cache-memory/state.json."},
		{name: "FAL-M7 imports defer protected files", fields: "imports: [shared/pr.md]\n" + "safe-outputs:\n  create-pull-request:\n    protected-files: blocked\n", prompt: "Update `package.json`."},
		{name: "FAL-M7 imports defer sandbox findings", fields: "imports: [shared/sandbox.md]\n", prompt: "Run docker build."},
		{name: "FAL-M7 authored sandbox is not deferred", fields: "imports: [shared/tools.md]\nsandbox: awf\n", prompt: "Run docker build.", want: []string{findingDocker}},
	})
}

func TestFileAccessExtraction(t *testing.T) {
	noWrite := "tools:\n  edit: false\n  bash: false\n"
	runFileAccessCases(t, []fileAccessCase{
		{name: "FAL-X1 non-shell fence ignored", fields: noWrite, prompt: "```markdown\nCreate a file `x.md`.\n```\n"},
		{name: "FAL-X1 non-shell fence ignores docker", prompt: "```text\ndocker run alpine\n```\n"},
		{name: "FAL-X1 memory paths inside any fence", prompt: "```text\n/tmp/gh-aw/repo-memory/notes\n```\n", want: []string{findingRepo}, lines: []int{2}},
		{name: "FAL-X1 tilde fence closes", prompt: "~~~bash\nls\n~~~\nRun docker build.\n", want: []string{findingDocker}, lines: []int{4}},
		{name: "FAL-X1 HTML comment ignored", fields: noWrite, prompt: "<!--\nCreate a file `x.md`.\n-->\nSummarize.\n"},
		{name: "FAL-X1 CRLF lines", fields: noWrite, prompt: "Summarize.\r\nCreate a file `x.md`.\r\n", want: []string{findingWrite}, lines: []int{2}},
		{name: "FAL-X2 never", fields: noWrite, prompt: "Never create files in the repository."},
		{name: "FAL-X2 sentence-scoped negation", fields: noWrite, prompt: "Do not open issues. Create a file `out.md`.", want: []string{findingWrite}},
		{name: "FAL-X2 negated docker", prompt: "You cannot run docker here."},
		{name: "FAL-X3 URL is not a path", fields: noWrite, prompt: "Update the status at https://example.com/status."},
		{name: "FAL-X3 prose slash is not a path", fields: noWrite, prompt: "Update the issue and/or discussion."},
		{name: "FAL-X3 expression is not a path", fields: noWrite, prompt: "Update `${{ github.event.issue.title }}` labels."},
		{name: "FAL-X3 workspace path", fields: noWrite, prompt: "Save results to $GITHUB_WORKSPACE/out/result.json.", want: []string{findingWrite}},
		{name: "FAL-X4 non-file update", fields: noWrite, prompt: "Update the pull request description."},
		{name: "FAL-X4 list and bold markers", fields: noWrite, prompt: "1. **Write** the summary to a file.", want: []string{findingWrite}},
		{name: "FAL-X4 then clause", fields: noWrite, prompt: "Analyze the logs and then save them to `logs.txt`.", want: []string{findingWrite}},
		{name: "FAL-X4 target after preposition", prompt: "Copy /usr/share/dict/words and write it to /etc/words.", want: []string{findingReadOnly}},
		{name: "FAL-X4 read is not write", fields: noWrite, prompt: "Read `README.md` and summarize it."},
		{name: "FAL-X5 docker socket", prompt: "Mount /var/run/docker.sock in the job.", want: []string{findingDocker}},
		{name: "FAL-X5 sudo shell prompt", prompt: "```console\n$ sudo docker ps\n```\n", want: []string{findingDocker}},
		{name: "FAL-X5 mention is not an instruction", prompt: "The project ships a docker image."},
		{name: "FAL-X5 docker hostname is not docker", prompt: "Run `redis-cli -h host.docker.internal ping`."},
		{name: "FAL-X5 docker must be the verb object", prompt: "- **Custom image**: Run AWF from a custom Docker image."},
		{name: "FAL-X5 determiner before docker", prompt: "Start a new Docker container for tests.", want: []string{findingDocker}},
		{name: "FAL-X5 docker manifest is daemonless", prompt: "```bash\ndocker manifest inspect ghcr.io/org/image:v1\n```\n"},
		{name: "FAL-X2 illustrative example", prompt: "Include named cache roots such as\n`/tmp/gh-aw/cache-memory-focus/`, as declared by each workflow."},
		{name: "FAL-X1 wrapped sentence reports the matching line", fields: "tools:\n  edit: false\n  bash: false\n", prompt: "When done,\nwrite the summary\nto `out.md`.\n\nRead /tmp/gh-aw/repo-memory/x.", want: []string{findingWrite, findingRepo}, lines: []int{1, 5}},
	})
}

func TestFileAccessFindings(t *testing.T) {
	runFileAccessCases(t, []fileAccessCase{
		{name: "FAL-R1 shell fence write is not prose", fields: "tools:\n  edit: false\n  bash: false\n", prompt: "```bash\nmkdir out\n```\n"},
		{name: "FAL-R1 sandbox-related location", fields: "sandbox:\n  agent: false\n", prompt: "Create a file `x.md`.", want: []string{findingWrite}},
		{name: "FAL-R2 workspace write allowed", prompt: "Write the report to /tmp/report.md."},
		{name: "FAL-R2 /optional is not /opt", prompt: "Write the report to /optional/report.md."},
		{name: "FAL-R3 multiple lines", prompt: "Run docker build.\nThen run docker push.\n", want: []string{findingDocker, findingDocker}, lines: []int{1, 2}},
		{name: "FAL-R4 repo memory unconfigured", prompt: "Append to /tmp/gh-aw/repo-memory/default/log.md.", want: []string{findingRepo}},
		{name: "FAL-R4 one finding per id", prompt: "Read /tmp/gh-aw/cache-memory/a and /tmp/gh-aw/cache-memory/b.", want: []string{findingCache}},
		{name: "FAL-R5 directory exclude", fields: "safe-outputs:\n  create-pull-request:\n    protected-files:\n      policy: blocked\n      exclude: [.github/]\n", prompt: "Edit `.github/workflows/ci.yml`."},
		{name: "FAL-R5 absolute path is not workspace-relative", fields: "safe-outputs:\n  create-pull-request:\n    protected-files: blocked\n", prompt: "Write to /tmp/package.json."},
	})
	t.Run("FAL-R1 related location names the disabled edit tool", func(t *testing.T) {
		diagnostics := runFileAccess(t, fileAccessCase{fields: "tools:\n  edit: false\n  bash: false\n", prompt: "Create a file `x.md`."})
		require.Len(t, diagnostics, 1)
		assert.Equal(t, []RelatedLocation{{Path: "tools.edit", Symbol: "edit"}}, diagnostics[0].Related)
	})
	t.Run("FAL-R5 related location names the policy", func(t *testing.T) {
		diagnostics := runFileAccess(t, fileAccessCase{fields: "safe-outputs:\n  create-pull-request:\n    protected-files: blocked\n", prompt: "Update `go.mod`."})
		require.Len(t, diagnostics, 1)
		assert.Equal(t, []RelatedLocation{{Path: "safe-outputs.create-pull-request.protected-files", Symbol: "blocked"}}, diagnostics[0].Related)
	})
}

func TestFileAccessDiagnostics(t *testing.T) {
	t.Run("FAL-D1 warning severity without threat id", func(t *testing.T) {
		rule := FileAccessRule()
		assert.Equal(t, Warning, rule.Severity)
		assert.Empty(t, rule.ThreatRuleID)
		assert.Equal(t, AWAST, rule.Targets)
	})
	t.Run("FAL-D1 cancellation", func(t *testing.T) {
		document, err := aw.Parse("---\n---\nCreate a file `x.md`.\n")
		require.NoError(t, err)
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		_, err = checkFileAccess(ctx, Input{Document: document})
		require.ErrorIs(t, err, context.Canceled)
	})
	t.Run("FAL-D1 messages do not echo expressions", func(t *testing.T) {
		diagnostics := runFileAccess(t, fileAccessCase{fields: "tools:\n  edit: false\n  bash: false\n", prompt: "Write ${{ secrets.TOKEN }} to a file."})
		require.Len(t, diagnostics, 1)
		assert.NotContains(t, diagnostics[0].Message, "secrets")
	})
	t.Run("FAL-D1 malformed configuration is an error", func(t *testing.T) {
		document, err := aw.Parse("---\nsandbox: [awf]\n---\nRun docker build.\n")
		require.NoError(t, err)
		_, err = checkFileAccess(context.Background(), Input{Document: document})
		require.Error(t, err)
	})
}

// TestFileAccessSpecTraceability keeps the spec and tests in lockstep: every
// requirement ID defined by the spec must be cited by at least one test.
func TestFileAccessSpecTraceability(t *testing.T) {
	_, file, _, ok := runtime.Caller(0)
	require.True(t, ok)
	spec, err := os.ReadFile(filepath.Join(filepath.Dir(file), "..", "..", "..", "specs", "file-access-lint-spec.md"))
	require.NoError(t, err)
	tests, err := os.ReadFile(file)
	require.NoError(t, err)
	definition := regexp.MustCompile(`(?m)^\| (FAL-[A-Z]\d+) \|`)
	var ids []string
	for _, match := range definition.FindAllStringSubmatch(string(spec), -1) {
		ids = append(ids, match[1])
	}
	require.NotEmpty(t, ids)
	for _, id := range ids {
		assert.True(t, strings.Contains(string(tests), `"`+id+" "), "spec requirement %s has no test case", id)
	}
	cases := regexp.MustCompile(`(?m)^\| (C\d+) \|`).FindAllStringSubmatch(string(spec), -1)
	require.NotEmpty(t, cases)
	for _, match := range cases {
		assert.True(t, slices.ContainsFunc(strings.Split(string(tests), "\n"), func(line string) bool {
			return strings.Contains(line, `name: "`+match[1]+" ")
		}), "spec conformance case %s has no test", match[1])
	}
}
