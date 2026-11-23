package main

import (
	"bytes"
	"reflect"
	"testing"

	"github.com/spf13/cobra"
)

func TestCommandTreePaths(t *testing.T) {
	root := &cobra.Command{Use: "gh aw"}
	visible := &cobra.Command{Use: "visible", Run: func(*cobra.Command, []string) {}}
	visible.AddCommand(&cobra.Command{Use: "nested", Run: func(*cobra.Command, []string) {}})
	root.AddCommand(visible, &cobra.Command{Use: "hidden", Hidden: true})
	root.AddCommand(&cobra.Command{Use: "topic", Short: "Help topic"})

	got := commandTreePaths(root)
	want := []string{"visible", "visible nested"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("commandTreePaths() = %v, want %v", got, want)
	}
}

func TestCommandTreeCommandWritesPathsToStdout(t *testing.T) {
	root := &cobra.Command{Use: "gh aw"}
	root.AddCommand(&cobra.Command{Use: "visible", Run: func(*cobra.Command, []string) {}})
	treeCommand := commandTreeCommand()
	var output bytes.Buffer
	treeCommand.SetOut(&output)
	root.AddCommand(treeCommand)
	root.SetArgs([]string{"__command-tree"})

	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	if got, want := output.String(), "completion\ncompletion bash\ncompletion fish\ncompletion powershell\ncompletion zsh\nhelp\nvisible\n"; got != want {
		t.Fatalf("command tree output = %q, want %q", got, want)
	}
}
