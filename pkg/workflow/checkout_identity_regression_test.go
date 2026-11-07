//go:build !integration

package workflow

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestTrialGitCredentialCheckoutIdentity(t *testing.T) {
	c := NewCompiler()
	c.trialMode = true
	c.trialLogicalRepoSlug = "org/trial"
	data := &WorkflowData{
		WorkflowID: "identity",
		Evals:      &EvalsConfig{Questions: []EvalDefinition{{ID: "ok", Question: "OK?"}}},
	}
	evalsJob, err := c.buildPushEvalsStateJob(data)
	require.NoError(t, err)
	require.NotNil(t, evalsJob)
	for name, steps := range map[string][]string{
		"repo memory": c.buildPushRepoMemorySetupAndCheckoutSteps(data, ""),
		"evals":       evalsJob.Steps,
		"experiments": c.buildPushExperimentsStateSteps(data),
	} {
		t.Run(name, func(t *testing.T) {
			content := strings.Join(steps, "")
			require.Contains(t, content, "GH_AW_GIT_REPOSITORY: ${{ github.repository }}")
			require.NotContains(t, content, `GH_AW_GIT_REPOSITORY: "org/trial"`)
		})
	}
	require.Contains(t, strings.Join(c.generateGitConfigurationStepsForData(nil), ""),
		`GH_AW_GIT_REPOSITORY: "org/trial"`)
	require.Contains(t, strings.Join(c.generateGitConfigurationSteps(), ""),
		`GH_AW_GIT_REPOSITORY: "org/trial"`)
	t.Run("explicit current checkout of workflow repo in subdirectory", func(t *testing.T) {
		data := &WorkflowData{CheckoutConfigs: []*CheckoutConfig{{Path: "current", Current: true}}}
		content := strings.Join(c.generateGitConfigurationStepsForData(data), "")
		require.Contains(t, content, `working-directory: "current"`)
		require.Contains(t, content, "GH_AW_GIT_REPOSITORY: ${{ github.repository }}")
	})
}

func TestEffectiveRootGitCredentialIdentity(t *testing.T) {
	tests := []struct {
		name     string
		configs  []*CheckoutConfig
		expected string
		trial    bool
	}{
		{"default wiki", []*CheckoutConfig{{Wiki: true}}, "${{ github.repository }}.wiki", false},
		{"external wiki", []*CheckoutConfig{{Repository: "org/docs", Wiki: true, Path: "."}}, "org/docs.wiki", false},
		{"explicit wiki suffix", []*CheckoutConfig{{Repository: "org/docs.wiki", Wiki: true}}, "org/docs.wiki", false},
		{"non-wiki default wins", []*CheckoutConfig{{}, {Wiki: true}}, "${{ github.repository }}", false},
		{"last external root wins", []*CheckoutConfig{{Repository: "org/docs", Wiki: true}, {Repository: "org/code"}}, "org/code", false},
		{"trial external root", []*CheckoutConfig{{Repository: "org/external", Path: "."}}, "org/external", true},
		{"trial external root wiki", []*CheckoutConfig{{Repository: "org/docs", Wiki: true, Path: "."}}, "org/docs.wiki", true},
		{"trial default wiki is overridden by trial checkout", []*CheckoutConfig{{Wiki: true}}, "org/trial", true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			c := NewCompiler()
			c.trialMode = tc.trial
			c.trialLogicalRepoSlug = "org/trial"
			data := &WorkflowData{
				CheckoutConfigs: tc.configs,
				SafeOutputs:     &SafeOutputsConfig{CreatePullRequests: &CreatePullRequestsConfig{}},
			}
			for name, steps := range map[string][]string{
				"agent":        c.generateGitConfigurationStepsForData(data),
				"safe outputs": c.buildSharedPRCheckoutSteps(data),
			} {
				t.Run(name, func(t *testing.T) {
					content := strings.Join(steps, "")
					repository := tc.expected
					if (name == "agent" && repository != "${{ github.repository }}") ||
						(name == "safe outputs" && tc.trial && repository == "org/trial") {
						repository = `"` + repository + `"`
					}
					require.Contains(t, content, "GH_AW_GIT_REPOSITORY: "+repository+"\n")
				})
			}
			if !tc.trial {
				require.Equal(t, tc.expected, NewCheckoutManager(tc.configs).effectiveRootCheckoutRepository())
			}
		})
	}
}

func TestCurrentWikiGitCredentialIdentity(t *testing.T) {
	for _, repository := range []string{"", "org/docs", "org/docs.wiki", "${{ inputs.repository }}"} {
		for _, path := range []string{".", "wiki"} {
			t.Run(repository+"/"+path, func(t *testing.T) {
				configs := []*CheckoutConfig{{Repository: repository, Wiki: true, Path: path, Current: true}}
				cm := NewCheckoutManager(configs)
				expected := wikiRepository(repository)
				require.Equal(t, expected, cm.GetCurrentRepository())
				content := strings.Join(NewCompiler().generateGitConfigurationStepsForData(&WorkflowData{CheckoutConfigs: configs}), "")
				require.Contains(t, content, `GH_AW_GIT_REPOSITORY: "`+expected+`"`)
				if path != "." {
					require.Contains(t, content, `working-directory: "wiki"`)
				}
			})
		}
	}
}

func TestTrialWithoutLogicalRepositoryGitCredentialIdentity(t *testing.T) {
	c := NewCompiler()
	c.trialMode = true
	data := &WorkflowData{
		CheckoutConfigs: []*CheckoutConfig{{Wiki: true}},
		SafeOutputs:     &SafeOutputsConfig{CreatePullRequests: &CreatePullRequestsConfig{}},
	}
	checkout := strings.Join(NewCheckoutManager(data.CheckoutConfigs).GenerateDefaultCheckoutStep(true, "", func(action string) string {
		return action
	}), "")
	require.NotContains(t, checkout, "repository:")
	for name, steps := range map[string][]string{
		"agent":        c.generateGitConfigurationStepsForData(data),
		"safe outputs": c.buildSharedPRCheckoutSteps(data),
	} {
		t.Run(name, func(t *testing.T) {
			content := strings.Join(steps, "")
			require.Contains(t, content, "GH_AW_GIT_REPOSITORY: ${{ github.repository }}\n")
			require.NotContains(t, content, ".wiki")
		})
	}
}

func TestCredentialCheckoutDashPath(t *testing.T) {
	for _, expression := range []bool{false, true} {
		for _, oldPWDSet := range []bool{false, true} {
			name := "literal"
			if expression {
				name = "expression"
			}
			if oldPWDSet {
				name += "/OLDPWD"
			} else {
				name += "/no-OLDPWD"
			}
			t.Run(name, func(t *testing.T) {
				dir := t.TempDir()
				workspace := filepath.Join(dir, "workspace")
				require.NoError(t, os.MkdirAll(filepath.Join(workspace, "-"), 0755))
				actions := filepath.Join(dir, "gh-aw", "actions")
				require.NoError(t, os.MkdirAll(actions, 0755))
				helper := "#!/usr/bin/env bash\nprintf '%s:%s\\n' \"$PWD\" \"$GH_AW_GIT_REPOSITORY\"\n"
				require.NoError(t, os.WriteFile(filepath.Join(actions, "configure_git_credentials.sh"), []byte(helper), 0755))
				repo := credentialSubrepository{repository: "org/sub", path: "-", envVarName: "GH_AW_SUBREPO_0"}
				if expression {
					repo.path = "${{ inputs.path }}"
					repo.pathEnvVarName = "GH_AW_SUBREPO_PATH_0"
				}
				lines := credentialCommandLines([]credentialSubrepository{repo})
				run := strings.Join(lines[1:], "")
				cmd := exec.Command("bash", "-e", "-c", run)
				cmd.Dir = workspace
				for _, env := range os.Environ() {
					if !strings.HasPrefix(env, "OLDPWD=") {
						cmd.Env = append(cmd.Env, env)
					}
				}
				cmd.Env = append(cmd.Env, "RUNNER_TEMP="+dir, "GH_AW_GIT_REPOSITORY=org/root", "GH_AW_SUBREPO_0=org/sub", "GH_AW_SUBREPO_PATH_0=-")
				if oldPWDSet {
					cmd.Env = append(cmd.Env, "OLDPWD="+dir)
				}
				output, err := cmd.CombinedOutput()
				require.NoError(t, err, "%s", output)
				require.Contains(t, string(output), filepath.Join(workspace, "-")+":org/sub\n")
			})
		}
	}
}
