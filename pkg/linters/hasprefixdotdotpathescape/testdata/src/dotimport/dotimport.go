package dotimport

import . "strings"

func checks(path string) bool {
	return HasPrefix(path, "..") // want `strings.HasPrefix\(value, "\.\."\) also matches`
}
