package errorstringformat

import (
	"fmt"
	"strings"
)

// Bad cases - should trigger warnings

func badToLower() {
	err := fmt.Errorf("ERROR MESSAGE")
	result := strings.ToLower(err.Error()) // want `err.Error\(\) call should be replaced with err for direct use with %v or error type`
	_ = result
}

func badToUpper() {
	err := fmt.Errorf("message")
	result := strings.ToUpper(err.Error()) // want `err.Error\(\) call should be replaced with err for direct use with %v or error type`
	_ = result
}

func badSprintfWithError() {
	err := fmt.Errorf("some error")
	result := fmt.Sprintf("%s", err.Error()) // want `err.Error\(\) call should be replaced with err for direct use with %v or error type`
	_ = result
}

func badSprintWithError() {
	err := fmt.Errorf("another error")
	result := fmt.Sprint(err.Error()) // want `err.Error\(\) call should be replaced with err for direct use with %v or error type`
	_ = result
}

func badTitle() {
	e := fmt.Errorf("some error")
	result := strings.Title(e.Error()) // want `err.Error\(\) call should be replaced with err for direct use with %v or error type`
	_ = result
}

func badTrimSpace() {
	err := fmt.Errorf("  error with spaces  ")
	result := strings.TrimSpace(err.Error()) // want `err.Error\(\) call should be replaced with err for direct use with %v or error type`
	_ = result
}

func badHasPrefix() {
	err := fmt.Errorf("test error")
	contains := strings.HasPrefix(err.Error(), "test") // want `err.Error\(\) call should be replaced with err for direct use with %v or error type`
	_ = contains
}

func badMultipleCallsInSameFunc() {
	err := fmt.Errorf("error 1")
	l := strings.ToLower(err.Error()) // want `err.Error\(\) call should be replaced with err for direct use with %v or error type`

	err2 := fmt.Errorf("error 2")
	u := strings.ToUpper(err2.Error()) // want `err.Error\(\) call should be replaced with err for direct use with %v or error type`

	_ = l
	_ = u
}
