package main

import "github.com/github/gh-aw/pkg/cli"

func init() {
	command := cli.NewParseCommand()
	command.GroupID = "utilities"
	rootCmd.AddCommand(command)
}
