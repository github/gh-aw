package errorsnewerrorstring

import (
	"errors"
	"fmt"
)

type myErr struct{}

func (myErr) Error() string { return "my" }

type notErr struct{}

func (notErr) Error(prefix string) string { return prefix }

func bad(err error) error {
	return errors.New(err.Error()) // want `errors.New\(err.Error\(\)\) discards the wrapped error chain`
}

func badParen(err error) error {
	return errors.New((err).Error()) // want `errors.New\(err.Error\(\)\) discards the wrapped error chain`
}

func badConcrete(e myErr) error {
	return errors.New(e.Error()) // want `errors.New\(err.Error\(\)\) discards the wrapped error chain`
}

func suppressed(err error) error {
	return errors.New(err.Error()) //nolint:errorsnewerrorstring
}

func goodLiteral() error {
	return errors.New("x")
}

func goodSprintf(err error) error {
	return errors.New(fmt.Sprintf("x %d", 1))
}

func goodWrap(err error) error {
	return fmt.Errorf("ctx: %w", err)
}

func goodOther(err error) string {
	return err.Error()
}

func goodNonError(e notErr) error {
	return errors.New(e.Error("x"))
}
