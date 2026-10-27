package readerstringconv

import . "strings"

func dotImport(data []byte) {
	_ = NewReader(string(data)) // want `use bytes.NewReader`
}
