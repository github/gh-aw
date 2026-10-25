package readerstringconv

import (
	"bytes"
	"strings"
)

func testFile(b []byte, s string) {
	_ = strings.NewReader(string(b))
	_ = bytes.NewReader([]byte(s))
}
