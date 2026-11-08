package filepathwalkpreferwalkdir

import (
	"io/fs"
	"path/filepath"
	fp "path/filepath"
)

type walker struct{}

func (walker) Walk(root string, fn filepath.WalkFunc) error { return nil }

func Walker(root string, fn filepath.WalkFunc) error { return nil }

func badStatement() {
	filepath.Walk(".", func(path string, info fs.FileInfo, err error) error { return nil }) // want `filepath\.Walk performs an os\.Lstat`
}

func badAssignment() error {
	err := filepath.Walk(".", nil) // want `filepath\.Walk performs an os\.Lstat`
	return err
}

func badReturn() error {
	return filepath.Walk(".", nil) // want `filepath\.Walk performs an os\.Lstat`
}

func badAlias() error {
	return fp.Walk(".", nil) // want `filepath\.Walk performs an os\.Lstat`
}

func badFuncValueCall() error {
	return (filepath.Walk)(".", nil) // want `filepath\.Walk performs an os\.Lstat`
}

func badSuppressedNot() error {
	var err error
	if err = filepath.Walk(".", nil); err != nil { // want `filepath\.Walk performs an os\.Lstat`
		return err
	}
	return nil
}

func goodWalkDir() error {
	return filepath.WalkDir(".", func(path string, d fs.DirEntry, err error) error { return nil })
}

func goodMethod(w walker) error {
	return w.Walk(".", nil)
}

func goodLocalFunc() error {
	return Walker(".", nil)
}

func goodNolint() error {
	return filepath.Walk(".", nil) //nolint:filepathwalkpreferwalkdir
}
