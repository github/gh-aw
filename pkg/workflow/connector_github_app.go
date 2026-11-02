package workflow

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"path"
	"regexp"
	"slices"
	"strings"

	"github.com/github/gh-aw/pkg/connectors"
	"github.com/github/gh-aw/pkg/sliceutil"
)

const connectorAppSecretPrefix = "GH_AW_CONNECTOR_APP_"

var (
	connectorAppIDPattern    = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]*$|^\$\{\{\s*(?:vars|secrets)\.[A-Za-z_][A-Za-z0-9_]*\s*\}\}$`)
	connectorAppKeyPattern   = regexp.MustCompile(`^\$\{\{\s*secrets\.[A-Za-z_][A-Za-z0-9_]*\s*\}\}$`)
	connectorAppOwnerPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9-]{0,38}$`)
	connectorAppRepoPattern  = regexp.MustCompile(`^[A-Za-z0-9_.-]+$`)
)

type ConnectorGitHubApp struct {
	GitHubAppConfig
	LegacyAppID string `json:"app-id,omitempty"`
	phase       string
}

func connectorAppSecret(alias, credential string) string {
	digest := sha256.Sum256([]byte(path.Join(alias, credential)))
	return fmt.Sprintf("%s%X", connectorAppSecretPrefix, digest[:16])
}

func connectorAppStepID(secret string) string {
	return "connector-app-" + strings.ToLower(strings.TrimPrefix(secret, connectorAppSecretPrefix))
}

func (app *ConnectorGitHubApp) validate(phase string) error {
	if phase != "agent" && phase != "safe-output" {
		return errors.New("connector github-app requires the agent or safe-output phase")
	}
	app.phase = phase
	if app.AppID == "" {
		app.AppID = app.LegacyAppID
	}
	if !connectorAppIDPattern.MatchString(app.AppID) || !connectorAppKeyPattern.MatchString(app.PrivateKey) {
		return errors.New("connector github-app requires a literal client-id or vars/secrets reference and a private-key secrets reference")
	}
	if app.IgnoreIfMissing {
		return errors.New("connector github-app cannot ignore missing credentials")
	}
	if app.Owner != "" && !connectorAppOwnerPattern.MatchString(app.Owner) {
		return errors.New("connector github-app.owner must be a literal installation owner")
	}
	if app.Owner != "" && len(app.Repositories) == 0 {
		return errors.New("connector github-app with an explicit owner requires exact repositories")
	}
	for _, repo := range app.Repositories {
		if !connectorAppRepoPattern.MatchString(repo) || repo == "." || repo == ".." {
			return errors.New("connector github-app.repositories requires exact repository names without wildcards or expressions")
		}
	}
	if len(app.Permissions) == 0 {
		return errors.New("connector github-app.permissions must explicitly bound the installation token")
	}
	for scope, level := range app.Permissions {
		if _, ok := GitHubAppManifestPermissionKey(PermissionScope(scope)); !ok ||
			(level != "read" && level != "write" && level != "none") {
			return fmt.Errorf("connector github-app has invalid permission %q", scope)
		}
		if phase == "agent" && level == "write" {
			return errors.New("agent-phase connector github-app permissions must be read-only")
		}
	}
	return nil
}

func validateConnectorGitHubApps(alias string, registration ConnectorRegistration) error {
	for name, ref := range registration.Credentials {
		if strings.HasPrefix(ref.Secret, connectorAppSecretPrefix) &&
			(ref.Secret != connectorAppSecret(alias, name) ||
				(registration.GitHubApps[name] == nil && registration.OIDCTokens[name] == nil)) {
			return errors.New("connector minted-token references cannot alias another credential")
		}
	}
	for name, app := range registration.GitHubApps {
		credential, declared := registration.Spec.Credentials[name]
		if app == nil || !declared || registration.Credentials[name].Secret != connectorAppSecret(alias, name) {
			return errors.New("connector github-app must reference its own declared credential")
		}
		if credential.Header != "Authorization" || (credential.Scheme != "bearer" && credential.Scheme != "token") {
			return errors.New("connector github-app credentials require Authorization with bearer or token scheme")
		}
		if err := app.validate(credential.Phase); err != nil {
			return err
		}
	}
	return nil
}

func connectorSecretExpression(config *WorkflowConnector, secret string) string {
	for name := range config.GitHubApps {
		if connectorAppSecret(config.Alias, name) == secret {
			return "${{ steps." + connectorAppStepID(secret) + ".outputs.token }}"
		}
	}
	for name := range config.OIDCTokens {
		if connectorAppSecret(config.Alias, name) == secret {
			return "${{ steps." + connectorAppStepID(secret) + ".outputs.token }}"
		}
	}
	return "${{ secrets." + secret + " }}"
}

func (c *Compiler) connectorGitHubAppSteps(data *WorkflowData, phase string) []string {
	condition, mintEnabled := connectorMintCondition(c, data, phase)
	if !mintEnabled {
		return nil
	}
	var steps []string
	for _, alias := range sliceutil.SortedKeys(data.Connectors) {
		config := data.Connectors[alias]
		var plan *connectors.Plan
		if phase == "agent" {
			plan = config.Agent
		} else {
			plan = config.SafeOutputs
		}
		if plan == nil {
			continue
		}
		for _, name := range sliceutil.SortedKeys(config.GitHubApps) {
			secret := connectorAppSecret(alias, name)
			if !slices.Contains(plan.Secrets(), secret) {
				continue
			}
			app := config.GitHubApps[name]
			if app.phase != phase {
				continue
			}
			mint := c.buildGitHubAppTokenMintStepWithMeta(&app.GitHubAppConfig,
				&Permissions{}, "${{ github.event.repository.name }}", "",
				fmt.Sprintf("Mint connector GitHub App token %s/%s", alias, name), connectorAppStepID(secret))
			if condition != "" {
				mint = slices.Insert(mint, 2, condition)
			}
			steps = append(steps, mint...)
		}
	}
	return append(steps, c.connectorOIDCSteps(data, phase)...)
}
