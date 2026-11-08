package filepathwalkpreferwalkdir

import (
	"io/fs"
	. "path/filepath"
)

func badDotImported() error {
	return Walk(".", nil) // want `filepath\.Walk performs an os\.Lstat`
}

func goodDotWalkDir() error {
	return WalkDir(".", func(path string, d fs.DirEntry, err error) error { return nil })
}
