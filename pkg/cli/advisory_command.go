package cli

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/github/gh-aw/pkg/workflow"
	"github.com/spf13/cobra"
)

const maxAdvisoryPatchBytes = 4 << 20

var (
	advisoryIDPattern   = regexp.MustCompile(`^GHSA(-[23456789cfghjmpqrvwx]{4}){3}$`)
	advisorySHA         = regexp.MustCompile(`^[0-9a-f]{40}$`)
	advisoryRepoPattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9-]{0,38}/[a-zA-Z0-9_.-]{1,100}$`)
	advisoryRefPattern  = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9/_.-]{0,199}$`)
)

type advisoryHandoff struct {
	upstream, id, base, baseBranch, branch, filename, approval string
	sideRepo                                                   string
	runID, artifactID                                          int64
}

type advisoryFork struct {
	ID       int64  `json:"id"`
	FullName string `json:"full_name"`
	Private  bool   `json:"private"`
}

// NewAdvisoryCommand creates the local security advisory handoff commands.
func NewAdvisoryCommand() *cobra.Command {
	cmd := &cobra.Command{Use: "advisory", Short: "Review and transfer a fix to a private advisory fork"}
	cmd.AddCommand(newAdvisoryHandoffCommand(false), newAdvisoryHandoffCommand(true))
	return cmd
}

func newAdvisoryHandoffCommand(transfer bool) *cobra.Command {
	var opts advisoryHandoff
	cmd := &cobra.Command{
		Use:   "inspect <GHSA-ID>",
		Short: "Review a patch and get its transfer approval digest",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			opts.id = strings.Join(args, "")
			if prefix, suffix, ok := strings.Cut(opts.id, "-"); ok && strings.EqualFold(prefix, "GHSA") {
				opts.id = "GHSA-" + strings.ToLower(suffix)
			}
			return opts.run(cmd, transfer)
		},
	}
	if transfer {
		cmd.Use = "transfer <GHSA-ID>"
		cmd.Short = "Push the approved patch to a new private advisory fork branch"
		cmd.Flags().StringVar(&opts.approval, "approve", "", "Approval digest produced by inspect")
		_ = cmd.MarkFlagRequired("approve")
	}
	cmd.Flags().StringVar(&opts.upstream, "repo", "", "Upstream GitHub repository (OWNER/REPO)")
	cmd.Flags().StringVar(&opts.base, "base", "", "Exact tested base commit SHA")
	cmd.Flags().StringVar(&opts.baseBranch, "base-branch", "", "Branch in the advisory fork to check against the tested base")
	cmd.Flags().StringVar(&opts.branch, "branch", "", "New branch in the advisory fork")
	cmd.Flags().StringVar(&opts.filename, "patch", "", "Path to the reviewed git diff")
	cmd.Flags().StringVar(&opts.sideRepo, "side-repo", "", "Private source repository (OWNER/REPO)")
	cmd.Flags().Int64Var(&opts.runID, "run", 0, "Source workflow run ID")
	cmd.Flags().Int64Var(&opts.artifactID, "artifact", 0, "Exact source artifact ID")
	cmd.MarkFlagsRequiredTogether("side-repo", "run", "artifact")
	cmd.MarkFlagsMutuallyExclusive("patch", "side-repo")
	cmd.MarkFlagsOneRequired("patch", "side-repo")
	for _, flag := range []string{"repo", "base", "base-branch", "branch"} {
		_ = cmd.MarkFlagRequired(flag)
	}
	return cmd
}

func (o advisoryHandoff) run(cmd *cobra.Command, transfer bool) error {
	if !advisoryIDPattern.MatchString(o.id) || !validAdvisoryRepo(o.upstream) || !advisorySHA.MatchString(o.base) {
		return errors.New("provide a GHSA ID, a GitHub OWNER/REPO, and an exact 40-character base SHA")
	}
	if !advisoryRefPattern.MatchString(o.baseBranch) || !advisoryRefPattern.MatchString(o.branch) || o.baseBranch == o.branch {
		return errors.New("provide distinct branch names for --base-branch and --branch")
	}
	patch, err := o.loadPatch(cmd.Context())
	if err != nil {
		return err
	}
	fork, err := o.resolveFork(cmd.Context())
	if err != nil {
		return err
	}
	approval := o.approvalDigest(fork, patch)
	if !transfer {
		output := cmd.OutOrStdout()
		if _, err := fmt.Fprintf(output, "Advisory: %s/%s\nPrivate fork: %s\nBase: %s (%s)\nNew branch: %s\n\n", o.upstream, o.id, fork.FullName, o.baseBranch, o.base, o.branch); err != nil {
			return err
		}
		if o.sideRepo != "" {
			if _, err := fmt.Fprintf(output, "Verified source: %s, run %d, artifact %d\n\n", o.sideRepo, o.runID, o.artifactID); err != nil {
				return err
			}
		}
		if _, err := output.Write(patch); err != nil {
			return err
		}
		_, err = fmt.Fprintf(output, "\nApproval digest: %s\n", approval)
		return err
	}
	if approval != o.approval {
		return errors.New("approval digest does not match this patch and destination; run advisory inspect again")
	}
	if err := o.pushPatch(cmd.Context(), fork, patch); err != nil {
		return err
	}
	_, err = fmt.Fprintf(cmd.OutOrStdout(), "Branch %s is ready in the private fork. Open the PR from https://github.com/%s/security/advisories/%s\n", o.branch, o.upstream, o.id)
	return err
}

func validAdvisoryRepo(repo string) bool {
	_, name, _ := strings.Cut(repo, "/")
	return advisoryRepoPattern.MatchString(repo) && name != "." && name != ".." && !strings.HasSuffix(strings.ToLower(name), ".git")
}

func (o advisoryHandoff) resolveFork(ctx context.Context) (advisoryFork, error) {
	out, err := workflow.RunGHContextWithHost(ctx, "Verifying the private advisory fork", "github.com", "api", "--hostname", "github.com",
		path.Join("repos", o.upstream, "security-advisories", o.id))
	if err != nil {
		return advisoryFork{}, errors.New("could not read the advisory on github.com; verify the repository, advisory ID, and collaborator access")
	}
	return o.parseFork(out)
}

func (o advisoryHandoff) parseFork(out []byte) (advisoryFork, error) {
	var advisory struct {
		ID          string       `json:"ghsa_id"`
		State       string       `json:"state"`
		PrivateFork advisoryFork `json:"private_fork"`
	}
	if err := json.Unmarshal(out, &advisory); err != nil || advisory.ID != o.id {
		return advisoryFork{}, errors.New("GitHub did not return the requested advisory")
	}
	if advisory.State != "draft" && advisory.State != "triage" {
		return advisoryFork{}, errors.New("only unpublished draft or triage advisories are supported")
	}
	fork := advisory.PrivateFork
	// GitHub may report fork:false for a temporary advisory fork. The advisory's
	// private_fork association, not the generic repository fork flag, identifies it.
	if fork.ID <= 0 || !fork.Private || !validAdvisoryRepo(fork.FullName) || strings.EqualFold(fork.FullName, o.upstream) {
		return advisoryFork{}, errors.New("the advisory has no verified temporary private fork; create or check it in GitHub before transferring")
	}
	return fork, nil
}

func (o advisoryHandoff) approvalDigest(fork advisoryFork, patch []byte) string {
	header := fmt.Appendf(nil, "gh-aw-advisory-v2\n%s\n%s\n%d\n%s\n%s\n%s\n%s\n%s\n%d\n%d\n", o.upstream, o.id, fork.ID, fork.FullName, o.base, o.baseBranch, o.branch, o.sideRepo, o.runID, o.artifactID)
	digest := sha256.Sum256(append(header, patch...))
	return hex.EncodeToString(digest[:])
}

func readAdvisoryPatch(filename string) ([]byte, error) {
	info, err := os.Stat(filename)
	if err != nil || !info.Mode().IsRegular() || info.Size() > maxAdvisoryPatchBytes {
		return nil, fmt.Errorf("provide a regular patch file of at most %d bytes", maxAdvisoryPatchBytes)
	}
	file, err := os.Open(filename)
	if err != nil {
		return nil, fmt.Errorf("open patch: %w", err)
	}
	patch, err := io.ReadAll(io.LimitReader(file, maxAdvisoryPatchBytes+1))
	if err := errors.Join(err, file.Close()); err != nil {
		return nil, fmt.Errorf("read patch: %w", err)
	}
	return validateAdvisoryPatch(patch)
}

func validateAdvisoryPatch(patch []byte) ([]byte, error) {
	if len(patch) > maxAdvisoryPatchBytes || !bytes.HasPrefix(patch, []byte("diff --git ")) || !utf8.Valid(patch) {
		return nil, errors.New("provide a bounded, UTF-8 git diff")
	}
	for _, char := range string(patch) {
		if char != '\n' && char != '\t' && (unicode.IsControl(char) || unicode.Is(unicode.Cf, char)) {
			return nil, errors.New("patch contains control characters that cannot be safely reviewed")
		}
	}
	for line := range bytes.SplitSeq(patch, []byte("\n")) {
		if bytes.HasPrefix(line, []byte("GIT binary patch")) || bytes.HasPrefix(line, []byte("Binary files ")) {
			return nil, errors.New("binary patches are not supported")
		}
	}
	return patch, nil
}
