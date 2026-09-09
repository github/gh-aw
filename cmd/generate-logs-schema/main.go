package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"

	"github.com/github/gh-aw/pkg/cli"
)

func generateLogsSchema(w io.Writer) error {
	schema, err := cli.GenerateSchema[cli.LogsData]()
	if err != nil {
		return fmt.Errorf("generate logs schema: %w", err)
	}

	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(schema); err != nil {
		return fmt.Errorf("write logs schema: %w", err)
	}
	return nil
}

func main() {
	if err := generateLogsSchema(os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
