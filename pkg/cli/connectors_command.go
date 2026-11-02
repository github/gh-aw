package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"os"

	"github.com/github/gh-aw/pkg/connectors"
	"github.com/github/gh-aw/pkg/constants"
	"github.com/spf13/cobra"
)

type connectorValidationResult struct {
	File  string `json:"file"`
	ID    string `json:"id,omitempty"`
	Valid bool   `json:"valid"`
	Error string `json:"error,omitempty"`
}

// NewConnectorsCommand creates inspection and trusted-host connector commands.
func NewConnectorsCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:           "connectors",
		Short:         "Inspect and validate experimental connector definitions",
		SilenceUsage:  true,
		SilenceErrors: true,
		Long: "Validate and inspect external connector definitions offline. " +
			"The serve and invoke subcommands execute compiler-frozen capabilities in trusted hosts.",
		Example: string(constants.CLIExtensionPrefix) + " connectors validate .github/aw/connectors/linear.yaml",
	}
	cmd.SetOut(os.Stdout)
	validate := &cobra.Command{
		Use:   "validate <file>...",
		Short: "Validate experimental YAML or JSON connector definitions",
		Long: "Validate one or more local ConnectorSpec files against the experimental versioned contract. " +
			"All supplied definitions are checked. Validation does not approve provider access, " +
			"verify external credentials, or activate webhook ingress.",
		Example: `  ` + string(constants.CLIExtensionPrefix) + ` connectors validate .github/aw/connectors/linear.yaml
  ` + string(constants.CLIExtensionPrefix) + ` connectors validate .github/aw/connectors/*.yaml --json`,
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			jsonOutput, err := cmd.Flags().GetBool("json")
			if err != nil {
				return err
			}
			return validateConnectorDefinitions(cmd.OutOrStdout(), args, jsonOutput)
		},
	}
	addJSONFlag(validate)
	cmd.AddCommand(validate, newConnectorPlanCommand(), newConnectorSchemaCommand(), newConnectorServeCommand(), newConnectorInvokeCommand())
	return cmd
}

func validateConnectorDefinitions(output io.Writer, paths []string, jsonOutput bool) error {
	results := make([]connectorValidationResult, 0, len(paths))
	failures := 0
	for _, path := range paths {
		result := connectorValidationResult{File: path}
		spec, err := connectors.LoadSpec(path)
		if err != nil {
			result.Error = err.Error()
			failures++
		} else {
			result.ID = spec.ID
			result.Valid = true
		}
		results = append(results, result)
	}
	if jsonOutput {
		if err := json.NewEncoder(output).Encode(results); err != nil {
			return fmt.Errorf("write connector validation results: %w", err)
		}
	} else {
		for _, result := range results {
			var err error
			if result.Valid {
				_, err = fmt.Fprintf(output, "%s: valid ConnectorSpec %s\n", result.File, result.ID)
			} else {
				_, err = fmt.Fprintf(output, "%s: invalid: %s\n", result.File, result.Error)
			}
			if err != nil {
				return fmt.Errorf("write connector validation results: %w", err)
			}
		}
		if _, err := fmt.Fprintln(output, "Experimental configuration validation only; no provider access or webhook activation."); err != nil {
			return fmt.Errorf("write connector validation notice: %w", err)
		}
	}
	if failures > 0 {
		return fmt.Errorf("connector validation failed for %d of %d definition(s)", failures, len(paths))
	}
	return nil
}
