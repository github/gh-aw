package workflow

import "github.com/github/gh-aw/pkg/constants"

// NoneEngine is the sentinel runtime for `engine: none`. It is never registered in the
// engine registry, so it is absent from engine listings, suggestions and the add wizard.
// Workflows selecting it run a deterministic `program:` instead of an agent: the
// program job is assembled by generateProgramJobSteps, and every engine hook returns
// nothing so engine-aware validators and job builders treat it as "no engine".
type NoneEngine struct {
	BaseEngine
}

var noneEngine = &NoneEngine{BaseEngine: BaseEngine{
	id:          string(constants.NoneEngine),
	displayName: "None (deterministic program)",
	description: "Runs a deterministic program instead of an agentic engine",
}}

// GetInstallationSteps returns no steps: a program needs no engine CLI.
func (e *NoneEngine) GetInstallationSteps(_ *WorkflowData) []GitHubActionStep {
	return nil
}

// GetExecutionSteps returns no steps: the program step is emitted by generateProgramRunSteps.
func (e *NoneEngine) GetExecutionSteps(_ *WorkflowData, _ string) []GitHubActionStep {
	return nil
}

func isNoneEngineID(id string) bool {
	return id == string(constants.NoneEngine)
}
