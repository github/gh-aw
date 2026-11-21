package actions

import "strings"

// SourceForInspection retains source comments and graph edits while permitting
// typed script interpolation for analysis. Use Emit for executable workflows;
// its rejection of interpolated typed scripts remains unchanged.
func SourceForInspection(input *Workflow) (string, error) {
	value, err := lowerWorkflow(input, func(step Step, consumer JobID, resolved *Workflow) (map[string]any, error) {
		run, ok := step.(RunStep)
		if !ok || !strings.Contains(run.Run, "${{") {
			return LowerStep(step, consumer, resolved)
		}
		script := run.Run
		run.Run = "gh-aw-lint-script"
		fields, err := LowerStep(run, consumer, resolved)
		if err != nil {
			return nil, err
		}
		fields["run"] = script
		return fields, nil
	})
	if err != nil {
		return "", err
	}
	return emitLowered(input, value)
}
