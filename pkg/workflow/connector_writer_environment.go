package workflow

import (
	"errors"
	"fmt"
	"strings"

	"github.com/github/gh-aw/pkg/constants"
)

// An Entra federated credential can trust an environment subject, not a job
// name. A distinct writer client ID is not a boundary if another job can
// request an assertion with that subject.
func (c *Compiler) isolateConnectorWriterEnvironment(data *WorkflowData) error {
	for _, config := range data.Connectors {
		if config.SafeOutputs == nil {
			continue
		}
		for _, secret := range config.SafeOutputs.Secrets() {
			if config.azureRoute(secret) == nil {
				continue
			}
			if _, _, live := connectorCredentialMintCondition(c, data, config, secret, "safe-output"); live {
				return c.reserveConnectorWriterEnvironment(data)
			}
		}
	}
	return nil
}

func (c *Compiler) reserveConnectorWriterEnvironment(data *WorkflowData) error {
	name := jobEnvironmentName(data.SafeOutputs.Environment)
	if name == "" || ValidateEnvironmentOverride(name) != nil || c.environmentOverride != "" {
		return errors.New("live Azure connector writes require a dedicated literal safe-outputs.environment; do not use --environment")
	}
	for id, job := range c.jobManager.GetAllJobs() {
		switch id {
		case string(constants.SafeOutputsJobName):
			if !strings.EqualFold(jobEnvironmentName(job.Environment), name) {
				return errors.New("connector writer job lost its dedicated safe-outputs.environment")
			}
			continue
		case string(constants.ConclusionJobName), string(constants.PreActivationJobName):
			// Normally these framework jobs inherit the safe-output environment.
			// Keep their independent admission identity, not the writer subject.
			job.Environment = c.indentYAMLLines(data.Environment, "    ")
		}
		if env := jobEnvironmentName(job.Environment); strings.TrimSpace(job.Environment) != "" &&
			(env == "" || ValidateEnvironmentOverride(env) != nil || strings.EqualFold(env, name)) {
			return fmt.Errorf("job %s must not share or dynamically select the Azure connector writer's safe-outputs.environment", id)
		}
	}
	return nil
}
