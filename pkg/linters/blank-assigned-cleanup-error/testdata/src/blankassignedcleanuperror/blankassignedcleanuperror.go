package blankassignedcleanuperror

import (
	"os"
)

func bad(dir string) {
	_ = os.RemoveAll(dir)             // want `error from os.RemoveAll is discarded`
	_ = os.Remove(dir)                // want `error from os.Remove is discarded`
	_ = os.WriteFile(dir, nil, 0o600) // want `error from os.WriteFile is discarded`
	_ = os.MkdirAll(dir, 0o755)       // want `error from os.MkdirAll is discarded`
	_ = (os.RemoveAll(dir))           // want `error from os.RemoveAll is discarded`
	(_) = os.Remove(dir)              // want `error from os.Remove is discarded`
	remove := os.RemoveAll
	_ = remove(dir) // not resolved through alias; no diagnostic
	func() {
		_ = os.Remove(dir) // want `error from os.Remove is discarded`
	}()
}

func good(dir string) error {
	if err := os.RemoveAll(dir); err != nil {
		return err
	}
	defer func() {
		_ = os.RemoveAll(dir) // deliberate best-effort cleanup
	}()
	defer (func() {
		_ = os.Remove(dir)
	})()
	f, err := os.Open(dir)
	if err != nil {
		return err
	}
	_ = f.Close() // handled by closeerrorunchecked, not this linter
	_ = os.Getenv("X")
	_, _ = os.Stat(dir)
	_ = os.Remove(dir) //nolint:blankassignedcleanuperror
	return nil
}
