//go:build !js && !wasm

package parser

import (
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"strings"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/fileutil"
)

// ResolveIncludePath resolves include path based on workflowspec format or relative path
func ResolveIncludePath(filePath, baseDir string, cache *ImportCache) (string, error) {
	remoteLog.Printf("Resolving include path: file_path=%s, base_dir=%s", filePath, baseDir)

	if builtinPath, handled, err := resolveBuiltinIncludePath(filePath); handled {
		return builtinPath, err
	}

	if IsWorkflowSpec(filePath) {
		remoteLog.Printf("Detected workflowspec format: %s", filePath)
		return downloadIncludeFromWorkflowSpec(filePath, cache)
	}

	remoteLog.Printf("Using local file resolution for: %s", filePath)
	resolveBase, securityBase, normalizedFilePath := computeIncludeResolveAndSecurityBases(filePath, baseDir)
	return resolveAndValidateLocalIncludePath(normalizedFilePath, resolveBase, securityBase)
}

func resolveBuiltinIncludePath(filePath string) (string, bool, error) {
	if !strings.HasPrefix(filePath, BuiltinPathPrefix) {
		return "", false, nil
	}
	if !BuiltinVirtualFileExists(filePath) {
		return "", true, fmt.Errorf("builtin file not found: %s", filePath)
	}
	remoteLog.Printf("Resolved builtin path: %s", filePath)
	return filePath, true, nil
}

func resolveAndValidateLocalIncludePath(filePath, resolveBase, securityBase string) (string, error) {
	if stripped, ok := strings.CutPrefix(filepath.ToSlash(filePath), "/"); ok {
		if !strings.HasPrefix(stripped, constants.GithubDir) && !strings.HasPrefix(stripped, ".agents/") {
			remoteLog.Printf("Security: Path not within .github or .agents: %s", filePath)
			return "", fmt.Errorf("security: path %s must be within .github or .agents folder", filePath)
		}
	}
	fullPath := filepath.Join(resolveBase, filePath)
	normalizedSecurityBase := filepath.Clean(securityBase)
	normalizedFullPath := filepath.Clean(fullPath)
	relativePath, err := filepath.Rel(normalizedSecurityBase, normalizedFullPath)
	if err != nil || relativePath == ".." || strings.HasPrefix(relativePath, ".."+string(filepath.Separator)) || filepath.IsAbs(relativePath) {
		allowedFolder := filepath.Base(normalizedSecurityBase)
		remoteLog.Printf("Security: Path escapes allowed folder: %s (resolves to: %s)", filePath, relativePath)
		return "", fmt.Errorf("security: path %s must be within %s folder (resolves to: %s)", filePath, allowedFolder, relativePath)
	}

	if err := validateIncludeSecurityBase(normalizedSecurityBase); err != nil {
		return "", err
	}
	if err := fileutil.ValidatePathWithinBase(normalizedSecurityBase, normalizedFullPath); err != nil {
		return "", fmt.Errorf("security: path %s must be within %s folder: %w", filePath, filepath.Base(normalizedSecurityBase), err)
	}
	// Strict resolution rejects dangling links and other resolution failures;
	// ValidatePathWithinBase also supports paths that do not exist yet.
	if _, err := filepath.EvalSymlinks(fullPath); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return "", fmt.Errorf("file not found: %s: %w", fullPath, err)
		}
		return "", fmt.Errorf("failed to resolve local include %s: %w", fullPath, err)
	}
	remoteLog.Printf("Resolved to local file: %s", fullPath)
	return fullPath, nil
}
