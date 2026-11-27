//go:build integration

package workflow

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/testutil"
	"github.com/stretchr/testify/require"
)

func TestImportedDetectorPrerequisites(t *testing.T) {
	dir := filepath.Join(testutil.TempDir(t, "imported-detector-*"), constants.GetWorkflowDir())
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "shared"), 0o755))
	write := func(name, content string) string {
		t.Helper()
		file := filepath.Join(dir, name)
		require.NoError(t, os.WriteFile(file, []byte(content), 0o644))
		return file
	}
	write("shared/python-engine.md", `---
imports: [unrelated.md]
engine:
  id: python-agent
  detection-engine: python-agent
  display-name: Python Agent
  version: "2.9.2"
  behaviors:
    execution:
      command-name: python3
    harness-script: |
      console.log("source harness");
    installation:
      step-name: Install locked Pydantic
      package-manager: pip
      package-name: pydantic
      requirements:
        - name: pydantic
          version: "2.9.2"
          sha256: aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
runtimes:
  python:
    version: "3.12"
pre-agent-steps:
  - name: Prepare Python engine
    uses: actions/github-script@v9.0.0
    with:
      script: console.log("prepare");
steps:
  - name: Primary job only
    run: echo primary
---
`)
	write("shared/unrelated.md", `---
runtimes:
  ruby:
    version: "3.3"
pre-agent-steps:
  - name: Nested import only
    run: echo nested
---
`)
	for _, tt := range []struct{ name, primary, detection string }{
		{"inherited", "", ""},
		{"explicit", "engine: copilot", "  threat-detection:\n    engine: python-agent"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			file := write(tt.name+".md", fmt.Sprintf(`---
on: workflow_dispatch
permissions:
  contents: read
features:
  gh-aw-detection: true
imports: [shared/python-engine.md]
%s
pre-agent-steps:
  - name: Workflow only
    run: echo workflow
safe-outputs:
  add-comment:
%s
---
Inspect the repository.
`, tt.primary, tt.detection))
			require.NoError(t, NewCompiler().CompileWorkflow(file))
			lock, err := os.ReadFile(filepath.Join(dir, tt.name+".lock.yml"))
			require.NoError(t, err)
			detection := extractJobSection(string(lock), "detection")
			require.NotEmpty(t, detection)
			for _, name := range []string{"Primary job only", "Nested import only", "Workflow only", "Setup Ruby"} {
				require.Contains(t, extractJobSection(string(lock), "agent"), name)
				require.NotContains(t, detection, name)
			}
			require.Regexp(t, `uses: actions/setup-python@[0-9a-f]{40}`, detection)
			require.Regexp(t, `uses: actions/github-script@[0-9a-f]{40}`, detection)
			require.Contains(t, detection, "python-version: '3.12'")
			require.Contains(t, detection, "pydantic==2.9.2 --hash=sha256:")
			require.Equal(t, 1, strings.Count(detection, "- name: Setup Node.js"))
			last := -1
			for _, marker := range []string{"- name: Setup Python", "- name: Install locked Pydantic", "- name: Prepare Python engine", `> "${RUNNER_TEMP}/gh-aw/actions/python-agent_harness.cjs"`, "stage_threat_detection_runtime.cjs"} {
				index := strings.Index(detection, marker)
				require.Greater(t, index, last, "missing or out-of-order %q", marker)
				last = index
			}
		})
	}
}
