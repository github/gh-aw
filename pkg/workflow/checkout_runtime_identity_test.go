//go:build !integration

package workflow

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

func TestRuntimeCheckoutRootCredentials(t *testing.T) {
	tests := []struct {
		name      string
		path      string
		otherPath string
		configs   []*CheckoutConfig
		rootRepo  string
		trial     bool
		mismatch  bool
		missing   bool
	}{
		{name: "dot", path: ".", rootRepo: "org/external"},
		{name: "empty", path: "", rootRepo: "org/external"},
		{name: "slash dot", path: "./", rootRepo: "org/external"},
		{name: "normalized parent", path: "nested/..", rootRepo: "org/external"},
		{name: "absolute root", path: "$WORKSPACE", rootRepo: "org/external"},
		{name: "subdirectory", path: "sub", rootRepo: "org/workflow"},
		{name: "literal dash", path: "-", rootRepo: "org/workflow"},
		{name: "shell syntax stays data", path: "sub; touch injected", rootRepo: "org/workflow"},
		{name: "trial root", path: ".", rootRepo: "org/external", trial: true},
		{name: "wrong origin fails closed", path: ".", rootRepo: "org/incorrect", mismatch: true},
		{name: "missing checkout fails closed", path: "absent", rootRepo: "org/workflow", missing: true},
		{name: "later literal root wins", path: ".", rootRepo: "org/last", configs: []*CheckoutConfig{
			{Repository: "org/external", Path: "${{ inputs.path }}"},
			{Repository: "org/last", Path: "."},
		}},
		{name: "later runtime root wins", path: ".", rootRepo: "org/external", configs: []*CheckoutConfig{
			{Repository: "org/first", Path: "."},
			{Repository: "org/external", Path: "${{ inputs.path }}"},
		}},
		{name: "runtime subdirectory preserves literal root", path: "sub", rootRepo: "org/first", configs: []*CheckoutConfig{
			{Repository: "org/first", Path: "."},
			{Repository: "org/external", Path: "${{ inputs.path }}"},
		}},
		{name: "runtime wiki root", path: ".", rootRepo: "org/external.wiki", configs: []*CheckoutConfig{
			{Repository: "${{ inputs.repository }}", Path: "${{ inputs.path }}", Wiki: true},
		}},
		{name: "runtime workflow repository root", path: ".", rootRepo: "org/workflow", configs: []*CheckoutConfig{
			{Repository: "org/first", Path: "."},
			{Path: "${{ inputs.path }}"},
		}},
		{name: "current runtime root overwritten", path: ".", rootRepo: "org/last", configs: []*CheckoutConfig{
			{Repository: "org/external", Path: "${{ inputs.path }}", Current: true},
			{Repository: "org/last", Path: "."},
		}},
		{name: "current runtime subdirectory", path: "sub", rootRepo: "org/workflow", configs: []*CheckoutConfig{
			{Repository: "org/external", Path: "${{ inputs.path }}", Current: true},
		}},
		{name: "last of two runtime roots wins", path: ".", otherPath: ".", rootRepo: "org/last", configs: []*CheckoutConfig{
			{Repository: "org/external", Path: "${{ inputs.path }}"},
			{Repository: "org/last", Path: "${{ inputs.other_path }}"},
		}},
		{name: "later runtime subdirectory does not replace root", path: ".", otherPath: "other", rootRepo: "org/external", configs: []*CheckoutConfig{
			{Repository: "org/external", Path: "${{ inputs.path }}"},
			{Repository: "org/last", Path: "${{ inputs.other_path }}"},
		}},
	}
	for _, tc := range tests {
		for _, job := range []string{"agent", "safe outputs"} {
			t.Run(tc.name+"/"+job, func(t *testing.T) {
				workspace, env := setupRuntimeCredentialWorkspace(t)
				path := strings.ReplaceAll(tc.path, "$WORKSPACE", workspace)
				require.NoError(t, os.MkdirAll(filepath.Join(workspace, "nested"), 0755))
				createCredentialTestRepo(t, workspace, tc.rootRepo, env)
				if !tc.missing && path != "" && path != "." && path != "./" && path != "nested/.." && path != workspace {
					createCredentialTestRepo(t, filepath.Join(workspace, path), "org/external", env)
				}
				if tc.otherPath == "other" {
					createCredentialTestRepo(t, filepath.Join(workspace, tc.otherPath), "org/last", env)
				}
				configs := tc.configs
				if configs == nil {
					configs = []*CheckoutConfig{{Repository: "org/external", Path: "${{ inputs.path }}"}}
				}
				c := NewCompiler()
				c.trialMode = tc.trial
				c.trialLogicalRepoSlug = "org/trial"
				data := &WorkflowData{CheckoutConfigs: configs, SafeOutputs: &SafeOutputsConfig{CreatePullRequests: &CreatePullRequestsConfig{}}}
				steps := c.generateGitConfigurationStepsForData(data)
				if job == "safe outputs" {
					steps = c.buildSharedPRCheckoutSteps(data)
				}
				step := runtimeCredentialStep(t, steps)
				replacer := strings.NewReplacer(
					"${{ inputs.path }}", path,
					"${{ inputs.other_path }}", tc.otherPath,
					"${{ inputs.repository }}", "org/external",
					"${{ github.repository }}", "org/workflow",
					"${{ github.server_url }}", "https://github.com",
				)
				for key, value := range step.Env {
					if key == "GIT_TOKEN" || key == "GITHUB_TOKEN" {
						value = "fake-test-token"
					}
					env = append(env, key+"="+replacer.Replace(value))
				}
				require.NotContains(t, step.Run, "${{")
				cmd := exec.Command("bash", "-e", "-c", step.Run)
				cmd.Dir = workspace
				if step.WorkingDirectory != "" {
					workingDirectory := replacer.Replace(step.WorkingDirectory)
					if filepath.IsAbs(workingDirectory) {
						cmd.Dir = workingDirectory
					} else {
						cmd.Dir = filepath.Join(workspace, workingDirectory)
					}
				}
				cmd.Env = env
				output, err := cmd.CombinedOutput()
				config, readErr := os.ReadFile(filepath.Join(workspace, ".git", "config"))
				require.NoError(t, readErr)
				if tc.missing {
					require.Error(t, err, "%s", output)
					require.Contains(t, string(output), "Unable to resolve checkout directory")
					require.NotContains(t, string(config), "fake-test-token")
				} else if tc.mismatch {
					require.Error(t, err, "%s", output)
					require.Contains(t, string(output), "failed before credential configuration")
					require.NotContains(t, string(config), "fake-test-token")
				} else {
					require.NoError(t, err, "%s", output)
					if job == "agent" && cmd.Dir != workspace {
						currentConfig, readErr := os.ReadFile(filepath.Join(cmd.Dir, ".git", "config"))
						require.NoError(t, readErr)
						require.Contains(t, string(currentConfig), "@github.com/org/external.git")
						require.NotContains(t, string(config), "fake-test-token")
					} else {
						require.Contains(t, string(config), "@github.com/"+tc.rootRepo+".git")
					}
				}
				require.NotContains(t, string(output), "fake-test-token")
				if !tc.missing && job == "safe outputs" && path != "" && path != "." && path != "./" && path != "nested/.." && path != workspace {
					subConfig, readErr := os.ReadFile(filepath.Join(workspace, path, ".git", "config"))
					require.NoError(t, readErr)
					require.Contains(t, string(subConfig), "fake-test-token@github.com/org/external.git")
				}
				if job == "safe outputs" && tc.otherPath == "other" {
					subConfig, readErr := os.ReadFile(filepath.Join(workspace, tc.otherPath, ".git", "config"))
					require.NoError(t, readErr)
					require.Contains(t, string(subConfig), "fake-test-token@github.com/org/last.git")
				}
				_, err = os.Stat(filepath.Join(workspace, "injected"))
				require.True(t, os.IsNotExist(err))
			})
		}
	}
}

type renderedCredentialStep struct {
	Name             string
	Env              map[string]string
	Run              string
	WorkingDirectory string `yaml:"working-directory"`
}

func runtimeCredentialStep(t *testing.T, steps []string) renderedCredentialStep {
	t.Helper()
	var document struct {
		Steps []renderedCredentialStep
	}
	require.NoError(t, yaml.Unmarshal([]byte("steps:\n"+strings.Join(steps, "")), &document))
	for _, step := range document.Steps {
		if step.Name == "Configure Git credentials" {
			return step
		}
	}
	t.Fatal("Configure Git credentials step not generated")
	return renderedCredentialStep{}
}

func setupRuntimeCredentialWorkspace(t *testing.T) (string, []string) {
	t.Helper()
	dir := t.TempDir()
	workspace := filepath.Join(dir, "workspace")
	actions := filepath.Join(dir, "gh-aw", "actions")
	require.NoError(t, os.MkdirAll(workspace, 0755))
	require.NoError(t, os.MkdirAll(actions, 0755))
	helper, err := os.ReadFile("../../actions/setup/sh/configure_git_credentials.sh")
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(actions, "configure_git_credentials.sh"), helper, 0755))
	var env []string
	for _, value := range os.Environ() {
		key, _, _ := strings.Cut(value, "=")
		if strings.HasPrefix(key, "GIT_") || strings.HasPrefix(key, "GH_AW_") || strings.HasPrefix(key, "GITHUB_") || key == "HOME" || key == "RUNNER_TEMP" {
			continue
		}
		env = append(env, value)
	}
	env = append(env, "HOME="+dir, "GIT_CONFIG_GLOBAL="+filepath.Join(dir, "gitconfig"), "GIT_CONFIG_NOSYSTEM=1",
		"GITHUB_WORKSPACE="+workspace, "RUNNER_TEMP="+dir)
	return workspace, env
}

func createCredentialTestRepo(t *testing.T, dir, repository string, env []string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(dir, 0755))
	for _, args := range [][]string{{"init", "-q"}, {"remote", "add", "origin", "https://github.com/" + repository + ".git"}} {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = env
		output, err := cmd.CombinedOutput()
		require.NoError(t, err, "%s", output)
	}
}
