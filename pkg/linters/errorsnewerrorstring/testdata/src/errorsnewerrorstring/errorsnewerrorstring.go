package errorsnewerrorstring

import "errors"

type testError struct{}

func (testError) Error() string { return "test error" }

type ptrError struct{}

func (*ptrError) Error() string { return "pointer error" }

func bad(err error) error {
	return errors.New(err.Error()) // want `errors\.New\(err\.Error\(\)\) discards the original error`
}

func badPointerReceiver() error {
	var err ptrError
	return errors.New(err.Error()) // want `errors\.New\(err\.Error\(\)\) discards the original error`
}

func badConcrete(err testError) error {
	return (errors.New)((err.Error)()) // want `errors\.New\(err\.Error\(\)\) discards the original error`
}

func suppressed(err error) error {
	return errors.New(err.Error()) //nolint:errorsnewerrorstring
}

func good(err error) error {
	return err
}

func goodErrorsJoin(err error) error {
	return errors.Join(errors.New("context"), err)
}

func goodString(message string) error {
	return errors.New(message)
}

type differentErrorMethod struct{}

func (differentErrorMethod) Error(int) string { return "not an error" }

func goodDifferentMethod(err differentErrorMethod) error {
	return errors.New(err.Error(1))
}
