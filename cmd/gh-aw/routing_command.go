package main

import "github.com/github/gh-aw/pkg/cli"

func init() {
	command := cli.NewRoutingCommand()
	command.GroupID = "analysis"
	rootCmd.AddCommand(command)
}
