package basic

import (
	"io"
	"os"
)

type NamedCloser interface {
	io.Closer
}

type EmbeddedCloser struct {
	io.Closer
}

type GenericCloser interface {
	Close() error
}

func bad(filename string) error {
	f, err := os.Open(filename)
	if err != nil {
		return err
	}
	defer f.Close() // want "defer close\\(\\) call ignores error return value"
	// ... use f
	return nil
}

func badIOCloser(c io.Closer) {
	defer c.Close() // want "defer close\\(\\) call ignores error return value"
}

func badNamedInterface(c NamedCloser) {
	defer c.Close() // want "defer close\\(\\) call ignores error return value"
}

func badEmbeddedCloser(c EmbeddedCloser) {
	defer c.Close() // want "defer close\\(\\) call ignores error return value"
}

func badTypeParameter[T GenericCloser](c T) {
	defer c.Close() // want "defer close\\(\\) call ignores error return value"
}
