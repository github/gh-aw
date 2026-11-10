package ignoredlocalerror

import (
	"os"
	"strconv"

	"github.com/github/gh-aw/pkg/helper"
)

func localErr() error { return nil }

func localPair() (string, error) { return "", nil }

func localNone() {}

func blankSamePackage() {
	_ = localErr() // want `error returned by local function localErr is discarded`
}

func blankImported() {
	_ = helper.Do() // want `error returned by local function helper\.Do is discarded`
}

func bare() {
	localErr()   // want `error returned by local function localErr is discarded`
	helper.Do()  // want `error returned by local function helper\.Do is discarded`
	(localErr()) // want `error returned by local function localErr is discarded`
}

func onlyErrorBlanked() {
	v, _ := localPair() // want `error returned by local function localPair is discarded`
	_ = v
	n, _ := helper.Pair() // want `error returned by local function helper\.Pair is discarded`
	_ = n
}

func handled() error {
	if err := localErr(); err != nil {
		return err
	}
	s, err := localPair()
	_ = s
	if err != nil {
		return err
	}
	_, err = helper.Pair()
	return err
}

func valueBlankedOnly() error {
	_, err := localPair()
	return err
}

func noErrorResult() {
	localNone()
	helper.Nothing()
}

func nonLocal() {
	os.Remove("x")
	_ = os.Remove("x")
	_, _ = strconv.Atoi("1")
}

func suppressed() {
	_ = localErr() //nolint:ignoredlocalerror
}
