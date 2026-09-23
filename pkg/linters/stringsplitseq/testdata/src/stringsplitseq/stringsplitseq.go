package stringsplitseq

import "strings"

const newline = "\n"

// flagged: strings.Split() used directly in range loop with string literal separator
func rangeOverSplitNewline(content string) {
	for _, part := range strings.Split(content, "\n") { // want `strings\.Split\(\) allocates a slice for iteration`
		println(part)
	}
}

// flagged: strings.Split() used directly in range loop with variable separator
func rangeOverSplitVar(s, sep string) {
	for _, part := range strings.Split(s, sep) { // want `strings\.Split\(\) allocates a slice for iteration`
		println(part)
	}
}

// flagged: strings.Split() used directly in range loop with const separator
func rangeOverSplitConst(s string) {
	for _, part := range strings.Split(s, newline) { // want `strings\.Split\(\) allocates a slice for iteration`
		println(part)
	}
}

// flagged: range with multiple key values from strings.Split()
func rangeOverSplitMultipleVars(s, sep string) {
	for i, part := range strings.Split(s, sep) { // want `strings\.Split\(\) allocates a slice for iteration`
		println(i, part)
	}
}

// not flagged: strings.Split() assigned to a variable, not used directly in range
func splitAssignedToVar(s string) {
	parts := strings.Split(s, ",")
	for _, part := range parts {
		println(part)
	}
}

// not flagged: strings.SplitSeq() used directly in range loop (already optimal)
func rangeOverSplitSeq(s, sep string) {
	for part := range strings.SplitSeq(s, sep) {
		println(part)
	}
}

// not flagged: strings.Split() used in non-range context
func splitNotRanged(s string) []string {
	return strings.Split(s, ",")
}

// not flagged: strings.Split() assigned to variable in return
func splitInReturn(s string) []string {
	return strings.Split(s, ",")
}

// not flagged: a nolint directive suppresses the diagnostic
func suppressedRange(s string) {
	for _, part := range strings.Split(s, ",") { //nolint:stringsplitseq
		println(part)
	}
}

// flagged: nested range loop with strings.Split()
func nestedRangeOverSplit(content string) {
	for _, line := range strings.Split(content, "\n") { // want `strings\.Split\(\) allocates a slice for iteration`
		for _, char := range line {
			println(char)
		}
	}
}

// not flagged: method-like Split call (not package selector)
type customSplitter struct{}

func (cs *customSplitter) Split(s, sep string) []string {
	return strings.Split(s, sep)
}

func rangeOverCustom(cs *customSplitter, s string) {
	for _, part := range cs.Split(s, ",") {
		println(part)
	}
}
