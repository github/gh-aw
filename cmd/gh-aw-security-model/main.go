package main

import (
	"fmt"
	"os"

	"github.com/github/gh-aw/specs/workflow-security/conformance"
)

func main() {
	if err := conformance.Run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
