package sprintfbool

import "fmt"

func badParenCallee(b bool) string {
	return (fmt.Sprintf)("%t", b) // want `use strconv\.FormatBool\(b\) instead of fmt\.Sprintf\("%t", b\)`
}

func badParenFormat(b bool) string {
	return fmt.Sprintf(("%t"), b) // want `use strconv\.FormatBool\(b\) instead of fmt\.Sprintf\("%t", b\)`
}
