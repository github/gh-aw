package errorstringformat

import (
	"fmt"
	"strings"
)

// Good cases - should not trigger warnings

func goodDirectErrorUsage() {
	err := fmt.Errorf("test error")
	// Using error directly without .Error() is correct
	result := fmt.Sprintf("%v", err)
	_ = result
}

func goodStringVariable() {
	s := "hello world"
	// strings functions on non-error strings are fine
	lower := strings.ToLower(s)
	_ = lower
}

func goodStringLiteral() {
	// Calling on a literal string is fine
	upper := strings.ToUpper("hello")
	_ = upper
}

func goodErrorDirectlyToFmt() {
	err := fmt.Errorf("some error")
	// Passing error directly to fmt functions is correct
	msg := fmt.Sprint(err)
	_ = msg
}

func goodNoErrorMethod() {
	// Regular types without .Error() method shouldn't trigger
	name := "user"
	trimmed := strings.TrimSpace(name)
	_ = trimmed
}

func goodSuppressionComment() {
	err := fmt.Errorf("test")
	//nolint:errorstringformat
	result := strings.ToLower(err.Error())
	_ = result
}
