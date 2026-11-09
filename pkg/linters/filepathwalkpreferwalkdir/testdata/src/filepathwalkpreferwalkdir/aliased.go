package filepathwalkpreferwalkdir

import fp "path/filepath"

func importedAlias(root string) {
	fp.Walk(root, nil) // want "filepath.Walk is less efficient"
}
