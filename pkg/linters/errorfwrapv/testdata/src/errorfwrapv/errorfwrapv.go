package errorfwrapv

import (
	"errors"
	"fmt"
)

var ErrBase = errors.New("base error")

type myError struct {
	msg string
}

func (e *myError) Error() string { return e.msg }

// BadVWrap uses %v to format an error — should be %w.
func BadVWrap(err error) error {
	return fmt.Errorf("operation failed: %v", err) // want `fmt\.Errorf formats an error argument with %v`
}

// BadVWrapExtra has %v for an error arg alongside a non-error arg.
func BadVWrapExtra(err error, count int) error {
	return fmt.Errorf("error %v occurred %d times", err, count) // want `fmt\.Errorf formats an error argument with %v`
}

// BadExplicitIndexV uses an explicit positional index for the error argument.
func BadExplicitIndexV(name string, err error) error {
	return fmt.Errorf("%[2]v while handling %[1]q", name, err) // want `fmt\.Errorf formats an error argument with %v`
}

// BadDynamicWidthWrap uses a dynamic width before the error argument.
func BadDynamicWidthWrap(err error) error {
	return fmt.Errorf("%*v", 10, err) // want `fmt\.Errorf formats an error argument with %v`
}

// BadConcretePointerWrap passes a concrete pointer type that implements error.
func BadConcretePointerWrap(err *myError) error {
	return fmt.Errorf("wrapped: %v", err) // want `fmt\.Errorf formats an error argument with %v`
}

// GoodWWrap uses %w to properly wrap the error.
func GoodWWrap(err error) error {
	return fmt.Errorf("operation failed: %w", err)
}

// GoodIndexedWidthWrap uses indexed width and indexed value with %w.
func GoodIndexedWidthWrap(err error) error {
	return fmt.Errorf("%[2]*[1]w", err, 10)
}

// GoodIndexedDynamicWidthWrap uses an explicit index for the dynamic width and
// wraps the following argument with %w.
func GoodIndexedDynamicWidthWrap(width int, err error) error {
	return fmt.Errorf("%[1]*w", width, err)
}

// BadIndexedWidthNoW uses indexed width and value but does not wrap the error.
func BadIndexedWidthNoW(err error) error {
	return fmt.Errorf("%[2]*[1]s", err, 10) // want `fmt\.Errorf passes an error argument without %w`
}

// BadMissingWrapNoW passes an error argument but has no %w verb.
func BadMissingWrapNoW(err error) error {
	return fmt.Errorf("operation failed: %s", err) // want `fmt\.Errorf passes an error argument without %w`
}

// BadMixedWrapAndS wraps one error but not the second.
func BadMixedWrapAndS(cause, detail error) error {
	return fmt.Errorf("failed: %w (%s)", cause, detail) // want `fmt\.Errorf passes an error argument without %w`
}

// BadMissingWrapNoVerbs passes an error argument with no formatting verbs.
func BadMissingWrapNoVerbs(err error) error {
	return fmt.Errorf("operation failed", err) // want `fmt\.Errorf passes an error argument without %w`
}

// GoodTypeVerb uses %T to print an error type without wrapping.
func GoodTypeVerb(err error) error {
	return fmt.Errorf("error type: %T", err)
}

// GoodPointerVerb uses %p with a concrete error pointer.
func GoodPointerVerb(err *myError) error {
	return fmt.Errorf("error pointer: %p", err)
}

// GoodNonErrorVerb uses %v for a non-error argument only.
func GoodNonErrorVerb(name string) error {
	return fmt.Errorf("operation %v failed", name)
}

// GoodMixedVerbs uses %w for the error and %v for a non-error.
func GoodMixedVerbs(name string, err error) error {
	return fmt.Errorf("operation %v failed: %w", name, err)
}

// BadConcatVWrap builds its format string via string concatenation of literals.
// The trailing error argument is formatted with %v instead of %w.
func BadConcatVWrap(err error) error {
	return fmt.Errorf("context: %d\n"+ // want `fmt\.Errorf formats an error argument with %v`
		"Original error: %v", 42, err)
}

// GoodConcatWWrap builds its format string via string concatenation of literals
// and correctly wraps the trailing error with %w.
func GoodConcatWWrap(err error) error {
	return fmt.Errorf("context: %d\n"+
		"Original error: %w", 42, err)
}

// OpaqueConcatVWrap builds its format string with a caller-supplied opaque prefix;
// because the format string contains non-literal components, it cannot be safely analyzed.
func OpaqueConcatVWrap(prefix string, err error) error {
	return fmt.Errorf(prefix+"\n\n"+
		"context: %d\n"+
		"Original error: %v", 42, err)
}

// SuppressedByNolint is intentionally suppressed.
func SuppressedByNolint(err error) error {
	return fmt.Errorf("operation failed: %v", err) //nolint:errorfwrapv
}

// SuppressedMissingWrap is intentionally suppressed for missing %w.
func SuppressedMissingWrap(err error) error {
	return fmt.Errorf("operation failed: %s", err) //nolint:errorfwrapv
}
