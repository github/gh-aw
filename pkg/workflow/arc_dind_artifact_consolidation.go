package workflow

import (
	"fmt"
	"path"
	"strings"
)

type arcDindArtifactCopy struct {
	sourcePath string
	targetPath string
}

// generateArcDindArtifactConsolidationStep stages selected runtime metadata at
// its artifact-relative path. Runtime files already share one private root;
// never consolidate them into the immutable setup assets tree.
func (c *Compiler) generateArcDindArtifactConsolidationStep(yaml *strings.Builder, extraCopies ...arcDindArtifactCopy) {
	arcDindLog.Print("Generating ARC/DinD artifact consolidation step")
	yaml.WriteString("      - name: Consolidate artifacts for ARC/DinD\n")
	yaml.WriteString("        if: always()\n")
	yaml.WriteString("        shell: bash\n")
	yaml.WriteString("        run: |\n")
	if len(extraCopies) == 0 {
		yaml.WriteString("          : # Runtime artifacts already share a single private root\n")
	}
	for _, copy := range extraCopies {
		source := path.Join("${GH_AW_RUNTIME_DIR}", copy.sourcePath)
		target := path.Join("${GH_AW_RUNTIME_DIR}", copy.targetPath)
		targetDir := path.Dir(copy.targetPath)
		writeArcDindArtifactLine(yaml, "          if [ -f %q ]; then\n", source)
		writeArcDindArtifactLine(yaml, "            mkdir -p %q\n", path.Join("${GH_AW_RUNTIME_DIR}", targetDir))
		writeArcDindArtifactLine(yaml, "            cp %q %q || true\n", source, target)
		yaml.WriteString("          fi\n")
	}
}

func writeArcDindArtifactLine(yaml *strings.Builder, format string, args ...any) {
	if _, err := fmt.Fprintf(yaml, format, args...); err != nil {
		arcDindLog.Printf("Failed to write ARC/DinD artifact line: %v", err)
	}
}
