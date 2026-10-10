package basic

import (
	"fmt"
	"os"
)

func good(filename string) error {
	f, err := os.Open(filename)
	if err != nil {
		return err
	}
	defer func() {
		if err := f.Close(); err != nil {
			fmt.Fprintf(os.Stderr, "close error: %v\n", err)
		}
	}()
	// ... use f
	return nil
}

// NoReturnClose is a mock type that has Close() with no error return
type NoReturnClose struct{}

func (n *NoReturnClose) Close() {}

func goodNoError() {
	n := &NoReturnClose{}
	defer n.Close() // OK: Close() returns nothing
}
