package workflow

import (
	"fmt"

	"github.com/github/gh-aw/pkg/parser"
)

//go:generate go run ../../scripts/generate_builtin_connectors

func init() {
	if err := registerBuiltinConnectorDeclarations(); err != nil {
		panic(fmt.Sprintf("failed to register built-in connector declarations: %v", err))
	}
}

func registerBuiltinConnectorDeclarations() error {
	for id, content := range builtinConnectorSources {
		if !connectorNamePattern.MatchString(id) {
			return fmt.Errorf("invalid built-in connector name %q", id)
		}
		parser.RegisterBuiltinVirtualFile(builtinConnectorPath(id), content)
	}
	return nil
}

func builtinConnectorPath(id string) string {
	return parser.BuiltinPathPrefix + "connectors/" + id + ".md"
}
