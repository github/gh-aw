//go:build linux || darwin || freebsd || openbsd || netbsd || dragonfly

package connectors

import (
	"errors"
	"os"
	"syscall"
)

func openConnectorStagedFile(root *os.Root, name string) (*os.File, error) {
	file, err := root.OpenFile(name, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, err
	}
	info, err := file.Stat()
	if err != nil {
		if closeErr := file.Close(); closeErr != nil {
			log.Printf("close rejected connector file: %v", closeErr)
		}
		return nil, err
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || stat.Nlink != 1 {
		if closeErr := file.Close(); closeErr != nil {
			log.Printf("close rejected connector file: %v", closeErr)
		}
		return nil, errors.New("connector staged file must be an isolated regular file")
	}
	return file, nil
}
