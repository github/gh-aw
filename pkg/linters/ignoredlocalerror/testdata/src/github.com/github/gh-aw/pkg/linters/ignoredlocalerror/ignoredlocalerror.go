package ignoredlocalerror

import (
	"errors"
	"strconv"

	"github.com/github/gh-aw/pkg/testsupport"
)

func localError() error {
	return errors.New("failure")
}

func localResult() (int, error) {
	return 0, localError()
}

func (receiver) methodError() error {
	return localError()
}

type receiver struct{}

type localValidationError struct{}

func (*localValidationError) Error() string {
	return "validation"
}

type localExtendedError interface {
	error
	ValidationCode() int
}

type localValidationErrorWithCode struct{}

func (localValidationErrorWithCode) Error() string {
	return "validation"
}

func (localValidationErrorWithCode) ValidationCode() int {
	return 1
}

func localConcreteError() *localValidationError {
	return &localValidationError{}
}

func localExtended() localExtendedError {
	return localValidationErrorWithCode{}
}

func localGenericError[T any]() error {
	return localError()
}

func localGenericResult[T, U any](value T) (T, error) {
	return value, localError()
}

var _ = localError()         // want `error return from a github.com/github/gh-aw function is discarded`
var _, _ = localResult()     // want `error return from a github.com/github/gh-aw function is discarded`
var value, _ = localResult() // want `error return from a github.com/github/gh-aw function is discarded`

func badCalls() {
	localError()                      // want `error return from a github.com/github/gh-aw function is discarded`
	_, _ = localResult()              // want `error return from a github.com/github/gh-aw function is discarded`
	_ = localError()                  // want `error return from a github.com/github/gh-aw function is discarded`
	value, _ = 1, testsupport.Error() // want `error return from a github.com/github/gh-aw function is discarded`
	(testsupport.Error())             // want `error return from a github.com/github/gh-aw function is discarded`
	_, err := localResult()
	_ = err
	receiver{}.methodError()                         // want `error return from a github.com/github/gh-aw function is discarded`
	testsupport.Error()                              // want `error return from a github.com/github/gh-aw function is discarded`
	_, _ = testsupport.Result()                      // want `error return from a github.com/github/gh-aw function is discarded`
	testsupport.ConcreteError()                      // want `error return from a github.com/github/gh-aw function is discarded`
	_ = testsupport.ConcreteError()                  // want `error return from a github.com/github/gh-aw function is discarded`
	testsupport.Extended()                           // want `error return from a github.com/github/gh-aw function is discarded`
	_ = testsupport.Extended()                       // want `error return from a github.com/github/gh-aw function is discarded`
	localConcreteError()                             // want `error return from a github.com/github/gh-aw function is discarded`
	_ = localConcreteError()                         // want `error return from a github.com/github/gh-aw function is discarded`
	localExtended()                                  // want `error return from a github.com/github/gh-aw function is discarded`
	_ = localExtended()                              // want `error return from a github.com/github/gh-aw function is discarded`
	localGenericError[int]()                         // want `error return from a github.com/github/gh-aw function is discarded`
	_ = localGenericError[int]()                     // want `error return from a github.com/github/gh-aw function is discarded`
	_, _ = localGenericResult[int, string](0)        // want `error return from a github.com/github/gh-aw function is discarded`
	testsupport.GenericError[int]()                  // want `error return from a github.com/github/gh-aw function is discarded`
	_, _ = testsupport.GenericResult[int, string](0) // want `error return from a github.com/github/gh-aw function is discarded`
	defer localError()                               // want `error return from a github.com/github/gh-aw function is discarded`
	go localError()                                  // want `error return from a github.com/github/gh-aw function is discarded`
	strconv.Atoi("1")
}

func suppressedCalls() {
	//nolint:ignoredlocalerror
	localError()
	localError() //nolint:ignoredlocalerror
}
