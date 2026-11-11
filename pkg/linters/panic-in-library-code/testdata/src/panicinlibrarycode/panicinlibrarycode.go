package panicinlibrarycode

import (
	"errors"
	"fmt"
	"sync"
)

// bad: panic in a pkg/ package.
func panicWithStringLiteral() {
	panic("something went wrong") // want `avoid panic in library code; return an error instead`
}

// bad: panic with a value
func panicWithErrorValue() {
	panic(errors.New("error")) // want `avoid panic in library code; return an error instead`
}

// bad: panic with fmt.Sprintf that does not start with BUG:
func panicWithFormattedMessage(n int) {
	panic(fmt.Sprintf("unexpected value: %d", n)) // want `avoid panic in library code; return an error instead`
}

// ok: function that returns an error instead of panicking.
func returnNilErrorInsteadOfPanicking() error {
	return nil
}

// ok: user-defined panic function (not the builtin)
type myType struct{}

func (m myType) panic(msg string) {
	// This is a custom method, not builtin panic
}

func callCustomPanicMethod() {
	m := myType{}
	m.panic("this is ok") // Should not be flagged
}

// ok: panic in top-level init() — init() cannot return an error.
func init() {
	panic("startup registration failure") // should not be flagged
}

func init() {
	handler := func() {
		panic("handler panic outside init flow") // want `avoid panic in library code; return an error instead`
	}
	_ = handler
}

// ok: panic inside a sync.Once.Do callback.
var once sync.Once

func panicInsideSyncOnceDoCallback() {
	once.Do(func() {
		panic("lazy init failure") // should not be flagged
	})
}

var onceValue = sync.OnceValue(func() int {
	panic("lazy init failure in sync.OnceValue") // should not be flagged
	return 0
})

var onceFunc = sync.OnceFunc(func() {
	panic("lazy init failure in sync.OnceFunc") // should not be flagged
})

// ok: panic whose message starts with "BUG:" — invariant violation.
func panicWithBUGPrefix() {
	panic(fmt.Sprintf("BUG: unreachable: %v", errors.New("boom"))) // should not be flagged
}

// ok: panic with plain "BUG:" string literal.
func panicOnNegativeInvariant(x int) {
	if x < 0 {
		panic("BUG: x must be non-negative") // should not be flagged
	}
}

// panicOnEmptyModePrecondition panics if the caller passes an invalid mode.
func panicOnEmptyModePrecondition(mode string) {
	if mode == "" {
		panic("invalid mode") // should not be flagged — documented panic contract
	}
}

// registerCallbackWithDocumentedPanic panics if called with an empty input.
func registerCallbackWithDocumentedPanic(input string) {
	callback := func() {
		panic("callback panic should be reported") // want `avoid panic in library code; return an error instead`
	}
	_ = callback
	_ = input
}

// ok: method named init is NOT a top-level init; its panic should be flagged.
type myInitType struct{}

func (m myInitType) panicInMethodNamedInit() {
	panic("method init panic") // want `avoid panic in library code; return an error instead`
}

func panicIgnoredByPreviousLineNolint() {
	//nolint:panicinlibrarycode
	panic("intentional panic for compatibility")
}

func panicIgnoredBySameLineNolint() {
	panic("intentional panic for compatibility") //nolint:panicinlibrarycode
}

// bad: a parenthesized builtin callee must still be flagged.
func parenthesizedPanicCallee() {
	(panic)("parenthesized callee") // want `avoid panic in library code; return an error instead`
}

// ok: parenthesized fmt.Sprintf and BUG: operands are still recognized.
func parenthesizedBUGSprintf(n int) {
	panic((fmt.Sprintf)("BUG: unreachable: %d", n)) // should not be flagged
}

// ok: parenthesized concatenation whose first operand is a BUG: prefix.
func parenthesizedBUGConcat(n int) {
	panic(("BUG: " + fmt.Sprint(n))) // should not be flagged
}

// ok: parenthesized sync.Once.Do callee.
func parenthesizedOnceDo() {
	(once.Do)(func() {
		panic("lazy init failure") // should not be flagged
	})
}
