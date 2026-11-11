//go:build !js && !wasm

package parser

import (
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"strings"

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
	fullPath, err := resolveLocalIncludePath(filePath, resolveBase, securityBase)
	if err != nil {
		remoteLog.Printf("Local include path rejected: %v", err)
		return "", err
	}
	normalizedSecurityBase := filepath.Clean(securityBase)
	normalizedFullPath := filepath.Clean(fullPath)

	if err := validateIncludeSecurityBase(normalizedSecurityBase); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return "", fmt.Errorf("file not found: %s: %w", fullPath, err)
		}
		return "", err
	}
	absoluteSecurityBase, err := filepath.Abs(normalizedSecurityBase)
	if err != nil {
		return "", fmt.Errorf("failed to resolve absolute include base %s: %w", normalizedSecurityBase, err)
	}
	absoluteFullPath, err := filepath.Abs(normalizedFullPath)
	if err != nil {
		return "", fmt.Errorf("failed to resolve absolute local include %s: %w", normalizedFullPath, err)
	}
	if err := fileutil.ValidatePathWithinBase(absoluteSecurityBase, absoluteFullPath); err != nil {
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
	registerLocalIncludeReadBase(absoluteFullPath, absoluteSecurityBase)
	remoteLog.Printf("Resolved to local file: %s", fullPath)
	return fullPath, nil
}
