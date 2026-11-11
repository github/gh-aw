package cli

import (
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"maps"
	"path"
	"regexp"
	"slices"
	"strings"
	"unicode"

	"github.com/github/gh-aw/pkg/connectors"
	"github.com/github/gh-aw/pkg/console"
	"github.com/github/gh-aw/pkg/parser"
	"github.com/github/gh-aw/pkg/workflow"
)

var connectorIdentityReference = regexp.MustCompile(`^\$\{\{\s*(secrets|vars)\.([A-Za-z_][A-Za-z0-9_]*)\s*\}\}$`)

type connectorCredentialGuide struct {
	Credential string `console:"header:Credential"`
	Phase      string `console:"header:Phase"`
	Route      string `console:"header:Route"`
	Reference  string `console:"header:Reference"`
	Scopes     string `console:"header:Required scopes"`
}

func setupDevelopmentConnector(output io.Writer, id string, declaration *workflow.DevelopmentConnector, phase string) error {
	registration := declaration.Registration
	rows := developmentConnectorCredentialRows(registration, phase)
	if len(rows) == 0 {
		return fmt.Errorf("connector has no credentials in requested phase %q", phase)
	}
	var text strings.Builder
	label := "Connector"
	if registration.Experimental {
		label = "Experimental connector"
	}
	fmt.Fprintf(&text, "%s %s\nSource: %s\n\n", label, id, connectorConsoleText(declaration.Source)) //nolint:fprintferrorunchecked // strings.Builder cannot fail.
	text.WriteString(console.RenderStruct(rows))
	writeConnectorImportGuidance(&text, declaration.Markdown)
	writeConnectorDevelopmentCapabilities(&text, registration, phase)
	text.WriteString("\nProvision only the listed phase's minimum provider permissions and exact resource access.\n")
	text.WriteString("Importing selects nothing. Select tools.<id> reads and safe-outputs.<id>-<operation> independently.\n")
	text.WriteString("For hosted MCP, select exact declared native methods, not an entire profile. Start writes staged.\n")
	text.WriteString("Scopes describe requirements, not grants; token acquisition, consent, refresh and provider permissions remain external.\n")
	text.WriteString("Independent resource lists do not establish parent, tenant or tuple membership. Never widen grants after a denial.\n")
	writeConnectorNetworkGuidance(&text, registration)
	writeConnectorCredentialGuidance(&text, registration, rows)
	writeConnectorSetupDocumentation(&text, declaration.Markdown)
	if len(registration.Docs) > 0 {
		text.WriteString("\nProvider documentation (not fetched):\n")
		for _, reference := range registration.Docs {
			fmt.Fprintf(&text, "  %s\n", connectorConsoleText(reference)) //nolint:fprintferrorunchecked // strings.Builder cannot fail.
		}
	}
	text.WriteString("\nNo secrets were read, written, minted, or refreshed. GitHub secrets are not downloaded locally.\n")
	text.WriteString("No capabilities, native methods, write credentials or webhook ingress were enabled.\n")
	text.WriteString("Use gh aw connector <id> test --help for exact capabilities, JSON arguments, and resource grants.\n")
	_, err := io.WriteString(output, text.String())
	return err
}

func developmentConnectorCredentialRows(registration workflow.ConnectorRegistration, phase string) []connectorCredentialGuide {
	var rows []connectorCredentialGuide
	for _, name := range slices.Sorted(maps.Keys(registration.Spec.Credentials)) {
		credential := registration.Spec.Credentials[name]
		if phase != "" && credential.Phase != phase {
			continue
		}
		row := connectorCredentialGuide{
			Credential: name, Phase: credential.Phase, Route: "token",
			Reference: registration.Credentials[name].Secret,
			Scopes:    connectorConsoleText(strings.Join(credential.Scopes, ", ")),
		}
		if row.Reference == "" {
			row.Route, row.Reference = "unconfigured", "(no credential route)"
		}
		if app := registration.GitHubApps[name]; app != nil {
			row.Route, row.Reference = "GitHub App", connectorConsoleText(app.PrivateKey)
		}
		if oidc := registration.OIDCTokens[name]; oidc != nil {
			row.Route, row.Reference = "GitHub OIDC", connectorConsoleText(oidc.Audience)
		}
		rows = append(rows, row)
	}
	if phase == "" || phase == "safe-output" {
		for _, name := range slices.Sorted(maps.Keys(registration.Spec.SafeOutputs)) {
			op := registration.Spec.SafeOutputs[name]
			for _, input := range slices.Sorted(maps.Keys(op.TrustedInputs)) {
				if secret := op.TrustedInputs[input].Secret; secret != "" {
					rows = append(rows, connectorCredentialGuide{
						Credential: path.Join(name, input), Phase: "safe-output", Route: "trusted input",
						Reference: secret, Scopes: "host-owned resource identity",
					})
				}
			}
		}
	}
	return rows
}

func writeConnectorCredentialGuidance(text *strings.Builder, registration workflow.ConnectorRegistration, rows []connectorCredentialGuide) {
	for _, row := range rows {
		fmt.Fprintf(text, "\n%s (%s):\n", row.Credential, row.Phase) //nolint:fprintferrorunchecked // strings.Builder cannot fail.
		switch row.Route {
		case "token":
			writeConnectorTokenGuidance(text, row.Reference)
		case "unconfigured":
			text.WriteString("  Configure this credential's token.secret NAME, github-app, or github-oidc route in the declaration.\n")
			text.WriteString("  Use its normal import-schema bindings with --input name=JSON when the route is parameterized.\n")
		case "trusted input":
			fmt.Fprintf(text, "  Fixed host-owned resource input, not an authentication token: gh secret set %s\n", row.Reference) //nolint:fprintferrorunchecked // strings.Builder cannot fail.
			text.WriteString("  Store the exact approved resource identity. Do not pass this trusted input in --args.\n")
			text.WriteString("  Local safe-output staging does not read or resolve this secret.\n")
		case "GitHub App":
			app := registration.GitHubApps[row.Credential]
			text.WriteString("  Install the GitHub App on the exact declared owner/repositories with its declared permissions.\n")
			if app.Owner != "" {
				fmt.Fprintf(text, "  Declared installation owner: %s\n", app.Owner) //nolint:fprintferrorunchecked // strings.Builder cannot fail.
			}
			if len(app.Repositories) > 0 {
				fmt.Fprintf(text, "  Declared installation repositories: %s\n", strings.Join(app.Repositories, ", ")) //nolint:fprintferrorunchecked // strings.Builder cannot fail.
			} else {
				text.WriteString("  Installation is limited to the workflow repository.\n")
			}
			for _, permission := range slices.Sorted(maps.Keys(app.Permissions)) {
				fmt.Fprintf(text, "  Required GitHub App permission %s: %s\n", permission, app.Permissions[permission]) //nolint:fprintferrorunchecked // strings.Builder cannot fail.
			}
			writeConnectorIdentityGuidance(text, app.AppID, false)
			writeConnectorIdentityGuidance(text, app.PrivateKey, true)
			text.WriteString("  Local read tests cannot mint installation tokens; a token environment fallback is not accepted.\n")
		case "GitHub OIDC":
			fmt.Fprintf(text, "  Configure provider trust for GitHub Actions OIDC and exact audience %q.\n", row.Reference) //nolint:fprintferrorunchecked // strings.Builder cannot fail.
			text.WriteString("  The workflow's selected phase needs id-token: write. No static token secret provisions this route.\n")
			text.WriteString("  Local read tests cannot acquire GitHub Actions identity; a token environment fallback is not accepted.\n")
		}
	}
}

func writeConnectorTokenGuidance(text *strings.Builder, name string) {
	text.WriteString("  Obtain the provider token using the documentation below; do not put it in Markdown, --input, or --args.\n")
	fmt.Fprintf(text, "  Local environment (Bash, hidden input): read -r -s %s; export %s; printf '\\n'\n", name, name) //nolint:fprintferrorunchecked // strings.Builder cannot fail.
	fmt.Fprintf(text, "  GitHub Actions repository secret: gh secret set %s\n", name)                                   //nolint:fprintferrorunchecked // strings.Builder cannot fail.
	text.WriteString("  Run gh secret set in the intended repository; add --repo OWNER/REPO or --env ENVIRONMENT when needed.\n")
}

func writeConnectorIdentityGuidance(text *strings.Builder, reference string, privateKey bool) {
	match := connectorIdentityReference.FindStringSubmatch(reference)
	if len(match) < 3 {
		fmt.Fprintf(text, "  Declared GitHub App client ID: %q\n", connectorConsoleText(reference)) //nolint:fprintferrorunchecked // strings.Builder cannot fail.
		return
	}
	var captures [3]string
	copy(captures[:], match)
	kind, name := captures[1], captures[2]
	if privateKey {
		fmt.Fprintf(text, "  GitHub App private-key secret %s (not a provider token): gh secret set %s < path/to/private-key.pem\n", name, name) //nolint:fprintferrorunchecked // strings.Builder cannot fail.
	} else if kind == "vars" {
		fmt.Fprintf(text, "  GitHub App client-ID variable %s: gh variable set %s\n", name, name) //nolint:fprintferrorunchecked // strings.Builder cannot fail.
	} else {
		fmt.Fprintf(text, "  GitHub App client-ID secret %s: gh secret set %s\n", name, name) //nolint:fprintferrorunchecked // strings.Builder cannot fail.
	}
}

func connectorConsoleText(value string) string {
	return strings.Join(strings.FieldsFunc(value, func(r rune) bool {
		return unicode.IsControl(r) || unicode.IsSpace(r) || unicode.Is(unicode.Cf, r)
	}), " ")
}

func writeConnectorDevelopmentCapabilities(text *strings.Builder, registration workflow.ConnectorRegistration, phase string) {
	text.WriteString("\nDeclared capabilities (select one with test --phase PHASE --capability NAME):\n")
	write := func(name, capabilityPhase string, inputs map[string]connectors.Input, resources map[string]string) {
		if phase != "" && capabilityPhase != phase {
			return
		}
		fmt.Fprintf(text, "  %s %s\n", capabilityPhase, name) //nolint:fprintferrorunchecked // strings.Builder cannot fail.
		for _, input := range slices.Sorted(maps.Keys(inputs)) {
			required := "optional"
			if inputs[input].Required {
				required = "required"
			}
			fmt.Fprintf(text, "    --args field %s: %s (%s)\n", input, inputs[input].Type, required) //nolint:fprintferrorunchecked // strings.Builder cannot fail.
		}
		for _, resource := range slices.Sorted(maps.Keys(resources)) {
			fmt.Fprintf(text, "    --grant %s='[\"approved-id\"]' (exact constraint for %s)\n", resources[resource], resource) //nolint:fprintferrorunchecked // strings.Builder cannot fail.
		}
	}
	if registration.Spec == nil {
		return
	}
	for _, name := range slices.Sorted(maps.Keys(registration.Spec.MCP)) {
		op := registration.Spec.MCP[name]
		write(name, "agent", op.Inputs, op.Resources)
		writeConnectorLookupCapabilities(text, name, "agent", phase, op.Operation)
	}
	for _, group := range []struct {
		phase string
		ops   map[string]connectors.Operation
	}{{"context", registration.Spec.Context}, {"safe-output", registration.Spec.SafeOutputs}} {
		for _, name := range slices.Sorted(maps.Keys(group.ops)) {
			op := group.ops[name]
			inputs, resources := maps.Clone(op.Inputs), maps.Clone(op.Resources)
			for input := range op.TrustedInputs {
				delete(inputs, input)
				delete(resources, input)
			}
			write(name, group.phase, inputs, resources)
			writeConnectorLookupCapabilities(text, name, group.phase, phase, op)
		}
	}
	for _, name := range slices.Sorted(maps.Keys(registration.Spec.HTTPMCP)) {
		server := registration.Spec.HTTPMCP[name]
		for _, tool := range slices.Sorted(maps.Keys(server.Tools)) {
			write(name+" --remote-tool "+tool, "agent", server.Tools[tool].Inputs, server.Tools[tool].Resources)
		}
	}
}

func writeConnectorLookupCapabilities(text *strings.Builder, name, capabilityPhase, selectedPhase string, op connectors.Operation) {
	if len(op.Lookups) == 0 || (selectedPhase != "" && capabilityPhase != selectedPhase) {
		return
	}
	fmt.Fprintf(text, "    %s: automatic aliases use at most %d fixed reads with the same %s credential; provision its provider Read permissions too.\n", name, len(op.Lookups), capabilityPhase) //nolint:fprintferrorunchecked // strings.Builder cannot fail.
	grants := map[string]struct{}{}
	for _, lookup := range op.Lookups {
		for _, resources := range []map[string]string{lookup.Resources, lookup.ResponseResources} {
			for _, resource := range resources {
				grants[resource] = struct{}{}
			}
		}
	}
	for _, resource := range slices.Sorted(maps.Keys(grants)) {
		fmt.Fprintf(text, "    --grant %s='[\"approved-id\"]' (conditional lookup grant, independent of canonical grants)\n", resource) //nolint:fprintferrorunchecked // strings.Builder cannot fail.
	}
	text.WriteString("    Staging keeps aliases unresolved and makes no lookup requests.\n")
}

// XML supplies text only. encoding/xml does not resolve external entities, and
// terminal controls are removed before any provider-authored text is displayed.
func connectorCredentialXML(markdown string) string {
	var sections []string
	for {
		start := strings.Index(markdown, "<connector-documentation")
		if start < 0 {
			return strings.Join(sections, "\n")
		}
		fragment, rest, found := strings.Cut(markdown[start:], "</connector-documentation>")
		if !found {
			return "  Structured credential documentation could not be decoded; inspect the declaration source."
		}
		sections = append(sections, connectorCredentialFragment(fragment+"</connector-documentation>"))
		markdown = rest
	}
}

func connectorCredentialFragment(fragment string) string {
	decoder := xml.NewDecoder(strings.NewReader(fragment))
	depth := 0
	var lines []string
	for {
		token, err := decoder.Token()
		if errors.Is(err, io.EOF) {
			return strings.Join(lines, "\n")
		}
		if err != nil {
			return "  Structured credential documentation could not be decoded; inspect the declaration source."
		}
		switch value := token.(type) {
		case xml.StartElement:
			if value.Name.Local == "credentials" || value.Name.Local == "authentication" || depth > 0 {
				depth++
				for _, attribute := range value.Attr {
					lines = append(lines, "  "+connectorConsoleText(value.Name.Local+" "+attribute.Name.Local+"="+attribute.Value))
				}
			}
		case xml.EndElement:
			if depth > 0 {
				depth--
			}
		case xml.CharData:
			if depth > 0 {
				if text := connectorConsoleText(string(value)); text != "" {
					lines = append(lines, "  "+text)
				}
			}
		}
	}
}

func writeConnectorImportGuidance(text *strings.Builder, markdown string) {
	parsed, err := parser.ExtractFrontmatterFromContent(markdown)
	if err != nil {
		text.WriteString("\nImport metadata could not be decoded; inspect the declaration source.\n")
		return
	}
	schema, ok := parsed.Frontmatter["import-schema"].(map[string]any)
	if !ok || len(schema) == 0 {
		return
	}
	text.WriteString("\nImport bindings (workflow imports[].with, or setup --input name=JSON):\n")
	for _, name := range slices.Sorted(maps.Keys(schema)) {
		input, ok := schema[name].(map[string]any)
		if !ok {
			continue
		}
		kind, ok := input["type"].(string)
		if !ok {
			text.WriteString("  Input type metadata missing; inspect the declaration source.\n")
			continue
		}
		requirement := "optional"
		if input["required"] == true {
			requirement = "required"
		}
		if _, hasDefault := input["default"]; hasDefault {
			requirement += "; declaration default available"
		}
		fmt.Fprintf(text, "  %s: %s (%s)\n", connectorConsoleText(name), connectorConsoleText(kind), requirement) //nolint:fprintferrorunchecked // strings.Builder cannot fail.
	}
	text.WriteString("Use exact author-approved resource IDs and secret NAMES only, never token values. Empty/default example resources are not approval.\n")
}

func writeConnectorNetworkGuidance(text *strings.Builder, registration workflow.ConnectorRegistration) {
	text.WriteString("\nDeclared endpoints (not fetched or added to network.allowed):\n")
	for _, name := range slices.Sorted(maps.Keys(registration.Spec.Endpoints)) {
		fmt.Fprintf(text, "  %s: %s\n", name, connectorConsoleText(registration.Spec.Endpoints[name])) //nolint:fprintferrorunchecked // strings.Builder cannot fail.
	}
	for _, name := range slices.Sorted(maps.Keys(registration.Spec.HTTPMCP)) {
		fmt.Fprintf(text, "  MCP %s: %s\n", name, connectorConsoleText(registration.Spec.HTTPMCP[name].URL)) //nolint:fprintferrorunchecked // strings.Builder cannot fail.
	}
	text.WriteString("Allow only the hosts needed by explicitly selected reads. Safe-output credentials and writes stay in the trusted phase, not agent tools or network access.\n")
	text.WriteString("Trigger declarations require separate reviewed source bindings and receiver enrollment; compilation/setup never deploys a receiver.\n")
}
