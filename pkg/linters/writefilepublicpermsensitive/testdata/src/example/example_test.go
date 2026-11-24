package example

import (
	"github.com/github/gh-aw/pkg/constants"
	"os"
)

func testFile(data []byte) {
	os.WriteFile("secret", data, constants.FilePermPublic)
}
