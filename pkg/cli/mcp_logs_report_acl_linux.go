package cli

import (
	"io/fs"
	"os"
)

// Linux POSIX ACLs mask named-user and group permissions with the mode's
// group class. The cache's mode checks therefore also bound those grants.
func validateMCPLogsACL(*os.File) error { return nil }

func validateMCPLogsDirectoryACL(*os.Root, string, fs.FileInfo) error { return nil }
