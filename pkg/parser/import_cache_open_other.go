//go:build !unix

package parser

import "os"

func openImportCacheFile(dir *os.Root, name string) (*os.File, error) {
	return dir.Open(name)
}
