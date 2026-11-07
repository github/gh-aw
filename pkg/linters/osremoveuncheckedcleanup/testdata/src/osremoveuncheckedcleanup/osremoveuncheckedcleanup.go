package osremoveuncheckedcleanup

import (
	stdos "os"
	"osremoveuncheckedcleanup/other"
)

func discarded(path string) {
	stdos.Remove(path)     // want `error return from os\.Remove is discarded`
	stdos.RemoveAll(path)  // want `error return from os\.RemoveAll is discarded`
	(stdos.Remove(path))   // want `error return from os\.Remove is discarded`
	_ = stdos.Remove(path) // want `error return from os\.Remove is discarded`
}

func handled(path string) error {
	err := stdos.Remove(path)
	if err != nil {
		return err
	}
	if err := stdos.RemoveAll(path); err != nil {
		return err
	}
	return nil
}

func returned(path string) error {
	return stdos.Remove(path)
}

func deferred(path string) {
	defer stdos.Remove(path)
}

func sameNamedMethods(path string) {
	c := other.Cleaner{}
	c.Remove(path)
	c.RemoveAll(path)
}

func suppressed(path string) {
	stdos.Remove(path)        //nolint:osremoveuncheckedcleanup
	_ = stdos.RemoveAll(path) //nolint:osremoveuncheckedcleanup
}
