package walk_dir_ignored_error

import (
	"io/fs"
	"path/filepath"
)

// badWalkBlankAssign flags filepath.Walk with discarded error in blank assignment
func badWalkBlankAssign() {
	_ = filepath.Walk(".", func(path string, info fs.FileInfo, err error) error { // want `error return from filepath\.Walk is discarded`
		return nil
	})
}

// badWalkDirBlankAssign flags filepath.WalkDir with discarded error in blank assignment
func badWalkDirBlankAssign() {
	_ = filepath.WalkDir(".", func(path string, info fs.DirEntry, err error) error { // want `error return from filepath\.WalkDir is discarded`
		return nil
	})
}

// badWalkBareCall flags bare filepath.Walk call with error ignored
func badWalkBareCall() {
	filepath.Walk(".", func(path string, info fs.FileInfo, err error) error { // want `error return from filepath\.Walk is discarded`
		return nil
	})
}

// badWalkDirBareCall flags bare filepath.WalkDir call with error ignored
func badWalkDirBareCall() {
	filepath.WalkDir(".", func(path string, info fs.DirEntry, err error) error { // want `error return from filepath\.WalkDir is discarded`
		return nil
	})
}

// badWalkParenthesized flags parenthesized filepath.Walk call
func badWalkParenthesized() {
	_ = (filepath.Walk(".", func(path string, info fs.FileInfo, err error) error { // want `error return from filepath\.Walk is discarded`
		return nil
	}))
}

func badParenthesizedBareCalls() {
	(filepath.Walk(".", func(path string, info fs.FileInfo, err error) error { // want `error return from filepath\.Walk is discarded`
		return nil
	}))
	(filepath.WalkDir(".", func(path string, info fs.DirEntry, err error) error { // want `error return from filepath\.WalkDir is discarded`
		return nil
	}))
}

// badParenthesizedCallee flags filepath.Walk/WalkDir whose callee is
// parenthesized; the call is resolved by type information, so the report
// must not depend on call.Fun being a bare selector.
func badParenthesizedCallee() {
	_ = (filepath.Walk)(".", func(path string, info fs.FileInfo, err error) error { // want `error return from filepath\.Walk is discarded`
		return nil
	})
	(filepath.WalkDir)(".", func(path string, info fs.DirEntry, err error) error { // want `error return from filepath\.WalkDir is discarded`
		return nil
	})
}

// goodWalk checks error from filepath.Walk correctly
func goodWalk() {
	err := filepath.Walk(".", func(path string, info fs.FileInfo, err error) error {
		return nil
	})
	if err != nil {
		return
	}
}

// goodWalkDeclare declares and checks error from filepath.Walk
func goodWalkDeclare() {
	if err := filepath.Walk(".", func(path string, info fs.FileInfo, err error) error {
		return nil
	}); err != nil {
		return
	}
}

// goodWalkDir checks error from filepath.WalkDir correctly
func goodWalkDir() {
	err := filepath.WalkDir(".", func(path string, info fs.DirEntry, err error) error {
		return nil
	})
	if err != nil {
		return
	}
}

// goodWalkDirDeclare declares and checks error from filepath.WalkDir
func goodWalkDirDeclare() {
	if err := filepath.WalkDir(".", func(path string, info fs.DirEntry, err error) error {
		return nil
	}); err != nil {
		return
	}
}

// goodWalkInlineCheck checks error inline from filepath.Walk
func goodWalkInlineCheck() {
	if filepath.Walk(".", func(path string, info fs.FileInfo, err error) error {
		return nil
	}) != nil {
		return
	}
}

// suppressed disables the linter via nolint comment
func suppressed() {
	//nolint:walkdirignoredeerror
	_ = filepath.Walk(".", func(path string, info fs.FileInfo, err error) error {
		return nil
	})
	//nolint:walkdirignoredeerror
	filepath.WalkDir(".", func(path string, info fs.DirEntry, err error) error {
		return nil
	})
	//nolint:walkdirignoredeerror
	(filepath.Walk(".", func(path string, info fs.FileInfo, err error) error {
		return nil
	}))
}
