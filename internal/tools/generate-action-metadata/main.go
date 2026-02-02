package main

import (
	"fmt"
	"os"

	"github.com/github/gh-aw/pkg/cli"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "Usage: generate-action-metadata <command>")
		fmt.Fprintln(os.Stderr, "")
		fmt.Fprintln(os.Stderr, "Commands:")
		fmt.Fprintln(os.Stderr, "  generate  Generate action.yml and README.md for JavaScript modules")
		os.Exit(1)
	}

	command := os.Args[1]
	var err error

	switch command {
	case "generate":
		err = cli.GenerateActionMetadataCommand()
	default:
		fmt.Fprintf(os.Stderr, "Unknown command: %s\n", command)
		os.Exit(1)
	}

	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
