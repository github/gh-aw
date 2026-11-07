package lint

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/workflow/actions"
	"github.com/github/gh-aw/pkg/workflow/aw"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCustomJobDetectionConditions(t *testing.T) {
	for _, tt := range []struct {
		name    string
		value   any
		safe    bool
		invalid bool
	}{
		{"implicit success", nil, true, false},
		{"boolean true", true, true, false},
		{"boolean false", false, true, false},
		{"ordinary predicate", "needs.agent.result == 'success'", true, false},
		{"string is not status call", "contains('always()', 'always')", true, false},
		{"always bypass", "always()", false, false},
		{"failure bypass", "failure()", false, false},
		{"cancelled bypass", "cancelled()", false, false},
		{"not cancelled bypass", "!cancelled()", false, false},
		{"not skipped is insufficient", "always() && needs.detection.result != 'skipped'", false, false},
		{"not failure is insufficient", "always() && needs.detection.result != 'failure'", false, false},
		{"guarded always", "always() && needs.detection.result == 'success'", true, false},
		{"wrapped guard", "${{ always() && needs.detection.result == 'success' }}", true, false},
		{"reversed guard", "'success' == needs.detection.result && always()", true, false},
		{"case and brackets", "ALWAYS() && NEEDS['Detection']['Result'] == 'SUCCESS'", true, false},
		{"success function", "success()", true, false},
		{"success function guard", "always() && success()", true, false},
		{"or bypass", "always() || needs.detection.result == 'success'", false, false},
		{"nested or bypass", "always() && (needs.detection.result == 'success' || inputs.override)", false, false},
		{"both branches guarded", "(always() && needs.detection.result == 'success') || success()", true, false},
		{"outer guard", "(always() || inputs.override) && needs.detection.result == 'success'", true, false},
		{"negated success bypass", "always() && !(needs.detection.result == 'success')", false, false},
		{"other job", "always() && needs.scan.result == 'success'", false, false},
		{"other field", "always() && needs.detection.outputs.result == 'success'", false, false},
		{"dynamic index", "always() && needs[inputs.job].result == 'success'", false, false},
		{"quoted guard", "always() && 'needs.detection.result == ''success'''", false, false},
		{"commented guard", "always() # needs.detection.result == 'success'", false, true},
		{"malformed", "always() &&", false, true},
		{"empty", "", false, true},
		{"incomplete wrapper", "${{ always()", false, true},
		{"trailing expression", "${{ always() }} || true", false, true},
		{"early closing delimiter", "always() }} && needs.detection.result == 'success'", false, true},
		{"non condition", []any{"success()"}, false, true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			safe, err := customJobDetectionCondition(tt.value)
			if tt.invalid {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.safe, safe)
		})
	}
}

func TestCustomJobDetectionAuthoredJobs(t *testing.T) {
	for _, tt := range []struct {
		name   string
		source string
		path   string
	}{
		{"missing detection", "jobs:\n  report:\n    needs: agent\n", "jobs.report.needs"},
		{"missing detection with explicit guard", "jobs:\n  report:\n    needs: agent\n    if: always() && needs.detection.result == 'success'\n", "jobs.report.needs"},
		{"always override", "jobs:\n  report:\n    needs: [agent, detection]\n    if: always()\n", "jobs.report.if"},
		{"detection only override", "jobs:\n  report:\n    needs: detection\n    if: always()\n", "jobs.report.if"},
		{"detection only success", "jobs:\n  report:\n    needs: detection\n", ""},
		{"transitive agent dependency", "jobs:\n  scan:\n    needs: [agent, detection]\n  report:\n    needs: scan\n", "jobs.report.needs"},
		{"transitive guarded dependency", "jobs:\n  scan:\n    needs: [agent, detection]\n  report:\n    needs: [scan, detection]\n    if: always() && needs.detection.result == 'success'\n", ""},
		{"after builtin output job", "jobs:\n  report:\n    needs: safe_outputs\n", "jobs.report.needs"},
		{"before detection after agent", "jobs:\n  scan:\n    needs: agent\n  detection:\n    needs: scan\n", "jobs.scan.needs"},
		{"implicit success", "jobs:\n  report:\n    needs: [agent, detection]\n", ""},
		{"explicit success", "jobs:\n  report:\n    needs: [agent, detection]\n    if: always() && needs.detection.result == 'success'\n", ""},
		{"unrelated job", "jobs:\n  prepare:\n    needs: activation\n", ""},
		{"no dependencies", "jobs:\n  prepare:\n    runs-on: ubuntu-latest\n", ""},
		{"compiler owned conclusion", "jobs:\n  conclusion:\n    needs: agent\n    if: always()\n", ""},
		{"disabled detection", "safe-outputs:\n  threat-detection: false\njobs:\n  report:\n    needs: agent\n", ""},
		{"disabled detection engine", "safe-outputs:\n  threat-detection:\n    engine: false\n    steps: []\njobs:\n  report:\n    needs: agent\n", ""},
		{"custom detection steps still run", "safe-outputs:\n  threat-detection:\n    engine: false\n    steps:\n      - run: echo scan\njobs:\n  report:\n    needs: agent\n", "jobs.report.needs"},
		{"conditional detection", "safe-outputs:\n  threat-detection: '${{ inputs.detect }}'\njobs:\n  report:\n    needs: agent\n", "jobs.report.needs"},
		{"custom jobs not inferred from safe output names", "safe-outputs:\n  jobs:\n    report:\n      needs: agent\n", ""},
	} {
		t.Run(tt.name, func(t *testing.T) {
			doc, err := aw.Parse("---\n" + tt.source + "---\nPrompt\n")
			require.NoError(t, err)
			runner, err := Default().Select("custom-job-detection")
			require.NoError(t, err)
			diagnostics, err := runner.Run(context.Background(), Input{Document: doc})
			require.NoError(t, err)
			if tt.path == "" {
				assert.Empty(t, diagnostics)
			} else {
				require.Len(t, diagnostics, 1)
				assert.Equal(t, tt.path, diagnostics[0].Path)
				assert.Equal(t, Error, diagnostics[0].Severity)
			}
		})
	}
}

func TestCustomJobDetectionStatusOnly(t *testing.T) {
	for _, tt := range []struct {
		name, jobFields, wantPath string
	}{
		{
			name: "status and gated safe output",
			jobFields: "needs: [agent, detection, safe_outputs]\n" +
				"    detection-gate: status-only\n" +
				"    if: always()\n" +
				"    runs-on: ubuntu-latest\n" +
				"    steps: [{run: 'echo ${{ needs.agent.result }} ${{ needs.detection.result }} ${{ needs.safe_outputs.outputs.created_issue_number }}'}]\n",
		},
		{
			name: "agent output",
			jobFields: "needs: [agent, detection]\n" +
				"    detection-gate: status-only\n" +
				"    if: always()\n" +
				"    runs-on: ubuntu-latest\n" +
				"    steps: [{run: 'echo ${{ needs.agent.outputs.result }}'}]\n",
			wantPath: "jobs.report.steps[0].run",
		},
		{
			name: "detection output",
			jobFields: "needs: [agent, detection]\n" +
				"    detection-gate: status-only\n" +
				"    if: always()\n" +
				"    runs-on: ubuntu-latest\n" +
				"    steps: [{run: 'echo ${{ needs.detection.outputs.detection_success }}'}]\n",
			wantPath: "jobs.report.steps[0].run",
		},
		{
			name: "artifact download",
			jobFields: "needs: [agent, detection]\n" +
				"    detection-gate: status-only\n" +
				"    if: always()\n" +
				"    runs-on: ubuntu-latest\n" +
				"    steps: [{uses: actions/download-artifact@v4}]\n",
			wantPath: "jobs.report.steps[0].uses",
		},
		{
			name: "non-detection-gated compiler output",
			jobFields: "needs: [agent, detection, activation]\n" +
				"    detection-gate: status-only\n" +
				"    if: always()\n" +
				"    runs-on: ubuntu-latest\n" +
				"    steps: [{run: 'echo ${{ needs.activation.outputs.result }}'}]\n",
			wantPath: "jobs.report.steps[0].run",
		},
		{
			name: "opaque reusable workflow",
			jobFields: "needs: [agent, detection]\n" +
				"    detection-gate: status-only\n" +
				"    if: always()\n" +
				"    uses: ./.github/workflows/status.yml\n",
			wantPath: "jobs.report.uses",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			source := "jobs:\n  report:\n    " + tt.jobFields
			doc, err := aw.Parse("---\nsafe-outputs:\n  create-issue:\n  threat-detection: true\n" + source + "---\nPrompt\n")
			require.NoError(t, err)
			runner, err := Default().Select("custom-job-detection")
			require.NoError(t, err)
			diagnostics, err := runner.Run(context.Background(), Input{Document: doc})
			require.NoError(t, err)
			if tt.wantPath == "" {
				assert.Empty(t, diagnostics)
				return
			}
			require.Len(t, diagnostics, 1)
			assert.Equal(t, tt.wantPath, diagnostics[0].Path)
			assert.Equal(t, Error, diagnostics[0].Severity)
		})
	}
}

func TestCustomJobDetectionStatusOnlyMustBeLeaf(t *testing.T) {
	doc, err := aw.Parse("---\nsafe-outputs:\n  create-issue:\n  threat-detection: true\njobs:\n  status:\n    needs: [agent, detection]\n    detection-gate: status-only\n    if: always()\n  notify:\n    needs: [status, detection]\n---\nPrompt\n")
	require.NoError(t, err)
	runner, err := Default().Select("custom-job-detection")
	require.NoError(t, err)
	diagnostics, err := runner.Run(context.Background(), Input{Document: doc})
	require.NoError(t, err)
	found := false
	for _, diagnostic := range diagnostics {
		found = found || diagnostic.Path == "jobs.status.detection-gate"
	}
	assert.True(t, found, "expected dependent custom jobs to invalidate status-only opt-in")
}

func TestCustomJobDetectionStatusOnlyCompiledPreAgentOutputs(t *testing.T) {
	for _, tt := range []struct {
		name, agentNeeds, producerNeeds, reporterNeeds string
		allowed, generated                             bool
	}{
		{"pre-agent", "[activation, eligibility]", "[]", "", true, false},
		{"before activation", "activation", "[]", "", true, false},
		{"parallel", "activation", "[]", "", false, false},
		{"post-agent", "activation", "agent", "", false, false},
		{"post-detection", "activation", "detection", "", false, false},
		{"generated pre-agent", "[activation, eligibility]", "[]", "", false, true},
		{"missing direct dependency", "[activation, eligibility]", "[]", "[agent, detection]", false, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			reporterNeeds := tt.reporterNeeds
			if reporterNeeds == "" {
				reporterNeeds = "[eligibility, agent, detection]"
			}
			source := "jobs:\n" +
				"  eligibility:\n    needs: " + tt.producerNeeds + "\n    runs-on: ubuntu-latest\n    steps: [{run: echo metadata}]\n" +
				"  report:\n    needs: " + reporterNeeds + "\n    detection-gate: status-only\n    if: always()\n" +
				"    runs-on: ubuntu-latest\n    steps:\n      - env:\n          PR: ${{ needs.eligibility.outputs.pr_number }}\n        run: echo \"$PR\"\n"
			authored := source
			if tt.generated {
				authored = "jobs:\n  report:\n" + strings.SplitN(source, "  report:\n", 2)[1]
			}
			document, err := aw.Parse("---\nsafe-outputs:\n  create-issue:\n" + authored + "---\nPrompt\n")
			require.NoError(t, err)
			activationNeeds := ""
			if tt.name == "before activation" {
				activationNeeds = "    needs: eligibility\n"
			}
			compiled := "jobs:\n" +
				"  activation:\n" + activationNeeds + "    runs-on: ubuntu-latest\n    steps: [{run: echo activation}]\n" +
				"  agent:\n    needs: " + tt.agentNeeds + "\n    runs-on: ubuntu-latest\n    steps: [{run: echo agent}]\n" +
				"  detection:\n    needs: agent\n    runs-on: ubuntu-latest\n    steps: [{run: echo detection}]\n" +
				strings.TrimPrefix(source, "jobs:\n")
			workflow, err := actions.Parse([]byte(strings.ReplaceAll(compiled, "    detection-gate: status-only\n", "")))
			require.NoError(t, err)
			runner, err := Default().Select("custom-job-detection")
			require.NoError(t, err)
			diagnostics, err := runner.Run(context.Background(), Input{Document: document, Workflow: workflow})
			require.NoError(t, err)
			if tt.allowed {
				assert.Empty(t, diagnostics)
				return
			}
			want := "needs.eligibility.outputs"
			if tt.name == "missing direct dependency" {
				want = "listed in needs"
			}
			assert.Condition(t, func() bool {
				for _, diagnostic := range diagnostics {
					if diagnostic.Path == "jobs.report.steps[0].env.PR" && strings.Contains(diagnostic.Message, want) {
						return true
					}
				}
				return false
			})
		})
	}
}

func TestCustomJobDetectionStatusOnlyPreAgentNeedsInAWOnlyMode(t *testing.T) {
	doc, err := aw.Parse(`---
safe-outputs:
  threat-detection: true
jobs:
  eligibility:
    runs-on: ubuntu-latest
  report:
    needs: [agent, detection]
    detection-gate: status-only
    if: always()
    steps:
      - env:
          HEAD_SHA: ${{ needs.eligibility.outputs.head_sha }}
        run: echo "$HEAD_SHA"
---
`)
	require.NoError(t, err)
	runner, err := Default().Select("custom-job-detection")
	require.NoError(t, err)
	diagnostics, err := runner.Run(context.Background(), Input{Document: doc})
	require.NoError(t, err)
	require.Len(t, diagnostics, 1)
	assert.Contains(t, diagnostics[0].Message, "listed in needs")
}

func TestCustomJobDetectionStatusOnlyPreAgentOutputsUseEnv(t *testing.T) {
	doc, err := aw.Parse(`---
safe-outputs:
  threat-detection: true
jobs:
  eligibility:
    runs-on: ubuntu-latest
  report:
    needs: [eligibility, agent, detection]
    detection-gate: status-only
    if: always()
    steps:
      - run: echo '${{ needs.eligibility.outputs.head_sha }}'
---
`)
	require.NoError(t, err)
	runner, err := Default().Select("custom-job-detection")
	require.NoError(t, err)
	diagnostics, err := runner.Run(context.Background(), Input{Document: doc})
	require.NoError(t, err)
	require.Len(t, diagnostics, 1)
	assert.Contains(t, diagnostics[0].Message, "through env")
}

func TestCustomJobDetectionStatusOnlyPreAgentOutputsNotInterpolatedIntoScript(t *testing.T) {
	doc, err := aw.Parse(`---
safe-outputs:
  threat-detection: true
jobs:
  eligibility:
    runs-on: ubuntu-latest
  report:
    needs: [eligibility, agent, detection]
    detection-gate: status-only
    if: always()
    steps:
      - uses: actions/github-script@v7
        with:
          script: console.log('${{ needs.eligibility.outputs.head_sha }}')
---
`)
	require.NoError(t, err)
	runner, err := Default().Select("custom-job-detection")
	require.NoError(t, err)
	diagnostics, err := runner.Run(context.Background(), Input{Document: doc})
	require.NoError(t, err)
	require.Len(t, diagnostics, 1)
	assert.Contains(t, diagnostics[0].Message, "through env")
}

func TestCustomJobDetectionCompiledImportedPreAgentOutput(t *testing.T) {
	doc, err := aw.Parse(`---
safe-outputs:
  threat-detection: true
jobs:
  report:
    needs: [eligibility, agent, detection]
    detection-gate: status-only
    if: always()
    steps:
      - env:
          HEAD_SHA: ${{ needs.eligibility.outputs.head_sha }}
        run: echo "$HEAD_SHA"
---
`)
	require.NoError(t, err)
	workflow, err := actions.Parse([]byte(`jobs:
  eligibility:
    runs-on: ubuntu-latest
    steps: [{run: echo eligibility}]
  agent:
    needs: eligibility
    runs-on: ubuntu-latest
    steps: [{run: echo agent}]
  detection:
    needs: agent
    runs-on: ubuntu-latest
    steps: [{run: echo detection}]
  report:
    needs: [eligibility, agent, detection]
    if: always()
    runs-on: ubuntu-latest
    steps: [{run: echo report}]
`))
	require.NoError(t, err)
	importedJobs := map[string]any{
		"eligibility": map[string]any{"runs-on": "ubuntu-latest"},
		"report":      map[string]any{"needs": []any{"eligibility", "agent", "detection"}, "detection-gate": "status-only", "if": "always()"},
	}
	runner, err := Default().Select("custom-job-detection")
	require.NoError(t, err)
	for _, tt := range []struct {
		name         string
		authoredJobs map[string]any
		want         string
	}{
		{"missing imported provenance", nil, "needs.eligibility.outputs"},
		{"imported producer provenance", importedJobs, ""},
	} {
		t.Run(tt.name, func(t *testing.T) {
			diagnostics, err := runner.Run(context.Background(), Input{Workflow: workflow, Document: doc, AuthoredJobs: tt.authoredJobs})
			require.NoError(t, err)
			if tt.want == "" {
				assert.Empty(t, diagnostics)
				return
			}
			require.Len(t, diagnostics, 1)
			assert.Contains(t, diagnostics[0].Message, tt.want)
		})
	}
}

func TestCustomJobDetectionAllowedOutputsRequiresSource(t *testing.T) {
	workflow, err := actions.Parse([]byte("jobs:\n  metadata:\n    runs-on: ubuntu-latest\n    steps: [{run: echo metadata}]\n  agent:\n    needs: metadata\n    runs-on: ubuntu-latest\n    steps: [{run: echo agent}]\n"))
	require.NoError(t, err)
	input := Input{Workflow: workflow}
	jobs, graph, err := customJobDetectionJobs(context.Background(), input)
	require.NoError(t, err)
	allowed, _ := customJobDetectionAllowedOutputs(input, jobs, graph)
	assert.NotContains(t, allowed, "metadata")
}

func TestCustomJobDetectionActions(t *testing.T) {
	for _, tt := range []struct {
		name, jobs, path string
	}{
		{"direct bypass", "  report:\n    needs: agent\n", "jobs.report.needs"},
		{"detection bypass", "  report:\n    needs: detection\n    if: always()\n", "jobs.report.if"},
		{"detection success", "  report:\n    needs: detection\n", ""},
		{"transitive bypass", "  scan:\n    needs: [agent, detection]\n  report:\n    needs: scan\n    if: always()\n", "jobs.report.needs"},
		{"parallel job", "  report:\n", "jobs.report.needs"},
		{"guarded branch", "  report:\n    needs: [agent, detection]\n    if: always() && needs.detection.result == 'success'\n", ""},
		{"unguarded branch", "  report:\n    needs: [agent, detection]\n    if: always() || needs.detection.result == 'success'\n", "jobs.report.if"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			source := "jobs:\n  agent:\n    runs-on: ubuntu-latest\n    steps: [{run: echo agent}]\n  detection:\n    needs: agent\n    runs-on: ubuntu-latest\n    steps: [{run: echo detection}]\n" + tt.jobs
			source = strings.ReplaceAll(source, "  report:\n", "  report:\n    uses: ./.github/workflows/report.yml\n")
			source = strings.ReplaceAll(source, "  scan:\n", "  scan:\n    uses: ./.github/workflows/scan.yml\n")
			workflow, err := actions.Parse([]byte(source))
			require.NoError(t, err)
			runner, err := Default().Select("custom-job-detection")
			require.NoError(t, err)
			diagnostics, err := runner.Run(context.Background(), Input{Workflow: workflow})
			require.NoError(t, err)
			if tt.path == "" {
				assert.Empty(t, diagnostics)
			} else {
				require.Len(t, diagnostics, 1)
				assert.Equal(t, tt.path, diagnostics[0].Path)
			}
		})
	}
}

func TestCustomJobDetectionCompiledJobProvenance(t *testing.T) {
	workflow, err := actions.Parse([]byte(`jobs:
  activation:
    runs-on: ubuntu-latest
    steps: [{run: echo activation}]
  agent:
    needs: activation
    runs-on: ubuntu-latest
    steps: [{run: echo agent}]
  detection:
    needs: agent
    runs-on: ubuntu-latest
    steps: [{run: echo detection}]
  conclusion:
    needs: [agent, detection]
    if: always()
    runs-on: ubuntu-latest
    steps: [{run: echo conclusion}]
  push_evals_state:
    needs: agent
    if: always()
    runs-on: ubuntu-latest
    steps: [{run: echo maintenance}]
`))
	require.NoError(t, err)
	runner, err := Default().Select("custom-job-detection")
	require.NoError(t, err)
	for _, tt := range []struct {
		name         string
		source       string
		authoredJobs map[string]any
		wantPath     string
	}{
		{name: "Actions only"},
		{name: "source without jobs", source: "engine: copilot\n"},
		{name: "source with null jobs", source: "jobs: null\n"},
		{name: "source with empty jobs", source: "jobs: {}\n"},
		{
			name:     "authored maintenance name",
			source:   "jobs:\n  push_evals_state:\n    needs: agent\n    runs-on: ubuntu-latest\n",
			wantPath: "jobs.push_evals_state.needs",
		},
		{
			name: "resolved provenance without document",
			authoredJobs: map[string]any{
				"push_evals_state": map[string]any{"needs": "agent", "runs-on": "ubuntu-latest"},
			},
			wantPath: "jobs.push_evals_state.needs",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			input := Input{Workflow: workflow, AuthoredJobs: tt.authoredJobs}
			if tt.source != "" {
				input.Document, err = aw.Parse("---\n" + tt.source + "---\nPrompt\n")
				require.NoError(t, err)
			}
			diagnostics, err := runner.Run(context.Background(), input)
			require.NoError(t, err)
			if tt.wantPath == "" {
				assert.Empty(t, diagnostics)
				return
			}
			require.Len(t, diagnostics, 1)
			assert.Equal(t, tt.wantPath, diagnostics[0].Path)
			assert.Equal(t, Error, diagnostics[0].Severity)
		})
	}
}

func TestCustomJobDetectionInvalidInput(t *testing.T) {
	rule := CustomJobDetectionRule()
	for _, source := range []string{
		"jobs: invalid\n",
		"jobs:\n  report: invalid\n",
		"jobs:\n  report:\n    needs: [agent, 123]\n",
		"jobs:\n  report:\n    needs: [agent, detection]\n    if: always() &&\n",
		"safe-outputs: invalid\n",
		"safe-outputs:\n  threat-detection:\n    engine: false\n    steps: invalid\n",
	} {
		doc, err := aw.Parse("---\n" + source + "---\n")
		require.NoError(t, err)
		_, err = rule.Check(context.Background(), Input{Document: doc})
		require.Error(t, err)
		assert.NotContains(t, err.Error(), "123")
	}
	_, err := rule.Check(context.Background(), Input{})
	require.Error(t, err)
	doc, err := aw.Parse("---\njobs:\n  report:\n    needs: agent\n---\n")
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = rule.Check(ctx, Input{Document: doc})
	require.ErrorIs(t, err, context.Canceled)
	diagnostics, err := rule.Check(context.Background(), Input{Document: doc, Workflow: &actions.Workflow{}})
	require.NoError(t, err)
	assert.Empty(t, diagnostics, "the compiled graph confirms detection is absent")
}

func TestCustomJobDetectionSharedFragments(t *testing.T) {
	files, err := filepath.Glob(filepath.Join("..", "..", "..", ".github", "workflows", "shared", "*.md"))
	require.NoError(t, err)
	require.NotEmpty(t, files)
	runner, err := Default().Select("custom-job-detection")
	require.NoError(t, err)
	for _, file := range files {
		t.Run(filepath.Base(file), func(t *testing.T) {
			source, err := os.ReadFile(file)
			require.NoError(t, err)
			doc, err := aw.Parse(string(source))
			require.NoError(t, err)
			diagnostics, err := runner.Run(context.Background(), Input{Document: doc})
			require.NoError(t, err)
			assert.Empty(t, diagnostics)
		})
	}
}
