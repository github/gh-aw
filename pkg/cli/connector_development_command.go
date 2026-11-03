package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/gitutil"
	"github.com/github/gh-aw/pkg/workflow"
	"github.com/spf13/cobra"
)

type connectorDevelopmentOptions struct {
	root, file, phase, capability, arguments, remoteTool, nodeRuntime string
	nativeEvent                                                       string
	inputs, grants                                                    []string
	timeout                                                           time.Duration
}

var connectorDevelopmentKey = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_-]*$`)

const connectorDevelopmentHelp = `Work with a self-contained AW Markdown connector using the normal compiler import-schema.
Use setup for credential provisioning guidance; it never reads or writes secret values.
Use test for one explicitly selected capability and exact resource grants. Read tests
contact the declared provider using only that phase's named environment secrets.
Safe-output tests are always staged: no credentials, requests, or live writes.
Trigger tests project native fixture JSON offline; they never enroll a receiver.

Declarations are discovered from .github/workflows/connectors/<id>.md, or an explicit
--file within the repository. There is no built-in provider catalog or source fallback.
For a remote source, create a local wrapper using the normal AW imports URL syntax;
its import-schema bindings and nested imports are processed by the normal compiler.
An invalid local or explicit --file is never ignored.
Declarations and their imports must be trusted, just as when compiling a workflow.
Documentation references are displayed only, never fetched.
Setup includes credentials/authentication metadata from normal wrapper imports,
typed binding names, declared endpoints, and matching local golden consumers.
Endpoint guidance does not widen network access. Setup does not install connectors,
select capabilities/native methods, provision write credentials or activate ingress.

Import bindings use repeated --input name=JSON (quote JSON strings). Exact grants use
repeated --grant resource=JSON-array; each supplied resource replaces, never appends
to, its declaration's grants. --args accepts one JSON object. No IDs are inferred.
Go validates the frozen phase plan and provider response checks. --node-runtime
additionally validates that same plan with a trusted local shared Node runtime;
safe-output staging is then checked by both runtimes without provider access.
Safe-output inputs annotated sanitize: title/body require --node-runtime and its
matching sibling connector_content.cjs plus dependencies. The shared publisher
formats these inputs once before both Node staging and Go revalidation, including
the default generated footer and honest local attribution without an Actions run URL.
Sanitized titles must contain at least 20 Unicode code points. Prepared content
is never printed. This command does not load a workflow's custom footer settings.
Node remains optional for operations without content annotations.`

const connectorDevelopmentExamples = `  ` + string(constants.CLIExtensionPrefix) + ` connector slack setup
  ` + string(constants.CLIExtensionPrefix) + ` connector jira setup --input 'mcp-personal-secret="JIRA_DEV_BASIC_AUTH"'
  ` + string(constants.CLIExtensionPrefix) + ` connector slack test --phase agent --capability read-thread \
    --grant 'channels=["C123ABC456"]' --grant 'threads=["1515449522.000016"]' \
    --args '{"channel":"C123ABC456","thread":"1515449522.000016"}'
  ` + string(constants.CLIExtensionPrefix) + ` connector slack test --phase safe-output --capability reply-in-thread \
    --args '{"channel":"C123ABC456","thread":"1515449522.000016","text":"Local staged test"}' \
    --node-runtime actions/setup/js/connector_runtime.cjs
  ` + string(constants.CLIExtensionPrefix) + ` connector custom test --file .github/workflows/connectors/custom.md \
    --phase agent --capability read-item --args '{"item":"approved-id"}'`

// NewConnectorCommand provides the id-first development interface independently
// of the existing plural trusted-host commands.
func NewConnectorCommand() *cobra.Command {
	options := &connectorDevelopmentOptions{}
	cmd := &cobra.Command{
		Use:           "connector <id> <setup|test>",
		Short:         "Configure and locally test an experimental connector",
		SilenceUsage:  true,
		SilenceErrors: true,
		Long:          connectorDevelopmentHelp,
		Example:       connectorDevelopmentExamples,
		Args: func(cmd *cobra.Command, args []string) error {
			_, _, err := parseDevelopmentConnectorInvocation(args)
			return err
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			return runDevelopmentConnector(cmd, args, options)
		},
	}
	cmd.Flags().StringVar(&options.root, "root", "", "Repository root (default: detected Git root, otherwise current directory)")
	cmd.Flags().StringVar(&options.file, "file", "", "Explicit repository-owned AW Markdown declaration or normal-import wrapper within --root")
	cmd.Flags().StringArrayVar(&options.inputs, "input", nil, "Import-schema binding name=JSON, e.g. 'reader-secret=\"DEV_READ_TOKEN\"' (repeatable)")
	cmd.Flags().StringArrayVar(&options.grants, "grant", nil, "Exact resource allowlist resource=JSON-array, e.g. 'issues=[\"ISSUE-1\"]' (repeatable)")
	cmd.Flags().StringVar(&options.phase, "phase", "", "Test phase: agent, context, safe-output, or offline trigger (required for test; credential phase for setup)")
	cmd.Flags().StringVar(&options.nativeEvent, "native-event", "", "Native event discriminator for offline --phase trigger; --args is the native fixture payload")
	cmd.Flags().StringVar(&options.capability, "capability", "", "Exact declared capability name to test (required for test)")
	cmd.Flags().StringVar(&options.arguments, "args", "{}", "Typed capability arguments as a JSON object; never contains credentials")
	cmd.Flags().StringVar(&options.remoteTool, "remote-tool", "", "Exact allowlisted tools/call name, required only for an HTTP MCP capability")
	cmd.Flags().StringVar(&options.nodeRuntime, "node-runtime", "", "Trusted matching connector_runtime.cjs; required with connector_content.cjs and dependencies for annotated safe-output content (offline)")
	cmd.Flags().DurationVar(&options.timeout, "timeout", 20*time.Second, "Total local test deadline, greater than zero and at most 20s; only explicitly declared retries, no redirects")
	return cmd
}

func parseDevelopmentConnectorInvocation(args []string) (string, string, error) {
	var invocation [2]string
	if len(args) != len(invocation) {
		return "", "", errors.New("use gh aw connector <id> setup or gh aw connector <id> test; see --help")
	}
	copy(invocation[:], args)
	id, action := invocation[0], invocation[1]
	if action != "setup" && action != "test" {
		return "", "", errors.New("use gh aw connector <id> setup or gh aw connector <id> test; see --help")
	}
	return id, action, nil
}

func runDevelopmentConnector(cmd *cobra.Command, args []string, options *connectorDevelopmentOptions) error {
	id, action, err := parseDevelopmentConnectorInvocation(args)
	if err != nil {
		return err
	}
	inputs, grants, err := connectorDevelopmentBindings(options)
	if err != nil {
		return err
	}
	declaration, err := workflow.LoadDevelopmentConnector(options.root, id, options.file, inputs, grants)
	if err != nil {
		return fmt.Errorf("load connector: %w", err)
	}
	if action == "setup" {
		for _, flag := range []string{"capability", "args", "remote-tool", "node-runtime", "native-event", "timeout"} {
			if cmd.Flags().Changed(flag) {
				return fmt.Errorf("--%s applies only to connector test", flag)
			}
		}
		if options.phase != "" && options.phase != "agent" && options.phase != "context" && options.phase != "safe-output" {
			return errors.New("--phase must be agent, context, or safe-output")
		}
		root := options.root
		if root == "" {
			root, err = gitutil.FindGitRoot()
			if err != nil {
				root = "."
			}
		}
		if err := loadConnectorSetupDocumentation(root, id, declaration, inputs); err != nil {
			return err
		}
		if err := setupDevelopmentConnector(cmd.OutOrStdout(), id, declaration, options.phase); err != nil {
			return err
		}
		return writeConnectorSetupExamples(cmd.OutOrStdout(), root, id)
	}
	return testDevelopmentConnector(cmd.Context(), cmd.OutOrStdout(), declaration, options, os.Getenv, nil)
}

func connectorDevelopmentBindings(options *connectorDevelopmentOptions) (map[string]any, map[string][]string, error) {
	inputs := map[string]any{}
	grants := map[string][]string{}
	for _, binding := range options.inputs {
		name, value, ok := strings.Cut(binding, "=")
		if !ok || !connectorDevelopmentKey.MatchString(name) || len(value) > 256*1024 || !json.Valid([]byte(value)) {
			return nil, nil, errors.New("--input requires name=JSON; quote string values, e.g. 'reader-secret=\"DEV_READ_TOKEN\"'")
		}
		if _, duplicate := inputs[name]; duplicate {
			return nil, nil, fmt.Errorf("duplicate --input %q", name)
		}
		var decoded any
		decoder := json.NewDecoder(strings.NewReader(value))
		decoder.UseNumber()
		if err := decoder.Decode(&decoded); err != nil {
			return nil, nil, fmt.Errorf("invalid --input %q", name)
		}
		decoded, err := connectorImportNumbers(decoded)
		if err != nil {
			return nil, nil, fmt.Errorf("--input %q contains a number outside the supported exact numeric range", name)
		}
		inputs[name] = decoded
	}
	for _, binding := range options.grants {
		name, value, ok := strings.Cut(binding, "=")
		if !ok || !connectorDevelopmentKey.MatchString(name) || len(value) > 256*1024 {
			return nil, nil, errors.New("--grant requires resource=JSON-array of exact nonempty strings")
		}
		if _, duplicate := grants[name]; duplicate {
			return nil, nil, fmt.Errorf("duplicate --grant %q", name)
		}
		var values []string
		if err := json.Unmarshal([]byte(value), &values); err != nil || len(values) == 0 {
			return nil, nil, errors.New("--grant requires a nonempty JSON array of exact strings; wildcards are not grants")
		}
		grants[name] = values
	}
	return inputs, grants, nil
}

func connectorImportNumbers(value any) (any, error) {
	switch typed := value.(type) {
	case json.Number:
		if !strings.ContainsAny(typed.String(), ".eE") {
			number, err := strconv.ParseInt(typed.String(), 10, 64)
			if err != nil || number > 9007199254740991 || number < -9007199254740991 {
				return nil, errors.New("unsafe integer")
			}
			return number, nil
		}
		number, err := typed.Float64()
		if err != nil || number > 9007199254740991 || number < -9007199254740991 {
			return nil, errors.New("unsafe number")
		}
		return number, nil
	case []any:
		for i, item := range typed {
			normalized, err := connectorImportNumbers(item)
			if err != nil {
				return nil, err
			}
			typed[i] = normalized
		}
	case map[string]any:
		for name, item := range typed {
			normalized, err := connectorImportNumbers(item)
			if err != nil {
				return nil, err
			}
			typed[name] = normalized
		}
	}
	return value, nil
}
