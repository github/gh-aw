package example

import (
	"example/other"
	c "github.com/github/gh-aw/pkg/constants"
	files "os"
	"path/filepath"
)

const sensitiveName = "credentials.json"
const publicName = "report.md"

func check(tokenData []byte, data []byte, authPath string) {
	files.WriteFile("token.txt", data, c.FilePermPublic)                            // want "os.WriteFile uses FilePermPublic"
	files.WriteFile(publicName, tokenData, (c.FilePermPublic))                      // want "os.WriteFile uses FilePermPublic"
	files.WriteFile(filepath.Join("config", sensitiveName), data, c.FilePermPublic) // want "os.WriteFile uses FilePermPublic"
	files.WriteFile("report.md", []byte("auth configuration"), c.FilePermPublic)    // want "os.WriteFile uses FilePermPublic"
	files.WriteFile("private-key", data, c.FilePermPublic)                          // want "os.WriteFile uses FilePermPublic"
	files.WriteFile(".env", data, c.FilePermPublic)                                 // want "os.WriteFile uses FilePermPublic"
	files.WriteFile("config.lock", data, c.FilePermPublic)                          // want "os.WriteFile uses FilePermPublic"
	files.OpenFile(authPath, files.O_CREATE|files.O_WRONLY, c.FilePermPublic)       // want "os.OpenFile uses FilePermPublic"
	files.MkdirAll("secret-dir", c.FilePermPublic)                                  // want "restricted directory mode"

	files.WriteFile(publicName, data, c.FilePermPublic)
	files.WriteFile("secret", data, c.FilePermSensitive)
	files.WriteFile("secret", data, 0644)
	files.WriteFile("secret", data, other.FilePermPublic)
	other.WriteFile("secret", data, c.FilePermPublic)
	files.WriteFile("secret", data, c.FilePermPublic) //nolint:writefilepublicpermsensitive
}
