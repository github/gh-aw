// Package a is the test fixture for positive cases of the reflect-deepequal-usage analyzer.
package a

import (
	"reflect"
)

// testDeepEqualInCondition should be flagged - using reflect.DeepEqual in if condition
func testDeepEqualInCondition(a, b interface{}) {
	if reflect.DeepEqual(a, b) { // want `reflect.DeepEqual\(\) is inefficient`
		println("equal")
	}
}

// testDeepEqualInAssignment should be flagged - assigning reflect.DeepEqual result
func testDeepEqualInAssignment(a, b interface{}) bool {
	result := reflect.DeepEqual(a, b) // want `reflect.DeepEqual\(\) is inefficient`
	return result
}

// testDeepEqualInReturn should be flagged - returning reflect.DeepEqual result
func testDeepEqualInReturn(a, b interface{}) bool {
	return reflect.DeepEqual(a, b) // want `reflect.DeepEqual\(\) is inefficient`
}

// testDeepEqualNegation should be flagged - negating reflect.DeepEqual result
func testDeepEqualNegation(a, b interface{}) bool {
	return !reflect.DeepEqual(a, b) // want `reflect.DeepEqual\(\) is inefficient`
}

// testDeepEqualWithAlias should be flagged - using an alias for reflect package
func testDeepEqualWithAlias(a, b interface{}) bool {
	r := reflect.DeepEqual // reference to function
	return r(a, b)         // want `reflect.DeepEqual\(\) is inefficient`
}

// testDeepEqualInComparison should be flagged - comparing result in expression
func testDeepEqualInComparison(a, b, c, d interface{}) bool {
	return reflect.DeepEqual(a, b) && reflect.DeepEqual(c, d) // want `reflect.DeepEqual\(\) is inefficient` `reflect.DeepEqual\(\) is inefficient`
}

// testDeepEqualStandalone should be flagged - standalone call that discards result
func testDeepEqualStandalone(a, b interface{}) {
	reflect.DeepEqual(a, b) // want `reflect.DeepEqual\(\) is inefficient`
}
