package conformance

import (
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/rhysd/actionlint"
)

const (
	falsePossible = 1
	truePossible  = 2
	unknownTruth  = falsePossible | truePossible
)

func parseCondition(condition string) (actionlint.ExprNode, error) {
	text := strings.TrimSpace(condition)
	if strings.HasPrefix(text, "${{") && strings.HasSuffix(text, "}}") {
		text = strings.TrimSpace(text[3 : len(text)-2])
	}
	if text == "" {
		text = "success()"
	}
	node, err := actionlint.NewExprParser().Parse(actionlint.NewExprLexer(text + "}}"))
	if err != nil {
		return nil, fmt.Errorf("invalid job condition: %s", err.Message)
	}
	return node, nil
}

func hasStatusFunction(node actionlint.ExprNode) bool {
	found := false
	actionlint.VisitExprNode(node, func(node, _ actionlint.ExprNode, entering bool) {
		if call, ok := node.(*actionlint.FuncCallNode); ok && entering {
			found = found || slices.Contains([]string{"always", "success", "failure", "cancelled"}, strings.ToLower(call.Callee))
		}
	})
	return found
}

func combineTruth(kind actionlint.LogicalOpNodeKind, left, right int) int {
	canTrue := left&truePossible != 0 && right&truePossible != 0
	canFalse := left&falsePossible != 0 || right&falsePossible != 0
	if kind == actionlint.LogicalOpNodeKindOr {
		canTrue = left&truePossible != 0 || right&truePossible != 0
		canFalse = left&falsePossible != 0 && right&falsePossible != 0
	}
	result := 0
	if canTrue {
		result |= truePossible
	}
	if canFalse {
		result |= falsePossible
	}
	return result
}

func invertTruth(value int) int {
	return ((value & falsePossible) << 1) | ((value & truePossible) >> 1)
}

func propertyPath(node actionlint.ExprNode) string {
	switch node := node.(type) {
	case *actionlint.VariableNode:
		return strings.ToLower(node.Name)
	case *actionlint.ObjectDerefNode:
		return propertyPath(node.Receiver) + "." + strings.ToLower(node.Property)
	case *actionlint.IndexAccessNode:
		if index, ok := node.Index.(*actionlint.StringNode); ok {
			return propertyPath(node.Operand) + "." + strings.ToLower(index.Value)
		}
	}
	return ""
}

func compareResult(node *actionlint.CompareOpNode, jobName, result string) int {
	left, right := node.Left, node.Right
	if propertyPath(right) == "needs."+jobName+".result" {
		left, right = right, left
	}
	literal, ok := right.(*actionlint.StringNode)
	if !ok || propertyPath(left) != "needs."+jobName+".result" {
		return unknownTruth
	}
	equal := strings.EqualFold(literal.Value, result)
	switch node.Kind {
	case actionlint.CompareOpNodeKindEq:
	case actionlint.CompareOpNodeKindNotEq:
		equal = !equal
	default:
		return unknownTruth
	}
	if equal {
		return truePossible
	}
	return falsePossible
}

func possibleResult(node actionlint.ExprNode, jobName, result string) int {
	switch node := node.(type) {
	case *actionlint.LogicalOpNode:
		return combineTruth(node.Kind, possibleResult(node.Left, jobName, result), possibleResult(node.Right, jobName, result))
	case *actionlint.NotOpNode:
		return invertTruth(possibleResult(node.Operand, jobName, result))
	case *actionlint.CompareOpNode:
		return compareResult(node, jobName, result)
	case *actionlint.BoolNode:
		if node.Value {
			return truePossible
		}
		return falsePossible
	case *actionlint.FuncCallNode:
		if strings.EqualFold(node.Callee, "success") && len(node.Args) == 0 && result != "success" {
			return falsePossible
		}
		if strings.EqualFold(node.Callee, "always") && len(node.Args) == 0 {
			return truePossible
		}
	}
	return unknownTruth
}

func requireJobResults(condition, jobName string, allowed []string, implicitSuccess bool) error {
	node, err := parseCondition(condition)
	if err != nil {
		return err
	}
	if implicitSuccess && !hasStatusFunction(node) {
		return nil
	}
	for _, result := range []string{"success", "failure", "cancelled", "skipped"} {
		if !slices.Contains(allowed, result) && possibleResult(node, jobName, result)&truePossible != 0 {
			return fmt.Errorf("condition can admit needs.%s.result = %q", jobName, result)
		}
	}
	return nil
}

func expressionIdentity(node actionlint.ExprNode) (string, error) {
	// AST fields are JSON-compatible; positions/tokens are unexported.
	data, err := json.Marshal(node)
	if err != nil {
		return "", fmt.Errorf("unsupported condition AST: %w", err)
	}
	return string(data), nil
}

func possibleWithPredicateFalse(node actionlint.ExprNode, predicate string, identities map[actionlint.ExprNode]string) int {
	if identities[node] == predicate {
		return falsePossible
	}
	switch node := node.(type) {
	case *actionlint.LogicalOpNode:
		return combineTruth(node.Kind, possibleWithPredicateFalse(node.Left, predicate, identities), possibleWithPredicateFalse(node.Right, predicate, identities))
	case *actionlint.NotOpNode:
		return invertTruth(possibleWithPredicateFalse(node.Operand, predicate, identities))
	case *actionlint.BoolNode:
		if node.Value {
			return truePossible
		}
		return falsePossible
	}
	return unknownTruth
}

func requirePredicate(condition, predicate string) error {
	node, err := parseCondition(condition)
	if err != nil {
		return err
	}
	expected, err := parseCondition(predicate)
	if err != nil {
		return err
	}
	expectedIdentity, err := expressionIdentity(expected)
	if err != nil {
		return err
	}
	identities := make(map[actionlint.ExprNode]string)
	actionlint.VisitExprNode(node, func(node, _ actionlint.ExprNode, entering bool) {
		if entering && err == nil {
			identities[node], err = expressionIdentity(node)
		}
	})
	if err != nil {
		return err
	}
	if possibleWithPredicateFalse(node, expectedIdentity, identities)&truePossible != 0 {
		return errors.New("condition does not enforce compiler-declared detection enablement")
	}
	return nil
}
