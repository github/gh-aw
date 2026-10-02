package fprintferrorunchecked

import (
	"bytes"
	"fmt"
)

// BadFprintfMultiReturnAllBlanks discards both return values of fmt.Fprintf with blanks.
func BadFprintfMultiReturnAllBlanks() {
	w := &bytes.Buffer{}
	_, _ = fmt.Fprintf(w, "text") // want `error return from fmt.Fprintf\(\) is not checked; write failures may be silently ignored`
}

// BadFprintMultiReturnAllBlanks discards both return values of fmt.Fprint with blanks.
func BadFprintMultiReturnAllBlanks() {
	w := &bytes.Buffer{}
	_, _ = fmt.Fprint(w, "text") // want `error return from fmt.Fprint\(\) is not checked; write failures may be silently ignored`
}

// BadFprintlnMultiReturnAllBlanks discards both return values of fmt.Fprintln with blanks.
func BadFprintlnMultiReturnAllBlanks() {
	w := &bytes.Buffer{}
	_, _ = fmt.Fprintln(w, "text") // want `error return from fmt.Fprintln\(\) is not checked; write failures may be silently ignored`
}

// BadComplexFprintf flags complex format string with both blanks.
func BadComplexFprintf() {
	w := &bytes.Buffer{}
	_, _ = fmt.Fprintf(w, "value: %d, name: %s, flag: %v", 42, "test", true) // want `error return from fmt.Fprintf\(\) is not checked; write failures may be silently ignored`
}

// GoodErrorCheckedViaShortDeclAndCheck checks the error with if statement.
func GoodErrorCheckedViaShortDeclAndCheck() {
	w := &bytes.Buffer{}
	n, err := fmt.Fprintf(w, "text")
	if err != nil {
		_ = n
		return
	}
}

// GoodErrorCheckedInlineComparison checks error inline with comparison.
func GoodErrorCheckedInlineComparison() {
	w := &bytes.Buffer{}
	n, err := fmt.Fprintf(w, "text")
	if err != nil {
		_ = n
	}
}

// GoodErrorAssigned assigns both return values and checks error.
func GoodErrorAssigned() {
	w := &bytes.Buffer{}
	n, err := fmt.Fprintf(w, "text")
	if err != nil {
		_ = n
		return
	}
	_ = n
}

// GoodReturnValueUsed assigns and uses both return values.
func GoodReturnValueUsed() {
	w := &bytes.Buffer{}
	n, _ := fmt.Fprintf(w, "text")
	_ = n // n is used
}

// GoodOnlyErrorUsed uses only the error return in check.
func GoodOnlyErrorUsed() {
	w := &bytes.Buffer{}
	_, err := fmt.Fprintf(w, "text")
	if err != nil {
		return
	}
}

// GoodWithNoErrorCheck assigns both to non-blanks and uses them.
func GoodWithNoErrorCheck() {
	w := &bytes.Buffer{}
	n, err := fmt.Fprintf(w, "text")
	_ = n
	_ = err
}

// GoodSuppressedWithNolint uses nolint comment to suppress the check.
func GoodSuppressedWithNolint() {
	w := &bytes.Buffer{}
	_, _ = fmt.Fprintf(w, "text") //nolint:fprintferrorunchecked
}

// GoodFprintfCheckedWithFunctionCall checks error via return statement.
func GoodFprintfCheckedWithFunctionCall() (int, error) {
	w := &bytes.Buffer{}
	return fmt.Fprintf(w, "text") // error is returned and checked by caller
}

// GoodBothAssignedAndUsedLater assigns both values and uses them.
func GoodBothAssignedAndUsedLater() {
	w := &bytes.Buffer{}
	n, err := fmt.Fprintf(w, "value")
	if n > 0 && err == nil {
		return
	}
}

// BadBothBlankedWithMultipleCalls discards all returns from multiple fprintf calls.
func BadBothBlankedWithMultipleCalls() {
	w := &bytes.Buffer{}
	_, _ = fmt.Fprintf(w, "line1\n") // want `error return from fmt.Fprintf\(\) is not checked; write failures may be silently ignored`
	_, _ = fmt.Fprint(w, "line2")    // want `error return from fmt.Fprint\(\) is not checked; write failures may be silently ignored`
	_, _ = fmt.Fprintln(w, "line3")  // want `error return from fmt.Fprintln\(\) is not checked; write failures may be silently ignored`
}
