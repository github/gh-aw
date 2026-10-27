package cli

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/github/gh-aw/pkg/tty"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

type orgRolloutKey struct{}

type orgRolloutOptions struct {
	command, enterprise string
	args                []string
	workflows           []string
	cacheDir            string
	interactive, dryRun bool
	replace, worker     bool
	concurrency         int
}

func orgRolloutFromContext(ctx context.Context) *orgRolloutOptions {
	if opts, ok := ctx.Value(orgRolloutKey{}).(*orgRolloutOptions); ok {
		return opts
	}
	return nil
}

func addOrgRolloutFlags(cmd *cobra.Command) {
	cmd.Flags().String("enterprise", "", "Target all accessible organizations in an enterprise (requires enterprise organization visibility)")
	cmd.Flags().Bool("interactive", false, "Select repositories and watch live PR progress in a TUI (requires --org or --enterprise)")
	cmd.Flags().Bool("dry-run", false, "Preview an org or enterprise rollout without creating or closing PRs or issues")
	cmd.Flags().Int("concurrency", 3, "Maximum concurrent repository workers (1-10); starts are paced to protect API limits")
	cmd.Flags().Bool("replace-existing", false, "Replace existing rollout PRs, closing them only after a replacement is created")
	cmd.Flags().Bool("rollout-worker", false, "Run an isolated repository rollout worker")
	cmd.Flags().String("rollout-cache-dir", "", "Share read-only dependency lookups between rollout workers")
	_ = cmd.Flags().MarkHidden("rollout-worker")
	_ = cmd.Flags().MarkHidden("rollout-cache-dir")
}

func configureOrgRollout(cmd *cobra.Command, org, repo string, createPR, createIssue bool) (string, error) {
	enterprise, _ := cmd.Flags().GetString("enterprise")
	interactive, _ := cmd.Flags().GetBool("interactive")
	dryRun, _ := cmd.Flags().GetBool("dry-run")
	replace, _ := cmd.Flags().GetBool("replace-existing")
	worker, _ := cmd.Flags().GetBool("rollout-worker")
	cacheDir, _ := cmd.Flags().GetString("rollout-cache-dir")
	concurrency, _ := cmd.Flags().GetInt("concurrency")
	if cmd.Flags().Changed("org") && strings.TrimSpace(org) == "" {
		return "", errors.New("--org cannot be empty")
	}
	if cmd.Flags().Changed("enterprise") && strings.TrimSpace(enterprise) == "" {
		return "", errors.New("--enterprise cannot be empty")
	}
	if concurrency < 1 || concurrency > 10 {
		return "", errors.New("--concurrency must be between 1 and 10")
	}
	if enterprise != "" && (org != "" || repo != "") {
		return "", errors.New("--enterprise cannot be combined with --org or --repo")
	}
	if enterprise != "" && !isValidOrgSlug(enterprise) {
		return "", fmt.Errorf("invalid --enterprise slug %q", enterprise)
	}
	bulk := org != "" || enterprise != ""
	if worker && (bulk || repo == "" || (!createPR && !dryRun)) {
		return "", errors.New("--rollout-worker requires --repo and either --create-pull-request or --dry-run, without --org or --enterprise")
	}
	if cacheDir != "" && !worker {
		return "", errors.New("--rollout-cache-dir is only valid for an isolated rollout worker")
	}
	if !bulk && !worker {
		if interactive || dryRun || replace || cmd.Flags().Changed("concurrency") {
			return "", errors.New("--interactive, --dry-run, --replace-existing, and --concurrency require --org or --enterprise")
		}
		return org, nil
	}
	if interactive && (!tty.IsStdinTerminal() || !tty.IsStderrTerminal() || isRunningInCIFn()) {
		return "", errors.New("--interactive requires a terminal on stdin and stderr; use --yes for unattended rollouts")
	}
	if replace && (!createPR || createIssue) && !worker {
		return "", errors.New("--replace-existing requires --create-pull-request")
	}
	if audit := cmd.Flags().Lookup("audit"); audit != nil && audit.Value.String() == "true" {
		return "", errors.New("--audit cannot be combined with an org or enterprise rollout")
	}
	opts := &orgRolloutOptions{
		command: cmd.Name(), enterprise: enterprise, interactive: interactive,
		dryRun: dryRun, replace: replace, worker: worker, concurrency: concurrency, cacheDir: cacheDir,
	}
	opts.args = orgRolloutWorkerArgs(cmd)
	opts.workflows = slices.Clone(cmd.Flags().Args())
	cmd.SetContext(context.WithValue(cmd.Context(), orgRolloutKey{}, opts))
	if enterprise != "" {
		return enterprise, nil
	}
	return org, nil
}

func orgRolloutWorkerArgs(cmd *cobra.Command) []string {
	excluded := []string{"org", "enterprise", "repo", "repos", "interactive", "dry-run", "concurrency",
		"replace-existing", "rollout-worker", "rollout-cache-dir", "create-pull-request", "pr", "create-issue", "yes"}
	args := []string{cmd.Name(), "--rollout-worker", "--yes"}
	cmd.Flags().Visit(func(flag *pflag.Flag) {
		if slices.Contains(excluded, flag.Name) {
			return
		}
		if value, ok := flag.Value.(pflag.SliceValue); ok {
			// Clear the default first so an explicitly empty slice stays empty.
			args = append(args, "--"+flag.Name+"="+strings.Join(value.GetSlice(), ","))
		} else {
			args = append(args, "--"+flag.Name+"="+flag.Value.String())
		}
	})
	if cmd.Name() == "upgrade" && !cmd.Flags().Changed("skip-extension-upgrade") {
		args = append(args, "--skip-extension-upgrade")
	}
	return args
}
