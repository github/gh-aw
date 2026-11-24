package main

import "github.com/github/gh-aw/pkg/cli"

// This file sorts before main.go, so the command exists when main.go's init sets every
// command's help flag text.
func init() {
	command := cli.NewRoutingCommand()
	command.GroupID = "analysis"
	rootCmd.AddCommand(command)
}
