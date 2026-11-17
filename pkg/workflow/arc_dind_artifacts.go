package workflow

import (
	"path"
	"strings"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/logger"
)

var arcDindLog = logger.New("workflow:arc_dind_artifacts")

// rewriteTmpGhAwPathsForArcDind normalizes legacy ARC artifact paths to the
// logical runtime root. The final compiler pass resolves that root to the private
// runtime directory after artifact redaction validation. Keeping one root
// preserves the relative layout expected by downstream artifact downloads.
func rewriteTmpGhAwPathsForArcDind(paths []string) []string {
	arcDindLog.Printf("Rewriting %d artifact paths for ARC/DinD single-root layout", len(paths))
	result := make([]string, 0, len(paths))
	rewritten := 0
	for _, p := range paths {
		rewrittenPath := p
		if p == constants.GhAwRootDir {
			rewrittenPath = constants.TmpGhAwDir
		} else if after, ok := strings.CutPrefix(p, constants.GhAwRootDirSlash); ok {
			rewrittenPath = path.Join(constants.TmpGhAwDir, after)
			if strings.HasSuffix(p, "/") && !strings.HasSuffix(rewrittenPath, "/") {
				rewrittenPath += "/"
			}
		}
		if rewrittenPath != p {
			rewritten++
		}
		result = append(result, rewrittenPath)
	}
	arcDindLog.Printf("Rewrote %d of %d artifact paths to the logical runtime root", rewritten, len(paths))
	return result
}
