//go:build !integration

package workflow

import (
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/constants"
)

func TestReserveConnectorWriterEnvironment(t *testing.T) {
	t.Parallel()

	const writer = "environment: writer"
	tests := []struct {
		name        string
		writerEnv   string
		workflowEnv string
		override    string
		writerJob   string
		extraJobs   []Job
		wantErr     []string
	}{
		{
			name:        "framework jobs use workflow environment",
			writerEnv:   writer,
			workflowEnv: "environment:\n  name: production\n  url: https://example.com",
			writerJob:   "environment: Writer",
			extraJobs:   []Job{{Name: "agent", Environment: "environment: agent"}, {Name: "no_environment"}},
		},
		{name: "framework jobs clear environment", writerEnv: writer, writerJob: writer},
		{name: "missing writer environment", writerJob: writer, wantErr: []string{"dedicated literal safe-outputs.environment"}},
		{name: "dynamic writer environment", writerEnv: "environment: ${{ inputs.env }}", writerJob: writer, wantErr: []string{"dedicated literal safe-outputs.environment"}},
		{name: "environment override", writerEnv: writer, writerJob: writer, override: "debug", wantErr: []string{"do not use --environment"}},
		{name: "writer job lost environment", writerEnv: writer, writerJob: "environment: other", wantErr: []string{"writer job lost"}},
		{name: "custom job shares writer case insensitive", writerEnv: writer, writerJob: writer, extraJobs: []Job{{Name: "custom", Environment: "environment: WRITER"}}, wantErr: []string{"job custom", "must not share"}},
		{name: "custom job dynamic environment", writerEnv: writer, writerJob: writer, extraJobs: []Job{{Name: "custom", Environment: "environment: ${{ inputs.env }}"}}, wantErr: []string{"job custom", "dynamically select"}},
		{name: "custom job unverifiable environment", writerEnv: writer, writerJob: writer, extraJobs: []Job{{Name: "custom", Environment: "environment:\n  url: https://example.com"}}, wantErr: []string{"job custom", "dynamically select"}},
		{name: "reusable caller", writerEnv: writer, writerJob: writer, extraJobs: []Job{{Name: "caller", Uses: "./.github/workflows/reusable.yml"}}, wantErr: []string{"job caller", "nested environments cannot be verified", "separate workflow"}},
		{name: "inherited safe output environment", writerEnv: writer, writerJob: writer, extraJobs: []Job{{Name: "upload_assets", Environment: writer}}, wantErr: []string{"job upload_assets", "inherits", "unsupported", "separate workflow"}},
		{name: "framework inherits shared workflow environment", writerEnv: writer, writerJob: writer, workflowEnv: "environment: WRITER", wantErr: []string{"must not share", "separate workflow"}},
		{name: "framework inherits dynamic workflow environment", writerEnv: writer, writerJob: writer, workflowEnv: "environment: ${{ inputs.env }}", wantErr: []string{"dynamically select", "separate workflow"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			c := NewCompiler()
			c.environmentOverride = tt.override
			data := &WorkflowData{Environment: tt.workflowEnv, SafeOutputs: &SafeOutputsConfig{Environment: tt.writerEnv}}
			jobs := []Job{
				{Name: string(constants.SafeOutputsJobName), Environment: tt.writerJob},
				{Name: string(constants.ConclusionJobName), Environment: writer},
				{Name: string(constants.PreActivationJobName), Environment: writer},
			}
			jobs = append(jobs, tt.extraJobs...)
			for i := range jobs {
				if err := c.jobManager.AddJob(&jobs[i]); err != nil {
					t.Fatal(err)
				}
			}

			err := c.reserveConnectorWriterEnvironment(data)
			if len(tt.wantErr) != 0 {
				if err == nil {
					t.Fatal("expected an error")
				}
				for _, text := range tt.wantErr {
					if !strings.Contains(err.Error(), text) {
						t.Errorf("error %q does not contain %q", err, text)
					}
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			for _, name := range []string{string(constants.ConclusionJobName), string(constants.PreActivationJobName)} {
				job, _ := c.jobManager.GetJob(name)
				if want := c.indentYAMLLines(tt.workflowEnv, "    "); job.Environment != want {
					t.Errorf("job %s environment = %q, want %q", name, job.Environment, want)
				}
			}
			for _, original := range append([]Job{{Name: string(constants.SafeOutputsJobName), Environment: tt.writerJob}}, tt.extraJobs...) {
				job, _ := c.jobManager.GetJob(original.Name)
				if job.Environment != original.Environment {
					t.Errorf("job %s environment = %q, want %q", original.Name, job.Environment, original.Environment)
				}
			}
		})
	}
}
