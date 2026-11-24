package other

import "io/fs"

const FilePermPublic fs.FileMode = 0644

func WriteFile(string, []byte, fs.FileMode) error { return nil }
