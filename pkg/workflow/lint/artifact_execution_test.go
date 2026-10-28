package lint

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const artifactTrigger = "on: {workflow_run: {workflows: [CI], branches: [main]}}\n"
const artifactDownloadSteps = "    steps:\n      - uses: actions/download-artifact@v4\n        with: {github-token: '${{ github.token }}', run-id: '${{ github.event.workflow_run.id }}', path: incoming}\n"

func TestArtifactExecutionSinks(t *testing.T) {
	for _, tt := range []struct {
		script, fields string
		count          int
	}{
		{"bash incoming/run.sh", "", 1},
		{"sh 'incoming/run.sh'", "", 1},
		{`python3 "incoming/run.py"`, "", 1},
		{"node incoming/run.js", "", 1},
		{"source incoming/env.sh", "", 1},
		{". incoming/env.sh", "", 1},
		{"./incoming/app", "", 1},
		{"./incoming/../incoming/app", "", 1},
		{"npm ci", "working-directory: incoming", 1},
		{"npm run build", "working-directory: incoming", 1},
		{"npm run-script build", "working-directory: incoming", 1},
		{"npm test", "working-directory: incoming", 1},
		{"npm run", "working-directory: incoming", 0},
		{"npm run-script", "working-directory: incoming", 0},
		{"npm run --help", "working-directory: incoming", 0},
		{"make release", "working-directory: incoming", 1},
		{"npx build", "working-directory: incoming", 1},
		{"bash run.sh", "working-directory: incoming", 1},
		{"./app", "working-directory: incoming", 1},
		{"bash run.sh", "working-directory: '${{github.workspace}}/incoming/'", 1},
		{"bash run.sh", `working-directory: "${{ github['workspace'] }}/incoming/"`, 1},
		{"bash ${{github.workspace}}/incoming/run.sh", "", 1},
		{`bash "${{ github['workspace'] }}/incoming/run.sh"`, "", 1},
		{`bash '${{ github.workspace }}/incoming/run.sh'`, "", 1},
		{"bash $GITHUB_WORKSPACE/incoming/run.sh", "", 1},
		{`bash "${GITHUB_WORKSPACE}/incoming/run.sh"`, "", 1},
		{`"$GITHUB_WORKSPACE/incoming/app"`, "", 1},
		{`bash '$GITHUB_WORKSPACE/incoming/run.sh'`, "", 0},
		{`bash \$GITHUB_WORKSPACE/incoming/run.sh`, "", 0},
		{`bash "${GITHUB_WORKSPACE:-other}/incoming/run.sh"`, "", 0},
		{"bash ${{ runner.temp }}/incoming/run.sh", "", 0},
		{"bash $OTHER/incoming/run.sh", "", 0},
		{"bash incoming/run\\ script.sh", "", 1},
		{"jq . incoming/data.json", "", 0},
		{"cat incoming/report.txt", "", 0},
		{"sha256sum incoming/app", "", 0},
		{"chmod +x incoming/app", "", 0},
		{"echo 'bash incoming/run.sh'", "", 0},
		{"# bash incoming/run.sh", "", 0},
		{"bash trusted/run.sh", "", 0},
		{"bash incoming-other/run.sh", "", 0},
		{"bash /usr/bin/true", "", 0},
		{"bash ../trusted.sh", "working-directory: incoming", 0},
		{"npm ci", "working-directory: trusted", 0},
		{"npm --version", "working-directory: incoming", 0},
		{"npm view package version", "working-directory: incoming", 0},
		{"make --version", "working-directory: incoming", 0},
		{"print('incoming/run.sh')", "shell: python", 0},
	} {
		t.Run(tt.script+"/"+tt.fields, func(t *testing.T) {
			source := artifactTrigger + contractJob + artifactDownloadSteps + "      - run: |\n          " + tt.script + "\n"
			if tt.fields != "" {
				source += "        " + tt.fields + "\n"
			}
			for _, target := range []ASTTargets{AWAST, ActionsAST} {
				diagnostics := integrityTestRun(t, ArtifactExecutionRule(), ruleContractInput(t, source, target, false))
				require.Len(t, diagnostics, tt.count)
				for _, diagnostic := range diagnostics {
					assert.Equal(t, "jobs.check.steps[1].run", diagnostic.Path)
					assert.Equal(t, Warning, diagnostic.Severity)
					assert.Equal(t, 1, diagnostic.Line)
					assert.Equal(t, []RelatedLocation{{Path: "jobs.check.steps[0].with.path"}}, diagnostic.Related)
				}
			}
		})
	}
}

func TestArtifactDownloadBoundary(t *testing.T) {
	for _, tt := range []struct {
		name, action, inputs string
		count                int
	}{
		{"omitted path", "actions/download-artifact@v4", "run-id: '${{ github.event.workflow_run.id }}'", 1},
		{"workspace", "actions/download-artifact@v4", "run-id: 123, path: '${{ github.workspace }}'", 1},
		{"workspace variable literal", "actions/download-artifact@v4", "run-id: 123, path: '$GITHUB_WORKSPACE'", 0},
		{"dot path", "actions/download-artifact@v4", "run-id: 123, path: ./", 1},
		{"parent cleanup", "actions/download-artifact@v4", "run-id: 123, path: incoming/..", 1},
		{"workspace parent", "actions/download-artifact@v4", "run-id: 123, path: ..", 1},
		{"filesystem root", "actions/download-artifact@v4", "run-id: 123, path: /", 1},
		{"workspace no spaces", "actions/download-artifact@v4", "run-id: 123, path: '${{github.workspace}}/'", 1},
		{"workspace bracket", "actions/download-artifact@v4", `run-id: 123, path: "${{ github['workspace'] }}/"`, 1},
		{"dynamic path", "actions/download-artifact@v4", "run-id: 123, path: '${{ vars.PRIVATE_VALUE }}'", 1},
		{"data directory", "actions/download-artifact@v4", "run-id: 123, path: incoming", 0},
		{"workspace subdir", "actions/download-artifact@v4", "run-id: 123, path: '${{ github.workspace }}/incoming'", 0},
		{"workspace subdir no spaces", "actions/download-artifact@v4", "run-id: 123, path: '${{github.workspace}}/incoming/'", 0},
		{"workspace subdir bracket", "actions/download-artifact@v4", `run-id: 123, path: "${{ github['workspace'] }}/incoming/"`, 0},
		{"current run implicit", "actions/download-artifact@v4", "path: .", 0},
		{"current run explicit", "actions/download-artifact@v4", "run-id: '${{ github.run_id }}', path: .", 0},
		{"current run literal expression", "actions/download-artifact@v4", `run-id: "${{ github['run_id'] }}", path: .`, 0},
		{"third party cross-run", "dawidd6/action-download-artifact@v6", "workflow: CI.yml", 1},
		{"third party current run", "dawidd6/action-download-artifact@v6", "run_id: '${{ github.run_id }}'", 0},
		{"trusted custom action", "trusted/artifact-action@v1", "run-id: 123", 0},
	} {
		t.Run(tt.name, func(t *testing.T) {
			inputs := tt.inputs
			if tt.action == "actions/download-artifact@v4" {
				inputs = "github-token: '${{ github.token }}', " + inputs
			}
			source := artifactTrigger + contractJob + "    steps:\n      - uses: " + tt.action + "\n        with: {" + inputs + "}\n"
			for _, target := range []ASTTargets{AWAST, ActionsAST} {
				diagnostics := integrityTestRun(t, ArtifactExecutionRule(), ruleContractInput(t, source, target, false))
				require.Len(t, diagnostics, tt.count)
				for _, diagnostic := range diagnostics {
					assert.Equal(t, "jobs.check.steps[0].with.path", diagnostic.Path)
					assert.NotContains(t, diagnostic.Message, "PRIVATE_VALUE")
				}
			}
		})
	}
}

func TestArtifactDownloadTokenBoundary(t *testing.T) {
	for _, tt := range []struct {
		name, token string
		count       int
	}{
		{"omitted", "", 0},
		{"empty", "github-token: '', ", 0},
		{"whitespace", "github-token: '  ', ", 0},
		{"null", "github-token: null, ", 0},
		{"empty expression", `github-token: "${{ '' }}", `, 0},
		{"whitespace expression", `github-token: "${{ '  ' }}", `, 0},
		{"null expression", `github-token: "${{ null }}", `, 0},
		{"dynamic", "github-token: '${{ secrets.TOKEN }}', ", 2},
		{"literal", "github-token: token, ", 2},
	} {
		t.Run(tt.name, func(t *testing.T) {
			source := artifactTrigger + contractJob + "    steps:\n      - uses: actions/download-artifact@v4\n        with: {" +
				tt.token + "run-id: 123, path: .}\n      - run: bash run.sh\n"
			for _, target := range []ASTTargets{AWAST, ActionsAST} {
				diagnostics := integrityTestRun(t, ArtifactExecutionRule(), ruleContractInput(t, source, target, false))
				assert.Len(t, diagnostics, tt.count)
			}
		})
	}
}

func TestArtifactPathResolution(t *testing.T) {
	for _, tt := range []struct {
		name, directory, script, workingDirectory string
		count                                     int
	}{
		{"workspace parent", "..", "bash run.sh", "", 2},
		{"workspace root ancestor", "/", "bash run.sh", "", 2},
		{"parent sibling execution", "..", "bash ../other/run.sh", "", 2},
		{"parent does not contain system", "..", "bash /usr/bin/true", "", 1},
		{"unknown runner directory", ".", "bash ${{ runner.temp }}/tool.sh", "", 1},
		{"unknown environment directory", ".", "bash $OTHER/tool.sh", "", 1},
		{"unknown quoted expression", ".", `bash "${{ vars.PATH }}/tool.sh"`, "", 1},
		{"literal action variable", "$GITHUB_WORKSPACE/incoming", "bash incoming/run.sh", "", 0},
		{"literal action braced variable", "${GITHUB_WORKSPACE}/incoming", "bash incoming/run.sh", "", 0},
		{"literal action variable execution", "$GITHUB_WORKSPACE/incoming", `bash '$GITHUB_WORKSPACE/incoming/run.sh'`, "", 1},
		{"expanded shell differs from literal input", "$GITHUB_WORKSPACE/incoming", `bash "$GITHUB_WORKSPACE/incoming/run.sh"`, "", 0},
		{"literal working directory variable", "incoming", "bash run.sh", "$GITHUB_WORKSPACE/incoming", 0},
		{"workspace shell prefix ignores cwd", "incoming", "bash $GITHUB_WORKSPACE/incoming/run.sh", "trusted", 1},
		{"workspace expression ignores cwd", "incoming", "bash ${{github.workspace}}/incoming/run.sh", "trusted", 1},
		{"workspace suffix normalization", "${{github.workspace}}/other/../incoming/", "bash incoming/run.sh", "", 1},
		{"workspace bracket normalization", "${{ github['workspace'] }}/incoming/", "bash incoming/run.sh", "", 1},
		{"unknown expression after root download", ".", "${{ vars.COMMAND }}/tool.sh", "", 1},
	} {
		t.Run(tt.name, func(t *testing.T) {
			source := artifactTrigger + contractJob + "    steps:\n      - uses: actions/download-artifact@v4\n" +
				"        with:\n          github-token: ${{ github.token }}\n          run-id: 123\n          path: " + tt.directory +
				"\n      - run: |\n          " + tt.script + "\n"
			if tt.workingDirectory != "" {
				source += "        working-directory: " + tt.workingDirectory + "\n"
			}
			for _, target := range []ASTTargets{AWAST, ActionsAST} {
				diagnostics := integrityTestRun(t, ArtifactExecutionRule(), ruleContractInput(t, source, target, false))
				assert.Len(t, diagnostics, tt.count)
			}
		})
	}
}

func TestArtifactOriginalOperandOffsets(t *testing.T) {
	source := artifactTrigger + contractJob + artifactDownloadSteps + `      - run: |
          echo 'unicode: café' "${{ vars.UNRELATED }}"
          bash "${{ github['workspace'] }}/incoming/run.sh"
          bash "${{ runner.temp }}/tool.sh"
          bash "$GITHUB_WORKSPACE/incoming/run.sh"
`
	for _, target := range []ASTTargets{AWAST, ActionsAST} {
		diagnostics := integrityTestRun(t, ArtifactExecutionRule(), ruleContractInput(t, source, target, false))
		require.Len(t, diagnostics, 2)
		assert.Equal(t, 2, diagnostics[0].Line)
		assert.Equal(t, 4, diagnostics[1].Line)
		for _, diagnostic := range diagnostics {
			assert.Equal(t, []RelatedLocation{{Path: "jobs.check.steps[0].with.path"}}, diagnostic.Related)
		}
	}
}

func TestArtifactExecutionOrderingAndScopes(t *testing.T) {
	cases := []struct {
		name, source, path string
	}{
		{"local action", artifactTrigger + contractJob + artifactDownloadSteps + "      - uses: ./incoming/action\n", "jobs.check.steps[1].uses"},
		{"earlier execution", artifactTrigger + contractJob + "    steps:\n      - run: bash incoming/run.sh\n      - uses: actions/download-artifact@v4\n        with: {github-token: '${{ github.token }}', run-id: 123, path: incoming}\n", ""},
		{"other job", artifactTrigger + contractJob + artifactDownloadSteps + "  other:\n    runs-on: ubuntu-latest\n    steps: [{run: bash incoming/run.sh}]\n", ""},
		{"current run execution", artifactTrigger + contractJob + "    steps:\n      - uses: actions/download-artifact@v4\n        with: {path: incoming}\n      - run: bash incoming/run.sh\n", ""},
		{"not workflow run", "on: push\n" + contractJob + artifactDownloadSteps + "      - run: bash incoming/run.sh\n", ""},
		{"job default directory", artifactTrigger + contractJob + "    defaults: {run: {working-directory: incoming}}\n" + artifactDownloadSteps + "      - run: bash run.sh\n", "jobs.check.steps[1].run"},
		{"workflow default directory", artifactTrigger + "defaults: {run: {working-directory: incoming}}\n" + contractJob + artifactDownloadSteps + "      - run: bash run.sh\n", "jobs.check.steps[1].run"},
		{"step overrides default", artifactTrigger + contractJob + "    defaults: {run: {working-directory: incoming}}\n" + artifactDownloadSteps + "      - run: bash run.sh\n        working-directory: trusted\n", ""},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			for _, target := range []ASTTargets{AWAST, ActionsAST} {
				diagnostics := integrityTestRun(t, ArtifactExecutionRule(), ruleContractInput(t, tt.source, target, false))
				if tt.path == "" {
					assert.Empty(t, diagnostics)
				} else {
					require.Len(t, diagnostics, 1)
					assert.Equal(t, tt.path, diagnostics[0].Path)
				}
			}
		})
	}
}

func TestArtifactExecutionAWPostSteps(t *testing.T) {
	source := artifactTrigger + "steps:\n  - uses: actions/download-artifact@v4\n    with: {github-token: '${{ github.token }}', run-id: 123, path: incoming}\npost-steps:\n  - run: bash incoming/run.sh\n"
	diagnostics := integrityTestRun(t, ArtifactExecutionRule(), ruleContractInput(t, source, AWAST, false))
	require.Len(t, diagnostics, 1)
	assert.Equal(t, "post-steps[0].run", diagnostics[0].Path)
}

func TestPrivilegedCodeMalformedShellCoverage(t *testing.T) {
	for _, rule := range []Rule{PrivilegedCheckoutRule(), ArtifactExecutionRule()} {
		source := artifactTrigger + contractJob + artifactDownloadSteps + "      - run: 'bash PRIVATE_VALUE; if'\n"
		input := ruleContractInput(t, source, AWAST, false)
		diagnostics := integrityTestRun(t, rule, input)
		require.Len(t, diagnostics, 1)
		assert.Contains(t, diagnostics[0].Message, "Incomplete privileged-code analysis")
		assert.NotContains(t, diagnostics[0].Message, "PRIVATE_VALUE")
		runner, err := New(rule)
		require.NoError(t, err)
		input.Document.Frontmatter["jobs"] = map[string]any{"invalid": "PRIVATE_VALUE"}
		_, err = runner.Run(context.Background(), input)
		require.Error(t, err)
		assert.NotContains(t, err.Error(), "PRIVATE_VALUE")
	}
}

func TestArticleArtifactChain(t *testing.T) {
	source := artifactTrigger + contractJob + `    steps:
      - uses: dawidd6/action-download-artifact@v6
        with:
          workflow: unprivileged-ci.yml
          run_id: ${{ github.event.workflow_run.id }}
          path: incoming
      - run: |
          chmod +x incoming/compute.sh
          ./incoming/compute.sh
`
	for _, target := range []ASTTargets{AWAST, ActionsAST} {
		input := ruleContractInput(t, source, target, false)
		diagnostics := integrityTestRun(t, ArtifactExecutionRule(), input)
		require.Len(t, diagnostics, 1)
		assert.Equal(t, 2, diagnostics[0].Line)
		assert.Contains(t, diagnostics[0].Message, "repository identity alone")
	}
}
