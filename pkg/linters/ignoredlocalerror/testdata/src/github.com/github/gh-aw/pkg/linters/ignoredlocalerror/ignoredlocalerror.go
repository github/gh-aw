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

func badCalls() {
	localError()         // want `error return from a github.com/github/gh-aw function is discarded`
	_, _ = localResult() // want `error return from a github.com/github/gh-aw function is discarded`
	_ = localError()     // want `error return from a github.com/github/gh-aw function is discarded`
	_, err := localResult()
	_ = err
	receiver{}.methodError()    // want `error return from a github.com/github/gh-aw function is discarded`
	testsupport.Error()         // want `error return from a github.com/github/gh-aw function is discarded`
	_, _ = testsupport.Result() // want `error return from a github.com/github/gh-aw function is discarded`
	strconv.Atoi("1")
}

func suppressedCalls() {
	//nolint:ignoredlocalerror
	localError()
	localError() //nolint:ignoredlocalerror
}
