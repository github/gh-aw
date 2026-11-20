package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
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
No upstream secrets, integrations, or git remote are transferred. Copilot inference
uses copilot-requests: write. Actions remains disabled: provision any additional
clone-scoped secrets and review organization secret access before enabling it.
This command never dispatches workflows or publishes fixes upstream.`,
		Example: `  gh aw private-clone acme/project security/project-fix-42 --workflows ./fix-workflows --alert 42
  gh aw private-clone acme/project security/project-advisory --workflows ./fix-workflows --advisory GHSA-2345-6789-cfgh --ref main`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			opts.Source, opts.Destination = args[0], args[1] //nolint:uncheckedsliceindex // Cobra ExactArgs(2) validates both positions before RunE.
			if err := configurePrivateClone(cmd.Context(), opts); err != nil {
				return err
			}
			if _, err := fmt.Fprintln(cmd.ErrOrStderr(), console.FormatSuccessMessage("Configured private remediation repository: "+trialRepositoryURL(opts.Destination))); err != nil {
				return err
			}
			_, err := fmt.Fprintln(cmd.ErrOrStderr(), console.FormatInfoMessage("Actions is disabled. Provision only clone-scoped secrets, review organization secret access, and enable Actions after reviewing the installed workflows. No workflows were dispatched."))
			return err
		},
	}
	cmd.Flags().StringVar(&opts.Workflows, "workflows", "", "Required local directory of workflow Markdown files and self-contained local imports")
	cmd.Flags().StringVar(&opts.Ref, "ref", "HEAD", "Upstream branch, tag, or commit to snapshot")
	cmd.Flags().IntSliceVar(&opts.Alerts, "alert", nil, "Code scanning alert number to download (repeatable or comma-separated)")
	cmd.Flags().StringSliceVar(&opts.Advisories, "advisory", nil, "Repository security advisory GHSA ID to download (repeatable or comma-separated)")
	_ = cmd.MarkFlagRequired("workflows")
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
	return nil
}

func configurePrivateClone(ctx context.Context, opts privateCloneOptions) error {
	if err := validatePrivateCloneOptions(opts); err != nil {
		return err
	}
	bundle, err := readPrivateCloneWorkflows(opts.Workflows)
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
	if err := installPrivateCloneBundle(dir, bundle, snapshot); err != nil {
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
	bootstrapSHA, err := commitPrivateCloneBootstrap(ctx, dir)
	if err != nil {
		return err
	}
	if err := publishPrivateClone(ctx, dir, opts.Destination); err != nil {
		return err
	}
	privateCloneLog.Printf("Configured workspace: repository=%s", opts.Destination)
	fmt.Fprintln(os.Stderr, console.FormatInfoMessage("Bootstrap SHA: "+strings.TrimSpace(string(bootstrapSHA))+". Export only changes after this commit; do not export the bootstrap or confidential context."))
	return nil
}

const privateCloneContextDir = ".github/private-clone"

func writePrivateCloneFile(root, name string, data []byte) error {
	path := filepath.Join(root, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o600)
}
