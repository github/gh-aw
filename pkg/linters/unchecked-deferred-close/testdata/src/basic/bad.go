package basic

import (
	"os"
)

func bad(filename string) error {
	f, err := os.Open(filename)
	if err != nil {
		return err
	}
	defer f.Close() // want "defer close\\(\\) call ignores error return value"
	// ... use f
	return nil
}
