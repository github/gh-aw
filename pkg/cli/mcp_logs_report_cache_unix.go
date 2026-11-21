//go:build unix

package cli

import (
	"crypto/sha256"
	"errors"
	"fmt"
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
}

func (cache *mcpLogsReportCache) store(report string) (string, error) {
	cache.mu.Lock()
	defer cache.mu.Unlock()

	if err := cache.prepare(); err != nil {
		return "", err
	}
	root, err := os.OpenRoot(cache.dir)
	if err != nil {
		return "", fmt.Errorf("open private logs cache: %w", err)
	}
	defer root.Close()
	info, err := root.Stat(".")
	if err != nil || !os.SameFile(cache.info, info) || !isPrivateMCPLogsDirectory(info) {
		return "", errors.New("logs cache directory changed or is not owner-private")
	}

	name := fmt.Sprintf("%x.json", sha256.Sum256([]byte(report)))
	existing, err := root.Lstat(name)
	switch {
	case err == nil:
		if !isPrivateMCPLogsFile(existing) || existing.Size() != int64(len(report)) {
			return "", errors.New("existing logs report is not a private regular file of the expected size")
		}
	case errors.Is(err, fs.ErrNotExist):
		if err := writeMCPLogsReport(root, name, report); err != nil {
			return "", err
		}
	default:
		return "", fmt.Errorf("inspect private logs report: %w", err)
	}
	return filepath.Join(cache.dir, name), nil
}

func (cache *mcpLogsReportCache) prepare() error {
	if cache.dir != "" {
		if err := validateMCPLogsTempDirectory(filepath.Dir(cache.dir)); err != nil {
			return err
		}
		info, err := os.Lstat(cache.dir)
		if err != nil || !os.SameFile(cache.info, info) || !isPrivateMCPLogsDirectory(info) {
			return errors.New("logs cache directory changed or is not owner-private")
		}
		return nil
	}
	base := cache.tempDir
	shared := false
	if base == "" {
		base = os.Getenv("GH_AW_MCP_LOGS_REPORT_DIR")
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
	info, err := os.Lstat(dir)
	if err != nil || !isPrivateMCPLogsDirectory(info) {
		_ = os.Remove(dir) // The new directory is still empty.
		return errors.New("new logs cache directory is not owner-private")
	}
	cache.dir, cache.info = dir, info
	return nil
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
	if !ok || !info.IsDir() || (stat.Uid != 0 && stat.Uid != uint32(os.Geteuid())) ||
		(mode.Perm()&0o022 != 0 && mode&os.ModeSticky == 0) {
		return fmt.Errorf("unsafe logs cache ancestor %q", dir)
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

func writeMCPLogsReport(root *os.Root, name, report string) (err error) {
	f, err := root.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, constants.FilePermSensitive)
	if err != nil {
		return fmt.Errorf("create private logs report: %w", err)
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
		return errors.New("new logs report is not an owner-private regular file")
	}
	if _, err := f.WriteString(report); err != nil {
		return fmt.Errorf("write private logs report: %w", err)
	}
	return nil
}
