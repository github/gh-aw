package osopenreadallpreferreadfile

import (
	"io"
	"io/ioutil"
	"os"
)

func flagged(path string) ([]byte, error) {
	f, err := os.Open(path) // want `use os.ReadFile instead`
	if err != nil {
		return nil, err
	}
	defer f.Close()
	data, err := io.ReadAll(f)
	return data, err
}

func flaggedIoutilReturn(path string) ([]byte, error) {
	f, err := os.Open(path) // want `use os.ReadFile instead`
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return ioutil.ReadAll(f)
}

var sentinel error

func differentErrorCheck(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != sentinel {
		return nil, err
	}
	defer f.Close()
	return io.ReadAll(f)
}

func otherUse(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	_, _ = f.Stat()
	return io.ReadAll(f)
}

func noDefer(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	data, err := io.ReadAll(f)
	f.Close()
	return data, err
}

func wrappedReader(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return io.ReadAll(io.LimitReader(f, 10))
}

func suppressed(path string) ([]byte, error) {
	f, err := os.Open(path) //nolint:osopenreadallpreferreadfile // test
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return io.ReadAll(f)
}

// Write half is intentionally not reported.
func writeNotFlagged(path string, b []byte) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.Write(b)
	return err
}
