package typeassertionokdiscarded

import "fmt"

// Bad: two-value type assertion with blank ok identifier.
func BadBlankOkAssign(v interface{}) {
	s, _ := v.(string) // want `type assertion ok value is explicitly discarded with blank identifier`
	fmt.Println(s)
}

// Bad: two-value var declaration with blank ok identifier.
func BadBlankOkVarDecl(v interface{}) {
	var s, _ = v.(string) // want `type assertion ok value is explicitly discarded with blank identifier`
	fmt.Println(s)
}

// Bad: two-value reassignment with blank ok identifier.
func BadBlankOkReassign(v interface{}) string {
	var s string
	s, _ = v.(string) // want `type assertion ok value is explicitly discarded with blank identifier`
	return s
}

// Good: single-value type assertion that may panic.
func GoodSingleValue(v interface{}) string {
	return v.(string)
}

// Good: single-value assignment.
func GoodSingleValueAssign(v interface{}) {
	s := v.(string)
	fmt.Println(s)
}

// Good: two-value assertion with checked ok.
func GoodTwoValueChecked(v interface{}) {
	s, ok := v.(string)
	if ok {
		fmt.Println(s)
	}
}

// Good: two-value assertion with named ok variable.
func GoodTwoValueNamed(v interface{}) {
	s, err := v.(string)
	fmt.Println(s, err)
}

// Good: type switch is safe.
func GoodTypeSwitch(v interface{}) {
	switch t := v.(type) {
	case string:
		fmt.Println(t)
	}
}

// Good: parenthesized two-value assignment with checked ok.
func GoodParenTwoValueChecked(v interface{}) {
	s, ok := (v.(string))
	if ok {
		fmt.Println(s)
	}
}

// Good: parenthesized single-value assertion.
func GoodParenSingleValue(v interface{}) string {
	return (v.(string))
}

func suppressed(v interface{}) {
	//nolint:typeassertionokdiscarded
	s, _ := v.(string)
	fmt.Println(s)
}
