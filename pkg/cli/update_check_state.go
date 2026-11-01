package cli

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/logger"
)

func getUpdateCheckFilePathFor(fileName string, log *logger.Logger) string {
	cacheDir, err := os.UserCacheDir()
	if err != nil {
		log.Printf("Could not determine user cache directory: %v", err)
		return ""
	}

	if err := os.MkdirAll(cacheDir, constants.DirPermOwnerOnly); err != nil {
		log.Printf("Error creating user cache directory: %v", err)
		return ""
	}
	stateDir := filepath.Join(cacheDir, "gh-aw-update-check")
	if err := os.Mkdir(stateDir, constants.DirPermOwnerOnly); err != nil && !os.IsExist(err) {
		log.Printf("Error creating update check directory: %v", err)
		return ""
	}
	info, err := os.Lstat(stateDir)
	if err != nil {
		log.Printf("Error inspecting update check directory: %v", err)
		return ""
	}
	if !info.IsDir() || (runtime.GOOS != "windows" && info.Mode().Perm()&0o077 != 0) {
		log.Print("Update check directory must be a non-symlink directory (owner-only permissions required on non-Windows systems)")
		return ""
	}

	return filepath.Join(stateDir, fileName)
}

func shouldRunUpdateCheckAtPath(lastCheckFile string, interval time.Duration, label string, log *logger.Logger) bool {
	if lastCheckFile == "" {
		log.Printf("Could not determine %s file path", label)
		return false
	}

	info, err := os.Lstat(lastCheckFile)
	if err == nil && !info.Mode().IsRegular() {
		log.Printf("Refusing non-regular %s file", label)
		return false
	}
	if err != nil {
		if !errors.Is(err, fs.ErrNotExist) {
			log.Printf("Error inspecting %s file: %v", label, err)
		}
		return true
	}
	root, err := os.OpenRoot(filepath.Dir(lastCheckFile))
	if err != nil {
		log.Printf("Error opening %s directory: %v", label, err)
		return false
	}
	defer func() {
		if err := root.Close(); err != nil {
			log.Printf("Error closing %s directory: %v", label, err)
		}
	}()
	data, err := root.ReadFile(filepath.Base(lastCheckFile))
	if err != nil {
		if !errors.Is(err, fs.ErrNotExist) {
			log.Printf("Error reading %s file: %v", label, err)
		}
		return true
	}

	lastCheck, err := time.Parse(time.RFC3339, strings.TrimSpace(string(data)))
	if err != nil {
		log.Printf("Error parsing %s time: %v", label, err)
		return true
	}

	elapsed := time.Since(lastCheck)
	if elapsed < interval {
		log.Printf("Last %s was %v ago, skipping", label, elapsed)
		return false
	}

	return true
}

func writeUpdateCheckTime(path string, perm os.FileMode, label string, log *logger.Logger) {
	if path == "" {
		return
	}

	timestamp := time.Now().Format(time.RFC3339)
	if err := writeUpdateCheckTimestamp(path, []byte(timestamp), perm); err != nil {
		log.Printf("Error writing %s time: %v", label, err)
	}
}

func writeUpdateCheckTimestamp(path string, timestamp []byte, perm os.FileMode) (err error) {
	file, err := os.CreateTemp(filepath.Dir(path), ".update-check-")
	if err != nil {
		return err
	}
	closed := false
	defer func() {
		if !closed {
			err = errors.Join(err, file.Close())
		}
		if removeErr := os.Remove(file.Name()); removeErr != nil && !errors.Is(removeErr, fs.ErrNotExist) {
			err = errors.Join(err, removeErr)
		}
	}()
	if err := file.Chmod(perm); err != nil {
		return err
	}
	if _, err := file.Write(timestamp); err != nil {
		return err
	}
	err = file.Close()
	closed = true
	if err != nil {
		return err
	}
	// Rename replaces a preexisting marker or symlink instead of following it.
	return os.Rename(file.Name(), path)
}
