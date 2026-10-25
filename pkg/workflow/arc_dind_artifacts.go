package workflow

import (
	"path"
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
	result := make([]string, 0, len(paths))
	rewritten := 0
	for _, p := range paths {
		if after, ok := strings.CutPrefix(p, constants.TmpGhAwDirSlash); ok {
			// /tmp/gh-aw/foo → ${{ runner.temp }}/gh-aw/foo
			result = append(result, path.Join(constants.GhAwRootDir, after))
			rewritten++
		} else if p == constants.TmpGhAwDir {
			result = append(result, constants.GhAwRootDir)
			rewritten++
		} else {
			result = append(result, p)
		}
	}
	arcDindLog.Printf("Rewrote %d of %d artifact paths from /tmp/gh-aw to runner.temp", rewritten, len(paths))
	return result
}
