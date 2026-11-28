//go:build js || wasm

package parser

import (
	"fmt"
	"path/filepath"
	"strings"
)

func ResolveIncludePath(filePath, baseDir string, cache *ImportCache) (string, error) {
	parserLog.Printf("ResolveIncludePath: filePath=%s, baseDir=%s", filePath, baseDir)

	if builtinPath, handled, err := resolveBuiltinConnectorAlias(filePath); handled {
		return builtinPath, err
	}
	// Handle builtin paths - these are embedded files that bypass filesystem resolution.
	if strings.HasPrefix(filePath, BuiltinPathPrefix) {
		if !BuiltinVirtualFileExists(filePath) {
			return "", fmt.Errorf("builtin file not found: %s", filePath)
		}
		return filePath, nil
	}

	if IsWorkflowSpec(filePath) {
		parserLog.Printf("ResolveIncludePath: rejecting remote workflowspec in Wasm build: %s", filePath)
		return "", fmt.Errorf("remote imports not available in Wasm: %s", filePath)
	}

	resolveBase, securityBase, normalizedFilePath := computeIncludeResolveAndSecurityBases(filePath, baseDir)
	if resolveBase == "" {
		return "", fmt.Errorf("security: path %s must be within .github or .agents folder", normalizedFilePath)
	}
	fullPath := filepath.Join(resolveBase, normalizedFilePath)

	normalizedSecurityBase := filepath.Clean(securityBase)
	normalizedFullPath := filepath.Clean(fullPath)

	relativePath, err := filepath.Rel(normalizedSecurityBase, normalizedFullPath)
	if err != nil || relativePath == ".." || strings.HasPrefix(relativePath, ".."+string(filepath.Separator)) || filepath.IsAbs(relativePath) {
		allowedFolder := filepath.Base(normalizedSecurityBase)
		parserLog.Printf("ResolveIncludePath: security boundary violation: path=%s, allowedFolder=%s", normalizedFilePath, allowedFolder)
		return "", fmt.Errorf("security: path %s must be within %s folder (resolves to: %s)", normalizedFilePath, allowedFolder, relativePath)
	}

	// In wasm builds, check the virtual filesystem first
	if VirtualFileExists(fullPath) {
		return fullPath, nil
	}

	parserLog.Printf("ResolveIncludePath: file not found in virtual filesystem: %s", fullPath)
	return "", fmt.Errorf("file not found: %s", fullPath)
}
