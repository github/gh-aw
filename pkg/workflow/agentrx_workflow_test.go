//go:build !integration

package workflow

import (
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

var agentRxHashedRequirement = regexp.MustCompile(`^[a-z0-9][a-z0-9.-]*==[0-9][a-zA-Z0-9.]* \\\n    --hash=sha256:[0-9a-f]{64}( \\\n    --hash=sha256:[0-9a-f]{64})*$`)

func TestAgentRxSetupUsesHashedDependencyLock(t *testing.T) {
	source, err := os.ReadFile("../../.github/workflows/daily-agentrx-trace-optimizer.md")
	require.NoError(t, err)
	parts := strings.SplitN(string(source), "---", 3)
	require.Len(t, parts, 3)
	var frontmatter struct {
		Steps []struct {
			Name string `yaml:"name"`
			Run  string `yaml:"run"`
		} `yaml:"steps"`
	}
	require.NoError(t, yaml.Unmarshal([]byte(parts[1]), &frontmatter))
	require.NotEmpty(t, frontmatter.Steps)
	setup := frontmatter.Steps[0].Run
	require.Contains(t, setup, `uv pip sync --require-hashes --only-binary=:all: --python "$AGENTRX_HOME/.venv/bin/python" "${GITHUB_WORKSPACE}/.github/workflows/requirements-agentrx.txt"`)
	require.NotContains(t, setup, "uv pip install", "installing the source project dynamically resolves build and runtime dependencies")
	require.Contains(t, setup, `uv pip check --python "$AGENTRX_HOME/.venv/bin/python"`)
	require.Contains(t, setup, `PYTHONPATH="$AGENTRX_HOME/src" "$AGENTRX_HOME/.venv/bin/python" -c 'import agentrx.pipeline.globals, agentrx.ir.trajectory_ir, agentrx.invariants.domain_registry, pydantic, pydantic_core'`)
	require.Contains(t, setup, `"$AGENTRX_HOME/.venv/bin/python" "$AGENTRX_HOME/src/run.py" --help`)
	require.Contains(t, setup, `"$AGENTRX_HOME/src/run.py" "$SMOKE_DIR/trajectory.md" --stage ir --run-dir "$SMOKE_DIR/run"`)

	compiled, err := os.ReadFile("../../.github/workflows/daily-agentrx-trace-optimizer.lock.yml")
	require.NoError(t, err)
	var workflow struct {
		Jobs map[string]struct {
			Steps []struct {
				Name string `yaml:"name"`
				Run  string `yaml:"run"`
			} `yaml:"steps"`
		} `yaml:"jobs"`
	}
	require.NoError(t, yaml.Unmarshal(compiled, &workflow))
	var compiledSetup string
	for _, step := range workflow.Jobs["agent"].Steps {
		if step.Name == frontmatter.Steps[0].Name {
			compiledSetup = step.Run
		}
	}
	require.Equal(t, strings.TrimSpace(setup), compiledSetup, "the emitted workflow must preserve the hash-enforced setup")

	lock, err := os.ReadFile("../../.github/workflows/requirements-agentrx.txt")
	require.NoError(t, err)
	require.Contains(t, string(lock), "pydantic==2.14.1")
	require.Contains(t, string(lock), "pydantic-core==2.50.1 \\\n    --hash=sha256:93ba4e9d8210d941c200431a56b2c0400b131865947903937ed3ec5404307d2e")
	require.NotContains(t, string(lock), "pydantic-core==2.50.2")
	revision := regexp.MustCompile(`fetch --depth 1 origin ([0-9a-f]{40})`).FindStringSubmatch(setup)
	require.Len(t, revision, 2, "AgentRx source must remain pinned to an immutable commit")
	require.Contains(t, string(lock), "# AgentRx "+revision[1]+" runtime dependencies.")

	var requirement strings.Builder
	count := 0
	for line := range strings.SplitSeq(string(lock), "\n") {
		if strings.HasPrefix(line, "#") || strings.TrimSpace(line) == "" {
			continue
		}
		if requirement.Len() > 0 {
			requirement.WriteByte('\n')
		}
		requirement.WriteString(line)
		if strings.HasSuffix(line, `\`) {
			continue
		}
		require.Regexp(t, agentRxHashedRequirement, requirement.String(), "every dependency must have an exact version and one or more SHA-256 wheel hashes")
		requirement.Reset()
		count++
	}
	require.Zero(t, requirement.Len(), "dependency entries must not be truncated")
	require.Positive(t, count)
}

func TestAgentRxHashedDependencyEntry(t *testing.T) {
	hash := "    --hash=sha256:" + strings.Repeat("a", 64)
	for _, tt := range []struct {
		name  string
		entry string
		valid bool
	}{
		{"single hash", "pillow==13.0.0 \\\n" + hash, true},
		{"multiple hashes", "pillow==13.0.0 \\\n" + hash + " \\\n" + hash, true},
		{"missing hash", "pillow==13.0.0", false},
		{"unpinned version", "pillow>=13.0.0 \\\n" + hash, false},
		{"wildcard version", "pillow==13.* \\\n" + hash, false},
		{"invalid first hash", "pillow==13.0.0 \\\n" + hash[:len(hash)-1], false},
		{"invalid subsequent hash", "pillow==13.0.0 \\\n" + hash + " \\\n" + hash[:len(hash)-1], false},
		{"nonhex hash", "pillow==13.0.0 \\\n    --hash=sha256:" + strings.Repeat("z", 64), false},
		{"unhashed subsequent entry", "pillow==13.0.0 \\\n" + hash + " \\\nnumpy==2.5.4", false},
		{"truncated continuation", "pillow==13.0.0 \\\n" + hash + " \\", false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.valid, agentRxHashedRequirement.MatchString(tt.entry))
		})
	}
}
