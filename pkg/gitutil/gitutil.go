package gitutil

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	stdpath "path"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/github/gh-aw/pkg/fileutil"
	"github.com/github/gh-aw/pkg/logger"
)

var gitutilLog = logger.New("gitutil:gitutil")
var ErrNotGitRepository = errors.New("not in a git repository (run this command from inside a git repository, or use 'git init' to create one)")
var osGetwd = os.Getwd
var osUserHomeDir = os.UserHomeDir

var fullSHARegex = regexp.MustCompile(`^[0-9a-f]{40}$`)
var gitObjectIDRegex = regexp.MustCompile(`^(?:[0-9a-f]{40}|[0-9a-f]{64})$`)

// IsHexString checks if a string contains only hexadecimal characters.
// This is used to validate Git commit SHAs and other hexadecimal identifiers.
func IsHexString(s string) bool {
	if s == "" {
		return false
	}
	for _, c := range s {
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') && (c < 'A' || c > 'F') {
			return false
		}
	}
	return true
}

// IsValidFullSHA checks if s is a valid 40-character lowercase hexadecimal SHA.
func IsValidFullSHA(s string) bool {
	return fullSHARegex.MatchString(s)
}

// IsValidFullSHACaseInsensitive checks if s is a valid 40-character hexadecimal SHA.
func IsValidFullSHACaseInsensitive(s string) bool {
	return len(s) == 40 && IsHexString(s)
}

// ValidateGitRef returns an error if ref would be unsafe to pass as a positional
// argument to a git subprocess. A ref starting with '-' would be parsed as an
// option flag rather than a value (argument injection, CWE-88). Refs containing
// '..' can trigger git object traversal expressions.
func ValidateGitRef(ref string) error {
	if ref == "" {
		return errors.New("git ref must not be empty")
	}
	if strings.HasPrefix(ref, "-") {
		return fmt.Errorf("invalid git ref %q: refs must not start with '-' to prevent argument injection", ref)
	}
	if strings.ContainsRune(ref, '\x00') {
		return fmt.Errorf("invalid git ref %q: refs must not contain NUL bytes", ref)
	}
	if strings.Contains(ref, "..") {
		return fmt.Errorf("invalid git ref %q: refs must not contain '..'", ref)
	}
	return nil
}

// ValidateGitPath returns an error if path would be unsafe to pass as a positional
// argument to a git subprocess. A path starting with '-' would be parsed as an
// option flag rather than a value (argument injection, CWE-88).
func ValidateGitPath(path string) error {
	if path == "" {
		return errors.New("git path must not be empty")
	}
	if strings.HasPrefix(path, "-") {
		return fmt.Errorf("invalid git path %q: paths must not start with '-' to prevent argument injection", path)
	}
	if strings.ContainsRune(path, '\x00') {
		return fmt.Errorf("invalid git path %q: paths must not contain NUL bytes", path)
	}
	if stdpath.IsAbs(path) {
		return fmt.Errorf("invalid git path %q: paths must not be absolute", path)
	}
	cleaned := stdpath.Clean(path)
	if cleaned == ".." || strings.HasPrefix(cleaned, "../") {
		return fmt.Errorf("invalid git path %q: paths must not contain '..' path traversal", path)
	}
	return nil
}

// ExtractBaseRepo extracts the base repository (owner/repo) from a repository path
// that may include subfolders.
// For "actions/checkout" -> "actions/checkout"
// For "github/codeql-action/upload-sarif" -> "github/codeql-action"
func ExtractBaseRepo(repoPath string) string {
	parts := strings.Split(repoPath, "/")
	if len(parts) >= 2 {
		// Intentionally strings.Join instead of filepath.Join/path.Join: those
		// helpers Clean the result, collapsing segments like ".." or "." that
		// must be preserved verbatim here (repoPath is an owner/repo slug, not
		// a filesystem path).
		return strings.Join(parts[:2], "/")
	}
	return repoPath
}

// Getwd returns the current working directory. Unlike calling os.Getwd()
// directly, the returned error includes actionable recovery guidance so
// call sites do not need to duplicate their own wrapping message.
func Getwd() (string, error) {
	dir, err := osGetwd()
	if err != nil {
		return "", fmt.Errorf("failed to determine current working directory: %w; check that the process has a valid working directory and read permissions", err)
	}
	return dir, nil
}

// UserHomeDir returns the current user's home directory. Unlike calling
// os.UserHomeDir() directly, the returned error includes actionable recovery
// guidance so call sites do not need to duplicate their own wrapping message.
func UserHomeDir() (string, error) {
	home, err := osUserHomeDir()
	if err != nil {
		return "", fmt.Errorf("failed to determine home directory: %w; set HOME (Unix) or USERPROFILE/HOMEDRIVE/HOMEPATH (Windows), or run this command as a user with a valid home directory", err)
	}
	return home, nil
}

// FindGitRoot finds the root directory of the git repository.
// Uses pure Go filesystem traversal to avoid requiring the git executable,
// which can fail when the binary runs under Rosetta 2 on macOS ARM64 or in
// environments where git is not on PATH.
// Returns an error if not in a git repository.
func FindGitRoot() (string, error) {
	gitutilLog.Print("Finding git root directory")

	dir, err := Getwd()
	if err != nil {
		gitutilLog.Printf("Failed to get current directory: %v", err)
		return "", err
	}

	root, err := FindGitRootFrom(dir)
	if err != nil {
		gitutilLog.Printf("Failed to find git root: %v", err)
		return "", err
	}

	gitutilLog.Printf("Found git root: %s", root)
	return root, nil
}

// FindGitRootFrom finds the root directory of the git repository starting from
// the given directory. It traverses upward until it finds a .git entry (file or
// directory) or reaches the filesystem root.
// Returns an error if not in a git repository.
func FindGitRootFrom(startDir string) (string, error) {
	dir, err := filepath.Abs(startDir)
	if err != nil {
		return "", fmt.Errorf("failed to resolve absolute path for %q: %w", startDir, err)
	}
	dir = filepath.Clean(dir)
	for {
		gitPath := filepath.Join(dir, ".git")
		info, err := os.Stat(gitPath)
		if err == nil {
			// .git exists — accept if it's a directory (normal repo) or a
			// regular file (worktree / git-submodule pointer).
			if info.IsDir() {
				return dir, nil
			}
			// Worktree marker: must be a regular file beginning with "gitdir:"
			if info.Mode().IsRegular() {
				data, readErr := os.ReadFile(gitPath)
				if readErr != nil {
					return "", fmt.Errorf("failed to read .git file at %q: %w", gitPath, readErr)
				}
				if strings.HasPrefix(strings.TrimSpace(string(data)), "gitdir:") {
					return dir, nil
				}
			}
		} else if !errors.Is(err, os.ErrNotExist) {
			// Unexpected error (e.g. permission denied) — surface it.
			return "", fmt.Errorf("failed to stat %q: %w", gitPath, err)
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", ErrNotGitRepository
		}
		dir = parent
	}
}

// ReadFileFromHEAD reads a file from git HEAD using a pre-computed repository root.
// filePath is resolved with filepath.Abs, so relative paths are interpreted from the
// current process working directory (not gitRoot). Prefer passing an absolute path
// within gitRoot, such as filepath.Join(gitRoot, "path/to/file").
// The implementation avoids git show HEAD:path interpolation by resolving a
// literal tree entry with git ls-tree, validating the resulting blob object ID,
// and then reading the blob with git cat-file.
// Use this when the caller already knows the git root (e.g. from a cached value).
func ReadFileFromHEAD(filePath, gitRoot string) (string, error) {
	if gitRoot == "" {
		return "", fmt.Errorf("gitRoot must not be empty when reading %q from HEAD", filePath)
	}

	cleanGitRoot, err := fileutil.ValidateAbsolutePath(gitRoot)
	if err != nil {
		return "", fmt.Errorf("invalid git repository root %q: %w", gitRoot, err)
	}

	absPath, err := filepath.Abs(filePath)
	if err != nil {
		return "", fmt.Errorf("cannot resolve absolute path for %q: %w", filePath, err)
	}
	if err := fileutil.ValidatePathWithinBase(cleanGitRoot, absPath); err != nil {
		return "", fmt.Errorf("path %q is outside the git repository root %q", filePath, gitRoot)
	}

	// git ls-tree pathspecs require the path to be relative to the repository root
	// and to use forward slashes even on Windows.
	relPath, err := filepath.Rel(cleanGitRoot, absPath)
	if err != nil {
		return "", fmt.Errorf("cannot compute path of %q relative to git root %q: %w", absPath, cleanGitRoot, err)
	}

	// Reject paths that escape the repository (e.g. "../secret").
	if strings.HasPrefix(relPath, "..") {
		return "", fmt.Errorf("path %q is outside the git repository root %q", filePath, gitRoot)
	}

	relPath = filepath.ToSlash(relPath)

	gitutilLog.Printf("Reading %q from git HEAD (relative path: %s)", filePath, relPath)

	blobID, err := resolveHEADBlobID(cleanGitRoot, relPath)
	if err != nil {
		gitutilLog.Printf("File %q not found in HEAD commit: %v", filePath, err)
		return "", fmt.Errorf("file %q not found in HEAD commit: %w", filePath, err)
	}

	cmd := exec.Command("git", "-C", cleanGitRoot, "cat-file", "blob", blobID)
	output, err := cmd.Output()
	if err != nil {
		gitutilLog.Printf("File %q not found in HEAD commit: %v", filePath, err)
		return "", fmt.Errorf("file %q not found in HEAD commit: %w", filePath, err)
	}
	return string(output), nil
}

func resolveHEADBlobID(gitRoot, relPath string) (string, error) {
	pathspec := ":(literal)" + relPath
	cmd := exec.Command("git", "-C", gitRoot, "ls-tree", "-z", "--full-tree", "HEAD", "--", pathspec)
	output, err := cmd.Output()
	if err != nil {
		return "", err
	}
	if len(output) == 0 {
		return "", fmt.Errorf("path %q not found in HEAD", relPath)
	}

	entry := strings.TrimSuffix(string(output), "\x00")
	metadata, entryPath, found := strings.Cut(entry, "\t")
	if !found {
		return "", fmt.Errorf("unexpected git ls-tree output for %q", relPath)
	}
	fields := strings.Fields(metadata)
	if len(fields) != 3 {
		return "", fmt.Errorf("unexpected git ls-tree metadata for %q", relPath)
	}
	// relPath is already normalized to forward slashes via filepath.ToSlash above,
	// and git ls-tree also emits forward slashes on all platforms.
	if entryPath != relPath {
		return "", fmt.Errorf("path %q resolved to unexpected entry %q", relPath, entryPath)
	}
	if fields[1] != "blob" {
		return "", fmt.Errorf("path %q in HEAD is not a file", relPath)
	}
	if !isGitObjectID(fields[2]) {
		return "", fmt.Errorf("path %q in HEAD resolved to invalid object ID %q", relPath, fields[2])
	}

	return fields[2], nil
}

func isGitObjectID(s string) bool {
	return gitObjectIDRegex.MatchString(s)
}
