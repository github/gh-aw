//go:build linux || darwin

package cli

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sync"
	"syscall"

	"github.com/github/gh-aw/pkg/constants"
)

var defaultMCPLogsCache = mcpLogsReportCache{}

type mcpLogsReportCache struct {
	mu      sync.Mutex
	tempDir string // Empty uses the system temporary directory.
	dir     string
	info    fs.FileInfo
	root    *os.Root // Pins the directory identity even if its name is removed.
	reports map[string]fs.FileInfo
	closed  bool
}

func (cache *mcpLogsReportCache) store(report string) (string, error) {
	cache.mu.Lock()
	defer cache.mu.Unlock()

	if err := cache.prepare(); err != nil {
		return "", err
	}
	digest := sha256.Sum256([]byte(report))
	name := fmt.Sprintf("%x.json", digest)
	existing, err := cache.root.Lstat(name)
	switch {
	case err == nil:
		if !isPrivateMCPLogsFile(existing) || existing.Size() != int64(len(report)) {
			return "", errors.New("existing logs report is not a private regular file of the expected size")
		}
		if err := verifyMCPLogsReport(cache.root, name, existing, digest); err != nil {
			return "", err
		}
	case errors.Is(err, fs.ErrNotExist):
		info, err := writeMCPLogsReport(cache.root, name, report)
		if err != nil {
			return "", err
		}
		cache.reports[name] = info
	default:
		return "", fmt.Errorf("inspect private logs report: %w", err)
	}
	return filepath.Join(cache.dir, name), nil
}

func (cache *mcpLogsReportCache) prepare() error {
	if cache.closed {
		return errors.New("logs report cache is closed")
	}
	if cache.dir != "" {
		if err := validateMCPLogsTempDirectory(filepath.Dir(cache.dir)); err != nil {
			return err
		}
		info, err := os.Lstat(cache.dir)
		if errors.Is(err, fs.ErrNotExist) {
			if err := cache.root.Close(); err != nil {
				return fmt.Errorf("close missing logs cache: %w", err)
			}
			cache.dir, cache.info, cache.root, cache.reports = "", nil, nil, nil
			return cache.create()
		}
		if err != nil || !os.SameFile(cache.info, info) || !isPrivateMCPLogsDirectory(info) {
			return errors.New("logs cache directory changed or is not owner-private")
		}
		return validateMCPLogsDirectoryACL(cache.root, ".", info)
	}
	return cache.create()
}

func (cache *mcpLogsReportCache) create() error {
	base := cache.tempDir
	shared := false
	if base == "" {
		base = os.Getenv("GH_AW_MCP_LOGS_REPORT_DIR") //nolint:osgetenvlibrary // Set by the MCP gateway for this server process.
		shared = base != ""
	}
	if base == "" {
		base = os.TempDir()
	}
	if !filepath.IsAbs(base) {
		return errors.New("logs cache requires an absolute temporary directory")
	}
	// Resolve system aliases (such as /var on macOS), but do not redirect an
	// explicit shared mount: its path must be the same in both containers.
	if !shared {
		resolved, err := filepath.EvalSymlinks(base)
		if err != nil {
			return fmt.Errorf("resolve logs cache temporary directory: %w", err)
		}
		base = resolved
	}
	if err := validateMCPLogsTempDirectory(base); err != nil {
		return err
	}
	dir, err := os.MkdirTemp(base, "gh-aw-mcp-logs-")
	if err != nil {
		return fmt.Errorf("create private logs cache: %w", err)
	}
	pathInfo, err := os.Lstat(dir)
	if err != nil || !isPrivateMCPLogsDirectory(pathInfo) {
		return errors.New("new logs cache directory is not owner-private")
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		return fmt.Errorf("open new private logs cache: %w", err)
	}
	info, err := root.Lstat(".")
	if err == nil && (!os.SameFile(pathInfo, info) || !isPrivateMCPLogsDirectory(info)) {
		err = errors.New("new logs cache directory is not owner-private")
	}
	if err == nil {
		err = validateMCPLogsDirectoryACL(root, ".", info)
	}
	if err != nil {
		if current, statErr := os.Lstat(dir); statErr == nil && os.SameFile(pathInfo, current) && isPrivateMCPLogsDirectory(current) {
			err = errors.Join(err, syscall.Rmdir(dir))
		}
		return errors.Join(err, root.Close())
	}
	cache.dir, cache.info, cache.root, cache.reports = dir, info, root, make(map[string]fs.FileInfo)
	return nil
}

func (cache *mcpLogsReportCache) close() (err error) {
	cache.mu.Lock()
	defer cache.mu.Unlock()
	cache.closed = true
	if cache.root == nil {
		return nil
	}
	defer func() {
		err = errors.Join(err, cache.root.Close())
		cache.dir, cache.info, cache.root, cache.reports = "", nil, nil, nil
	}()

	if err := validateMCPLogsTempDirectory(filepath.Dir(cache.dir)); err != nil {
		return err
	}
	info, err := os.Lstat(cache.dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil || !os.SameFile(cache.info, info) || !isPrivateMCPLogsDirectory(info) {
		return errors.New("refusing to clean up a changed or non-private logs cache directory")
	}
	if err := validateMCPLogsDirectoryACL(cache.root, ".", info); err != nil {
		return err
	}
	// Only unlink report names created by this cache, without walking any tree.
	for name, expected := range cache.reports {
		report, statErr := cache.root.Lstat(name)
		if errors.Is(statErr, fs.ErrNotExist) {
			continue
		}
		if statErr != nil || !os.SameFile(expected, report) || !isPrivateMCPLogsFile(report) {
			err = errors.Join(err, fmt.Errorf("refusing to remove changed logs report %q", name))
			continue
		}
		if removeErr := cache.root.Remove(name); removeErr != nil && !errors.Is(removeErr, fs.ErrNotExist) {
			err = errors.Join(err, removeErr)
		}
	}
	current, statErr := os.Lstat(cache.dir)
	if errors.Is(statErr, fs.ErrNotExist) {
		return err
	}
	if statErr != nil || !os.SameFile(cache.info, current) || !isPrivateMCPLogsDirectory(current) {
		return errors.Join(err, errors.New("refusing to remove a changed or non-private logs cache directory"))
	}
	if removeErr := syscall.Rmdir(cache.dir); removeErr != nil && !errors.Is(removeErr, fs.ErrNotExist) {
		err = errors.Join(err, fmt.Errorf("remove private logs cache directory: %w", removeErr))
	}
	return err
}

func validateMCPLogsTempDirectory(dir string) error {
	// Validate from the root down, so a child is inspected only after the
	// containing directory is known to protect its entry.
	parent := filepath.Dir(dir)
	if parent != dir {
		if err := validateMCPLogsTempDirectory(parent); err != nil {
			return err
		}
	}
	info, err := os.Lstat(dir)
	if err != nil {
		return fmt.Errorf("inspect logs cache ancestor: %w", err)
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	mode := info.Mode()
	owner := "unknown"
	if ok {
		owner = fmt.Sprint(stat.Uid)
	}
	euid := uint32(os.Geteuid())
	if !ok || !info.IsDir() || (stat.Uid != 0 && stat.Uid != euid) ||
		(mode.Perm()&0o022 != 0 && mode&os.ModeSticky == 0) {
		return fmt.Errorf("unsafe logs cache ancestor %q (owner UID %s, mode %s): require a real directory owned by root or UID %d, with no group/other write bits unless sticky", dir, owner, mode, euid)
	}
	if err := validateMCPLogsDirectoryACL(nil, dir, info); err != nil {
		return fmt.Errorf("unsafe logs cache ancestor %q (owner UID %s, mode %s): could not verify the extended ACL policy: %w", dir, owner, mode, err)
	}
	return nil
}

func isPrivateMCPLogsDirectory(info fs.FileInfo) bool {
	stat, ok := info.Sys().(*syscall.Stat_t)
	return ok && stat.Uid == uint32(os.Geteuid()) && info.Mode() == os.ModeDir|constants.DirPermOwnerOnly
}

func isPrivateMCPLogsFile(info fs.FileInfo) bool {
	stat, ok := info.Sys().(*syscall.Stat_t)
	return ok && stat.Uid == uint32(os.Geteuid()) && stat.Nlink == 1 && info.Mode() == constants.FilePermSensitive
}

func verifyMCPLogsReport(root *os.Root, name string, expected fs.FileInfo, digest [sha256.Size]byte) error {
	f, err := root.OpenFile(name, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		return fmt.Errorf("open existing private logs report: %w", err)
	}
	defer func() {
		if err := f.Close(); err != nil {
			mcpLogsGuardrailLog.Printf("Failed to close existing logs report: %v", err)
		}
	}()
	info, err := f.Stat()
	if err != nil || !os.SameFile(expected, info) || !isPrivateMCPLogsFile(info) || info.Size() != expected.Size() {
		return errors.New("existing logs report changed or is not owner-private")
	}
	if err := validateMCPLogsACL(f); err != nil {
		return err
	}
	hash := sha256.New()
	n, err := io.Copy(hash, io.LimitReader(f, info.Size()+1))
	if err != nil {
		return fmt.Errorf("hash existing logs report: %w", err)
	}
	if n != info.Size() || !bytes.Equal(hash.Sum(nil), digest[:]) {
		return errors.New("existing logs report does not match its SHA-256 digest")
	}
	return nil
}

func writeMCPLogsReport(root *os.Root, name, report string) (_ fs.FileInfo, err error) {
	f, err := root.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, constants.FilePermSensitive)
	if err != nil {
		return nil, fmt.Errorf("create private logs report: %w", err)
	}
	defer func() {
		if closeErr := f.Close(); err == nil && closeErr != nil {
			err = fmt.Errorf("close private logs report: %w", closeErr)
		}
		if err != nil {
			if cleanupErr := root.Remove(name); cleanupErr != nil {
				err = errors.Join(err, fmt.Errorf("remove incomplete logs report: %w", cleanupErr))
			}
		}
	}()
	info, err := f.Stat()
	if err != nil || !isPrivateMCPLogsFile(info) {
		return nil, errors.New("new logs report is not an owner-private regular file")
	}
	if err := validateMCPLogsACL(f); err != nil {
		return nil, err
	}
	if _, err := f.WriteString(report); err != nil {
		return nil, fmt.Errorf("write private logs report: %w", err)
	}
	return info, nil
}
