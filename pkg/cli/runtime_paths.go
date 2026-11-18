package cli

import (
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/github/gh-aw/pkg/constants"
)

var cliRuntimeRootPattern = regexp.MustCompile(`^/tmp/gh-aw-run\.[A-Za-z0-9]+$`)

// cliRuntimePath resolves canonical paths for binary consumers that cannot be
// rewritten by the workflow compiler. An unset root preserves standalone CLI use.
// This validates path syntax, not filesystem ownership.
func cliRuntimePath(canonical string) (string, error) {
	root := os.Getenv("GH_AW_RUNTIME_ROOT") //nolint:osgetenvlibrary
	if root == "" {
		return canonical, nil
	}
	if !cliRuntimeRootPattern.MatchString(root) {
		return "", errors.New("invalid GH_AW_RUNTIME_ROOT: expected /tmp/gh-aw-run.<alphanumeric>")
	}
	if canonical == constants.TmpGhAwDir {
		return root, nil
	}
	relative, ok := strings.CutPrefix(canonical, constants.TmpGhAwDirSlash)
	if !ok {
		return "", fmt.Errorf("runtime path must be under %s", constants.TmpGhAwDir)
	}
	return path.Join(root, relative), nil
}

func flattenedAgentLogPath(logPath string) (string, bool) {
	if relative, ok := strings.CutPrefix(logPath, constants.TmpGhAwDirSlash); ok {
		return relative, filepath.IsLocal(relative)
	}

	root, relative, ok := strings.Cut(strings.TrimPrefix(logPath, "/tmp/"), "/")
	if !ok || !cliRuntimeRootPattern.MatchString("/tmp/"+root) || !strings.HasPrefix(logPath, "/tmp/") {
		return "", false
	}
	return relative, filepath.IsLocal(relative)
}

func openMCPLogsCacheRoot(cacheDir string, private bool) (*os.Root, error) {
	parent := filepath.Dir(cacheDir)
	if !private {
		if err := os.MkdirAll(parent, constants.DirPermPublic); err != nil {
			return nil, err
		}
	}
	root, err := os.OpenRoot(parent)
	if err != nil {
		return nil, err
	}
	if private {
		info, statErr := root.Stat(".")
		pathInfo, pathErr := os.Lstat(parent)
		if statErr != nil || pathErr != nil || !info.IsDir() || info.Mode().Perm() != 0o700 ||
			!cliRuntimeOwnedByCurrentUser(info) || pathInfo.Mode()&os.ModeSymlink != 0 || !os.SameFile(info, pathInfo) {
			return nil, errors.Join(
				errors.New("runtime root must be an existing private directory owned by the current user"),
				root.Close(),
			)
		}
	}
	return root, nil
}
