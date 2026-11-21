package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/github/gh-aw/pkg/console"
	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/repoutil"
	"github.com/spf13/cobra"
)

var privateCloneLog = logger.New("cli:private_clone")

var privateCloneAdvisoryPattern = regexp.MustCompile(`^GHSA-[23456789cfghjmpqrvwx]{4}-[23456789cfghjmpqrvwx]{4}-[23456789cfghjmpqrvwx]{4}$`)

type privateCloneOptions struct {
	Source      string
	Destination string
	Ref         string
	Workflows   string
	Starter     string
	NoStart     bool
	Secrets     []string
	Alerts      []int
	Advisories  []string
}

// NewPrivateCloneCommand creates a disposable, confidential remediation workspace.
func NewPrivateCloneCommand() *cobra.Command {
	var opts privateCloneOptions
	cmd := &cobra.Command{
		Use:   "private-clone <source-owner/repo> <destination-owner/repo>",
		Short: "Configure a private clone for confidential security fixes",
		Long: `Create a new standalone private repository for a single security remediation.

Supply a local directory containing your agentic workflows and their local imports.
Selected code scanning alerts and repository advisories are downloaded locally and
committed under .github/private-clone/ as separate JSON files. Workflows must use
this offline context instead of querying the original repository.

Inherited workflows are removed before your workflows are compiled and installed.
No upstream secrets, integrations, or git remote are transferred. After publishing,
secrets named with --secret are set from the operator's environment, Actions is
enabled, and the starter workflow is dispatched on main to start the investigation.
The starter defaults to the only root workflow in the bundle. Use --no-start to
leave Actions disabled for review instead. Before running this command, review the
workflow bundle and ambient organization secrets, Apps, and credentials.`,
		Example: `  gh aw private-clone acme/project security/project-fix-42 --workflows ./fix-workflows --alert 42
  BUILD_TOKEN=... gh aw private-clone acme/project security/project-advisory --workflows ./fix-workflows --starter-workflow investigate --secret BUILD_TOKEN --advisory GHSA-2345-6789-cfgh --ref main
  gh aw private-clone acme/project security/project-review --workflows ./fix-workflows --alert 42 --no-start`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			opts.Source, opts.Destination = args[0], args[1] //nolint:uncheckedsliceindex // Cobra ExactArgs(2) validates both positions before RunE.
			if err := configurePrivateClone(cmd.Context(), opts); err != nil {
				return err
			}
			if _, err := fmt.Fprintln(cmd.ErrOrStderr(), console.FormatSuccessMessage("Configured private remediation repository: "+trialRepositoryURL(opts.Destination))); err != nil {
				return err
			}
			next := "Actions is enabled and the starter investigation workflow was dispatched. Watch it with: gh run list --repo " + opts.Destination
			if opts.NoStart {
				next = "Actions remains disabled and no workflow was dispatched (--no-start). Review the repository, then enable Actions and run the starter workflow."
			}
			_, err := fmt.Fprintln(cmd.ErrOrStderr(), console.FormatInfoMessage(next))
			return err
		},
	}
	cmd.Flags().StringVar(&opts.Workflows, "workflows", "", "Required local directory of workflow Markdown files and self-contained local imports")
	cmd.Flags().StringVar(&opts.Starter, "starter-workflow", "", "Root workflow to dispatch after enabling Actions (defaults to the only root workflow)")
	cmd.Flags().BoolVar(&opts.NoStart, "no-start", false, "Leave Actions disabled and dispatch nothing so the repository can be reviewed first")
	cmd.Flags().StringSliceVar(&opts.Secrets, "secret", nil, "Repository secret to set from the same-named environment variable (repeatable)")
	cmd.Flags().StringVar(&opts.Ref, "ref", "HEAD", "Upstream branch, tag, or commit to snapshot")
	cmd.Flags().IntSliceVar(&opts.Alerts, "alert", nil, "Code scanning alert number to download (repeatable or comma-separated)")
	cmd.Flags().StringSliceVar(&opts.Advisories, "advisory", nil, "Repository security advisory GHSA ID to download (repeatable or comma-separated)")
	_ = cmd.MarkFlagRequired("workflows")
	cmd.MarkFlagsMutuallyExclusive("starter-workflow", "no-start")
	return cmd
}

func validatePrivateCloneOptions(opts privateCloneOptions) error {
	for _, repo := range []string{opts.Source, opts.Destination} {
		if !localRepoSlugPattern.MatchString(repo) {
			return fmt.Errorf("invalid repository %q: expected owner/repo", repo)
		}
		if _, _, err := repoutil.SplitRepoSlug(repo); err != nil {
			return fmt.Errorf("invalid repository %q: %w", repo, err)
		}
		for part := range strings.SplitSeq(repo, "/") {
			if part == "." || part == ".." || strings.HasPrefix(part, "-") {
				return fmt.Errorf("invalid repository %q: expected owner/repo", repo)
			}
		}
	}
	if strings.EqualFold(opts.Source, opts.Destination) {
		return errors.New("private-clone destination must differ from the upstream repository")
	}
	if opts.Ref == "" || strings.HasPrefix(opts.Ref, "-") {
		return errors.New("private-clone ref must be a nonempty branch, tag, or commit")
	}
	if len(opts.Alerts)+len(opts.Advisories) == 0 {
		return errors.New("private-clone requires at least one --alert or --advisory")
	}
	for _, number := range opts.Alerts {
		if number <= 0 {
			return errors.New("private-clone alert numbers must be positive integers")
		}
	}
	for _, id := range opts.Advisories {
		if !privateCloneAdvisoryPattern.MatchString(id) {
			return fmt.Errorf("invalid advisory ID %q: expected GHSA-xxxx-xxxx-xxxx", id)
		}
	}
	if opts.Workflows == "" {
		return errors.New("private-clone requires --workflows with an operator-provided local workflow directory")
	}
	if opts.Starter != "" && (strings.ContainsAny(opts.Starter, `/\`) || strings.HasPrefix(opts.Starter, "-") || strings.HasPrefix(opts.Starter, ".") ||
		(filepath.Ext(opts.Starter) != ".md" && filepath.Ext(opts.Starter) != "")) {
		return errors.New("--starter-workflow must name a root workflow, for example investigate or investigate.md")
	}
	return validatePrivateCloneSecretNames(opts.Secrets)
}

func configurePrivateClone(ctx context.Context, opts privateCloneOptions) error {
	if err := validatePrivateCloneOptions(opts); err != nil {
		return err
	}
	bundle, executables, err := readPrivateCloneWorkflows(opts.Workflows)
	if err != nil {
		return err
	}
	starter, secrets, err := preparePrivateCloneActivation(bundle, opts)
	if err != nil {
		return err
	}
	snapshot, err := downloadPrivateCloneContext(ctx, opts)
	if err != nil {
		return err
	}
	dir, err := os.MkdirTemp("", "gh-aw-private-clone-")
	if err != nil {
		return err
	}
	defer func() {
		if err := os.RemoveAll(dir); err != nil {
			fmt.Fprintln(os.Stderr, console.FormatWarningMessage("Could not remove private-clone scratch directory: "+err.Error()))
		}
	}()

	if err := preparePrivateCloneSource(ctx, dir, opts.Source, snapshot.SourceSHA); err != nil {
		return err
	}
	if err := installPrivateCloneBundle(dir, bundle, executables, snapshot); err != nil {
		return err
	}
	if err := withWorkingDir(dir, func() error {
		_, err := CompileWorkflows(ctx, CompileConfig{
			WorkflowDir: constants.WorkflowsDir,
			Strict:      true,
			Validate:    true,
			Approve:     true,
		})
		return err
	}); err != nil {
		return fmt.Errorf("compile private-clone workflows: %w", err)
	}
	if err := checkPrivateCloneSecretCoverage(snapshot.Secrets, secrets, !opts.NoStart); err != nil {
		return err
	}
	bootstrapSHA, err := commitPrivateCloneBootstrap(ctx, dir)
	if err != nil {
		return err
	}
	if err := publishPrivateClone(ctx, dir, opts.Destination); err != nil {
		return err
	}
	if err := activatePrivateClone(ctx, opts.Destination, starter, secrets); err != nil {
		return err
	}
	privateCloneLog.Printf("Configured workspace: repository=%s, starter=%s", opts.Destination, starter)
	fmt.Fprintln(os.Stderr, console.FormatInfoMessage("Bootstrap SHA: "+strings.TrimSpace(string(bootstrapSHA))+". Export only reviewed changes after this commit, never the bootstrap or confidential context."))
	return nil
}

const privateCloneContextDir = ".github/private-clone"

func writePrivateCloneFile(root, name string, data []byte) error {
	return writePrivateCloneFileMode(root, name, data, constants.FilePermSensitive)
}

// writePrivateCloneBundleFile installs an operator bundle file under the workflows
// directory, preserving the executable bit so git records scripts as executable.
func writePrivateCloneBundleFile(root, name string, data []byte, executables map[string]struct{}) error {
	perm := constants.FilePermSensitive
	if _, ok := executables[name]; ok {
		perm = constants.FilePermExecutable
	}
	return writePrivateCloneFileMode(root, path.Join(constants.WorkflowsDir, name), data, perm)
}

func writePrivateCloneFileMode(root, name string, data []byte, perm os.FileMode) error {
	target := filepath.Join(root, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(target), constants.DirPermOwnerOnly); err != nil {
		return err
	}
	if err := os.WriteFile(target, data, perm); err != nil {
		return err
	}
	// WriteFile does not change the mode of an existing file.
	return os.Chmod(target, perm)
}
