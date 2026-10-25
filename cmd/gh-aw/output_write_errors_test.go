//go:build !integration

package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"testing"

	"github.com/spf13/cobra"
)

type commandOutputFailingWriter struct {
	calls  int
	failAt int
}

func (w *commandOutputFailingWriter) Write(p []byte) (int, error) {
	w.calls++
	if w.calls == w.failAt {
		return 0, io.ErrClosedPipe
	}
	return len(p), nil
}

func TestCommandOutputPropagatesWriteErrors(t *testing.T) {
	tests := []struct {
		name   string
		render func(io.Writer) error
	}{
		{"version", printVersion},
		{"ungrouped usage", func(w io.Writer) error {
			cmd := &cobra.Command{Use: "gh", Aliases: []string{"alias"}, Example: "gh aw test"}
			cmd.Flags().Bool("test", false, "test flag")
			cmd.AddCommand(&cobra.Command{Use: "child", Short: "Child", Run: func(*cobra.Command, []string) {}})
			cmd.SetOut(w)
			return rootUsageFunc(cmd)
		}},
		{"grouped usage", func(w io.Writer) error {
			cmd := &cobra.Command{Use: "gh"}
			cmd.AddGroup(&cobra.Group{ID: "group", Title: "Group:"})
			cmd.AddCommand(&cobra.Command{Use: "grouped", GroupID: "group", Run: func(*cobra.Command, []string) {}})
			cmd.AddCommand(&cobra.Command{Use: "additional", Run: func(*cobra.Command, []string) {}})
			cmd.SetOut(w)
			return rootUsageFunc(cmd)
		}},
		{"fully grouped usage", func(w io.Writer) error {
			cmd := &cobra.Command{Use: "gh"}
			cmd.AddGroup(&cobra.Group{ID: "group", Title: "Group:"})
			cmd.AddCommand(&cobra.Command{Use: "grouped", GroupID: "group", Run: func(*cobra.Command, []string) {}})
			cmd.SetOut(w)
			return rootUsageFunc(cmd)
		}},
		{"leaf usage", func(w io.Writer) error {
			parent := &cobra.Command{Use: "gh"}
			parent.PersistentFlags().Bool("global", false, "global flag")
			cmd := &cobra.Command{Use: "leaf", Run: func(*cobra.Command, []string) {}}
			parent.AddCommand(cmd)
			cmd.SetOut(w)
			return rootUsageFunc(cmd)
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			success := &commandOutputFailingWriter{}
			if err := tt.render(success); err != nil {
				t.Fatalf("successful writer returned error: %v", err)
			}
			if success.calls == 0 {
				t.Fatal("renderer produced no output")
			}
			for failAt := 1; failAt <= success.calls; failAt++ {
				t.Run(fmt.Sprintf("write %d", failAt), func(t *testing.T) {
					w := &commandOutputFailingWriter{failAt: failAt}
					if err := tt.render(w); !errors.Is(err, io.ErrClosedPipe) {
						t.Fatalf("write error = %v, want %v", err, io.ErrClosedPipe)
					}
					if w.calls != failAt {
						t.Fatalf("writes = %d, want %d: continued after failure", w.calls, failAt)
					}
				})
			}
		})
	}
}

func TestVersionCommandsPropagateClosedOutput(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	stdout := os.Stdout
	os.Stdout = w
	defer func() { os.Stdout = stdout }()
	if err := versionCmd.RunE(versionCmd, nil); !errors.Is(err, os.ErrClosed) {
		t.Fatalf("version error = %v, want closed output error", err)
	}
	if err := rootCmd.Flags().Set("version", "true"); err != nil {
		t.Fatal(err)
	}
	if err := rootCmd.RunE(rootCmd, nil); !errors.Is(err, os.ErrClosed) {
		t.Fatalf("--version error = %v, want closed output error", err)
	}
	if flag, err := rootCmd.Flags().GetBool("version"); err != nil || flag {
		t.Fatalf("--version state was not reset after failure: value=%t error=%v", flag, err)
	}
}
