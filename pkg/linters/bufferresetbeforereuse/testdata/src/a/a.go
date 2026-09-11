// Package a is the test fixture for the bufferresetbeforereuse analyzer.
package a

import (
	"bytes"
	"strings"
)

// okSingleWrite is OK - buffer used once
func okSingleWrite() {
	var buf bytes.Buffer
	buf.WriteString("hello")
	_ = buf.String()
}

// okResetBetweenWrites is OK - Reset called between writes
func okResetBetweenWrites() {
	var buf bytes.Buffer
	buf.WriteString("first")
	_ = buf.String()

	buf.Reset()
	buf.WriteString("second")
	_ = buf.String()
}

// notOkReuseWithoutReset is NOT OK - reused without Reset
func notOkReuseWithoutReset() {
	var buf bytes.Buffer
	buf.WriteString("first") // First write (OK)
	_ = buf.String()
	buf.WriteString("second") // want "buf is reused without calling Reset"
	_ = buf.String()
}

// okDifferentBuffers is OK - different variables
func okDifferentBuffers() {
	var buf1 bytes.Buffer
	var buf2 bytes.Buffer
	buf1.WriteString("first")
	buf2.WriteString("second")
	_ = buf1.String() + buf2.String()
}

// okBuilderWithReset is OK - builder reset between writes
func okBuilderWithReset() {
	var sb strings.Builder
	sb.WriteString("first")
	_ = sb.String()

	sb.Reset()
	sb.WriteString("second")
	_ = sb.String()
}

// notOkBuilderReuseWithoutReset is NOT OK - builder reused without Reset
func notOkBuilderReuseWithoutReset() {
	var sb strings.Builder
	sb.WriteString("first") // First write (OK)
	_ = sb.String()
	sb.WriteString("second") // want "sb is reused without calling Reset"
	_ = sb.String()
}

// okMultipleWritesAfterReset is OK - multiple writes after reset each time
func okMultipleWritesAfterReset() {
	var buf bytes.Buffer
	buf.WriteByte('a')
	buf.WriteRune('b')
	_ = buf.String()

	buf.Reset()
	buf.WriteByte('c')
	buf.WriteRune('d')
	_ = buf.String()
}

// notOkMultipleWritesWithoutReset is NOT OK - multiple writes without reset
func notOkMultipleWritesWithoutReset() {
	var buf bytes.Buffer
	buf.WriteByte('a') // First write (OK)
	_ = buf.String()
	buf.WriteByte('b') // want "buf is reused without calling Reset"
	buf.WriteRune('c') // want "buf is reused without calling Reset"
	_ = buf.String()
}

// okSuppressed is OK - //nolint directive suppresses the diagnostic
func okSuppressed() {
	var buf bytes.Buffer
	buf.WriteString("first")
	_ = buf.String()
	//nolint:bufferresetbeforereuse
	buf.WriteString("second")
	_ = buf.String()
}

// okPointerBuffer is OK - testing with pointer receiver
func okPointerBuffer() {
	buf := &bytes.Buffer{}
	buf.WriteString("first")
	_ = buf.String()

	buf.Reset()
	buf.WriteString("second")
	_ = buf.String()
}

// notOkPointerBufferReuseWithoutReset is NOT OK - pointer buffer reused without Reset
func notOkPointerBufferReuseWithoutReset() {
	buf := &bytes.Buffer{}
	buf.WriteString("first") // First write (OK)
	_ = buf.String()
	buf.WriteString("second") // want "buf is reused without calling Reset"
	_ = buf.String()
}

// okShadowedBuffer is OK - inner buf is a distinct object.
func okShadowedBuffer() {
	var buf bytes.Buffer
	buf.WriteString("outer")
	_ = buf.String()

	{
		var buf bytes.Buffer
		buf.WriteString("inner")
		_ = buf.String()
	}
}

// notOkFuncLiteralReuseWithoutReset is reported once for the function literal.
func notOkFuncLiteralReuseWithoutReset() {
	func() {
		var buf bytes.Buffer
		buf.WriteString("first")
		_ = buf.String()
		buf.WriteString("second") // want "buf is reused without calling Reset"
	}()
}

// okMutuallyExclusiveBranches is OK - the read returns before the later write path.
func okMutuallyExclusiveBranches(cond bool) string {
	var buf bytes.Buffer
	buf.WriteString("init")
	if cond {
		return buf.String()
	}
	buf.WriteString("more")
	return buf.String()
}
