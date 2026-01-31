// Package sliceutil provides utility functions for working with slices.
package sliceutil

import "strings"

// Contains checks if a string slice contains a specific string.
func Contains(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}

// ContainsAny checks if a string contains any of the given substrings.
func ContainsAny(s string, substrings ...string) bool {
	for _, sub := range substrings {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}

// ContainsIgnoreCase checks if a string contains a substring, ignoring case.
func ContainsIgnoreCase(s, substr string) bool {
	return strings.Contains(strings.ToLower(s), strings.ToLower(substr))
}

// Filter returns a new slice containing only elements that match the predicate.
// This is a pure function that does not modify the input slice.
func Filter[T any](slice []T, predicate func(T) bool) []T {
	result := make([]T, 0, len(slice))
	for _, item := range slice {
		if predicate(item) {
			result = append(result, item)
		}
	}
	return result
}

// Map transforms each element in a slice using the provided function.
// This is a pure function that does not modify the input slice.
func Map[T, U any](slice []T, transform func(T) U) []U {
	result := make([]U, len(slice))
	for i, item := range slice {
		result[i] = transform(item)
	}
	return result
}

// MapToSlice converts a map's keys to a slice.
// The order of elements is not guaranteed as map iteration order is undefined.
// This is a pure function that does not modify the input map.
func MapToSlice[K comparable, V any](m map[K]V) []K {
	result := make([]K, 0, len(m))
	for key := range m {
		result = append(result, key)
	}
	return result
}
