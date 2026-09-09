package workflow

import "github.com/github/gh-aw/pkg/logger"

var maintenanceConditionsLog = logger.New("workflow:maintenance_conditions")

// maintenanceNoOperationValue is the sentinel value used for the workflow_dispatch
// "operation" choice input to represent "no operation selected". It replaces an
// empty string option, since actionlint rejects empty strings as workflow_dispatch
// choice options and requires the default to be one of the declared options.
// The workflow_call "operation" input remains a plain string with an empty-string
// default (not subject to the choice/options restriction), so the empty value and
// this sentinel value are treated as "no operation" throughout the generated conditions.
const maintenanceNoOperationValue = "none"

// buildOperationIsEmptyCondition creates a condition that is true when the
// `inputs.operation` value represents "no operation selected", i.e. it is
// either an empty string (workflow_call default) or the choice sentinel value
// used by the workflow_dispatch "operation" input.
func buildOperationIsEmptyCondition() ConditionNode {
	return BuildOr(
		BuildEquals(
			BuildPropertyAccess("inputs.operation"),
			BuildStringLiteral(""),
		),
		BuildEquals(
			BuildPropertyAccess("inputs.operation"),
			BuildStringLiteral(maintenanceNoOperationValue),
		),
	)
}

// buildOperationIsNotEmptyCondition creates a condition that is true when the
// `inputs.operation` value represents an operation being selected, i.e. it is
// neither the empty string (workflow_call default) nor the choice sentinel
// value used by the workflow_dispatch "operation" input. Built directly from
// BuildNotEquals (rather than negating buildOperationIsEmptyCondition) so it
// renders using the same `!=` style as the other operation exclusion checks.
func buildOperationIsNotEmptyCondition() ConditionNode {
	return BuildAnd(
		BuildNotEquals(
			BuildPropertyAccess("inputs.operation"),
			BuildStringLiteral(""),
		),
		BuildNotEquals(
			BuildPropertyAccess("inputs.operation"),
			BuildStringLiteral(maintenanceNoOperationValue),
		),
	)
}

// buildNotForkCondition creates a condition to check the repository is not a fork.
func buildNotForkCondition() ConditionNode {
	return &NotNode{
		Child: BuildPropertyAccess("github.event.repository.fork"),
	}
}

// buildNotDispatchOrCallOrEmptyOperation creates a condition that is true when the event
// is not a workflow_dispatch or workflow_call, or the operation input is empty.
// Uses the `inputs.operation` context which works for both workflow_dispatch and workflow_call.
func buildNotDispatchOrCallOrEmptyOperation() ConditionNode {
	return BuildOr(
		BuildAnd(
			BuildNotEquals(
				BuildPropertyAccess("github.event_name"),
				BuildStringLiteral("workflow_dispatch"),
			),
			BuildNotEquals(
				BuildPropertyAccess("github.event_name"),
				BuildStringLiteral("workflow_call"),
			),
		),
		buildOperationIsEmptyCondition(),
	)
}

// buildNotForkAndScheduled creates a condition for jobs that should run on any
// non-dispatch/call event including push, or on workflow_dispatch/workflow_call
// with an empty operation, and never on forks. Unlike buildNotForkAndScheduleOnly,
// this function does NOT exclude push events.
// Condition: !fork && ((event_name != 'workflow_dispatch' && event_name != 'workflow_call') || operation == ”)
func buildNotForkAndScheduled() ConditionNode {
	return BuildAnd(
		buildNotForkCondition(),
		buildNotDispatchOrCallOrEmptyOperation(),
	)
}

// buildNotForkAndScheduleOnly creates a condition for jobs that should run on schedule
// (or empty dispatch/call) but NOT on push events, and never on forks.
func buildNotForkAndScheduleOnly() ConditionNode {
	return BuildAnd(
		buildNotForkCondition(),
		BuildAnd(
			BuildNotEquals(
				BuildPropertyAccess("github.event_name"),
				BuildStringLiteral("push"),
			),
			buildNotDispatchOrCallOrEmptyOperation(),
		),
	)
}

// buildNotForkAndScheduleOnlyOrOperation creates a condition for jobs that run on
// schedule (or empty dispatch/call) or when a specific operation is selected,
// but NOT on push events, and never on forks.
func buildNotForkAndScheduleOnlyOrOperation(operation string) ConditionNode {
	maintenanceConditionsLog.Printf("Building not-fork-and-schedule-only-or-operation condition: %s", operation)
	return BuildAnd(
		buildNotForkCondition(),
		BuildAnd(
			BuildNotEquals(
				BuildPropertyAccess("github.event_name"),
				BuildStringLiteral("push"),
			),
			BuildOr(
				buildNotDispatchOrCallOrEmptyOperation(),
				BuildEquals(
					BuildPropertyAccess("inputs.operation"),
					BuildStringLiteral(operation),
				),
			),
		),
	)
}

// buildDispatchOperationCondition creates a condition for jobs that should run
// only when a specific workflow_dispatch or workflow_call operation is selected and not a fork.
// Condition: (dispatch || call) && operation == op && !fork
func buildDispatchOperationCondition(operation string) ConditionNode {
	return BuildAnd(
		BuildAnd(
			BuildOr(
				BuildEventTypeEquals("workflow_dispatch"),
				BuildEventTypeEquals("workflow_call"),
			),
			BuildEquals(
				BuildPropertyAccess("inputs.operation"),
				BuildStringLiteral(operation),
			),
		),
		buildNotForkCondition(),
	)
}

// buildLabeledDisableCondition creates a condition for the disable_agentic_workflow job
// that triggers when an issue is labeled with "agentic-workflows:disable".
// Condition: !fork && event_name == 'issues' && event.label.name == 'agentic-workflows:disable'
func buildLabeledDisableCondition() ConditionNode {
	return BuildAnd(
		buildNotForkCondition(),
		BuildAnd(
			BuildEventTypeEquals("issues"),
			BuildEquals(
				BuildPropertyAccess("github.event.label.name"),
				BuildStringLiteral("agentic-workflows:disable"),
			),
		),
	)
}

// buildLabeledApplySafeOutputsCondition creates a condition for the label_apply_safe_outputs job
// that triggers when an issue is labeled with "agentic-workflows:apply-safe-outputs".
// Condition: !fork && event_name == 'issues' && event.label.name == 'agentic-workflows:apply-safe-outputs'
func buildLabeledApplySafeOutputsCondition() ConditionNode {
	return BuildAnd(
		buildNotForkCondition(),
		BuildAnd(
			BuildEventTypeEquals("issues"),
			BuildEquals(
				BuildPropertyAccess("github.event.label.name"),
				BuildStringLiteral("agentic-workflows:apply-safe-outputs"),
			),
		),
	)
}

// buildRunOperationCondition creates the condition for the unified run_operation
// job that handles all dispatch/call operations except the ones with dedicated jobs.
// Condition: (dispatch || call) && operation != ” && operation != each excluded && !fork.
func buildRunOperationCondition(excludedOperations ...string) ConditionNode {
	maintenanceConditionsLog.Printf("Building run operation condition, excluding %d operation(s): %v", len(excludedOperations), excludedOperations)
	// Start with: event is workflow_dispatch or workflow_call AND operation is not empty
	condition := BuildAnd(
		BuildOr(
			BuildEventTypeEquals("workflow_dispatch"),
			BuildEventTypeEquals("workflow_call"),
		),
		buildOperationIsNotEmptyCondition(),
	)

	// Exclude each dedicated operation
	for _, op := range excludedOperations {
		condition = BuildAnd(
			condition,
			BuildNotEquals(
				BuildPropertyAccess("inputs.operation"),
				BuildStringLiteral(op),
			),
		)
	}

	// AND not a fork
	return BuildAnd(condition, buildNotForkCondition())
}
