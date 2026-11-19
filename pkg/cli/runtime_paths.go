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
var cliRuntimeOwnerPattern = regexp.MustCompile(`^[a-f0-9]{64}$`)

const cliRuntimeOwnerFile = ".gh-aw-runtime-owner"

// cliRuntimePath resolves canonical paths for binary consumers that cannot be
// rewritten by the workflow compiler. An unset root preserves standalone CLI use.
func cliRuntimePath(canonical string) (string, error) {
	root := os.Getenv("GH_AW_RUNTIME_ROOT") //nolint:osgetenvlibrary
	if root == "" {
		return canonical, nil
	}
	if !cliRuntimeRootPattern.MatchString(root) {
		return "", errors.New("invalid GH_AW_RUNTIME_ROOT: expected /tmp/gh-aw-run.<alphanumeric>")
	}
	if err := validateRuntimeRootOwnership(root); err != nil {
		return "", fmt.Errorf("invalid GH_AW_RUNTIME_ROOT ownership: %w", err)
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

func validateRuntimeRootOwnership(root string) error {
	expectedOwner := os.Getenv("GH_AW_RUNTIME_OWNER") //nolint:osgetenvlibrary
	if !cliRuntimeOwnerPattern.MatchString(expectedOwner) {
		return errors.New("missing or invalid GH_AW_RUNTIME_OWNER")
	}

	rootInfo, err := os.Lstat(root)
	if err != nil {
		return errors.New("runtime root is unavailable")
	}
	if rootInfo.Mode()&os.ModeSymlink != 0 || !rootInfo.IsDir() || rootInfo.Mode().Perm() != 0o700 ||
		!cliRuntimeOwnedByCurrentUser(rootInfo) {
		return errors.New("runtime root is not a private directory owned by the current user")
	}

	ownerPath := filepath.Join(root, cliRuntimeOwnerFile)
	ownerInfo, err := os.Lstat(ownerPath)
	if err != nil {
		return errors.New("runtime owner marker is unavailable")
	}
	if ownerInfo.Mode()&os.ModeSymlink != 0 || !ownerInfo.Mode().IsRegular() || ownerInfo.Mode().Perm() != 0o600 ||
		!cliRuntimeOwnedByCurrentUser(ownerInfo) {
		return errors.New("runtime owner marker is not a private regular file owned by the current user")
	}
	owner, err := os.ReadFile(ownerPath)
	if err != nil || strings.TrimSpace(string(owner)) != expectedOwner {
		return errors.New("runtime owner marker does not match the current invocation")
	}
	return nil
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
