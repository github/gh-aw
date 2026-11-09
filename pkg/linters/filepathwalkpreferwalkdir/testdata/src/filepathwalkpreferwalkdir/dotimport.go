package filepathwalkpreferwalkdir

import . "path/filepath"

func dotImported(root string) {
	Walk(root, nil) // want "filepath.Walk is less efficient"
}
