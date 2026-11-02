package workflow

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/github/gh-aw/pkg/sliceutil"
)

type ConnectorToken struct {
	Secret string `json:"secret"`
}

type ConnectorGitHubOIDC struct {
	Audience string `json:"audience"`
	phase    string
}

func validateConnectorOIDC(alias string, registration ConnectorRegistration) error {
	for name, oidc := range registration.OIDCTokens {
		credential, declared := registration.Spec.Credentials[name]
		if oidc == nil || !declared || registration.GitHubApps[name] != nil ||
			registration.Credentials[name].Secret != connectorAppSecret(alias, name) {
			return errors.New("connector github-oidc must reference its own declared credential and cannot also use github-app")
		}
		if credential.Phase != "agent" && credential.Phase != "safe-output" {
			return errors.New("connector github-oidc requires the agent or safe-output phase")
		}
		if credential.Header != "Authorization" || credential.Scheme != "bearer" {
			return errors.New("connector github-oidc credentials require Authorization with bearer scheme")
		}
		if oidc.Audience == "" || len(oidc.Audience) > 2048 || strings.ContainsAny(oidc.Audience, "\r\n\t") ||
			strings.Contains(oidc.Audience, "${{") || strings.TrimSpace(oidc.Audience) != oidc.Audience {
			return errors.New("connector github-oidc.audience must be a nonempty bounded literal, not an expression")
		}
		for _, character := range oidc.Audience {
			if character < 0x20 || character == 0x7f {
				return errors.New("connector github-oidc.audience cannot contain control characters")
			}
		}
		oidc.phase = credential.Phase
	}
	return nil
}

func connectorMintCondition(c *Compiler, data *WorkflowData, phase string) (string, bool) {
	if phase != "safe-output" {
		return "", true
	}
	if c.dryRun || c.trialMode || c.forceStaged || templatableBoolIsTrue(data.SafeOutputs.Staged) {
		return "", false
	}
	if staged := templatableBoolEnvVarValue(data.SafeOutputs.Staged); staged != nil {
		return "        if: ${{ format('{0}', " + stripExpressionWrapper(*staged) + ") != 'true' }}\n", true
	}
	return "", true
}

func (c *Compiler) connectorOIDCSteps(data *WorkflowData, phase string) []string {
	condition, mint := connectorMintCondition(c, data, phase)
	if !mint {
		return nil
	}
	var steps []string
	for _, alias := range sliceutil.SortedKeys(data.Connectors) {
		config := data.Connectors[alias]
		plan := config.Agent
		if phase == "safe-output" {
			plan = config.SafeOutputs
		}
		if plan == nil {
			continue
		}
		for _, name := range sliceutil.SortedKeys(config.OIDCTokens) {
			oidc := config.OIDCTokens[name]
			secret := connectorAppSecret(alias, name)
			if oidc.phase != phase || !slices.Contains(plan.Secrets(), secret) {
				continue
			}
			steps = append(steps,
				fmt.Sprintf("      - name: Mint connector GitHub OIDC token %s/%s\n", alias, name),
				"        id: "+connectorAppStepID(secret)+"\n")
			if condition != "" {
				steps = append(steps, condition)
			}
			steps = append(steps,
				"        uses: "+c.getActionPin("actions/github-script")+"\n",
				"        env:\n",
				formatYAMLEnv("          ", "GH_AW_CONNECTOR_OIDC_AUDIENCE", oidc.Audience),
				"        with:\n",
				"          script: |\n",
				"            const token = await core.getIDToken(process.env.GH_AW_CONNECTOR_OIDC_AUDIENCE);\n",
				"            core.setSecret(token);\n",
				"            core.setOutput('token', token);\n")
		}
	}
	return steps
}

func (c *Compiler) ensureConnectorOIDCJobPermissions(data *WorkflowData) {
	for _, job := range c.jobManager.GetAllJobs() {
		for _, config := range data.Connectors {
			for name := range config.OIDCTokens {
				stepID := "id: " + connectorAppStepID(connectorAppSecret(config.Alias, name)) + "\n"
				for _, step := range job.Steps {
					if !strings.Contains(step, stepID) {
						continue
					}
					permissions := NewPermissionsParser(job.Permissions).ToPermissions()
					permissions.Set(PermissionIdToken, PermissionWrite)
					job.Permissions = permissions.RenderToYAML()
				}
			}
		}
	}
}
