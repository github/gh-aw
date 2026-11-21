//go:build !integration

package workflow

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/goccy/go-yaml"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExperimentsBrokerCompiledWorkflow(t *testing.T) {
	for _, tc := range []struct {
		name     string
		storage  string
		reusable bool
		dryRun   bool
		graders  bool
		strict   bool
		noOIDC   bool
		mode     ActionMode
	}{
		{name: "broker", storage: "broker"},
		{name: "broker-reusable", storage: "broker", reusable: true},
		{name: "broker-oidc-none", storage: "broker", noOIDC: true},
		{name: "broker-reusable-oidc-none", storage: "broker", reusable: true, noOIDC: true},
		{name: "broker-custom-graders", storage: "broker", graders: true},
		{name: "broker-strict", storage: "broker", strict: true},
		{name: "broker-script-reusable", storage: "broker", reusable: true, mode: ActionModeScript},
		{name: "broker-release", storage: "broker", mode: ActionModeRelease},
		{name: "broker-dry-run", storage: "broker", dryRun: true},
		{name: "broker-reusable-dry-run", storage: "broker", reusable: true, dryRun: true},
		{name: "repo", storage: "repo"},
		{name: "cache", storage: "cache"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			trigger := "workflow_dispatch"
			if tc.reusable {
				trigger = "workflow_call"
			}
			extra := ""
			if tc.graders {
				extra = "graders:\n  custom-count:\n    script: return 1\n"
			}
			permissions := "contents: read"
			if tc.noOIDC {
				permissions += ", id-token: none"
			}
			source := fmt.Sprintf("---\non: %s\nengine: copilot\nstrict: %t\npermissions: {%s}\nexperiments:\n  storage: %s\n  prompt:\n    variants: [control, candidate]\n    weight: [70, 30]\n%s---\nTest.\n", trigger, tc.strict, permissions, tc.storage, extra)
			compiler := NewCompiler(WithVersion("v1.0.0"), WithNoEmit(true))
			if tc.mode != "" {
				compiler.actionMode = tc.mode
			}
			compiler.SetDryRun(tc.dryRun)
			file := filepath.Join(t.TempDir(), "broker-test.md")
			compiler.gitRoot = filepath.Dir(file)
			data, err := compiler.ParseWorkflowString(source, file)
			require.NoError(t, err)
			assert.Equal(t, ExperimentStorageMode(tc.storage), data.ExperimentsStorage)
			assert.NotContains(t, data.Experiments, "storage")
			generated, err := compiler.CompileToYAML(data, file)
			require.NoError(t, err)
			var workflow struct {
				Jobs map[string]struct {
					Needs       any               `yaml:"needs"`
					If          string            `yaml:"if"`
					Permissions map[string]string `yaml:"permissions"`
					Steps       []struct {
						ID   string            `yaml:"id"`
						Name string            `yaml:"name"`
						Env  map[string]string `yaml:"env"`
						With map[string]any    `yaml:"with"`
					} `yaml:"steps"`
				} `yaml:"jobs"`
			}
			require.NoError(t, yaml.Unmarshal([]byte(generated), &workflow))
			activation := workflow.Jobs["activation"]
			report, hasReport := workflow.Jobs[string(constants.ReportExperimentsJobName)]
			liveBroker := tc.storage == "broker" && !tc.dryRun
			require.Equal(t, liveBroker, hasReport)
			_, hasPush := workflow.Jobs[pushExperimentsStateJobName]
			assert.Equal(t, tc.storage == "repo", hasPush)
			var pickScript string
			var pickEnv map[string]string
			for _, step := range activation.Steps {
				if step.ID == "pick-experiment" {
					pickScript, _ = step.With["script"].(string)
					pickEnv = step.Env
				}
			}
			require.NotEmpty(t, pickScript)
			assert.JSONEq(t, `{"prompt":{"variants":["control","candidate"],"weight":[70,30]}}`, pickEnv["GH_AW_EXPERIMENT_SPEC"])
			if liveBroker {
				assert.Contains(t, pickScript, "experiments_broker.cjs")
				assert.Contains(t, pickScript, "await assign({core})")
				assert.NotContains(t, pickScript, "pick_experiment.cjs")
				assert.Equal(t, "${{ secrets.GH_AW_EXPERIMENTS_BROKER_URL }}", pickEnv["GH_AW_EXPERIMENTS_BROKER_URL"])
				assert.Equal(t, "broker-test", pickEnv["GH_AW_WORKFLOW_ID"])
				assert.Equal(t, "${{ job.workflow_ref }}", pickEnv["GH_AW_WORKFLOW_REF"])
				assert.Equal(t, experimentsCacheDir, pickEnv["GH_AW_EXPERIMENT_STATE_DIR"])
				assert.Equal(t, "write", activation.Permissions["id-token"])
				assert.Equal(t, map[string]string{"contents": "read", "id-token": "write"}, report.Permissions)
				if tc.noOIDC {
					assert.NotEqual(t, "write", workflow.Jobs["agent"].Permissions["id-token"])
				}
				reportNeeds := parseNeedsField(report.Needs)
				assert.Contains(t, reportNeeds, "activation")
				assert.Contains(t, reportNeeds, "agent")
				assert.Contains(t, reportNeeds, "conclusion")
				assert.Equal(t, tc.graders, slices.Contains(reportNeeds, "graders"))
				assert.NotContains(t, workflow.Jobs["conclusion"].Needs, "report_experiments", "reporting must not create a dependency cycle")
				assert.Contains(t, report.If, "always()")
				assert.Contains(t, report.If, "needs.activation.outputs.experiments != ''")
				assert.Contains(t, report.If, "needs.activation.outputs.experiments != '{}'")
				assert.NotContains(t, report.If, "needs.agent.result", "an agent failure must not suppress reporting")
				reportSection := extractJobSection(generated, "report_experiments")
				for _, forbidden := range []string{"contents: write", "actions/cache", "pick_experiment.cjs", "load_experiment_state_from_repo", "push_experiment_state", "custom_graders.cjs", "COPILOT_GITHUB_TOKEN", "persist-credentials: true"} {
					assert.NotContains(t, reportSection, forbidden)
				}
				assert.Contains(t, reportSection, "await report({core})")
				assert.Contains(t, reportSection, "GH_AW_EXPERIMENT_SPEC:")
				assert.Contains(t, reportSection, "GH_AW_EXPERIMENT_RESULTS_FILE: /tmp/gh-aw/usage/activity/summary.json")
				assert.Contains(t, reportSection, "GH_AW_EXPERIMENT_METADATA_FILE: /tmp/gh-aw/usage/aw_info.json")
				assert.Contains(t, reportSection, "GH_AW_EXPERIMENT_AGENT_RESULT: ${{ needs.agent.result }}")
				assert.Contains(t, reportSection, "secrets.GH_AW_EXPERIMENTS_BROKER_URL")
				artifact := "brokertest-experiment"
				activationInvocation, reportInvocation := "default", "default"
				if tc.reusable {
					artifact = experimentArtifactDownloadName(data)
					assert.Contains(t, artifact, "needs.activation.outputs")
					activationInvocation = artifactPrefixExprForActivationJob(data)
					reportInvocation = artifactPrefixExprForDownstreamJob(data)
					assert.Less(t, strings.Index(generated, "id: artifact-prefix"), strings.Index(generated, "id: pick-experiment"))
				}
				assert.Equal(t, activationInvocation, pickEnv["GH_AW_EXPERIMENT_INVOCATION_ID"])
				assert.Contains(t, reportSection, "GH_AW_EXPERIMENT_INVOCATION_ID: "+reportInvocation)
				assert.Contains(t, reportSection, "name: "+artifact)
				assert.Contains(t, reportSection, "name: "+artifactPrefixExprForDownstreamJob(data)+"usage")
			} else {
				assert.Contains(t, pickScript, "pick_experiment.cjs")
				assert.NotContains(t, generated, "/experiments_broker.cjs")
				assert.NotContains(t, generated, "GH_AW_EXPERIMENTS_BROKER_URL")
				if tc.storage == "broker" {
					assert.NotContains(t, activation.Permissions, "id-token", "admission=%v dryRun=%v safeOutputBroker=%v", data.AdmissionEnabled, data.DryRun, usesTokenBrokerSafeOutputs(data))
				}
			}
			if tc.storage == "broker" {
				activationSection := extractJobSection(generated, "activation")
				for _, forbidden := range []string{"actions/cache/restore", "actions/cache/save", "load_experiment_state_from_repo.cjs", "GH_AW_EXPERIMENT_BRANCH"} {
					assert.NotContains(t, activationSection, forbidden)
				}
			}
		})
	}
}

func TestExperimentsBrokerJobsWithoutAssignments(t *testing.T) {
	compiler := NewCompiler()
	data := &WorkflowData{ExperimentsStorage: ExperimentsStorageBroker}
	assert.Empty(t, compiler.generateExperimentSteps(data))
	require.NoError(t, compiler.addReportExperimentsJob(data))
	_, exists := compiler.jobManager.GetJob(string(constants.ReportExperimentsJobName))
	assert.False(t, exists)
	data.Experiments = map[string][]string{"prompt": {"control", "candidate"}}
	require.ErrorContains(t, compiler.addReportExperimentsJob(data), "requires a conclusion job")
	require.NoError(t, compiler.jobManager.AddJob(&Job{Name: "conclusion", Steps: []string{"      - run: echo conclusion\n"}}))
	require.ErrorContains(t, compiler.addReportExperimentsJob(data), "requires an agent job")
	name, err := compiler.buildPushExperimentsStateJobWrapper(data)
	require.NoError(t, err)
	assert.Empty(t, name, "broker storage must not produce a git persistence job")
	job, err := compiler.buildPushExperimentsStateJob(data)
	require.NoError(t, err)
	assert.Nil(t, job)
}

func TestExperimentsBrokerReportJobCannotBeCustomized(t *testing.T) {
	for _, customization := range []string{
		"steps:\n      - run: echo untrusted",
		"permissions: {contents: write}",
		"needs: activation",
		"env: {NODE_OPTIONS: --require=untrusted.cjs}",
		"runs-on: self-hosted",
	} {
		compiler := NewCompiler(WithNoEmit(true))
		source := "---\non: workflow_dispatch\nstrict: false\nexperiments:\n  storage: broker\n  prompt: [control, candidate]\njobs:\n  report_experiments:\n    " + customization + "\n---\nTest.\n"
		file := filepath.Join(t.TempDir(), "workflow.md")
		data, err := compiler.ParseWorkflowString(source, file)
		if err == nil {
			_, err = compiler.CompileToYAML(data, file)
		}
		require.Error(t, err)
		assert.Contains(t, err.Error(), "report_experiments")
	}
}

func TestExperimentsBrokerReportEnvironmentIsolation(t *testing.T) {
	for _, mode := range []ActionMode{ActionModeRelease, ActionModeDev, ActionModeScript} {
		for _, imported := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/imported=%t", mode, imported), func(t *testing.T) {
				dir := t.TempDir()
				file := filepath.Join(dir, "workflow.md")
				var env strings.Builder
				env.WriteString("env:\n")
				hooks := []string{"BASH_ENV", "ENV", "NODE_OPTIONS", "NODE_PATH", "LD_PRELOAD", "LD_LIBRARY_PATH", "PYTHONPATH", "PYTHONSTARTUP", "ARBITRARY_FUTURE_HOOK"}
				for _, name := range append(hooks, "PATH", "HOME", "TMPDIR") {
					env.WriteString("  " + name + ": /untrusted/startup\n")
				}
				source := "---\non: workflow_dispatch\nstrict: false\nexperiments:\n  storage: broker\n  prompt: [control, candidate]\n"
				if imported {
					require.NoError(t, os.WriteFile(filepath.Join(dir, "env.md"), []byte("---\n"+env.String()+"---\n"), 0o600))
					source += "imports: [env.md]\n"
				} else {
					source += env.String()
				}
				compiler := NewCompiler(WithNoEmit(true), WithVersion("v1.0.0"))
				compiler.SetActionMode(mode)
				compiler.gitRoot = dir
				data, err := compiler.ParseWorkflowString(source+"---\nTest.\n", file)
				require.NoError(t, err)
				generated, err := compiler.CompileToYAML(data, file)
				require.NoError(t, err)
				var workflow struct {
					Jobs map[string]struct {
						Env map[string]string `yaml:"env"`
					} `yaml:"jobs"`
				}
				require.NoError(t, yaml.Unmarshal([]byte(generated), &workflow))
				reportEnv := workflow.Jobs["report_experiments"].Env
				for _, hook := range hooks {
					value, exists := reportEnv[hook]
					assert.True(t, exists, "job override must exist before any %s action starts", mode)
					assert.Empty(t, value)
					assert.Contains(t, extractJobSection(generated, "report_experiments"), hook+`: ""`)
				}
				assert.Equal(t, "${{ runner.temp }}", reportEnv["HOME"])
				assert.Equal(t, "${{ runner.temp }}", reportEnv["TMPDIR"])
				assert.Equal(t, "/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin", reportEnv["PATH"])
			})
		}
	}
}

func TestExperimentsBrokerDryRunPickerIsEphemeral(t *testing.T) {
	compiler := NewCompiler()
	steps := strings.Join(compiler.generateExperimentSteps(&WorkflowData{
		WorkflowID:         "broker-test",
		ExperimentsStorage: ExperimentsStorageBroker,
		Experiments:        map[string][]string{"prompt": {"control", "candidate"}},
		DryRun:             true,
	}), "")
	assert.Contains(t, steps, "pick_experiment.cjs")
	assert.Contains(t, steps, "Upload experiment artifact")
	assert.Less(t, strings.Index(steps, "rm -f "+experimentsCacheDir+"/assignments.json"), strings.Index(steps, "pick_experiment.cjs"), "self-hosted previews must discard prior state before picking")
	for _, forbidden := range []string{"experiments_broker.cjs", "actions/cache", "load_experiment_state_from_repo", "GH_AW_EXPERIMENTS_BROKER_URL"} {
		assert.NotContains(t, steps, forbidden)
	}
}
