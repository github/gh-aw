package filepathwalkpreferwalkdir

import "path/filepath"

func qualified(root string) {
	filepath.Walk(root, nil) // want "filepath.Walk is less efficient"
}

func parenthesized(root string) {
	(filepath.Walk)(root, nil) // want "filepath.Walk is less efficient"
}

func functionAlias(root string) {
	walk := filepath.Walk
	walk(root, nil) // want "filepath.Walk is less efficient"
}

func reassignedFunctionAlias(root string) {
	walk := filepath.Walk
	walk = func(string, filepath.WalkFunc) error { return nil }
	walk(root, nil)
}

func alreadyWalkDir(root string) {
	filepath.WalkDir(root, nil)
}

func suppressed(root string) {
	filepath.Walk(root, nil) //nolint:filepathwalkpreferwalkdir
}

func otherMethod(root string, other struct {
	Walk func(string, filepath.WalkFunc) error
}) {
	other.Walk(root, nil)
}
