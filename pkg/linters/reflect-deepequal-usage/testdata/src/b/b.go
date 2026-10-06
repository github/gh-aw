// Package b is the test fixture for negative cases of the reflect-deepequal-usage analyzer.
package b

import (
	"reflect"
)

// testTypedEquality should NOT be flagged - using typed equality
func testTypedEquality(a, b int) bool {
	return a == b
}

// testStringEquality should NOT be flagged - comparing strings directly
func testStringEquality(a, b string) bool {
	return a == b
}

// testSliceEquality should NOT be flagged - comparing slices with other methods
func testSliceEquality(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// testMapEquality should NOT be flagged - comparing maps without reflect.DeepEqual
func testMapEquality(a, b map[string]int) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if b[k] != v {
			return false
		}
	}
	return true
}

// testReflectNotDeepEqual should NOT be flagged - using reflect but different function
func testReflectNotDeepEqual(t interface{}) bool {
	return reflect.TypeOf(t) != nil
}

// testReflectValueEqual should NOT be flagged - using reflect.Value.Equal
func testReflectValueEqual(a, b reflect.Value) bool {
	return a.Equal(b)
}

// testStructEquality should NOT be flagged - comparing structs with typed equality
func testStructEquality(a, b struct{ x int }) bool {
	return a == b
}

// testNilComparison should NOT be flagged - comparing with nil
func testNilComparison(a interface{}) bool {
	return a == nil
}

func testWithNolintDirective(a, b interface{}) bool {
	// This should NOT be flagged because of the nolint directive
	return reflect.DeepEqual(a, b) //nolint:reflectdeepequalusage
}
