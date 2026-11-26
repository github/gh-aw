package coverage

import "strings"

func examples(b []byte) {
	_ = strings.TrimSpace(string(b)) // want `consider bytes.TrimSpace`
	_ = strings.TrimSpace(string(b))
}
