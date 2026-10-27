package readerstringconv

import (
	b "bytes"
	s "strings"
)

func aliasedImports(data []byte, text string) {
	_ = s.NewReader(string(data)) // want `use bytes.NewReader`
	_ = b.NewReader([]byte(text)) // want `use strings.NewReader`
}
