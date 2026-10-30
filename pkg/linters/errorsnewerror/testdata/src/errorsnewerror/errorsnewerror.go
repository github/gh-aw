package errorsnewerror

import (
	"errors"
	"fmt"
	"strings"
)

type myErr struct{}

func (myErr) Error() string { return "my" }

type notErr struct{}

func (notErr) Error() string { return "not" }

func bad(err error) error {
	return errors.New(err.Error()) // want `errors.New\(err.Error\(\)\) discards the original error chain`
}

func badParen(err error) error {
	return errors.New((err.Error())) // want `errors.New\(err.Error\(\)\) discards the original error chain`
}

func badConcrete() error {
	var e myErr
	return errors.New(e.Error()) // want `errors.New\(err.Error\(\)\) discards the original error chain`
}

func goodWrap(err error) error {
	return fmt.Errorf("context: %w", err)
}

func goodString(s string) error {
	return errors.New(s)
}

func goodTransformed(err error) error {
	return errors.New(strings.ToUpper(err.Error()))
}

func goodConcat(err error) error {
	return errors.New("failed: " + err.Error())
}

func suppressed(err error) error {
	return errors.New(err.Error()) //nolint:errorsnewerror
}
