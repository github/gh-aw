package blankassignedcleanuperror

import (
	stdos "os"

	"blankassignedcleanuperror/other"
)

func discarded(path string, data []byte) {
	_ = stdos.Remove(path)                 // want `cleanup error from os\.Remove is discarded`
	_ = stdos.RemoveAll(path)              // want `cleanup error from os\.RemoveAll is discarded`
	_ = stdos.WriteFile(path, data, 0o600) // want `cleanup error from os\.WriteFile is discarded`
	_ = stdos.MkdirAll(path, 0o700)        // want `cleanup error from os\.MkdirAll is discarded`
	_ = (stdos.Remove(path))               // want `cleanup error from os\.Remove is discarded`
}

func parallelAssignments(path string) {
	_, kept := stdos.Remove(path), 1 // want `cleanup error from os\.Remove is discarded`
	if kept != 1 {
		return
	}
	kept, _ = 1, stdos.RemoveAll(path) // want `cleanup error from os\.RemoveAll is discarded`
}

func deferredCleanup(path string) {
	defer func() {
		_ = stdos.Remove(path) // want `cleanup error from os\.Remove is discarded`
	}()
	defer (func() {
		_ = stdos.RemoveAll(path) // want `cleanup error from os\.RemoveAll is discarded`
	})()
}

func handled(path string, data []byte) error {
	if err := stdos.WriteFile(path, data, 0o600); err != nil {
		return err
	}
	if err := stdos.MkdirAll(path, 0o700); err != nil {
		return err
	}
	return nil
}

func notCleanup(path string) {
	cleaner := other.Cleaner{}
	_ = cleaner.Remove(path)
	_ = cleaner.RemoveAll(path)
}

type closer struct{}

func (closer) Close() error { return nil }

func closeErrorAlreadyHandled(c closer) {
	_ = c.Close()
}

func suppressed(path string) {
	_ = stdos.Remove(path) //nolint:blankassignedcleanuperror
}

func suppressedWithLegacyDirective(path string) {
	_ = stdos.Remove(path)    //nolint:osremoveuncheckedcleanup
	_ = stdos.RemoveAll(path) //nolint:osremoveuncheckedcleanup
}
