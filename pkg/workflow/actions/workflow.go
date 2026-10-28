package actions

import "errors"

// Workflow contains only GitHub Actions workflow properties.
type Workflow struct {
	Name        string
	RunName     string
	On          any
	Permissions any
	Env         map[string]Expression
	Defaults    map[string]any
	Concurrency any
	CacheMode   string
	Jobs        []*Job

	source *workflowSource
}

type Job struct {
	ID      JobID
	Name    string
	RunsOn  any
	Needs   []JobID
	If      Expression
	Env     map[string]Expression
	Steps   []Step
	Outputs map[OutputName]Expression

	Permissions              any
	CacheMode                string
	Defaults                 map[string]any
	Concurrency              any
	Environment              any
	Strategy                 map[string]any
	Container                any
	Services                 map[string]any
	TimeoutMinutes           int
	TimeoutMinutesSet        bool // Preserves an explicit zero separately from an absent literal timeout.
	TimeoutMinutesExpression Expression
	ContinueOnError          Expression

	// Uses makes this a reusable-workflow call rather than a runner job.
	Uses           string
	With           map[string]Expression
	Secrets        map[string]Expression
	SecretsInherit bool
	// CallOutputs declares outputs exported by the called workflow. These
	// names are validated but are not emitted as caller-side output bindings.
	CallOutputs []OutputDecl
}

type Step interface {
	actionsStep()
}

type OutputDecl struct {
	Name OutputName
}

type StepOptions struct {
	ID                       StepID
	Name                     string
	If                       Expression
	Env                      map[string]Expression
	ContinueOnError          Expression
	TimeoutMinutes           int
	TimeoutMinutesExpression Expression
	Outputs                  []OutputDecl
}

type RunStep struct {
	StepOptions
	Run              string
	Shell            string
	WorkingDirectory string
}

type ActionStep struct {
	StepOptions
	Uses string
	With map[string]Expression
}

// RawStep is one complete authored step mapping, without a list marker.
// The graph indexes its id, but cannot discover output declarations or typed
// references inside arbitrary scripts/expressions. Use typed steps for those.
type RawStep struct {
	Fields map[string]any
}

func (RunStep) actionsStep()    {}
func (ActionStep) actionsStep() {}
func (RawStep) actionsStep()    {}

func options(step Step) (StepOptions, error) {
	switch value := step.(type) {
	case RunStep:
		return value.StepOptions, nil
	case ActionStep:
		return value.StepOptions, nil
	case RawStep:
		var result StepOptions
		if raw, exists := value.Fields["id"]; exists {
			id, ok := raw.(string)
			if !ok {
				return StepOptions{}, errors.New("raw workflow step id must be a string")
			}
			if err := validateIdentifier("step id", id); err != nil {
				return StepOptions{}, err
			}
			result.ID = StepID(id)
		}
		return result, nil
	default:
		return StepOptions{}, errInvalidStep
	}
}
