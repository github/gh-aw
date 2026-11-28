package parser

import (
	"fmt"
	"strings"
)

const builtinConnectorImportPrefix = "connector:"

func resolveBuiltinConnectorAlias(filePath string) (string, bool, error) {
	if !strings.HasPrefix(filePath, builtinConnectorImportPrefix) {
		return "", false, nil
	}

	id := strings.TrimPrefix(filePath, builtinConnectorImportPrefix)
	if !validBuiltinConnectorID(id) {
		return "", true, fmt.Errorf("invalid built-in connector import %q; expected connector:<id>", filePath)
	}
	path := BuiltinPathPrefix + "connectors/" + id + ".md"
	if !BuiltinVirtualFileExists(path) {
		return "", true, fmt.Errorf("built-in connector %q not found", id)
	}
	return path, true, nil
}

func validBuiltinConnectorID(id string) bool {
	if len(id) == 0 || len(id) > 63 || id[0] < 'a' || id[0] > 'z' {
		return false
	}
	for _, char := range id[1:] {
		if (char < 'a' || char > 'z') && (char < '0' || char > '9') && char != '-' {
			return false
		}
	}
	return true
}
