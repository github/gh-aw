//go:build unix

package parser

import (
	"os"
	"syscall"
)

func openImportCacheFile(dir *os.Root, name string) (*os.File, error) {
	return dir.OpenFile(name, os.O_RDONLY|syscall.O_NONBLOCK, 0)
}
