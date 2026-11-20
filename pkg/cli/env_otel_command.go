package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/github/gh-aw/pkg/workflow/compilerenv"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

type envOTELOptions struct {
	scope, endpoint, protocol, visibility string
	repos, orgs, enterprises, selected    []string
	headersStdin, metrics                 bool
	yes                                   bool
}

type envOTELPlan struct {
	target defaultsTarget
	key    repoPublicKey
	ids    []int64
}

var envOTELIdentifier = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]*$`)
var envOTELRepositoryName = regexp.MustCompile(`^[A-Za-z0-9._-][A-Za-z0-9._-]*$`)
var envOTELRunGH = envOTELGH

type envOTELGHOutput struct {
	buffer bytes.Buffer
}

func (b *envOTELGHOutput) Write(data []byte) (int, error) {
	if b.buffer.Len()+len(data) > 1024*1024 {
		return 0, errors.New("GitHub response exceeds 1 MiB")
	}
	return b.buffer.Write(data)
}

func newEnvOTELCommand() *cobra.Command {
	var o envOTELOptions
	cmd := &cobra.Command{
		Use:   "otel",
		Short: "Test and configure default OpenTelemetry export",
		Long: `Configure one OTLP endpoint for one or more repository, organization, or enterprise targets.

The command tests an authenticated empty OTLP traces export before saving anything.
Without --endpoint, a terminal prompts for the URL. Header credentials are read
without echo in a terminal, or from stdin with --headers-stdin. Never put credentials
in command arguments. Noninteractive use requires --endpoint and --headers-stdin.

Repeat --repo, --org, and --enterprise to configure multiple targets. Without
targets, use the current repository. --scope restricts targets to repo, org, or ent.
Selected visibility requires --selected entries (owner/repo for org; org for ent).
Enterprise scope expands to its visible organizations, since GitHub has no enterprise
Actions secrets API. Selected enterprises configure only the named organizations,
with private repository visibility. Other enterprise visibility applies to each org.
The command configures the existing endpoint and header secrets, plus protocol and
metrics variables. It never compiles or changes workflows. The endpoint secret wins
over endpoint variables, including repository variables. Organization setup requires
access to inspect every affected repository and rejects partial secret overrides or
changes to existing secret visibility. Unrelated defaults are preserved.

During updates, export is disabled with an invalid endpoint until all settings are
saved. A failed write can leave export disabled; rerun the command to recover. Avoid
concurrent settings changes or workflow starts during configuration. Explicit workflow
OTLP settings still win.`,
		Example: `  gh aw env otel
  gh aw env otel --repo myorg/one --repo myorg/two --endpoint https://collector.example --headers-stdin --yes < otlp-headers.txt
  gh aw env otel --org myorg --visibility selected --selected myorg/one --endpoint https://collector.example --headers-stdin --yes < otlp-headers.txt
  gh aw env otel --enterprise my-ent --endpoint https://collector.example --headers-stdin --yes < otlp-headers.txt`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return executeEnvOTELCommand(cmd, o)
		},
	}
	cmd.Flags().StringVar(&o.scope, "scope", "", "Restrict targets to repo|org|ent")
	cmd.Flags().StringSliceVarP(&o.repos, "repo", "r", nil, "Target repository owner/repo (repeatable)")
	cmd.Flags().StringSliceVar(&o.orgs, "org", nil, "Target organization (repeatable)")
	cmd.Flags().StringSliceVar(&o.enterprises, "enterprise", nil, "Target enterprise slug (repeatable)")
	cmd.Flags().StringVar(&o.endpoint, "endpoint", "", "OTLP URL (HTTPS, or HTTP on literal loopback; percent-encode quotes and backslashes)")
	cmd.Flags().StringVar(&o.protocol, "protocol", "http/json", "OTLP encoding: http/json|http/protobuf")
	cmd.Flags().BoolVar(&o.metrics, "metrics", true, "Enable OTLP metrics")
	cmd.Flags().BoolVar(&o.headersStdin, "headers-stdin", false, "Read secret exporter headers from stdin")
	cmd.Flags().BoolVarP(&o.yes, "yes", "y", false, "Approve the displayed targets without prompting")
	cmd.Flags().StringVar(&o.visibility, "visibility", defaultsVisibilityPriv, "Org/enterprise access: all|private|selected")
	cmd.Flags().StringSliceVar(&o.selected, "selected", nil, "Selected owner/repo (org) or organization (enterprise); repeatable")
	return cmd
}

func executeEnvOTELCommand(cmd *cobra.Command, o envOTELOptions) error {
	targets, err := envOTELTargets(o, cmd.Flags().Changed("visibility"))
	if err != nil {
		return err
	}
	targets, err = envOTELExpandEnterprises(cmd.Context(), targets, o.selected)
	if err != nil {
		return err
	}
	headers, err := envOTELInput(cmd, &o, targets)
	if err != nil {
		return err
	}
	if err := runEnvOTEL(cmd.Context(), targets, o, headers); err != nil {
		return err
	}
	return envOTELPrint(cmd.OutOrStdout(), "OTLP traces check passed; configured %d target(s). No workflow files changed.\n", len(targets))
}

func envOTELPrint(output io.Writer, format string, args ...any) error {
	if _, err := fmt.Fprintf(output, format, args...); err != nil {
		return errors.New("failed to write CLI output")
	}
	return nil
}

func envOTELInput(cmd *cobra.Command, o *envOTELOptions, targets []defaultsTarget) (string, error) {
	input := cmd.InOrStdin()
	file, terminal := input.(*os.File)
	terminal = terminal && term.IsTerminal(int(file.Fd()))
	if o.endpoint == "" {
		if !terminal || o.headersStdin {
			return "", errors.New("provide --endpoint for noninteractive use; supply headers with --headers-stdin")
		}
		if err := envOTELPrint(cmd.ErrOrStderr(), "OTLP endpoint URL (HTTPS, without credentials): "); err != nil {
			return "", err
		}
		var err error
		o.endpoint, err = envOTELReadLine(input)
		if err != nil {
			return "", errors.New("failed to read endpoint")
		}
	}
	if !o.headersStdin && !terminal {
		return "", errors.New("noninteractive use requires --headers-stdin")
	}
	if err := envOTELConfirm(cmd, *o, targets, terminal); err != nil {
		return "", err
	}
	if o.headersStdin {
		data, err := io.ReadAll(io.LimitReader(input, 48*1024+1))
		if err != nil || len(data) > 48*1024 {
			return "", errors.New("failed to read headers (maximum 48 KiB)")
		}
		return strings.TrimRight(string(data), "\r\n"), nil
	}
	if err := envOTELPrint(cmd.ErrOrStderr(), "OTLP headers (key=value, comma-separated; input hidden): "); err != nil {
		return "", err
	}
	data, err := term.ReadPassword(int(file.Fd()))
	if printErr := envOTELPrint(cmd.ErrOrStderr(), "\n"); printErr != nil {
		return "", printErr
	}
	if err != nil {
		return "", errors.New("failed to read headers")
	}
	return string(data), nil
}

func envOTELConfirm(cmd *cobra.Command, o envOTELOptions, targets []defaultsTarget, terminal bool) error {
	lines := []string{"Configure default OTLP for:"}
	for _, target := range targets {
		if target.scope == defaultsScopeOrg {
			lines = append(lines, fmt.Sprintf("  org %s (%s repositories)", target.displayName(), target.visibility))
		} else {
			lines = append(lines, "  repo "+target.displayName())
		}
	}
	if len(o.selected) > 0 {
		lines = append(lines, "Selected members: "+strings.Join(o.selected, ", "))
	}
	if err := envOTELPrint(cmd.ErrOrStderr(), "%s\n", strings.Join(lines, "\n")); err != nil {
		return err
	}
	if o.yes {
		return nil
	}
	if !terminal || o.headersStdin {
		return errors.New("use --yes to approve the displayed targets in noninteractive mode")
	}
	if err := envOTELPrint(cmd.ErrOrStderr(), "Test endpoint and save these settings? [y/N]: "); err != nil {
		return err
	}
	answer, err := envOTELReadLine(cmd.InOrStdin())
	if err != nil || !strings.EqualFold(answer, "y") {
		return errors.New("configuration cancelled; no settings saved")
	}
	return nil
}

// Read one byte at a time so endpoint input cannot consume the following secret.
func envOTELReadLine(input io.Reader) (string, error) {
	var result []byte
	b := make([]byte, 1)
	for len(result) < 8192 {
		n, err := input.Read(b)
		if n > 0 && len(b) > 0 {
			if b[0] == '\n' {
				return strings.TrimSuffix(string(result), "\r"), nil
			}
			result = append(result, b[0])
		}
		if err != nil {
			return "", err
		}
	}
	return "", errors.New("endpoint too long")
}

func envOTELTargets(o envOTELOptions, visibilitySet bool) ([]defaultsTarget, error) {
	if o.scope != "" && o.scope != defaultsScopeRepo && o.scope != defaultsScopeOrg && o.scope != defaultsScopeEnt {
		return nil, errors.New("--scope must be repo, org, or ent")
	}
	if o.visibility != defaultsVisibilityAll && o.visibility != defaultsVisibilityPriv && o.visibility != defaultsVisibilitySel {
		return nil, errors.New("--visibility must be all, private, or selected")
	}
	if (o.visibility == defaultsVisibilitySel) != (len(o.selected) > 0) {
		return nil, errors.New("--visibility selected requires --selected; --selected is invalid for other visibility values")
	}
	var targets []defaultsTarget
	seen := map[string]struct{}{}
	for _, group := range []struct {
		scope string
		names []string
	}{{defaultsScopeRepo, o.repos}, {defaultsScopeOrg, o.orgs}, {defaultsScopeEnt, o.enterprises}} {
		if len(group.names) > 0 && o.scope != "" && o.scope != group.scope {
			return nil, errors.New("target flags contradict --scope")
		}
		for _, name := range group.names {
			if !envOTELValidTarget(group.scope, name) {
				return nil, errors.New("invalid target: use owner/repo for repositories and a slug for organizations or enterprises")
			}
			target, err := resolveDefaultsTarget(group.scope, name, name, name, o.visibility, false)
			if err != nil {
				return nil, err
			}
			key := group.scope + ":" + strings.ToLower(name)
			if _, exists := seen[key]; !exists {
				targets = append(targets, target)
				seen[key] = struct{}{}
			}
		}
	}
	if len(targets) == 0 {
		if o.scope == defaultsScopeEnt {
			return nil, errors.New("--scope ent requires --enterprise")
		}
		target, err := resolveDefaultsTarget(o.scope, "", "", "", o.visibility, false)
		if err != nil {
			return nil, err
		}
		targets = append(targets, target)
	}
	for _, target := range targets {
		if target.scope == defaultsScopeRepo && (visibilitySet || len(o.selected) > 0) {
			return nil, errors.New("--visibility and --selected cannot be used with repository targets")
		}
	}
	if len(o.orgs) > 0 && len(o.enterprises) > 0 && len(o.selected) > 0 {
		return nil, errors.New("configure selected organization and enterprise targets in separate invocations")
	}
	return targets, nil
}

func envOTELValidTarget(scope, name string) bool {
	if scope != defaultsScopeRepo {
		return envOTELIdentifier.MatchString(name)
	}
	owner, repo, found := strings.Cut(name, "/")
	return found && envOTELIdentifier.MatchString(owner) && envOTELRepositoryName.MatchString(repo) && repo != "." && repo != ".."
}

// Secret-bearing requests never use runDefaultsGH, argv values, or raw GH errors.
func envOTELGH(ctx context.Context, input []byte, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	if ctx.Err() != nil {
		return nil, errors.New("GitHub operation cancelled; no operation started")
	}
	template := defaultsExecGH(args...)
	if template.Err != nil {
		return nil, errors.New("could not resolve gh; install and authenticate the GitHub CLI")
	}
	command := exec.CommandContext(ctx, template.Path)
	command.Args, command.Env, command.Dir = template.Args, template.Env, template.Dir
	command.Stdin = bytes.NewReader(input)
	var output envOTELGHOutput
	command.Stdout = &output
	command.Stderr = io.Discard
	command.WaitDelay = time.Second
	if err := command.Start(); err != nil {
		return nil, errors.New("could not start gh; install and authenticate the GitHub CLI")
	}
	err := command.Wait()
	if err != nil {
		return nil, errors.New("GitHub operation failed; check authentication, permissions, and host API support (response withheld)")
	}
	return output.buffer.Bytes(), nil
}

func envOTELExpandEnterprises(ctx context.Context, targets []defaultsTarget, selected []string) ([]defaultsTarget, error) {
	var result []defaultsTarget
	seen := map[string]struct{}{}
	selectedMembers := map[string]struct{}{}
	hasSelectedEnterprise := false
	add := func(target defaultsTarget) {
		key := target.scope + ":" + strings.ToLower(target.displayName())
		if _, exists := seen[key]; !exists {
			seen[key] = struct{}{}
			result = append(result, target)
		}
	}
	for _, target := range targets {
		if target.scope != defaultsScopeEnt {
			add(target)
			continue
		}
		query := `query($slug:String!,$endCursor:String){enterprise(slug:$slug){organizations(first:100,after:$endCursor){nodes{login} pageInfo{hasNextPage endCursor}}}}`
		data, err := envOTELRunGH(ctx, nil, "api", "graphql", "--paginate", "-f", "query="+query, "-f", "slug="+target.enterprise, "--jq", ".data.enterprise.organizations.nodes[].login")
		if err != nil {
			return nil, errors.New("failed to enumerate enterprise organizations; check enterprise access")
		}
		members := map[string]struct{}{}
		for name := range strings.FieldsSeq(string(data)) {
			if !envOTELIdentifier.MatchString(name) {
				return nil, errors.New("invalid enterprise organization response")
			}
			members[strings.ToLower(name)] = struct{}{}
		}
		if len(members) == 0 {
			return nil, errors.New("enterprise has no visible organizations; no settings saved")
		}
		names := strings.Fields(string(data))
		visibility := target.visibility
		if visibility == defaultsVisibilitySel {
			hasSelectedEnterprise = true
			names, visibility = nil, defaultsVisibilityPriv
			for _, name := range selected {
				if _, exists := members[strings.ToLower(name)]; exists {
					names = append(names, name)
					selectedMembers[strings.ToLower(name)] = struct{}{}
				}
			}
			if len(names) == 0 {
				return nil, errors.New("each selected enterprise target requires at least one visible --selected member organization")
			}
		}
		for _, name := range names {
			add(defaultsTarget{scope: defaultsScopeOrg, org: name, visibility: visibility})
		}
	}
	if hasSelectedEnterprise {
		for _, name := range selected {
			if _, exists := selectedMembers[strings.ToLower(name)]; !exists {
				return nil, errors.New("--selected organization is not a visible member of any target enterprise")
			}
		}
	}
	return result, nil
}

func runEnvOTEL(ctx context.Context, targets []defaultsTarget, o envOTELOptions, headers string) error {
	for _, target := range targets {
		if target.scope == defaultsScopeEnt {
			return errors.New("enterprise targets must be expanded to organizations before configuration")
		}
	}
	selections, err := envOTELOrgSelections(targets, o.selected)
	if err != nil {
		return err
	}
	probeURL, parsedHeaders, err := envOTELValidate(o.endpoint, o.protocol, headers)
	if err != nil {
		return err
	}
	if err := envOTELProbe(ctx, probeURL, parsedHeaders, o.protocol); err != nil {
		return err
	}
	// Persist exactly the validated headers, including Sentry's normalized name.
	keys := make([]string, 0, len(parsedHeaders))
	for key := range parsedHeaders {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	pairs := make([]string, 0, len(keys))
	for _, key := range keys {
		pairs = append(pairs, url.PathEscape(key)+"="+url.PathEscape(parsedHeaders.Get(key)))
	}
	headers = strings.Join(pairs, ",")
	if len(headers) > 48*1024 {
		return errors.New("canonical OTLP headers exceed GitHub's 48 KiB secret limit; no settings saved")
	}
	plans, err := envOTELPrepare(ctx, targets, selections)
	if err != nil {
		return err
	}
	for i, p := range plans {
		if err := envOTELSave(ctx, p, o, headers); err != nil {
			return fmt.Errorf("save failed for %s (target %d of %d); earlier settings may be saved: %w", p.target.displayName(), i+1, len(plans), err)
		}
	}
	return nil
}

// Preflight every target before the first write, including selected membership.
func envOTELPrepare(ctx context.Context, targets []defaultsTarget, selections map[string][]string) ([]envOTELPlan, error) {
	var plans []envOTELPlan
	for _, target := range targets {
		keyPath := strings.TrimSuffix(target.variablesEndpoint(), "variables") + "secrets/public-key"
		data, err := envOTELRunGH(ctx, nil, "api", keyPath)
		if err != nil {
			return nil, fmt.Errorf("preflight %s: %w", target.displayName(), err)
		}
		p := envOTELPlan{target: target}
		if json.Unmarshal(data, &p.key) != nil || p.key.ID == "" {
			return nil, errors.New("invalid GitHub public key response")
		}
		if _, err := encryptWithPublicKey(p.key.Key, ""); err != nil {
			return nil, errors.New("invalid GitHub encryption key")
		}
		if _, err := envOTELRunGH(ctx, nil, "api", target.variablesEndpoint(), "--jq", ".total_count"); err != nil {
			return nil, fmt.Errorf("preflight %s variable access: %w", target.displayName(), err)
		}
		if target.visibility == defaultsVisibilitySel {
			p.ids, err = envOTELSelected(ctx, target, selections[strings.ToLower(target.org)])
			if err != nil {
				return nil, fmt.Errorf("preflight %s selected repositories: %w", target.displayName(), err)
			}
		}
		if err := envOTELCheckOverrides(ctx, p, selections[strings.ToLower(target.org)]); err != nil {
			return nil, fmt.Errorf("preflight %s: %w", target.displayName(), err)
		}
		plans = append(plans, p)
	}
	return plans, nil
}

func envOTELSave(ctx context.Context, p envOTELPlan, o envOTELOptions, headers string) error {
	if err := envOTELSecret(ctx, p, compilerenv.DefaultOTLPEndpoint, envOTELDisabledEndpoint); err != nil {
		return err
	}
	if err := envOTELSecret(ctx, p, compilerenv.DefaultOTLPHeaders, headers); err != nil {
		return fmt.Errorf("export may remain disabled; rerun to finish configuration: %w", err)
	}
	for _, setting := range []struct{ name, value string }{
		{compilerenv.OTLPProtocol, o.protocol},
		{compilerenv.OTLPMetrics, strconv.FormatBool(o.metrics)},
	} {
		if err := envOTELVariable(ctx, p.target, p.ids, setting.name, setting.value); err != nil {
			return fmt.Errorf("export may remain disabled; rerun to finish configuration: %w", err)
		}
	}
	if err := envOTELSecret(ctx, p, compilerenv.DefaultOTLPEndpoint, o.endpoint); err != nil {
		return fmt.Errorf("export may remain disabled; rerun to finish configuration: %w", err)
	}
	return nil
}

func envOTELOrgSelections(targets []defaultsTarget, names []string) (map[string][]string, error) {
	selections := map[string][]string{}
	for _, target := range targets {
		if target.scope == defaultsScopeOrg && target.visibility == defaultsVisibilitySel {
			selections[strings.ToLower(target.org)] = nil
		}
	}
	if len(selections) == 0 {
		return selections, nil
	}
	for _, name := range names {
		owner, repo, found := strings.Cut(name, "/")
		if !found || !envOTELIdentifier.MatchString(owner) || !envOTELRepositoryName.MatchString(repo) || repo == "." || repo == ".." {
			return nil, errors.New("--selected must use owner/repo for organization targets")
		}
		owner = strings.ToLower(owner)
		if _, ok := selections[owner]; !ok {
			return nil, errors.New("--selected repository does not belong to any selected target organization")
		}
		selections[owner] = append(selections[owner], name)
	}
	for _, names := range selections {
		if len(names) == 0 {
			return nil, errors.New("each selected organization target requires at least one --selected repository")
		}
	}
	return selections, nil
}

func envOTELAccess(payload map[string]any, target defaultsTarget, ids []int64) {
	if target.scope != defaultsScopeRepo {
		payload["visibility"] = target.visibility
		if target.visibility == defaultsVisibilitySel {
			key := "selected_repository_ids"
			payload[key] = ids
		}
	}
}

func envOTELVariable(ctx context.Context, target defaultsTarget, ids []int64, name, value string) error {
	// List names rather than attempting a mutation to detect existence.
	data, err := envOTELRunGH(ctx, nil, "api", target.variablesEndpoint(), "--paginate", "--jq", ".variables[].name")
	if err != nil {
		return fmt.Errorf("%s variable access: %w", target.displayName(), err)
	}
	method, path := "POST", target.variablesEndpoint()
	for existing := range strings.FieldsSeq(string(data)) {
		if existing == name {
			method, path = "PATCH", target.variableEndpoint(name)
		}
	}
	payload := map[string]any{"name": name, "value": value}
	envOTELAccess(payload, target, ids)
	body, err := json.Marshal(payload)
	if err != nil {
		return errors.New("failed to encode OTLP variable")
	}
	_, err = envOTELRunGH(ctx, body, "api", "-X", method, path, "--input", "-")
	if err != nil {
		return fmt.Errorf("%s variable update: %w", target.displayName(), err)
	}
	return nil
}

func envOTELSelected(ctx context.Context, target defaultsTarget, names []string) ([]int64, error) {
	var ids []int64
	for _, name := range names {
		owner, repo, found := strings.Cut(name, "/")
		if !found || !strings.EqualFold(owner, target.org) || !envOTELRepositoryName.MatchString(repo) || repo == "." || repo == ".." {
			return nil, errors.New("--selected must name repositories belonging to each target organization")
		}
		data, err := envOTELRunGH(ctx, nil, "api", "repos/"+name, "--jq", ".id")
		if err != nil {
			return nil, fmt.Errorf("%s selected repository %s: %w", target.displayName(), name, err)
		}
		id, err := strconv.ParseInt(strings.TrimSpace(string(data)), 10, 64)
		if err != nil || id <= 0 {
			return nil, errors.New("invalid GitHub selected target ID")
		}
		ids = append(ids, id)
	}
	return ids, nil
}
