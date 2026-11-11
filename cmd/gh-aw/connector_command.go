package main

import "github.com/github/gh-aw/pkg/cli"

func init() {
	command := cli.NewConnectorCommand()
	command.GroupID = "development"
	rootCmd.AddCommand(command)
}
