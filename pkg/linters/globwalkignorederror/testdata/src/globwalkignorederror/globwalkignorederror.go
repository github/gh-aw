package globwalkignorederror

import (
	"os"
	"path/filepath"
)

func bad() {
	files, _ := filepath.Glob("*.go") // want `error return from filepath\.Glob is discarded`
	_ = files
	entries, _ := os.ReadDir(".") // want `error return from os\.ReadDir is discarded`
	_ = entries
}

func badBareCalls() {
	filepath.Glob("*.go") // want `error return from filepath\.Glob is discarded`
	os.ReadDir(".")       // want `error return from os\.ReadDir is discarded`
}

func good() {
	files, err := filepath.Glob("*.go")
	if err != nil {
		return
	}
	_ = files

	entries, err2 := os.ReadDir(".")
	if err2 != nil {
		return
	}
	_ = entries
}

func suppressed() {
	//nolint:globwalkignorederror
	files, _ := filepath.Glob("*.go")
	_ = files
	entries, _ := os.ReadDir(".") //nolint:globwalkignorederror
	_ = entries
}
