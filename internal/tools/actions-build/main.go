package main

import (
	"fmt"
	"os"

	"github.com/github/gh-aw/pkg/cli"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "Usage: actions-build <command>")
		fmt.Fprintln(os.Stderr, "")
		fmt.Fprintln(os.Stderr, "Commands:")
		fmt.Fprintln(os.Stderr, "  build     Build all custom GitHub Actions")
		fmt.Fprintln(os.Stderr, "  validate  Validate all action.yml files")
		fmt.Fprintln(os.Stderr, "  clean     Remove generated index.js files")
		os.Exit(1)
	}

	command := os.Args[1]
	var err error

	switch command {
	case "build":
		err = cli.ActionsBuildCommand()
	case "validate":
		err = cli.ActionsValidateCommand()
	case "clean":
		err = cli.ActionsCleanCommand()
	default:
		fmt.Fprintf(os.Stderr, "Unknown command: %s\n", command)
		os.Exit(1)
	}

	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
