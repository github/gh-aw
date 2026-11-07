package walk_dir_ignored_error

import (
	"io/fs"
	. "path/filepath"
)

// badDotImported flags dot-imported Walk/WalkDir calls with discarded errors.
func badDotImported() {
	_ = Walk(".", func(path string, info fs.FileInfo, err error) error { // want `error return from filepath\.Walk is discarded`
		return nil
	})
	WalkDir(".", func(path string, info fs.DirEntry, err error) error { // want `error return from filepath\.WalkDir is discarded`
		return nil
	})
}

// goodDotImported checks the error from a dot-imported Walk.
func goodDotImported() error {
	return Walk(".", func(path string, info fs.FileInfo, err error) error {
		return nil
	})
}
