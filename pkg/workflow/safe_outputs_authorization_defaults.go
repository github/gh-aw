package workflow

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/github/gh-aw/pkg/workflow/compilerenv"
)

// ValidateSafeOutputAuthorization validates a literal authority without acquiring credentials.
func ValidateSafeOutputAuthorization(endpoint, audience string) error {
	_, err := parseSafeOutputAuthorization(map[string]any{"endpoint": endpoint, "audience": audience})
	return err
}

func parseRepoSafeOutputAuthorization(raw json.RawMessage) (*SafeOutputAuthorization, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		return nil, fmt.Errorf("safe_output_authorization must be true or an endpoint/audience object: %w", err)
	}
	if value == true {
		return &SafeOutputAuthorization{FromEnvironment: true}, nil
	}
	if value == nil {
		return nil, errors.New("safe_output_authorization must be true or an endpoint/audience object")
	}
	config, err := parseSafeOutputAuthorization(value)
	if err != nil {
		return nil, fmt.Errorf("safe_output_authorization: %w", err)
	}
	return config, nil
}

func (c *Compiler) applyRepoSafeOutputAuthorization(data *WorkflowData) error {
	repo, err := c.loadRepoConfig()
	if err != nil {
		// generateAndValidateYAML rejects repository configuration errors before
		// emitting YAML; preserve the parsing API's existing error boundary.
		return nil
	}
	if repo == nil || repo.SafeOutputAuthorization == nil || data.SafeOutputs == nil {
		return nil
	}
	if !sameSafeOutputAuthority(repo.SafeOutputAuthorization, data.SafeOutputs.Authorization) {
		return errors.New("safe-outputs authorization conflicts with aw.json safe_output_authorization")
	}
	if data.SafeOutputs.Authorization == nil {
		authority := *repo.SafeOutputAuthorization
		data.SafeOutputs.Authorization = &authority
	}
	safeOutputAuthorizationLog.Printf("Applied repository authorization: from_environment=%t", repo.SafeOutputAuthorization.FromEnvironment)
	return nil
}

func safeOutputAuthorizationDefaultEnv(config *SafeOutputsConfig) map[string]string {
	if config == nil {
		return nil
	}
	for _, requirement := range config.AuthorizationRequirements {
		if requirement != nil && requirement.FromEnvironment {
			return map[string]string{
				compilerenv.DefaultSafeOutputAuthorizationURL:      "${{ vars." + compilerenv.DefaultSafeOutputAuthorizationURL + " }}",
				compilerenv.DefaultSafeOutputAuthorizationAudience: "${{ vars." + compilerenv.DefaultSafeOutputAuthorizationAudience + " }}",
			}
		}
	}
	return nil
}
