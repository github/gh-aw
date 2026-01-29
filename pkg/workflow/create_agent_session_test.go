//go:build !integration

package workflow

import (
	"testing"
)

func TestParseAgentTaskConfig(t *testing.T) {
	tests := []struct {
		name       string
		outputMap  map[string]any
		wantConfig bool
		wantBase   string
		wantRepo   string
	}{
		{
			name: "parse basic agent-session config",
			outputMap: map[string]any{
				"create-agent-session": map[string]any{},
			},
			wantConfig: true,
			wantBase:   "",
			wantRepo:   "",
		},
		{
			name: "parse agent-session config with base branch",
			outputMap: map[string]any{
				"create-agent-session": map[string]any{
					"base": "develop",
				},
			},
			wantConfig: true,
			wantBase:   "develop",
			wantRepo:   "",
		},
		{
			name: "parse agent-session config with target-repo",
			outputMap: map[string]any{
				"create-agent-session": map[string]any{
					"target-repo": "owner/repo",
				},
			},
			wantConfig: true,
			wantBase:   "",
			wantRepo:   "owner/repo",
		},
		{
			name: "parse agent-session config with all fields",
			outputMap: map[string]any{
				"create-agent-session": map[string]any{
					"base":        "main",
					"target-repo": "owner/repo",
					"max":         1,
				},
			},
			wantConfig: true,
			wantBase:   "main",
			wantRepo:   "owner/repo",
		},
		{
			name:       "no agent-session config",
			outputMap:  map[string]any{},
			wantConfig: false,
		},
		{
			name: "reject wildcard target-repo",
			outputMap: map[string]any{
				"create-agent-session": map[string]any{
					"target-repo": "*",
				},
			},
			wantConfig: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			compiler := NewCompiler()
			config := compiler.parseAgentSessionConfig(tt.outputMap)

			if (config != nil) != tt.wantConfig {
				t.Errorf("parseAgentSessionConfig() returned config = %v, want config existence = %v", config != nil, tt.wantConfig)
				return
			}

			if config != nil {
				if config.Base != tt.wantBase {
					t.Errorf("parseAgentSessionConfig().Base = %v, want %v", config.Base, tt.wantBase)
				}
				if config.TargetRepoSlug != tt.wantRepo {
					t.Errorf("parseAgentSessionConfig().TargetRepoSlug = %v, want %v", config.TargetRepoSlug, tt.wantRepo)
				}
				if config.Max != 1 {
					t.Errorf("parseAgentSessionConfig().Max = %v, want 1", config.Max)
				}
			}
		})
	}
}

func TestBuildCreateOutputAgentTaskJob(t *testing.T) {
	compiler := NewCompiler()
	workflowData := &WorkflowData{
		Name: "Test Workflow",
		SafeOutputs: &SafeOutputsConfig{
			CreateAgentSessions: &CreateAgentSessionConfig{
				BaseSafeOutputConfig: BaseSafeOutputConfig{
					Max: 1,
				},
				Base:           "main",
				TargetRepoSlug: "owner/repo",
			},
		},
	}

	job, err := compiler.buildCreateOutputAgentSessionJob(workflowData, "main_job")
	if err != nil {
		t.Fatalf("buildCreateOutputAgentSessionJob() error = %v", err)
	}

	if job == nil {
		t.Fatal("buildCreateOutputAgentSessionJob() returned nil job")
	}

	if job.Name != "create_agent_session" {
		t.Errorf("buildCreateOutputAgentSessionJob().Name = %v, want 'create_agent_session'", job.Name)
	}

	if job.TimeoutMinutes != 10 {
		t.Errorf("buildCreateOutputAgentSessionJob().TimeoutMinutes = %v, want 10", job.TimeoutMinutes)
	}

	if len(job.Outputs) != 2 {
		t.Errorf("buildCreateOutputAgentSessionJob().Outputs length = %v, want 2", len(job.Outputs))
	}

	if _, ok := job.Outputs["session_number"]; !ok {
		t.Error("buildCreateOutputAgentSessionJob().Outputs missing 'session_number'")
	}

	if _, ok := job.Outputs["session_url"]; !ok {
		t.Error("buildCreateOutputAgentSessionJob().Outputs missing 'session_url'")
	}

	if len(job.Steps) == 0 {
		t.Error("buildCreateOutputAgentSessionJob().Steps is empty")
	}

	if len(job.Needs) != 1 || job.Needs[0] != "main_job" {
		t.Errorf("buildCreateOutputAgentSessionJob().Needs = %v, want ['main_job']", job.Needs)
	}
}

func TestExtractSafeOutputsConfigWithAgentTask(t *testing.T) {
	compiler := NewCompiler()
	frontmatter := map[string]any{
		"safe-outputs": map[string]any{
			"create-agent-session": map[string]any{
				"base": "develop",
			},
		},
	}

	config := compiler.extractSafeOutputsConfig(frontmatter)

	if config == nil {
		t.Fatal("extractSafeOutputsConfig() returned nil")
	}

	if config.CreateAgentSessions == nil {
		t.Fatal("extractSafeOutputsConfig().CreateAgentSessions is nil")
	}

	if config.CreateAgentSessions.Base != "develop" {
		t.Errorf("extractSafeOutputsConfig().CreateAgentSessions.Base = %v, want 'develop'", config.CreateAgentSessions.Base)
	}
}

func TestHasSafeOutputsEnabledWithAgentTask(t *testing.T) {
	config := &SafeOutputsConfig{
		CreateAgentSessions: &CreateAgentSessionConfig{},
	}

	if !HasSafeOutputsEnabled(config) {
		t.Error("HasSafeOutputsEnabled() = false, want true when CreateAgentSessions is set")
	}

	emptyConfig := &SafeOutputsConfig{}
	if HasSafeOutputsEnabled(emptyConfig) {
		t.Error("HasSafeOutputsEnabled() = true, want false when no safe outputs are configured")
	}
}
