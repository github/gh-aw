package osopenreadallpreferreadfile

import (
	stream "io"
	files "os"
)

func importsAliased(path string) ([]byte, error) {
	f, err := files.Open(path) // want "use os.ReadFile instead of os.Open"
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return stream.ReadAll(f)
}

func packageShadowed(path string) {
	os := struct {
		Open func(string) (*files.File, error)
	}{files.Open}
	f, _ := os.Open(path)
	defer f.Close()
	stream.ReadAll(f)
}

func readShadowed(path string) {
	io := struct {
		ReadAll func(stream.Reader) ([]byte, error)
	}{stream.ReadAll}
	f, _ := files.Open(path)
	defer f.Close()
	io.ReadAll(f)
}
