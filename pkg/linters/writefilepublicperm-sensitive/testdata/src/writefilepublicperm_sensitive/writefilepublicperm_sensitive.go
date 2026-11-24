package writefilepublicperm_sensitive

import (
	"os"

	"constants"
)

func bad(tokenPath, name string, data []byte) {
	_ = os.WriteFile(tokenPath, data, constants.FilePermPublic)               // want `os.WriteFile uses FilePermPublic for sensitive-looking data \("token"\)`
	_ = os.WriteFile("out.txt", []byte(name), constants.FilePermPublic)       // ok: not sensitive
	_ = os.WriteFile("a/.env", data, constants.FilePermPublic)                // want `os.WriteFile uses FilePermPublic`
	_ = os.WriteFile(name, apiKey(), constants.FilePermPublic)                // want `os.WriteFile uses FilePermPublic for sensitive-looking data \("key"\)`
	_ = os.WriteFile(name, data, (constants.FilePermPublic))                  // ok: not sensitive
	_ = os.WriteFile(tokenPath, data, (constants.FilePermPublic))             // want `os.WriteFile uses FilePermPublic`
	_, _ = os.OpenFile("secrets.json", os.O_CREATE, constants.FilePermPublic) // want `os.OpenFile uses FilePermPublic for sensitive-looking data \("secrets"\)`
	_ = os.MkdirAll("dir/credentials", constants.FilePermPublic)              // want `os.MkdirAll uses FilePermPublic for sensitive-looking data \("credentials"\)`
	_ = os.MkdirAll(authDir, constants.FilePermPublic)                        // want `os.MkdirAll uses FilePermPublic`
}

var authDir = "x"

func apiKey() []byte { return nil }

func good(tokenPath, name string, data []byte) {
	_ = os.WriteFile(tokenPath, data, constants.FilePermSensitive)
	_ = os.WriteFile(name, data, constants.FilePermPublic)
	_ = os.WriteFile(tokenPath, data, 0o644)
	_, _ = os.OpenFile("monkeyboard.txt", os.O_CREATE, constants.FilePermPublic)
	_ = os.MkdirAll("author", constants.FilePermPublic)
	_ = os.MkdirAll("lockdir", constants.FilePermSensitive)
}

func bad2() {
	_, _ = os.OpenFile("x.lock", os.O_CREATE, constants.FilePermPublic) // want `os.OpenFile uses FilePermPublic`
}
