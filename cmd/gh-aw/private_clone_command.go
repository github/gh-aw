package main

import "github.com/github/gh-aw/pkg/cli"

func init() {
	command := cli.NewPrivateCloneCommand()
	command.GroupID = "setup"
	rootCmd.AddCommand(command)
	fixSubCommandHelpFlags()
}
