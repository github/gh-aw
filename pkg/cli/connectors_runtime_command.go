package cli

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"slices"

	"github.com/github/gh-aw/pkg/connectors"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/spf13/cobra"
)

func newConnectorServeCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "serve",
		Short: "Serve a compiler-frozen read-only connector over stdio MCP",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			plan, runtime, err := connectorHost("agent", os.Getenv)
			if err != nil {
				return err
			}
			server, err := plan.MCPServer(runtime)
			if err != nil {
				return err
			}
			return server.Run(cmd.Context(), &mcp.StdioTransport{})
		},
	}
}

func newConnectorInvokeCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "invoke <context|safe-output> <operation>",
		Short: "Execute a compiler-frozen connector operation with JSON arguments on stdin",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) < 2 {
				return errors.New("connector invoke requires a phase and operation")
			}
			allowWrites, err := cmd.Flags().GetBool("allow-writes")
			if err != nil {
				return err
			}
			stage, err := cmd.Flags().GetBool("stage")
			if err != nil {
				return err
			}
			return invokeConnector(cmd.Context(), cmd.InOrStdin(), cmd.OutOrStdout(), args[0], args[1], allowWrites, stage, os.Getenv) //nolint:uncheckedsliceindex // Cobra and the preceding guard require two arguments.
		},
	}
	cmd.Flags().Bool("allow-writes", false, "Authorize a safe-output effect in the trusted safe-output host")
	cmd.Flags().Bool("stage", false, "Validate and return a safe-output intent without resolving credentials or making requests")
	return cmd
}

func connectorHost(phase string, getenv func(string) string) (*connectors.Plan, connectors.Runtime, error) {
	plan, err := connectors.DecodePlan(getenv("GH_AW_CONNECTOR_PLAN"))
	if err != nil {
		return nil, connectors.Runtime{}, err
	}
	// Refuse a mixed-phase snapshot even when only one credential is requested.
	restricted, err := plan.ForPhase(phase)
	if err != nil {
		return nil, connectors.Runtime{}, err
	}
	if restricted.Digest != plan.Digest {
		return nil, connectors.Runtime{}, errors.New("connector host requires a compiler-restricted single-phase plan")
	}
	secrets := plan.Secrets()
	runtime := connectors.Runtime{
		Phase: phase, AllowedOrigins: plan.Origins(),
		ResolveSecret: func(_ context.Context, requestedPhase, name string) (string, error) {
			if requestedPhase != phase || !slices.Contains(secrets, name) {
				return "", errors.New("connector secret is outside the compiled phase")
			}
			value := getenv("GH_AW_CONNECTOR_SECRET_" + name)
			if value == "" {
				return "", fmt.Errorf("connector secret %q is unavailable in this phase", name)
			}
			return value, nil
		},
	}
	return plan, runtime, nil
}

func invokeConnector(ctx context.Context, input io.Reader, output io.Writer, phase, name string, allowWrites, stage bool, getenv func(string) string) error {
	if phase != "context" && phase != "safe-output" {
		return errors.New("connector invoke phase must be context or safe-output")
	}
	if stage && (phase != "safe-output" || allowWrites) {
		return errors.New("connector --stage requires safe-output without --allow-writes")
	}
	plan, runtime, err := connectorHost(phase, getenv)
	if err != nil {
		return err
	}
	data, err := io.ReadAll(io.LimitReader(input, connectors.MaxDocumentBytes+1))
	if err != nil {
		return fmt.Errorf("read connector arguments: %w", err)
	}
	var args map[string]any
	if err := connectors.Decode(data, &args); err != nil {
		return err
	}
	if args == nil {
		return errors.New("connector arguments must be a JSON object")
	}
	var result *connectors.Result
	if phase == "safe-output" {
		request, err := plan.PrepareSafeOutputIntent(name, args)
		if err != nil {
			return err
		}
		if stage {
			if err := json.NewEncoder(output).Encode(map[string]any{"staged": true, "request": request}); err != nil {
				return fmt.Errorf("write staged connector request: %w", err)
			}
			return nil
		}
		runtime.AllowWrites = allowWrites
		if !allowWrites {
			return errors.New("connector safe-output execution requires explicit host write authorization")
		}
		if run := getenv("GH_AW_CONNECTOR_BINDING_RUN"); run != "" {
			host, hostErr := connectorBindingHost(run, getenv)
			if hostErr != nil {
				return hostErr
			}
			result, err = plan.ExecuteCreatedSafeOutput(ctx, runtime, host, name, args)
		} else {
			result, err = plan.ExecuteSafeOutput(ctx, runtime, *request)
		}
		if err != nil {
			return err
		}
	} else {
		result, err = plan.Execute(ctx, runtime, phase, name, args)
		if err != nil {
			return err
		}
	}
	if err := json.NewEncoder(output).Encode(result); err != nil {
		return fmt.Errorf("write connector result: %w", err)
	}
	return nil
}

func connectorBindingHost(run string, getenv func(string) string) (connectors.CreatedResourceHost, error) {
	key, err := base64.StdEncoding.DecodeString(getenv("GH_AW_CONNECTOR_BINDING_KEY"))
	if err != nil {
		return connectors.CreatedResourceHost{}, errors.New("invalid trusted connector binding key")
	}
	var receipts []string
	if err := connectors.Decode([]byte(getenv("GH_AW_CONNECTOR_BINDING_RECEIPTS")), &receipts); err != nil {
		return connectors.CreatedResourceHost{}, errors.New("invalid trusted connector binding receipts")
	}
	return connectors.CreatedResourceHost{Run: run, Key: key, Receipts: receipts}, nil
}
