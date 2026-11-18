//go:build !aix && !darwin && !dragonfly && !freebsd && !linux && !netbsd && !openbsd && !solaris

package cli

import "os"

func cliRuntimeOwnedByCurrentUser(_ os.FileInfo) bool {
	return false
}
