package workflow

import (
	"fmt"
	"strings"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/logger"
)

var arcDindLog = logger.New("workflow:arc_dind_artifacts")

// rewriteTmpGhAwPathsForArcDind rewrites artifact paths that use /tmp/gh-aw/ to
// use ${{ runner.temp }}/gh-aw/ instead, so all paths share a single root for the
// artifact upload. This prevents upload-artifact from computing "/" as the common
// ancestor (which happens when paths span both /tmp/gh-aw/ and the runner.temp tree),
// causing a nested directory layout that breaks downstream artifact downloads.
func rewriteTmpGhAwPathsForArcDind(paths []string) []string {
	arcDindLog.Printf("Rewriting %d artifact paths for ARC/DinD single-root layout", len(paths))
	result := make([]string, len(paths))
	rewritten := 0
	for i, p := range paths {
		if after, ok := strings.CutPrefix(p, constants.TmpGhAwDirSlash); ok {
			// /tmp/gh-aw/foo → ${{ runner.temp }}/gh-aw/foo
			result[i] = strings.Join([]string{constants.GhAwRootDir, after}, "/")
			rewritten++
		} else if p == constants.TmpGhAwDir {
			result[i] = constants.GhAwRootDir
			rewritten++
		} else {
			result[i] = p
		}
	}
	arcDindLog.Printf("Rewrote %d of %d artifact paths from /tmp/gh-aw to runner.temp", rewritten, len(paths))
	return result
}

// generateArcDindArtifactConsolidationStep emits a workflow step that copies files
// from /tmp/gh-aw/ to ${{ runner.temp }}/gh-aw/ so the artifact upload has a single
// root directory. On ARC/DinD, agent output files (agent_output.json, safe_outputs.ndjson,
// aw-prompts/, patches, MCP logs) are written to /tmp/gh-aw/ during execution, but
// firewall logs are under ${{ runner.temp }}/gh-aw/. This step consolidates them.
func (c *Compiler) generateArcDindArtifactConsolidationStep(yaml *strings.Builder) {
	arcDindLog.Print("Generating ARC/DinD artifact consolidation step")
	yaml.WriteString("      - name: Consolidate artifacts for ARC/DinD\n")
	yaml.WriteString("        if: always()\n")
	yaml.WriteString("        shell: bash\n")
	yaml.WriteString("        run: |\n")
	// Use rsync-like cp to merge /tmp/gh-aw/ into ${RUNNER_TEMP}/gh-aw/ without
	// clobbering existing files (firewall logs already there). The -a flag preserves
	// permissions/timestamps, --no-clobber skips existing files.
	yaml.WriteString("          # Consolidate /tmp/gh-aw/ into ${RUNNER_TEMP}/gh-aw/ for single-root artifact upload\n")
	yaml.WriteString("          if [ -d /tmp/gh-aw ]; then\n")
	yaml.WriteString("            mkdir -p \"${RUNNER_TEMP}/gh-aw\"\n")
	fmt.Fprintf(yaml, "            cp -a --no-clobber /tmp/gh-aw/. \"${RUNNER_TEMP}/gh-aw/\" 2>/dev/null || \\\n")
	fmt.Fprintf(yaml, "              cp -a /tmp/gh-aw/. \"${RUNNER_TEMP}/gh-aw/\" 2>/dev/null || true\n")
	yaml.WriteString("          fi\n")
}
