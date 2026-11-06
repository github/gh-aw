package ioutildeprecated

import (
	"os"
	// dot-import makes all exported names of io/ioutil available unqualified
	. "io/ioutil"
)

func BadDotReadAll() {
	f, _ := os.Open("file.txt")
	defer f.Close()
	_, _ = ReadAll(f) // want `ioutil\.ReadAll is deprecated; use io\.ReadAll instead`
}

func BadDotReadFile() {
	_, _ = ReadFile("file.txt") // want `ioutil\.ReadFile is deprecated; use os\.ReadFile instead`
}

func BadDotWriteFile() {
	_ = WriteFile("file.txt", []byte{}, 0644) // want `ioutil\.WriteFile is deprecated; use os\.WriteFile instead`
}

func BadDotDiscard() {
	_ = Discard // want `ioutil\.Discard is deprecated; use io\.Discard instead`
}

// Discard used as a selector receiver was previously skipped because the
// identifier's parent is a SelectorExpr.
func BadDotDiscardMethod() {
	_, _ = Discard.Write([]byte("x")) // want `ioutil\.Discard is deprecated; use io\.Discard instead`
}
