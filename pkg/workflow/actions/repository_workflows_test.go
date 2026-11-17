package actions

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRepositoryWorkflowGraphs(t *testing.T) {
	files, err := filepath.Glob(filepath.Join("..", "..", "..", ".github", "workflows", "*.yml"))
	require.NoError(t, err)
	require.NotEmpty(t, files, "must discover repository workflows, including generated .lock.yml files")
	var jobs, edges atomic.Int64
	t.Cleanup(func() {
		t.Logf("Built Actions graphs for %d workflow files: %d jobs, %d dependency edges", len(files), jobs.Load(), edges.Load())
	})
	for _, file := range files {
		t.Run(filepath.Base(file), func(t *testing.T) {
			t.Parallel()
			content, err := os.ReadFile(file)
			require.NoError(t, err)
			input, err := Parse(content)
			require.NoError(t, err)
			require.NotEmpty(t, input.Jobs)
			resolved, err := Resolve(input)
			require.NoError(t, err)
			require.Len(t, resolved.Jobs, len(input.Jobs))
			for i, job := range resolved.Jobs {
				expected := slices.Clone(input.Jobs[i].Needs)
				slices.Sort(expected)
				assert.Equal(t, slices.Compact(expected), job.Needs)
				assert.Equal(t, input.Jobs[i].ID, job.ID)
				assert.Len(t, job.Steps, len(input.Jobs[i].Steps))
				jobs.Add(1)
				edges.Add(int64(len(job.Needs)))
			}
			_, err = Lower(input)
			require.NoError(t, err, "all authored steps must also lower as native GitHub Actions")
			emitted, err := Emit(resolved)
			require.NoError(t, err)
			assert.Equal(t, string(content), emitted, "parsed/resolved workflows must round-trip every source byte, including comments")
		})
	}
}

func TestAuthoredWorkflowGraphValidation(t *testing.T) {
	tests := []struct {
		name string
		jobs string
		want string
	}{
		{"scalar needs", "a: {runs-on: ubuntu-latest, steps: [{run: echo a}]}\nb: {uses: './worker.yml', needs: a}", ""},
		{"list needs", "a: {runs-on: ubuntu-latest, steps: [{run: echo a}]}\nb: {uses: './worker.yml', needs: [a]}", ""},
		{"missing dependency", "a: {uses: './worker.yml', needs: missing}", "non-existent job"},
		{"cycle", "a: {uses: './worker.yml', needs: b}\nb: {uses: './worker.yml', needs: a}", "cycle detected"},
		{"duplicate step IDs", "a: {runs-on: ubuntu-latest, steps: [{id: same, run: echo a}, {id: same, run: echo b}]}", "duplicate workflow step"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resolved, err := Parse([]byte("jobs:\n  " + strings.ReplaceAll(tt.jobs, "\n", "\n  ") + "\n"))
			if tt.want != "" {
				require.ErrorContains(t, err, tt.want)
				return
			}
			require.NoError(t, err)
			require.Len(t, resolved.Jobs, 2)
			assert.Equal(t, []JobID{"a"}, resolved.Jobs[1].Needs)
		})
	}
}
