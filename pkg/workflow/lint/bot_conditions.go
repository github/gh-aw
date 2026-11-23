package lint

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/github/gh-aw/pkg/workflow/actions"
	"github.com/github/gh-aw/pkg/workflow/aw"
	"github.com/github/gh-aw/pkg/workflow/taint"
	"github.com/rhysd/actionlint"
)

// BotConditionsRule reports job and step conditions that admit a bot identity
// through actor contexts on triggers that run with repository privileges or
// react to contributor activity. github.actor and github.triggering_actor name
// the last actor, not the pull request author, so an attacker can make a bot
// the actor of an event on attacker-controlled content (for example, by having
// Dependabot update a fork branch) and pass the gate.
//
// Only conditions with a positive bot match are reported. A conjunction with
// an author check (pull request user or author association) is treated as
// guarded. Actor checks outside if fields, dynamic actor names, and bot logins
// not recognizable from literals are not analyzed.
func BotConditionsRule() Rule {
	return Rule{
		ID:          "bot-conditions",
		Description: "Flag spoofable github.actor bot checks gating privileged or contributor-triggered workflows",
		Severity:    Warning,
		Targets:     ActionsAST | AWAST,
		Check:       checkBotConditions,
	}
}

// AW slash_command and command expand to comment triggers in compiled workflows.
var botConditionTriggers = []string{
	"pull_request_target", "workflow_run", "issue_comment",
	"pull_request_review", "pull_request_review_comment",
	"slash_command", "command",
}

var spoofableActorLogins = map[string]struct{}{
	"github.actor":              {},
	"github.triggering_actor":   {},
	"github.event.sender.login": {},
}

var spoofableActorIDs = map[string]struct{}{
	"github.actor_id":            {},
	"github.triggering_actor_id": {},
	"github.event.sender.id":     {},
}

// Numeric user IDs of common GitHub-hosted bot accounts.
var knownBotIDs = map[int]struct{}{
	49699333: {}, // dependabot[bot]
	41898282: {}, // github-actions[bot]
	29139614: {}, // renovate[bot]
}

func checkBotConditions(ctx context.Context, input Input) ([]Diagnostic, error) {
	if err := integrityInput(ctx, input); err != nil {
		return nil, err
	}
	var on any
	if input.Workflow != nil {
		on = input.Workflow.On
	} else {
		on = input.Document.Frontmatter["on"]
	}
	trigger := ""
	for _, name := range botConditionTriggers {
		_, found, err := authoredTrigger(ctx, on, name)
		if err != nil {
			return nil, err
		}
		if found {
			trigger = name
			break
		}
	}
	if trigger == "" {
		return nil, nil
	}
	conditions, err := botConditionFields(ctx, input)
	var diagnostics []Diagnostic
	failures := []error{err}
	for _, condition := range conditions {
		if err := ctx.Err(); err != nil {
			return diagnostics, errors.Join(append(failures, err)...)
		}
		actor, found, err := spoofableBotCondition(ctx, condition.path, condition.text)
		if err != nil {
			failures = append(failures, err)
			continue
		}
		if found {
			diagnostics = append(diagnostics, Diagnostic{
				Path: condition.path,
				Message: fmt.Sprintf("Condition admits a bot identity through spoofable %s in a workflow triggered by %s; %s names the last actor, not the pull request author. "+
					"Compare github.event.pull_request.user.login or require an author association instead", actor, trigger, actor),
			})
		}
	}
	return diagnostics, errors.Join(failures...)
}

type botConditionField struct {
	path, text string
}

// The compiled Actions graph is authoritative when supplied; AW conditions are
// inspected only when linting authored source on its own.
func botConditionFields(ctx context.Context, input Input) ([]botConditionField, error) {
	var fields []botConditionField
	add := func(path string, value any) {
		if text, ok := value.(string); ok && strings.TrimSpace(text) != "" {
			fields = append(fields, botConditionField{path: path, text: text})
		}
	}
	if input.Workflow == nil {
		blocks, err := aw.ExecutionBlocks(ctx, input.Document)
		for _, block := range blocks {
			add(block.FieldPath("if"), block.Fields["if"])
			for _, step := range block.Steps {
				add(step.Path+".if", step.Fields["if"])
			}
		}
		return fields, err
	}
	var failures []error
	for _, job := range input.Workflow.Jobs {
		if err := ctx.Err(); err != nil {
			return fields, errors.Join(append(failures, err)...)
		}
		path := "jobs." + string(job.ID)
		if job.If != nil {
			value, err := actions.RenderExpression(job.If, job.ID, input.Workflow, false)
			if err != nil {
				failures = append(failures, fmt.Errorf("bot-conditions cannot render the condition at %s.if; correct the job before linting", path))
			} else {
				add(path+".if", "${{ "+value+" }}")
			}
		}
		for index, step := range job.Steps {
			stepPath := fmt.Sprintf("%s.steps[%d]", path, index)
			lowered, err := actions.LowerStep(step, job.ID, input.Workflow)
			if err != nil {
				failures = append(failures, fmt.Errorf("bot-conditions cannot inspect %s: invalid Actions step; correct the step before linting", stepPath))
				continue
			}
			add(stepPath+".if", lowered["if"])
		}
	}
	return fields, errors.Join(failures...)
}

// Conditions may be bare expressions or ${{ }} interpolations.
func spoofableBotCondition(ctx context.Context, path, text string) (string, bool, error) {
	actor, found := "", false
	visit := func(_, _, _ int, node actionlint.ExprNode) error {
		if !found {
			actor, found = spoofableBotGate(node, true)
		}
		return ctx.Err()
	}
	if strings.Contains(text, "${{") {
		return actor, found, taint.VisitInterpolations(ctx, text, path, visit)
	}
	lexer := actionlint.NewExprLexer(text + " }}")
	node, err := actionlint.NewExprParser().Parse(lexer)
	if err != nil || lexer.Offset() != len(text+" }}") {
		return "", false, &taint.ExpressionError{Path: path, Line: 1}
	}
	return actor, found, visit(0, 0, 1, node)
}

// spoofableBotGate follows boolean structure only. positive tracks negation so
// that !(github.actor != 'x[bot]') is treated like an equality check.
func spoofableBotGate(node actionlint.ExprNode, positive bool) (string, bool) {
	switch node := node.(type) {
	case *actionlint.NotOpNode:
		return spoofableBotGate(node.Operand, !positive)
	case *actionlint.LogicalOpNode:
		// In a conjunction, an author check in the sibling operand guards the
		// gate; one beside the bot check in the same disjunction does not.
		conjunction := (node.Kind == actionlint.LogicalOpNodeKindAnd) == positive
		if actor, found := spoofableBotGate(node.Left, positive); found && !(conjunction && botAuthorGuard(node.Right)) {
			return actor, true
		}
		if actor, found := spoofableBotGate(node.Right, positive); found && !(conjunction && botAuthorGuard(node.Left)) {
			return actor, true
		}
		return "", false
	case *actionlint.CompareOpNode:
		equal := node.Kind == actionlint.CompareOpNodeKindEq && positive ||
			node.Kind == actionlint.CompareOpNodeKindNotEq && !positive
		if !equal {
			return "", false
		}
		if actor, found := botComparison(node.Left, node.Right); found {
			return actor, true
		}
		return botComparison(node.Right, node.Left)
	case *actionlint.FuncCallNode:
		if !positive || len(node.Args) != 2 {
			return "", false
		}
		switch strings.ToLower(node.Callee) {
		case "startswith", "endswith":
			if actor, ok := spoofableActorLogin(node.Args[0]); ok && botLoginLiteral(node.Args[1], true) {
				return actor, true
			}
		case "contains":
			if actor, ok := spoofableActorLogin(node.Args[0]); ok && botLoginLiteral(node.Args[1], true) {
				return actor, true
			}
			if actor, ok := spoofableActorLogin(node.Args[1]); ok && botLoginList(node.Args[0]) {
				return actor, true
			}
		}
	}
	return "", false
}

func botComparison(reference, literal actionlint.ExprNode) (string, bool) {
	if actor, ok := spoofableActorLogin(reference); ok && botLoginLiteral(literal, false) {
		return actor, true
	}
	path, ok := contextPath(reference)
	if _, spoofable := spoofableActorIDs[path]; !ok || !spoofable {
		return "", false
	}
	switch literal := literal.(type) {
	case *actionlint.IntNode:
		_, known := knownBotIDs[literal.Value]
		return path, known
	case *actionlint.StringNode:
		var id int
		if _, err := fmt.Sscanf(strings.TrimSpace(literal.Value), "%d", &id); err == nil {
			_, known := knownBotIDs[id]
			return path, known
		}
	}
	return "", false
}

func spoofableActorLogin(node actionlint.ExprNode) (string, bool) {
	path, ok := contextPath(node)
	if !ok {
		return "", false
	}
	_, spoofable := spoofableActorLogins[path]
	return path, spoofable
}

// App bot logins end in [bot]; machine users conventionally end in -bot or
// _bot. Partial matches also admit bare bot name prefixes.
func botLoginLiteral(node actionlint.ExprNode, partial bool) bool {
	literal, ok := node.(*actionlint.StringNode)
	if !ok {
		return false
	}
	return botLogin(literal.Value, partial)
}

func botLogin(value string, partial bool) bool {
	value = strings.ToLower(strings.TrimSpace(value))
	if strings.HasSuffix(value, "[bot]") || strings.HasSuffix(value, "-bot") || strings.HasSuffix(value, "_bot") ||
		partial && strings.Contains(value, "[bot]") {
		return true
	}
	return partial && strings.HasSuffix(value, "bot") && len(value) > len("bot")
}

// contains(fromJSON('["dependabot[bot]"]'), github.actor) or a literal list string.
func botLoginList(node actionlint.ExprNode) bool {
	if call, ok := node.(*actionlint.FuncCallNode); ok && strings.EqualFold(call.Callee, "fromjson") && len(call.Args) == 1 {
		literal, ok := call.Args[0].(*actionlint.StringNode)
		if !ok {
			return false
		}
		var entries []any
		if err := json.Unmarshal([]byte(literal.Value), &entries); err != nil {
			return false
		}
		for _, entry := range entries {
			if login, ok := entry.(string); ok && botLogin(login, false) {
				return true
			}
		}
		return false
	}
	literal, ok := node.(*actionlint.StringNode)
	return ok && strings.Contains(strings.ToLower(literal.Value), "[bot]")
}

// botAuthorGuard reports whether node always requires an author check. An
// author check under a disjunction (for example, one limited to another event
// by github.event_name != 'x' || ...) does not guard the bot gate.
func botAuthorGuard(node actionlint.ExprNode) bool {
	return botAuthorRequired(node, true)
}

func botAuthorRequired(node actionlint.ExprNode, positive bool) bool {
	switch node := node.(type) {
	case *actionlint.NotOpNode:
		return botAuthorRequired(node.Operand, !positive)
	case *actionlint.LogicalOpNode:
		if (node.Kind == actionlint.LogicalOpNodeKindAnd) == positive {
			return botAuthorRequired(node.Left, positive) || botAuthorRequired(node.Right, positive)
		}
		return botAuthorRequired(node.Left, positive) && botAuthorRequired(node.Right, positive)
	}
	return botAuthorReference(node)
}

func botAuthorReference(node actionlint.ExprNode) bool {
	guarded := false
	actionlint.VisitExprNode(node, func(child, _ actionlint.ExprNode, entering bool) {
		if !entering || guarded {
			return
		}
		path, ok := contextPath(child)
		if !ok {
			return
		}
		switch {
		case path == "github.event.pull_request.user.login", path == "github.event.pull_request.user.id",
			strings.HasPrefix(path, "github.event.") && strings.HasSuffix(path, ".author_association"):
			guarded = true
		}
	})
	return guarded
}

// contextPath returns a lowercase dotted path for literal property accesses.
func contextPath(node actionlint.ExprNode) (string, bool) {
	switch node := node.(type) {
	case *actionlint.VariableNode:
		return strings.ToLower(node.Name), true
	case *actionlint.ObjectDerefNode:
		receiver, ok := contextPath(node.Receiver)
		return receiver + "." + strings.ToLower(node.Property), ok
	case *actionlint.IndexAccessNode:
		key, literal := node.Index.(*actionlint.StringNode)
		if !literal {
			return "", false
		}
		operand, ok := contextPath(node.Operand)
		return operand + "." + strings.ToLower(key.Value), ok
	}
	return "", false
}
