package uncheckedsliceindex

// Bad cases: direct indexing without bounds checking

func badIndexSlice() {
	arr := []int{1, 2, 3}
	idx := 0
	_ = arr[idx] // want "direct slice indexing without bounds checking"
}

func badIndexZero() {
	arr := []int{1, 2, 3}
	_ = arr[0] // want "direct slice indexing without bounds checking"
}

func badIndexString() {
	s := "hello"
	idx := 0
	_ = s[idx] // want "direct string indexing without bounds checking"
}

func goodIndexStringLiteral() {
	_ = "hello"[0]
}

func badIndexHigherIndex() {
	arr := []int{1, 2, 3}
	idx := 5
	_ = arr[idx] // want "direct slice indexing without bounds checking"
}

// Good cases: bounds checking present or safe patterns

func goodCheckedLenGreater() {
	arr := []int{1, 2, 3}
	idx := 0
	if idx >= 0 && len(arr) > idx {
		_ = arr[idx]
	}
}

func goodCheckedIndexLess() {
	arr := []int{1, 2, 3}
	idx := 0
	if idx >= 0 && idx < len(arr) {
		_ = arr[idx]
	}
}

func goodCheckedLenGreaterZero() {
	arr := []int{1, 2, 3}
	if len(arr) > 0 {
		_ = arr[0]
	}
}

func goodCheckedLenNotZero() {
	arr := []int{1, 2, 3}
	if len(arr) != 0 {
		_ = arr[0]
	}
}

func goodCheckedWithAnd() {
	arr := []int{1, 2, 3}
	idx := 0
	if idx >= 0 && idx < len(arr) {
		_ = arr[idx]
	}
}

func goodRangeLoop() {
	arr := []int{1, 2, 3}
	for i := range arr {
		_ = arr[i]
	}
}

func goodRangeLoopWithValue() {
	arr := []int{1, 2, 3}
	for i, v := range arr {
		_ = arr[i]
		_ = v
	}
}

func goodStringRangeLoop() {
	s := "hello"
	for i := range s {
		_ = s[i]
	}
}

func goodSupressed() {
	arr := []int{1, 2, 3}
	//nolint:uncheckedsliceindex
	_ = arr[0]
}

// Safe index patterns with constant values
func goodConstantBoundCheck() {
	arr := [3]int{1, 2, 3}
	_ = arr[0] // Array with constant index is safe
}

// Edge case: nested if statements
func goodNestedCheck() {
	arr := []int{1, 2, 3}
	idx := 0
	if true {
		if idx >= 0 && idx < len(arr) {
			_ = arr[idx]
		}
	}
}

// Bad case: check in wrong scope
func badCheckWrongScope() {
	arr := []int{1, 2, 3}
	idx := 0
	if idx < len(arr) {
		_ = idx
	}
	_ = arr[idx] // want "direct slice indexing without bounds checking"
}

// Bad case: check but in else block
func badCheckInElse() {
	arr := []int{1, 2, 3}
	idx := 0
	if idx >= len(arr) {
		_ = idx
	} else {
		_ = arr[idx] // want "direct slice indexing without bounds checking"
	}
}

// Good case: multiple indices with proper checking
func goodMultipleWithCheck() {
	arr := []int{1, 2, 3, 4}
	i := 0
	j := 1
	if i >= 0 && i < len(arr) && j >= 0 && j < len(arr) {
		_ = arr[i]
		_ = arr[j]
	}
}

// Bad case: multiple indices but only one checked
func badMultiplePartialCheck() {
	arr := []int{1, 2, 3, 4}
	i := 0
	j := 1
	if i >= 0 && i < len(arr) {
		_ = arr[i]
		_ = arr[j] // want "direct slice indexing without bounds checking"
	}
}

// Good case: string check pattern
func goodStringCheck() {
	s := "hello"
	idx := 0
	if idx >= 0 && len(s) > idx {
		_ = s[idx]
	}
}

// Bad case: string without check
func badStringNoCheck() {
	s := "hello"
	idx := 1
	_ = s[idx] // want "direct string indexing without bounds checking"
}

func badRangeDifferentSlice(a, b []int) {
	for i := range a {
		_ = b[i] // want "direct slice indexing without bounds checking"
	}
}

func badRangeOffset(a []int) {
	for i := range a {
		_ = a[i+1] // want "direct slice indexing without bounds checking"
	}
}

func badInclusiveUpperBound(arr []int, idx int) {
	if idx >= 0 && idx <= len(arr) {
		_ = arr[idx] // want "direct slice indexing without bounds checking"
	}
}

func badOrBound(arr []int, idx int, fallback bool) {
	if idx >= 0 && idx < len(arr) || fallback {
		_ = arr[idx] // want "direct slice indexing without bounds checking"
	}
}

func badChangedAfterGuard(arr []int, idx int) {
	if idx >= 0 && idx < len(arr) {
		arr = nil
		_ = arr[idx] // want "direct slice indexing without bounds checking"
	}
}

func badShadowedIndex(arr []int, idx int) {
	if idx >= 0 && idx < len(arr) {
		idx := len(arr)
		_ = arr[idx] // want "direct slice indexing without bounds checking"
	}
}

func goodBoundedFor(arr []int) {
	for i := 0; i < len(arr); i++ {
		_ = arr[i]
	}
}

func goodBoundedOuterFor(arr []int) {
	for i := 0; i < len(arr); i++ {
		for range arr {
			_ = arr[i]
		}
	}
}

func goodEarlyReturnGuard(arr []int, idx int) int {
	if idx < 0 || idx >= len(arr) {
		return -1
	}
	return arr[idx]
}

func goodEarlyReturnWithSetup(arr []int, idx int) int {
	if idx < 0 || idx >= len(arr) {
		_ = "invalid index"
		return -1
	}
	return arr[idx]
}

func goodNestedEarlyReturnGuard(arr []int, idx int) int {
	if idx < 0 || idx >= len(arr) {
		return -1
	}
	if true {
		return arr[idx]
	}
	return -1
}
