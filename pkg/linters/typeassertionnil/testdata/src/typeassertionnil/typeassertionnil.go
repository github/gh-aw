package typeassertionnil

// Bad: unchecked type assertion to pointer type
func badUncheckedPointer(v interface{}) {
	x := v.(*MyType) // want "type assertion to \\*MyType without ok-check"
	_ = x
}

// Bad: unchecked type assertion to pointer type in return
func badReturnPointer(v interface{}) *MyType {
	return v.(*MyType) // want "type assertion to \\*MyType without ok-check"
}

// Bad: unchecked type assertion to pointer type in assignment
func badAssignPointer(v interface{}) {
	var x *MyType
	x = v.(*MyType) // want "type assertion to \\*MyType without ok-check"
	_ = x
}

// Good: type assertion with ok-check
func goodTwoValueCheck(v interface{}) {
	x, ok := v.(*MyType)
	if ok {
		_ = x
	}
}

// Good: type assertion with ok-check and error variable
func goodTwoValueError(v interface{}) {
	x, err := v.(*MyType)
	if err {
		_ = x
	}
}

// Good: type assertion to non-pointer type (no nil risk)
func goodNonPointer(v interface{}) {
	x := v.(string)
	_ = x
}

// Good: type assertion to non-pointer type returning int
func goodNonPointerInt(v interface{}) {
	x := v.(int)
	_ = x
}

// Good: type assertion to non-pointer type returning struct
func goodNonPointerStruct(v interface{}) {
	x := v.(MyType)
	_ = x
}

// Bad: unchecked pointer type assertion returned
func badSingleValuePointerReturn(v interface{}) *MyType {
	return v.(*MyType) // want "type assertion to \\*MyType without ok-check"
}

// Bad: unchecked pointer type assertion used in function call
func badPointerInFuncCall(v interface{}) {
	x := v.(*MyType) // want "type assertion to \\*MyType without ok-check"
	print(x)
}

// Good: type assertion with two-value and underscore ok (still safe)
func goodTwoValueUnderscore(v interface{}) {
	x, _ := v.(*MyType)
	_ = x
}

type MyType struct {
	Field string
}


