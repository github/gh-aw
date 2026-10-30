//go:build !integration

package workflow

import (
	"strings"
	"testing"

	"github.com/goccy/go-yaml"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestJobManagerCacheMode(t *testing.T) {
	for _, mode := range []string{"", "read", "write", "write-only", "none"} {
		t.Run(mode, func(t *testing.T) {
			for _, reusable := range []bool{false, true} {
				jm := NewJobManager()
				job := &Job{Name: "test", CacheMode: mode}
				if reusable {
					job.Uses = "./.github/workflows/worker.yml"
				} else {
					job.RunsOn = "runs-on: ubuntu-latest"
					job.Steps = []string{"      - run: echo data\n"}
				}
				require.NoError(t, jm.AddJob(job))
				require.NoError(t, jm.resolveActions())
				var output strings.Builder
				jm.WriteJobsYAML(&output)
				var parsed map[string]any
				require.NoError(t, yaml.Unmarshal([]byte(output.String()), &parsed))
				fields := parsed["jobs"].(map[string]any)["test"].(map[string]any)
				if mode == "" {
					assert.NotContains(t, fields, "cache-mode")
					assert.NotContains(t, output.String(), "cache-mode:")
				} else {
					assert.Equal(t, mode, fields["cache-mode"])
					assert.Contains(t, output.String(), "    cache-mode: "+mode+"\n")
				}
			}
		})
	}
}

func TestJobManagerRejectsInvalidCacheMode(t *testing.T) {
	tests := []struct {
		name string
		mode string
	}{
		{"unsupported", "read-write"},
		{"case", "READ"},
		{"newline injection", "read\n    permissions:\n      contents: write"},
		{"carriage return injection", "read\r    permissions: write-all"},
		{"crlf injection", "read\r\n    permissions: write-all"},
		{"trailing newline", "read\n"},
		{"leading space", " read"},
		{"trailing space", "read "},
		{"space only", " "},
		{"tab", "read\t"},
		{"unicode whitespace", "read\u00a0"},
		{"nul", "read\x00"},
		{"escape", "read\x1b"},
		{"delete", "read\x7f"},
		{"yaml comment", "read # private"},
		{"yaml mapping", "{permissions: write-all}"},
		{"expression", "${{ inputs.cache-mode }}"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			jm := NewJobManager()
			job := &Job{Name: "test", CacheMode: tt.mode}
			const message = "job 'test' cache-mode must be read, write, write-only, or none; omit cache-mode to use the default"
			require.EqualError(t, jm.AddJob(job), message)
			assert.Empty(t, jm.GetAllJobs())
			var output strings.Builder
			jm.WriteJobsYAML(&output)
			assert.Equal(t, "jobs:\n", output.String())

			job.CacheMode = "read"
			require.NoError(t, jm.AddJob(job))
			job.CacheMode = tt.mode
			require.EqualError(t, jm.resolveActions(), message, "revalidate mutations before compiler emission")
		})
	}
}
