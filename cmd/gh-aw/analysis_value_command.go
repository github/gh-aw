package main

import "github.com/github/gh-aw/pkg/cli"

func init() {
	command := cli.NewValueCommand()
	command.GroupID = "analysis"
	rootCmd.AddCommand(command)
}
