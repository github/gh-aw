package cli

import (
	"fmt"
	"io"
	"os"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/spf13/cobra"
)

// NewJSONSchemaCommand creates the json-schema command.
func NewJSONSchemaCommand() *cobra.Command {
	return newJSONSchemaCommand(os.Stdout)
}

func newJSONSchemaCommand(output io.Writer) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "json-schema <schema>",
		Short: "Generate JSON Schemas for structured command output",
		Long:  "Generate the JSON Schema for audit or logs JSON output.",
		Example: `  ` + string(constants.CLIExtensionPrefix) + ` json-schema audit
  ` + string(constants.CLIExtensionPrefix) + ` json-schema logs`,
		Args:      cobra.ExactArgs(1),
		ValidArgs: []string{"audit", "logs"},
		RunE: func(cmd *cobra.Command, args []string) error {
			data, err := GenerateNamedOutputSchema(args[0])
			if err != nil {
				return err
			}
			if _, err := output.Write(data); err != nil {
				return fmt.Errorf("failed to write %s schema: %w", args[0], err)
			}
			return nil
		},
		SilenceUsage: true,
	}
	return cmd
}
