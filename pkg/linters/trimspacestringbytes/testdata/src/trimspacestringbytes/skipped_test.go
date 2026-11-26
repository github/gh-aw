package trimspacestringbytes

import "strings"

func skippedTestFile(b []byte) { _ = strings.TrimSpace(string(b)) }
