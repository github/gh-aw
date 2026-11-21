package cli

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path"
	"regexp"
	"slices"
	"strings"

	"github.com/github/gh-aw/pkg/workflow"
)

var privateCloneSecretNamePattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

func validatePrivateCloneSecretNames(names []string) error {
	for _, name := range names {
		if !privateCloneSecretNamePattern.MatchString(name) || strings.HasPrefix(strings.ToUpper(name), "GITHUB_") {
			return fmt.Errorf("invalid --secret %q: expected an environment variable name that does not start with GITHUB_", name)
		}
	}
	return nil
}

// preparePrivateCloneActivation resolves the starter workflow (empty with --no-start)
// and reads secret values before any remote side effect.
func preparePrivateCloneActivation(files map[string][]byte, opts privateCloneOptions) (string, map[string]string, error) {
	starter := ""
	if !opts.NoStart {
		var err error
		if starter, err = resolvePrivateCloneStarter(files, opts.Starter); err != nil {
			return "", nil, err
		}
	}
	secrets, err := readPrivateCloneSecretValues(opts.Secrets)
	if err != nil {
		return "", nil, err
	}
	return starter, secrets, nil
}

// resolvePrivateCloneStarter returns the starter workflow's compiled lock file name,
// selecting the only root workflow when no starter was named.
func resolvePrivateCloneStarter(files map[string][]byte, name string) (string, error) {
	if name == "" {
		var roots []string
		for file := range files {
			if path.Ext(file) == ".md" && !strings.Contains(file, "/") {
				roots = append(roots, file)
			}
		}
		if len(roots) != 1 {
			slices.Sort(roots)
			return "", fmt.Errorf("the workflow bundle has %d root workflows (%s); select one with --starter-workflow or pass --no-start", len(roots), strings.Join(roots, ", "))
		}
		name = roots[0] //nolint:uncheckedsliceindex // len(roots) == 1 is checked above.
	} else if path.Ext(name) == "" {
		name += ".md"
	}
	return validatePrivateCloneStarterWorkflow(files, name)
}

// readPrivateCloneSecretValues reads each --secret value from the operator's
// environment so secret values never appear in argv or shell history.
func readPrivateCloneSecretValues(names []string) (map[string]string, error) {
	values := make(map[string]string, len(names))
	for _, name := range names {
		value := os.Getenv(name) //nolint:osgetenvlibrary // --secret values are intentionally read from the operator environment, never argv.
		if value == "" {
			return nil, fmt.Errorf("--secret %s requires a nonempty %s environment variable", name, name)
		}
		values[name] = value
	}
	return values, nil
}

func checkPrivateCloneSecretCoverage(required []string, supplied map[string]string, starting bool) error {
	requiredSet := make(map[string]struct{}, len(required))
	var missing []string
	for _, name := range required {
		requiredSet[name] = struct{}{}
		if _, ok := supplied[name]; !ok {
			missing = append(missing, name)
		}
	}
	for name := range supplied {
		if _, ok := requiredSet[name]; !ok {
			return fmt.Errorf("--secret %s is not referenced by the workflow bundle; supply only required secrets", name)
		}
	}
	if starting && len(missing) > 0 {
		return fmt.Errorf("the workflow bundle requires secrets %s; export each value and pass --secret NAME, or pass --no-start", strings.Join(missing, ", "))
	}
	return nil
}

func setPrivateCloneSecrets(ctx context.Context, destination string, secrets map[string]string) error {
	names := make([]string, 0, len(secrets))
	for name := range secrets {
		names = append(names, name)
	}
	slices.Sort(names)
	for _, name := range names {
		cmd := workflow.ExecGHContext(ctx, "secret", "set", name, "--repo", destination)
		cmd.Stdin = bytes.NewBufferString(secrets[name])
		if _, err := cmd.Output(); err != nil {
			return fmt.Errorf("source was pushed to %s, but secret %s could not be set (Actions remains disabled): %w", destination, name, err)
		}
	}
	return nil
}

func activatePrivateClone(ctx context.Context, destination, starter string, secrets map[string]string) error {
	if err := setPrivateCloneSecrets(ctx, destination, secrets); err != nil {
		return err
	}
	if starter == "" {
		return nil
	}
	return startPrivateCloneInvestigation(ctx, destination, starter)
}
