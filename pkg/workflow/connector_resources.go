package workflow

import (
	"fmt"
	"path"

	ghaw "github.com/github/gh-aw"
	"github.com/github/gh-aw/pkg/parser"
)

func init() {
	const directory = ".github/aw/connectors"
	entries, err := ghaw.ConnectorResources.ReadDir(directory)
	if err != nil {
		panic(fmt.Sprintf("read embedded connector declarations: %v", err))
	}
	for _, entry := range entries {
		content, err := ghaw.ConnectorResources.ReadFile(path.Join(directory, entry.Name()))
		if err != nil {
			panic(fmt.Sprintf("read embedded connector %s: %v", entry.Name(), err))
		}
		parser.RegisterBuiltinVirtualFile(parser.BuiltinPathPrefix+"connectors/"+entry.Name(), content)
	}
}
