package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
)

type advisoryRecord struct {
	SideRepoID int64  `json:"side_repo_id"`
	Upstream   string `json:"upstream"`
	Base       string `json:"base"`
	BaseBranch string `json:"base_branch"`
	Branch     string `json:"branch"`
	RunID      int64  `json:"run_id"`
	ArtifactID int64  `json:"artifact_id"`
}

func newAdvisoryRecordCommand() *cobra.Command {
	var record advisoryRecord
	cmd := &cobra.Command{
		Use:   "record <GHSA-ID>",
		Short: "Experimental: save an exact artifact handoff for later review",
		Args:  advisoryIDArg,
		RunE: func(cmd *cobra.Command, args []string) error {
			id := normalizeAdvisoryID(strings.Join(args, ""))
			saved := record
			if saved.Branch == "" {
				saved.Branch = "advisory/" + strings.ToLower(id)
			}
			return saveAdvisoryHandoff(cmd, id, saved)
		},
	}
	cmd.Flags().StringVar(&record.Upstream, "repo", "", "Upstream GitHub repository (OWNER/REPO)")
	cmd.Flags().StringVar(&record.Base, "base", "", "Exact tested base commit SHA")
	cmd.Flags().StringVar(&record.BaseBranch, "base-branch", "", "Branch in the advisory fork to check against the tested base")
	cmd.Flags().StringVar(&record.Branch, "branch", "", "New branch in the advisory fork (default: advisory/<lowercase-ghsa-id>)")
	cmd.Flags().Int64Var(&record.RunID, "run", 0, "Source workflow run ID")
	cmd.Flags().Int64Var(&record.ArtifactID, "artifact", 0, "Exact source artifact ID")
	for _, flag := range []string{"repo", "base", "base-branch", "run", "artifact"} {
		_ = cmd.MarkFlagRequired(flag)
	}
	return cmd
}

func (r advisoryRecord) handoff(id string, repo advisorySideRepository) advisoryHandoff {
	return advisoryHandoff{
		id: id, upstream: r.Upstream, base: r.Base, baseBranch: r.BaseBranch, branch: r.Branch,
		sideRepo: repo.FullName, sideRepoID: repo.ID, runID: r.RunID, artifactID: r.ArtifactID,
	}
}

func saveAdvisoryHandoff(cmd *cobra.Command, id string, record advisoryRecord) error {
	ctx := cmd.Context()
	account, err := currentAdvisoryAccount(ctx)
	if err != nil {
		return err
	}
	repo, err := account.sideRepository(ctx, false)
	if err != nil {
		return err
	}
	handoff := record.handoff(id, repo)
	if err := handoff.validate(); err != nil {
		return err
	}
	if _, err := handoff.resolveFork(ctx); err != nil {
		return err
	}
	if _, err := handoff.loadPatch(ctx); err != nil {
		return err
	}
	record.SideRepoID = repo.ID
	filename, err := advisoryRecordPath(account.ID, id)
	if err != nil {
		return err
	}
	data, err := json.Marshal(record)
	if err != nil {
		return fmt.Errorf("encode advisory record: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(filename), 0o700); err != nil {
		return fmt.Errorf("create private advisory record directory: %w", err)
	}
	if err := writeSessionFileAtomically(filename, data); err != nil {
		return fmt.Errorf("save private advisory record: %w", err)
	}
	_, err = fmt.Fprintf(cmd.OutOrStdout(), "Saved %s locally. Review with gh aw advisory inspect %s.\n", id, id)
	return err
}

func loadAdvisoryHandoff(ctx context.Context, id string) (advisoryHandoff, error) {
	account, err := currentAdvisoryAccount(ctx)
	if err != nil {
		return advisoryHandoff{}, err
	}
	filename, err := advisoryRecordPath(account.ID, id)
	if err != nil {
		return advisoryHandoff{}, err
	}
	data, err := os.ReadFile(filename)
	if errors.Is(err, os.ErrNotExist) {
		return advisoryHandoff{}, fmt.Errorf("no saved handoff for %s; run gh aw advisory record %s --help", id, id)
	}
	if err != nil {
		return advisoryHandoff{}, fmt.Errorf("read private advisory record: %w", err)
	}
	var record advisoryRecord
	if json.Unmarshal(data, &record) != nil || record.SideRepoID <= 0 {
		return advisoryHandoff{}, errors.New("the saved advisory record is invalid; run gh aw advisory record again")
	}
	repo, err := account.sideRepository(ctx, false)
	if err != nil {
		return advisoryHandoff{}, err
	}
	if repo.ID != record.SideRepoID {
		return advisoryHandoff{}, errors.New("your side repository changed since this handoff was saved; verify the new source and run gh aw advisory record again")
	}
	return record.handoff(id, repo), nil
}

func advisoryRecordPath(accountID int64, id string) (string, error) {
	if accountID <= 0 || !advisoryIDPattern.MatchString(id) {
		return "", errors.New("advisory records require a verified GitHub account and a GHSA identifier")
	}
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("locate user configuration for advisory records: %w", err)
	}
	return filepath.Join(dir, "gh-aw", "advisories", strconv.FormatInt(accountID, 10), id+".json"), nil
}
