package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"path"
	"strings"

	"github.com/github/gh-aw/pkg/workflow"
	"github.com/spf13/cobra"
)

const personalAdvisoryRepo = "gh-aw-advisories"

type advisoryAccount struct {
	ID    int64  `json:"id"`
	Login string `json:"login"`
	Type  string `json:"type"`
}

type advisorySideRepository struct {
	ID         int64           `json:"id"`
	FullName   string          `json:"full_name"`
	Visibility string          `json:"visibility"`
	Fork       bool            `json:"fork"`
	Archived   bool            `json:"archived"`
	Owner      advisoryAccount `json:"owner"`
}

func newAdvisorySetupCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "setup",
		Short: "Experimental: create or reuse your private advisory side repository",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			account, err := currentAdvisoryAccount(cmd.Context())
			if err != nil {
				return err
			}
			repo, err := account.sideRepository(cmd.Context(), true)
			if err != nil {
				return err
			}
			_, err = fmt.Fprintf(cmd.OutOrStdout(), "Private side repository: https://github.com/%s\nInstall the reviewed workflow and its upstream-only checkout credential before dispatching.\n", repo.FullName)
			return err
		},
	}
}

func currentAdvisoryAccount(ctx context.Context) (advisoryAccount, error) {
	var account advisoryAccount
	body, err := advisorySetupAPI(ctx, "user")
	if err != nil || json.Unmarshal(body, &account) != nil || account.ID <= 0 ||
		account.Type != "User" || strings.Contains(account.Login, "/") ||
		!validAdvisoryRepo(path.Join(account.Login, personalAdvisoryRepo)) {
		return advisoryAccount{}, errors.New("could not verify a personal GitHub.com account; sign in with gh auth login --hostname github.com")
	}
	return account, nil
}

func (a advisoryAccount) sideRepository(ctx context.Context, create bool) (advisorySideRepository, error) {
	name := path.Join(a.Login, personalAdvisoryRepo)
	body, err := advisorySetupAPI(ctx, path.Join("repos", name))
	var exitErr *exec.ExitError
	if create && errors.As(err, &exitErr) && strings.HasSuffix(strings.TrimSpace(string(exitErr.Stderr)), "(HTTP 404)") {
		body, err = advisorySetupAPI(ctx, "user/repos", "--method", "POST",
			"--raw-field", "name="+personalAdvisoryRepo, "--field", "private=true",
			"--field", "has_issues=false", "--field", "has_wiki=false")
		if err != nil {
			return advisorySideRepository{}, errors.New("could not create your private advisory repository; check repository creation access or rerun advisory setup if it already exists")
		}
	}
	if err != nil {
		return advisorySideRepository{}, errors.New("could not read your private advisory repository; run gh aw advisory setup and check repository access")
	}
	var repo advisorySideRepository
	if json.Unmarshal(body, &repo) != nil || repo.ID <= 0 || repo.Visibility != "private" ||
		repo.Fork || repo.Archived || !strings.EqualFold(name, repo.FullName) || repo.Owner.ID != a.ID || repo.Owner.Type != "User" {
		return advisorySideRepository{}, errors.New("the advisory side repository must be private, active, and owned by your personal GitHub account; check its settings")
	}
	if err := a.verifySideRepositoryAccess(ctx, repo.FullName); err != nil {
		return advisorySideRepository{}, err
	}
	return repo, nil
}

func (a advisoryAccount) verifySideRepositoryAccess(ctx context.Context, repo string) error {
	// A first page of two entries suffices: there can be at most one owner entry.
	for _, endpoint := range []string{"collaborators?per_page=2", "invitations?per_page=1"} {
		var entries []struct {
			ID int64 `json:"id"`
		}
		body, err := advisorySetupAPI(ctx, path.Join("repos", repo, endpoint))
		if err != nil || json.Unmarshal(body, &entries) != nil || entries == nil {
			return errors.New("could not verify personal side repository access; check GitHub CLI access to collaborators and invitations")
		}
		for _, entry := range entries {
			if endpoint != "collaborators?per_page=2" || entry.ID != a.ID {
				return errors.New("the personal advisory side repository is shared or has pending invitations; review repository access before reuse")
			}
		}
	}
	return nil
}

func advisorySetupAPI(ctx context.Context, endpoint string, options ...string) ([]byte, error) {
	args := append([]string{"api", "--hostname", "github.com", endpoint}, options...)
	return workflow.RunGHContextWithHost(ctx, "Checking personal advisory setup", "github.com", args...)
}
