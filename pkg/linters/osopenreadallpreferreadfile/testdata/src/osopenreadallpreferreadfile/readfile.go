package osopenreadallpreferreadfile

import (
	"io"
	"io/ioutil"
	"os"
)

func basic(path string) ([]byte, error) {
	f, err := os.Open(path) // want "use os.ReadFile instead of os.Open"
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return io.ReadAll(f)
}

func legacy(path string) ([]byte, error) {
	f, err := os.Open(path) // want "use os.ReadFile instead of os.Open"
	if err != nil {
		return nil, err
	}
	defer f.Close()
	data, err := ioutil.ReadAll(f)
	return data, err
}

func parenthesized(path string) {
	f, _ := (os.Open)(path) // want "use os.ReadFile instead of os.Open"
	defer (f).Close()
	_, _ = (io.ReadAll)((f))
}

func discarded(path string) {
	f, _ := os.Open(path) // want "use os.ReadFile instead of os.Open"
	defer f.Close()
	io.ReadAll(f)
}

func extraUse(path string) {
	f, _ := os.Open(path)
	defer f.Close()
	f.Stat()
	io.ReadAll(f)
}

func multipleReads(path string) {
	f, _ := os.Open(path)
	defer f.Close()
	io.ReadAll(f)
	io.ReadAll(f)
}

func missingDefer(path string) {
	f, _ := os.Open(path)
	io.ReadAll(f)
	f.Close()
}

func lateDefer(path string) {
	f, _ := os.Open(path)
	io.ReadAll(f)
	defer f.Close()
}

func reassigned(path string) {
	f, _ := os.Open(path)
	defer f.Close()
	f, _ = os.Open(path)
	io.ReadAll(f)
}

func alias(path string) {
	f, _ := os.Open(path)
	defer f.Close()
	other := f
	io.ReadAll(other)
}

func captured(path string) {
	f, _ := os.Open(path)
	defer f.Close()
	go func() { io.ReadAll(f) }()
}

func deferredRead(path string) {
	f, _ := os.Open(path)
	defer f.Close()
	defer io.ReadAll(f)
}

func deferredClosure(path string) {
	f, _ := os.Open(path)
	defer func() { f.Close() }()
	io.ReadAll(f)
}

func conditionalRead(path string, read bool) {
	f, _ := os.Open(path)
	defer f.Close()
	if read {
		io.ReadAll(f)
	}
}

func loopedRead(path string) {
	f, _ := os.Open(path)
	defer f.Close()
	for i := 0; i < 2; i++ {
		io.ReadAll(f)
	}
}

func writeSide(path string) {
	f, _ := os.Create(path)
	defer f.Close()
	io.ReadAll(f)
	g, _ := os.OpenFile(path, os.O_RDONLY, 0)
	defer g.Close()
	io.ReadAll(g)
}

func existingVariable(path string, f *os.File) {
	f, err := os.Open(path)
	if err != nil {
		return
	}
	defer f.Close()
	io.ReadAll(f)
}

var global *os.File

func globalVariable(path string) {
	global, _ = os.Open(path)
	defer global.Close()
	io.ReadAll(global)
}

func shadowed(path string) {
	f, _ := os.Open(path)
	defer f.Close()
	{
		f, _ := os.Open(path) // want "use os.ReadFile instead of os.Open"
		defer f.Close()
		io.ReadAll(f)
	}
	f.Stat()
}

func suppressed(path string) {
	f, _ := os.Open(path) //nolint:osopenreadallpreferreadfile
	defer f.Close()
	io.ReadAll(f)
}

func suppressedAll(path string) {
	f, _ := os.Open(path) //nolint:all
	defer f.Close()
	io.ReadAll(f)
}
