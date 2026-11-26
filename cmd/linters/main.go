// Command linters runs the gh-aw custom analysis linters.
//
// Usage:
//
//	linters [flags] [packages]
//
// Flags common to all linters are listed by running:
//
//	linters -help
//
// Each linter may also expose its own flags, e.g.:
//
//	linters -largefunc.max-lines=80 ./...
//
// Set GH_AW_LINT_TIER=fast or GH_AW_LINT_TIER=slow to run only that tier of
// analyzers (see linters.SelectTier); unset or "all" runs every analyzer.
package main

import (
	"fmt"
	"os"

	"golang.org/x/tools/go/analysis/multichecker"

	"github.com/github/gh-aw/pkg/linters"
)

func main() {
	analyzers, err := linters.SelectTier(os.Getenv("GH_AW_LINT_TIER"), os.Args[1:])
	if err != nil {
		fmt.Fprintln(os.Stderr, "linters:", err)
		os.Exit(1)
	}
	multichecker.Main(analyzers...)
}
