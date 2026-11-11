package testsupport

import "errors"

func Error() error {
	return errors.New("failure")
}

func Result() (int, error) {
	return 0, Error()
}

type ValidationError struct{}

func (*ValidationError) Error() string {
	return "validation"
}

func ConcreteError() *ValidationError {
	return &ValidationError{}
}

type ExtendedError interface {
	error
	ValidationCode() int
}

type validationErrorWithCode struct{}

func (validationErrorWithCode) Error() string {
	return "validation"
}

func (validationErrorWithCode) ValidationCode() int {
	return 1
}

func Extended() ExtendedError {
	return validationErrorWithCode{}
}

func GenericError[T any]() error {
	return Error()
}

func GenericResult[T, U any](value T) (T, error) {
	return value, Error()
}
