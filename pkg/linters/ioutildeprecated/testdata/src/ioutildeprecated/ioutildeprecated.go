package ioutildeprecated

import (
	"io"
	"io/fs"
	"io/ioutil"
	"os"
)

// fileMode is a named permission constant used for good-case examples, avoiding raw octal literals.
const fileMode fs.FileMode = 0o644

func BadReadAll() {
	f, _ := os.Open("file.txt")
	defer f.Close()
	_, _ = ioutil.ReadAll(f) // want `ioutil\.ReadAll is deprecated; use io\.ReadAll instead`
}

func BadReadFile() {
	_, _ = ioutil.ReadFile("file.txt") // want `ioutil\.ReadFile is deprecated; use os\.ReadFile instead`
}

func BadWriteFile() {
	_ = ioutil.WriteFile("file.txt", []byte("hello"), 0644) // want `ioutil\.WriteFile is deprecated; use os\.WriteFile instead`
}

func BadTempFile() {
	_, _ = ioutil.TempFile("", "prefix") // want `ioutil\.TempFile is deprecated; use os\.CreateTemp instead`
}

func BadTempDir() {
	_, _ = ioutil.TempDir("", "prefix") // want `ioutil\.TempDir is deprecated; use os\.MkdirTemp instead`
}

func BadReadDir() {
	_, _ = ioutil.ReadDir(".") // want `ioutil\.ReadDir is deprecated; use os\.ReadDir instead`
}

func BadNopCloser() {
	_ = ioutil.NopCloser(nil) // want `ioutil\.NopCloser is deprecated; use io\.NopCloser instead`
}

func BadDiscard() {
	_, _ = io.Copy(ioutil.Discard, nil) // want `ioutil\.Discard is deprecated; use io\.Discard instead`
}

// A qualified selector receiver is reported exactly once, on ioutil.Discard.
func BadDiscardMethod() {
	_, _ = ioutil.Discard.Write([]byte("x")) // want `ioutil\.Discard is deprecated; use io\.Discard instead`
}

type fieldHolder struct {
	Discard int
}

// A field that shares a deprecated name is not reported.
func GoodFieldNamedDiscard(h fieldHolder) int {
	return h.Discard
}

func GoodReadAll() {
	f, _ := os.Open("file.txt")
	defer f.Close()
	_, _ = io.ReadAll(f)
}

func GoodReadFile() {
	_, _ = os.ReadFile("file.txt")
}

func GoodWriteFile() {
	_ = os.WriteFile("file.txt", []byte("hello"), fileMode)
}

func GoodDiscard() {
	_, _ = io.Copy(io.Discard, nil)
}
