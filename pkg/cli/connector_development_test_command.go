package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/github/gh-aw/pkg/connectors"
	"github.com/github/gh-aw/pkg/workflow"
)

type connectorDevelopmentReadHost struct {
	getenv func(string) string
	client *http.Client
}

func testDevelopmentConnector(ctx context.Context, output io.Writer, declaration *workflow.DevelopmentConnector, options *connectorDevelopmentOptions, getenv func(string) string, client *http.Client) error {
	args, err := decodeDevelopmentConnectorArguments(options)
	if err != nil {
		return err
	}
	plan, capability, err := selectDevelopmentConnector(declaration, options)
	if err != nil {
		return fmt.Errorf("select connector capability: %w; supply exact --grant resource='[\"approved-id\"]' where required", err)
	}
	encoded, err := plan.Encode()
	if err != nil {
		return err
	}
	plan, err = connectors.DecodePlan(encoded)
	if err != nil {
		return fmt.Errorf("go connector plan conformance: %w", err)
	}
	if options.phase == "trigger" {
		if options.nativeEvent == "" {
			return errors.New("offline trigger testing requires --native-event; example: --phase trigger --capability issue-updated --native-event update --args '{\"action\":\"update\"}'")
		}
		if options.nodeRuntime != "" {
			return errors.New("offline trigger projection uses Go fixture validation; omit --node-runtime (shared predicate conformance is covered by the Go/Node fixture suite)")
		}
		payload, err := json.Marshal(args)
		if err != nil {
			return fmt.Errorf("encode trigger fixture: %w", err)
		}
		projection, err := plan.ProjectEvent(options.capability, options.nativeEvent, payload)
		if err != nil {
			return fmt.Errorf("trigger fixture did not match: %w; check native event, required transition evidence, variants and exact grants", err)
		}
		if err := json.NewEncoder(output).Encode(projection); err != nil {
			return fmt.Errorf("write offline trigger projection: %w", err)
		}
		return nil
	}
	if options.nativeEvent != "" {
		return errors.New("--native-event applies only to offline --phase trigger; omit it for provider reads or staged writes")
	}
	ctx, cancel := context.WithTimeout(ctx, options.timeout)
	defer cancel()
	contentRequired := developmentConnectorContentRequired(declaration, options)
	if contentRequired && options.nodeRuntime == "" {
		return errors.New("annotated safe-output content requires --node-runtime path/to/connector_runtime.cjs from a trusted matching runtime directory with connector_content.cjs and its dependencies; Go transport alone does not apply publication sanitization or generate footers")
	}
	var prepared *connectorDevelopmentPrepared
	if options.nodeRuntime != "" {
		prepared, err = validateDevelopmentConnectorNode(ctx, options.nodeRuntime, encoded, capability, options.capability, args, contentRequired)
		if err != nil {
			return err
		}
	}
	if options.phase == "safe-output" {
		return stageDevelopmentConnector(output, plan, options, args, prepared, contentRequired)
	}
	host := connectorDevelopmentReadHost{getenv: getenv, client: client}
	return testDevelopmentConnectorRead(ctx, output, declaration, plan, capability, options, args, host)
}

func decodeDevelopmentConnectorArguments(options *connectorDevelopmentOptions) (map[string]any, error) {
	if options.capability == "" {
		return nil, errors.New("test requires --capability with an exact declared name; inspect the declaration source and use --help")
	}
	if options.timeout <= 0 || options.timeout > 20*time.Second {
		return nil, errors.New("--timeout must be greater than zero and at most 20s")
	}
	if len(options.arguments) > connectors.MaxDocumentBytes || !json.Valid([]byte(options.arguments)) {
		return nil, errors.New("--args must be a JSON object no larger than 256 KiB")
	}
	var args map[string]any
	if err := connectors.Decode([]byte(options.arguments), &args); err != nil || args == nil {
		return nil, errors.New("--args must be one typed JSON object")
	}
	return args, nil
}

func developmentConnectorContentRequired(declaration *workflow.DevelopmentConnector, options *connectorDevelopmentOptions) bool {
	if options.phase != "safe-output" {
		return false
	}
	for _, input := range declaration.Registration.Spec.SafeOutputs[options.capability].Inputs {
		if input.Sanitize != "" {
			return true
		}
	}
	return false
}

func stageDevelopmentConnector(output io.Writer, plan *connectors.Plan, options *connectorDevelopmentOptions, args map[string]any, prepared *connectorDevelopmentPrepared, contentRequired bool) error {
	if prepared != nil {
		args = prepared.Arguments
	}
	intent, err := plan.PrepareSafeOutputIntent(options.capability, args)
	if err != nil {
		return fmt.Errorf("safe-output staging conformance: %w", err)
	}
	if prepared != nil {
		goIntent, goErr := json.Marshal(intent)
		nodeIntent, nodeErr := json.Marshal(prepared.Intent)
		if goErr != nil || nodeErr != nil || !bytes.Equal(goIntent, nodeIntent) {
			return errors.New("shared Node staged intent does not match Go revalidation of the prepared arguments")
		}
	}
	publicationNotice := ""
	if contentRequired {
		publicationNotice = "Default shared publication formatting passed for annotated inputs; prepared content is not printed.\n"
	}
	if len(intent.UnresolvedInputs) > 0 {
		publicationNotice += "Unresolved targets: " + strings.Join(intent.UnresolvedInputs, ", ") + ". Canonical identities and relationships remain unverified until authorized execution.\n"
	}
	_, err = fmt.Fprintf(output, "PASS: safe-output %s staged; Go plan and argument checks passed.\n%sNo credentials were read and no HTTP requests or writes were made. Provider credentials and response checks were not tested.\n%s", options.capability, publicationNotice, connectorNodeNotice(options.nodeRuntime))
	return err
}

func testDevelopmentConnectorRead(ctx context.Context, output io.Writer, declaration *workflow.DevelopmentConnector, plan *connectors.Plan, capability string, options *connectorDevelopmentOptions, args map[string]any, host connectorDevelopmentReadHost) error {
	secrets, err := developmentConnectorSecrets(output, declaration, plan, host.getenv)
	if err != nil {
		return err
	}
	runtime := connectors.Runtime{
		Phase: options.phase, AllowedOrigins: plan.Origins(), Client: host.client,
		ResolveSecret: func(_ context.Context, phase, name string) (string, error) {
			if phase != options.phase || secrets[name] == "" {
				return "", errors.New("connector credential is outside the selected phase")
			}
			return secrets[name], nil
		},
	}
	if capability == "httpMcp" {
		_, err = plan.CallMCP(ctx, runtime, options.capability, options.remoteTool, args)
	} else {
		_, err = plan.Execute(ctx, runtime, capability, options.capability, args)
	}
	if err != nil {
		message := err.Error()
		for _, value := range secrets {
			message = strings.ReplaceAll(message, value, "[REDACTED]")
		}
		return fmt.Errorf("connector read conformance failed: %s; check the selected arguments, grants, provider scopes and setup guidance; retries follow the declared bounded policy", connectorConsoleText(message))
	}
	_, err = fmt.Fprintf(output, "PASS: %s %s; Go plan, arguments and provider response checks passed.\nResponse content and credential values are not printed.\n%s", options.phase, options.capability, connectorNodeNotice(options.nodeRuntime))
	return err
}

func selectDevelopmentConnector(declaration *workflow.DevelopmentConnector, options *connectorDevelopmentOptions) (*connectors.Plan, string, error) {
	selection := connectors.Selection{}
	capability := options.phase
	switch options.phase {
	case "agent":
		var err error
		capability, err = declaration.Service.ToolCapability(options.capability)
		if err != nil {
			return nil, "", err
		}
		if capability == "httpMcp" {
			if options.remoteTool == "" {
				return nil, "", errors.New("HTTP MCP testing requires --remote-tool with an exact allowlisted name")
			}
			server := declaration.Registration.Spec.HTTPMCP[options.capability]
			if _, ok := server.Tools[options.remoteTool]; !ok {
				return nil, "", errors.New("--remote-tool is not in the connector's exact allowlist")
			}
			selection.HTTPMCP = []string{options.capability}
		} else {
			selection.MCP = []string{options.capability}
		}
	case "context":
		selection.Context = []string{options.capability}
	case "safe-output":
		selection.SafeOutputs = []string{options.capability}
	case "trigger":
		selection.Triggers = []string{options.capability}
	default:
		return nil, "", errors.New("test requires --phase agent, context, safe-output, or trigger; trigger tests are offline and never enroll a receiver")
	}
	if options.remoteTool != "" && capability != "httpMcp" {
		return nil, "", errors.New("--remote-tool applies only to HTTP MCP capabilities")
	}
	plan, err := declaration.Service.Select(selection)
	if err != nil {
		return nil, "", err
	}
	if options.phase == "trigger" {
		plan, err = plan.ForPhase("trigger")
		return plan, capability, err
	}
	plan, err = plan.ForPhase(options.phase)
	return plan, capability, err
}

func developmentConnectorSecrets(output io.Writer, declaration *workflow.DevelopmentConnector, plan *connectors.Plan, getenv func(string) string) (map[string]string, error) {
	registration := declaration.Registration
	selected := plan.Secrets()
	for _, name := range slices.Sorted(maps.Keys(registration.Spec.Credentials)) {
		if !slices.Contains(selected, registration.Credentials[name].Secret) {
			continue
		}
		if registration.GitHubApps[name] != nil {
			return nil, fmt.Errorf("credential %s uses GitHub App identity, not a token environment secret; local minting and token fallback are not supported; run gh aw connector <id> setup for private-key/client-ID provisioning", name)
		}
		if registration.OIDCTokens[name] != nil {
			return nil, fmt.Errorf("credential %s uses GitHub Actions OIDC identity; local OIDC acquisition and token fallback are not supported; run gh aw connector <id> setup for audience/trust configuration", name)
		}
	}
	secrets := map[string]string{}
	var missing []string
	for _, name := range selected {
		value := getenv(name)
		if value == "" {
			missing = append(missing, name)
		} else if strings.ContainsAny(value, "\r\n") {
			return nil, fmt.Errorf("environment secret %s must not contain CR/LF; obtain a single-line provider credential (values are never printed)", name)
		} else {
			secrets[name] = value
		}
	}
	if len(missing) > 0 {
		slices.Sort(missing)
		var text strings.Builder
		fmt.Fprintf(&text, "Missing environment secrets for the selected phase: %s\n", strings.Join(missing, ", ")) //nolint:fprintferrorunchecked // strings.Builder cannot fail.
		for _, name := range missing {
			writeConnectorTokenGuidance(&text, name)
		}
		text.WriteString("GitHub repository secrets are not downloaded by local tests. Run connector <id> setup for provider-specific provisioning.\n")
		if _, err := io.WriteString(output, text.String()); err != nil {
			return nil, err
		}
		return nil, fmt.Errorf("connector test requires environment secrets: %s", strings.Join(missing, ", "))
	}
	return secrets, nil
}

func connectorNodeNotice(path string) string {
	if path != "" {
		return "Shared Node plan conformance also passed (offline, without credentials).\n"
	}
	return "Node checks were not requested; add --node-runtime path/to/connector_runtime.cjs for shared-runtime conformance.\n"
}
