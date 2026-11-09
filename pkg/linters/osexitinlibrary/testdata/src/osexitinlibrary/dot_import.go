package osexitinlibrary

import . "os"

// flagged: dot-imported Exit still resolves to os.Exit.
func stopProcessDotImport() {
	Exit(1) // want `os.Exit called in library package`
}
