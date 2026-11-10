package testsupport

import "errors"

func Error() error {
	return errors.New("failure")
}

func Result() (int, error) {
	return 0, Error()
}
