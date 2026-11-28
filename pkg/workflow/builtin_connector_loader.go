package workflow

import (
	"embed"
	"fmt"
	"io/fs"
	"path/filepath"

	"github.com/github/gh-aw/pkg/parser"
)

//go:embed data/connectors/*.md
var builtinConnectorFS embed.FS

func init() {
	if err := registerBuiltinConnectorDeclarations(); err != nil {
		panic(fmt.Sprintf("failed to register built-in connector declarations: %v", err))
	}
}

func registerBuiltinConnectorDeclarations() error {
	return fs.WalkDir(builtinConnectorFS, "data/connectors", func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || filepath.Ext(path) != ".md" {
			return nil
		}
		id := filepath.Base(path[:len(path)-len(filepath.Ext(path))])
		if !connectorNamePattern.MatchString(id) {
			return fmt.Errorf("invalid built-in connector name %q", id)
		}
		content, err := builtinConnectorFS.ReadFile(path)
		if err != nil {
			return fmt.Errorf("read embedded connector %s: %w", path, err)
		}
		parser.RegisterBuiltinVirtualFile(builtinConnectorPath(id), content)
		return nil
	})
}

func builtinConnectorPath(id string) string {
	return parser.BuiltinPathPrefix + "connectors/" + id + ".md"
}
