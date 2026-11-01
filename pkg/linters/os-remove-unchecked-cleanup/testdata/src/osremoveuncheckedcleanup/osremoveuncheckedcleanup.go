package osremoveuncheckedcleanup

import (
	"log"
	"os"
)

type fake struct{}

func (fake) Remove(string) error { return nil }

func bad(p string) {
	os.Remove(p)       // want `os\.Remove error is discarded`
	os.RemoveAll(p)    // want `os\.RemoveAll error is discarded`
	(os.Remove)(p)     // want `os\.Remove error is discarded`
	os.Remove(p)       //nolint:osremoveuncheckedcleanup // intentional
	defer os.Remove(p) // deferred calls are covered by another linter
}

func good(p string) error {
	_ = os.Remove(p)
	if err := os.RemoveAll(p); err != nil {
		log.Println(err)
	}
	fake{}.Remove(p)
	return os.Remove(p)
}
