//go:build !linux && !darwin && !freebsd && !openbsd && !netbsd && !dragonfly

package connectors

import (
	"errors"
	"os"
)

func openConnectorStagedFile(_ *os.Root, _ string) (*os.File, error) {
	return nil, errors.New("connector staged files require a no-follow nonblocking regular-file host")
}
