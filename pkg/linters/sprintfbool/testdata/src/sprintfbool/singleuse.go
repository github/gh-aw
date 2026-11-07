package sprintfbool

import "fmt"

func singleUseSprintfBool(b bool) string {
	return fmt.Sprintf("%t", b) // want `use strconv\.FormatBool\(b\) instead of fmt\.Sprintf\("%t", b\)`
}
