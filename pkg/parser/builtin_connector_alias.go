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
	if id == "" || len(id) > 63 {
		return false
	}
	for index, char := range id {
		if index == 0 {
			if char < 'a' || char > 'z' {
				return false
			}
			continue
		}
		if (char < 'a' || char > 'z') && (char < '0' || char > '9') && char != '-' {
			return false
		}
	}
	return true
}
