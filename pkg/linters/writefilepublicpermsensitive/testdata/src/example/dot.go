package example

import (
	. "github.com/github/gh-aw/pkg/constants"
	. "os"
)

func dotImports(data []byte) {
	(WriteFile)("PASSWORD", data, (FilePermPublic)) // want "os.WriteFile uses FilePermPublic"
}
