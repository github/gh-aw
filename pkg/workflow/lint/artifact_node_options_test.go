package lint

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestArtifactNodeOptionsAfterEval(t *testing.T) {
	for _, tt := range []struct {
		script string
		count  int
	}{
		{"node --eval '1' --require ./incoming/setup.cjs", 1},
		{"node --eval='1' --import ./incoming/setup.mjs", 1},
		{"node -e '1' -r./incoming/setup.cjs", 1},
		{"node --print '1' --loader ./incoming/loader.mjs", 1},
		{"node -p '1' --experimental-loader=./incoming/loader.mjs", 1},
		{"node -pe '1' --require ./incoming/setup.cjs", 1},
		{"node --print --require ./incoming/setup.cjs --eval '1'", 1},
		{"node -p -e '1' --require ./incoming/setup.cjs", 1},
		{"node --eval '1' --require ./incoming/setup.cjs --version", 0},
		{"node --require ./incoming/setup.cjs -p '1' -v", 0},
		{"node --require ./incoming/setup.cjs --eval='1' --v8-options", 0},
		{"node --eval '1' --require ./incoming/setup.cjs --help", 1},
		{"node --eval '1' trusted/run.js --require ./incoming/setup.cjs", 0},
		{"node --eval '1' -- --require ./incoming/setup.cjs", 0},
	} {
		t.Run(tt.script, func(t *testing.T) {
			source := artifactTrigger + contractJob + artifactDownloadSteps + "      - run: |\n          " + tt.script + "\n"
			for _, target := range []ASTTargets{AWAST, ActionsAST} {
				diagnostics := integrityTestRun(t, ArtifactExecutionRule(), ruleContractInput(t, source, target, false))
				require.Len(t, diagnostics, tt.count)
				for _, diagnostic := range diagnostics {
					assert.Contains(t, diagnostic.Message, "execute cross-run artifact content")
					assert.Equal(t, "jobs.check.steps[1].run", diagnostic.Path)
					assert.Equal(t, 1, diagnostic.Line)
				}
			}
		})
	}
}

func TestArtifactNodeBarePreloadCoverage(t *testing.T) {
	for _, tt := range []struct {
		script string
		count  int
	}{
		{"node -r dotenv/config trusted/run.js", 1},
		{"node --import some-pkg trusted/run.js", 1},
		{"node --require incoming/setup.js trusted/run.js", 1},
		{"node --import incoming/setup.mjs trusted/run.js", 1},
		{"node --loader @scope/loader trusted/run.js", 1},
		{"node --experimental-loader=@scope/loader trusted/run.js", 1},
		{"node --eval '1' --require=dotenv/config", 1},
		{"node --require dotenv/config --import some-pkg --eval '1'", 1},
		{"node --require dotenv/config --eval '1' --version", 0},
		{"node --require dotenv/config --print '1' --v8-options", 0},
		{"node --require dotenv/config --help", 1},
		{"node trusted/run.js --require dotenv/config", 0},
		{"node --require node:fs --eval '1'", 0},
		{"node --require fs --eval '1'", 0},
		{"node --import fs/promises --eval '1'", 0},
		{"node --import data:text/javascript,void%280%29 --eval '1'", 0},
	} {
		t.Run(tt.script, func(t *testing.T) {
			source := artifactTrigger + contractJob + artifactDownloadSteps + "      - run: |\n          " + tt.script + "\n"
			for _, target := range []ASTTargets{AWAST, ActionsAST} {
				diagnostics := integrityTestRun(t, ArtifactExecutionRule(), ruleContractInput(t, source, target, false))
				require.Len(t, diagnostics, tt.count)
				for _, diagnostic := range diagnostics {
					assert.Contains(t, diagnostic.Message, "Incomplete artifact-execution analysis")
					assert.NotContains(t, diagnostic.Message, "A later step may execute")
					assert.Equal(t, "jobs.check.steps[1].run", diagnostic.Path)
					assert.Equal(t, Warning, diagnostic.Severity)
					assert.Equal(t, 1, diagnostic.Line)
					assert.Equal(t, []RelatedLocation{{Path: "jobs.check.steps[0].with.path"}}, diagnostic.Related)
				}
			}

		})
	}
}

func TestArtifactNodeBarePreloadDynamicDirectory(t *testing.T) {
	source := artifactTrigger + contractJob + artifactDownloadSteps + "      - run: node --require dotenv/config --eval '1'\n        working-directory: '${{ vars.DIR }}'\n"
	for _, target := range []ASTTargets{AWAST, ActionsAST} {
		diagnostics := integrityTestRun(t, ArtifactExecutionRule(), ruleContractInput(t, source, target, false))
		require.Len(t, diagnostics, 1)
		assert.Contains(t, diagnostics[0].Message, "Incomplete artifact-execution analysis")
	}
}
