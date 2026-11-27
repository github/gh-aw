package constants

import "io/fs"

const (
	FilePermPublic    fs.FileMode = 0644
	FilePermSensitive fs.FileMode = 0600
)
