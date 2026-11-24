package constants

import "os"

const (
	FilePermPublic    os.FileMode = 0o644
	FilePermSensitive os.FileMode = 0o600
)
