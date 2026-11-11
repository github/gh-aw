package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"maps"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/github/gh-aw/pkg/workflow"
)

var connectorSetupRepositoryPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9-]{0,38}/[A-Za-z0-9_.-]{1,100}$`)

type connectorSetupRequirement struct {
	kind string
	name string
}

func validateConnectorSetupRepository(repo string) error {
	if !connectorSetupRepositoryPattern.MatchString(repo) {
		return errors.New("--repo must name an explicit OWNER/REPO for read-only connector setup checks")
	}
	_, name, _ := strings.Cut(repo, "/")
	if name == "." || name == ".." {
		return errors.New("--repo must name an explicit OWNER/REPO for read-only connector setup checks")
	}
	return nil
}

func connectorSetupRequirements(registration workflow.ConnectorRegistration, phase string) []connectorSetupRequirement {
	requirements := map[connectorSetupRequirement]struct{}{}
	addReference := func(reference string) {
		match := connectorIdentityReference.FindStringSubmatch(reference)
		if len(match) != 3 {
			return
		}
		var captures [3]string
		copy(captures[:], match)
		kind := "secret"
		if captures[1] == "vars" {
			kind = "variable"
		}
		requirements[connectorSetupRequirement{kind: kind, name: strings.ToUpper(captures[2])}] = struct{}{}
	}
	for _, row := range developmentConnectorCredentialRows(registration, phase) {
		switch row.Route {
		case "token", "trusted input":
			requirements[connectorSetupRequirement{kind: "secret", name: row.Reference}] = struct{}{}
		case "GitHub App":
			app := registration.GitHubApps[row.Credential]
			addReference(app.AppID)
			addReference(app.PrivateKey)
		}
	}
	result := slices.Collect(maps.Keys(requirements))
	slices.SortFunc(result, func(left, right connectorSetupRequirement) int {
		if order := strings.Compare(left.kind, right.kind); order != 0 {
			return order
		}
		return strings.Compare(left.name, right.name)
	})
	return result
}

func checkConnectorSetupRequirements(ctx context.Context, output io.Writer, registration workflow.ConnectorRegistration, phase, repo string) error {
	requirements := connectorSetupRequirements(registration, phase)
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	existing := map[string][]string{}
	for _, requirement := range requirements {
		if _, checked := existing[requirement.kind]; checked {
			continue
		}
		var names []string
		var err error
		if requirement.kind == "secret" {
			names, err = listBootstrapRepoSecretNames(ctx, repo)
		} else {
			names, err = listBootstrapRepoVariableNames(ctx, repo)
		}
		if err != nil {
			return fmt.Errorf("connector setup could not check repository %s names; missing requirements are unknown: %w", requirement.kind, err)
		}
		existing[requirement.kind] = names
	}
	var text strings.Builder
	fmt.Fprintf(&text, "\nRepository credential requirements for %s (names only):\n", repo) //nolint:fprintferrorunchecked // strings.Builder cannot fail.
	missing := 0
	for _, requirement := range requirements {
		if slices.ContainsFunc(existing[requirement.kind], func(name string) bool { return strings.EqualFold(name, requirement.name) }) {
			continue
		}
		missing++
		fmt.Fprintf(&text, "  Missing %s %s: gh %s set %s --repo %s\n", requirement.kind, requirement.name, requirement.kind, requirement.name, repo) //nolint:fprintferrorunchecked // strings.Builder cannot fail.
	}
	fmt.Fprintf(&text, "%d of %d declared repository settings are missing.\n", missing, len(requirements)) //nolint:fprintferrorunchecked // strings.Builder cannot fail.
	text.WriteString("Only provision routes you select; this checks declared phase requirements, not workflow capability selection.\n")
	text.WriteString("No values were displayed and no settings were changed. Organization/environment settings were not checked.\n")
	text.WriteString("Name presence does not verify token validity, app installation/permissions, OIDC trust, or provider access.\n")
	_, err := io.WriteString(output, text.String())
	return err
}
