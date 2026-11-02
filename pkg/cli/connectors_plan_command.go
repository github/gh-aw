package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/github/gh-aw/pkg/connectors"
	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/logger"
	"github.com/spf13/cobra"
)

var connectorPlanLog = logger.New("cli:connectors_plan")

func newConnectorPlanCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "plan <binding>",
		Short: "Validate a connector binding and print its frozen content digest",
		Long: "Validate a local ConnectorBinding with its repository-confined definition and connection. " +
			"Print the frozen plan digest as JSON without resolving secrets or activating capabilities. " +
			"Definition and connection paths are relative to --root, not the binding file.",
		Example: `  ` + string(constants.CLIExtensionPrefix) + ` connectors plan .github/aw/bindings/linear.yaml --root .`,
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) < 1 {
				return errors.New("connector binding path is required")
			}
			root, err := cmd.Flags().GetString("root")
			if err != nil {
				return err
			}
			binding, err := readConnectorBinding(args[0]) //nolint:uncheckedsliceindex // Cobra and the preceding guard require one argument.
			if err != nil {
				return err
			}
			plan, err := connectors.Lower(root, binding)
			if err != nil {
				return err
			}
			result := struct {
				Digest      string `json:"digest"`
				Operational bool   `json:"operational"`
			}{Digest: plan.Digest}
			if err := json.NewEncoder(cmd.OutOrStdout()).Encode(result); err != nil {
				return fmt.Errorf("write connector plan: %w", err)
			}
			return nil
		},
	}
	cmd.Flags().String("root", ".", "Repository root for confined definition and connection paths")
	return cmd
}

func readConnectorBinding(path string) (connectors.Binding, error) {
	var binding connectors.Binding
	file, err := os.Open(path)
	if err != nil {
		return binding, fmt.Errorf("open connector binding: %w", err)
	}
	defer func() {
		if err := file.Close(); err != nil {
			connectorPlanLog.Printf("close connector binding: %v", err)
		}
	}()
	info, err := file.Stat()
	if err != nil {
		return binding, fmt.Errorf("inspect connector binding: %w", err)
	}
	if !info.Mode().IsRegular() {
		return binding, errors.New("connector binding must be a regular file")
	}
	data, err := io.ReadAll(io.LimitReader(file, connectors.MaxDocumentBytes+1))
	if err != nil {
		return binding, fmt.Errorf("read connector binding: %w", err)
	}
	if err := connectors.Decode(data, &binding); err != nil {
		return binding, fmt.Errorf("decode connector binding: %w", err)
	}
	return binding, nil
}

func newConnectorSchemaCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "schema <kind>",
		Short: "Print the structural JSON Schema for a connector document kind",
		Long: "Print the structural JSON Schema for ConnectorSpec, ConnectorConnection, or ConnectorBinding. " +
			"Use validate or plan for semantic checks beyond the structural schema.",
		Example:   `  ` + string(constants.CLIExtensionPrefix) + ` connectors schema ConnectorSpec`,
		Args:      cobra.ExactArgs(1),
		ValidArgs: []string{"ConnectorSpec", "ConnectorConnection", "ConnectorBinding"},
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) < 1 {
				return errors.New("connector schema kind is required")
			}
			data, err := connectors.Schema(args[0]) //nolint:uncheckedsliceindex // Cobra and the preceding guard require one argument.
			if err != nil {
				return err
			}
			if _, err := fmt.Fprintln(cmd.OutOrStdout(), string(data)); err != nil {
				return fmt.Errorf("write connector schema: %w", err)
			}
			return nil
		},
	}
}
