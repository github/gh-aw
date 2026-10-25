package workflow

import (
	"fmt"
	"path"
	"strings"

	"github.com/github/gh-aw/pkg/constants"
)

type arcDindArtifactCopy struct {
	sourcePath string
	targetPath string
}

// generateArcDindArtifactConsolidationStep emits a workflow step that copies files
// from /tmp/gh-aw/ to ${{ runner.temp }}/gh-aw/ and stages extra files from shared
// paths so the artifact upload has a single root directory.
func (c *Compiler) generateArcDindArtifactConsolidationStep(yaml *strings.Builder, extraCopies ...arcDindArtifactCopy) {
	arcDindLog.Print("Generating ARC/DinD artifact consolidation step")
	yaml.WriteString("      - name: Consolidate artifacts for ARC/DinD\n")
	yaml.WriteString("        if: always()\n")
	yaml.WriteString("        shell: bash\n")
	yaml.WriteString("        run: |\n")
	// Use rsync-like cp to merge /tmp/gh-aw/ into ${RUNNER_TEMP}/gh-aw/ without
	// clobbering existing files (firewall logs already there). The -a flag preserves
	// permissions/timestamps, --no-clobber skips existing files.
	writeArcDindArtifactLine(yaml, "          # Consolidate %s into %s for single-root artifact upload\n", constants.TmpGhAwDirSlash, constants.GhAwRootDirShell)
	writeArcDindArtifactLine(yaml, "          if [ -d %s ]; then\n", constants.TmpGhAwDir)
	writeArcDindArtifactLine(yaml, "            mkdir -p %q\n", constants.GhAwRootDirShell)
	writeArcDindArtifactLine(yaml, "            cp -a --no-clobber %s. %q 2>/dev/null || \\\n", constants.TmpGhAwDirSlash, constants.GhAwRootDirShellSlash)
	writeArcDindArtifactLine(yaml, "              cp -a %s. %q 2>/dev/null || true\n", constants.TmpGhAwDirSlash, constants.GhAwRootDirShellSlash)
	yaml.WriteString("          fi\n")
	for _, copy := range extraCopies {
		source := path.Join(constants.GhAwRootDirShell, copy.sourcePath)
		target := path.Join(constants.GhAwRootDirShell, copy.targetPath)
		tmpTarget := path.Join(constants.TmpGhAwDir, copy.targetPath)
		targetDir := path.Dir(copy.targetPath)
		writeArcDindArtifactLine(yaml, "          if [ -f %q ]; then\n", source)
		writeArcDindArtifactLine(yaml, "            mkdir -p %q %q\n", path.Join(constants.GhAwRootDirShell, targetDir), path.Join(constants.TmpGhAwDir, targetDir))
		writeArcDindArtifactLine(yaml, "            cp %q %q || true\n", source, target)
		writeArcDindArtifactLine(yaml, "            cp %q %q || true\n", source, tmpTarget)
		yaml.WriteString("          fi\n")
	}
}

func writeArcDindArtifactLine(yaml *strings.Builder, format string, args ...any) {
	if _, err := fmt.Fprintf(yaml, format, args...); err != nil {
		arcDindLog.Printf("Failed to write ARC/DinD artifact line: %v", err)
	}
}
