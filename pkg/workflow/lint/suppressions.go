package lint

import (
	"errors"
	"fmt"
	"maps"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/github/gh-aw/pkg/workflow/actions"
	"github.com/goccy/go-yaml/ast"
	"github.com/goccy/go-yaml/lexer"
	"github.com/goccy/go-yaml/parser"
	"github.com/goccy/go-yaml/token"
)

var suppressionCommentMarker = regexp.MustCompile(`#[ \t]*gh-aw:`)
var suppressionDirective = regexp.MustCompile(`^gh-aw:\s*ignore\b`)
var suppressionPattern = regexp.MustCompile(`^gh-aw:\s*ignore\[([^\]]*)\]\s+--\s+(\S.*)$`)

type lintSuppression struct {
	rules map[string]struct{}
	paths map[string]struct{}
}

type lintSuppressions struct {
	actions []lintSuppression
	aw      []lintSuppression
}

type suppressionField struct {
	column int
	paths  map[string]struct{}
}

func (r *Runner) suppressionRuleIDs() map[string]struct{} {
	ids := maps.Clone(r.registered)
	if ids == nil {
		ids = make(map[string]struct{})
	}
	for _, rule := range r.rules {
		ids[rule.ID] = struct{}{}
	}
	return ids
}

func (r *Runner) commentSuppressions(input Input) (lintSuppressions, error) {
	known := r.suppressionRuleIDs()
	// A selected or compiler-specific registry may omit other built-in rules.
	for _, rule := range Default().rules {
		known[rule.ID] = struct{}{}
	}
	var result lintSuppressions
	var failures []error
	if input.Workflow != nil && hasSuppressionComments(input.Workflow.OriginalSource()) {
		source, err := actions.SourceForInspection(input.Workflow)
		if err != nil {
			failures = append(failures, errors.New("cannot render Actions source for lint suppressions"))
		} else {
			result.actions, err = parseLintSuppressions(source, known)
			failures = append(failures, err)
		}
	}
	if input.Document != nil {
		source := strings.Join(input.Document.Location.FrontmatterLines, "\n")
		var err error
		result.aw, err = parseLintSuppressions(source, known)
		failures = append(failures, err)
	}
	return result, errors.Join(failures...)
}

func (s lintSuppressions) contains(ruleID string, diagnostic Diagnostic) bool {
	scopes := s.aw
	if diagnostic.Source == ActionsAST {
		scopes = s.actions
	}
	return slices.ContainsFunc(scopes, func(scope lintSuppression) bool {
		_, ruleMatches := scope.rules[ruleID]
		_, pathMatches := scope.paths[diagnostic.Path]
		return ruleMatches && pathMatches
	})
}

func hasSuppressionComments(source string) bool {
	if !suppressionCommentMarker.MatchString(source) {
		return false
	}
	return slices.ContainsFunc(lexer.Tokenize(source), func(item *token.Token) bool {
		return item.Type == token.CommentType && suppressionDirective.MatchString(suppressionCommentText(item))
	})
}

func suppressionCommentText(item *token.Token) string {
	return strings.TrimSpace(strings.TrimPrefix(item.Value, "#"))
}

func parseLintSuppressions(source string, known map[string]struct{}) ([]lintSuppression, error) {
	if !hasSuppressionComments(source) {
		return nil, nil
	}
	source = strings.ReplaceAll(source, "\r\n", "\n")
	tokens := lexer.Tokenize(source)
	file, err := parser.Parse(tokens, parser.ParseComments)
	if err != nil {
		return nil, errors.New("cannot parse YAML comments for lint suppressions")
	}
	fields := make(map[int][]suppressionField)
	for _, document := range file.Docs {
		anchors := make(map[string]ast.Node)
		for _, node := range ast.Filter(ast.AnchorType, document.Body) {
			anchor, ok := node.(*ast.AnchorNode)
			if !ok {
				return nil, errors.New("cannot inspect YAML anchor for lint suppressions; correct the YAML before linting")
			}
			anchors[anchor.Name.GetToken().Value] = anchor.Value
		}
		collectSuppressionFields(document.Body, "", fields, anchors)
	}
	var lines []string
	for line := range strings.Lines(source) {
		lines = append(lines, strings.TrimSuffix(line, "\n"))
	}
	var suppressions []lintSuppression
	for _, item := range tokens {
		if item.Type != token.CommentType {
			continue
		}
		text := suppressionCommentText(item)
		if !suppressionDirective.MatchString(text) {
			continue
		}
		scope, err := suppressionComment(item.Position.Line, text, lines, fields, known)
		if err != nil {
			return nil, err
		}
		suppressions = append(suppressions, scope)
	}
	return suppressions, nil
}

func suppressionComment(line int, text string, lines []string, fields map[int][]suppressionField, known map[string]struct{}) (lintSuppression, error) {
	invalid := fmt.Errorf("invalid lint suppression at YAML line %d: use a standalone # gh-aw: ignore[rule-id] -- reason immediately before a YAML field", line)
	match := suppressionPattern.FindStringSubmatch(text)
	if len(match) < 2 {
		return lintSuppression{}, invalid
	}
	ruleList := match[1] //nolint:uncheckedsliceindex // Capture count is checked above.
	lineIndex := line - 1
	if lineIndex < 0 || lineIndex >= len(lines) {
		return lintSuppression{}, invalid
	}
	commentLine := lines[lineIndex]
	if !strings.HasPrefix(strings.TrimSpace(commentLine), "#") {
		return lintSuppression{}, invalid
	}
	rules := make(map[string]struct{})
	for id := range strings.SplitSeq(ruleList, ",") {
		id = strings.TrimSpace(id)
		if _, ok := known[id]; !ok || !ruleIDPattern.MatchString(id) {
			return lintSuppression{}, fmt.Errorf("invalid lint suppression at YAML line %d: every rule ID must name a registered lint rule", line)
		}
		rules[id] = struct{}{}
	}
	if line < 0 || line >= len(lines) {
		return lintSuppression{}, invalid
	}
	for offset, followingLine := range lines[line:] {
		if strings.HasPrefix(strings.TrimSpace(followingLine), "#") {
			continue
		}
		candidates := fields[line+offset+1]
		if len(candidates) == 0 || suppressionIndent(followingLine) != suppressionIndent(commentLine) {
			return lintSuppression{}, invalid
		}
		field := slices.MinFunc(candidates, func(a, b suppressionField) int { return a.column - b.column })
		return lintSuppression{paths: field.paths, rules: rules}, nil
	}
	return lintSuppression{}, invalid
}

func suppressionIndent(line string) int {
	return len(line) - len(strings.TrimLeft(line, " \t"))
}

func collectSuppressionFields(node ast.Node, path string, fields map[int][]suppressionField, anchors map[string]ast.Node) suppressionField {
	result := suppressionField{paths: map[string]struct{}{path: {}}}
	if fields == nil {
		switch node.(type) {
		case *ast.MappingNode, *ast.MappingValueNode:
			for key, value := range suppressionMapping(node, anchors) {
				child := key
				if path != "" {
					child = path + "." + key
				}
				result.add(collectSuppressionFields(value, child, nil, anchors))
			}
			return result
		}
	}
	switch value := node.(type) {
	case *ast.MappingNode:
		for _, field := range value.Values {
			result.add(collectSuppressionFields(field, path, fields, anchors))
		}
	case *ast.MappingValueNode:
		key := value.Key.GetToken()
		child := key.Value
		if path != "" {
			child = path + "." + child
		}
		descendants := collectSuppressionFields(value.Value, child, fields, anchors)
		descendants.column = key.Position.Column
		if fields != nil {
			fields[key.Position.Line] = append(fields[key.Position.Line], descendants)
		}
		result.add(descendants)
	case *ast.SequenceNode:
		for index, item := range value.Values {
			result.add(collectSuppressionFields(item, path+"["+strconv.Itoa(index)+"]", fields, anchors))
		}
	case *ast.AnchorNode:
		result.add(collectSuppressionFields(value.Value, path, fields, anchors))
	case *ast.TagNode:
		result.add(collectSuppressionFields(value.Value, path, fields, anchors))
	case *ast.AliasNode:
		name := value.Value.GetToken().Value
		if anchor, ok := anchors[name]; ok {
			remaining := maps.Clone(anchors)
			delete(remaining, name)
			// Resolve at the alias path without registering the anchor's
			// source lines as fields belonging to this separate occurrence.
			result.add(collectSuppressionFields(anchor, path, nil, remaining))
		}
	}
	return result
}

func suppressionMapping(node ast.Node, anchors map[string]ast.Node) map[string]ast.Node {
	var fields []*ast.MappingValueNode
	switch value := node.(type) {
	case *ast.MappingNode:
		fields = value.Values
	case *ast.MappingValueNode:
		fields = []*ast.MappingValueNode{value}
	case *ast.AnchorNode:
		return suppressionMapping(value.Value, anchors)
	case *ast.TagNode:
		return suppressionMapping(value.Value, anchors)
	case *ast.AliasNode:
		name := value.Value.GetToken().Value
		remaining := maps.Clone(anchors)
		delete(remaining, name)
		return suppressionMapping(anchors[name], remaining)
	case *ast.SequenceNode:
		result := make(map[string]ast.Node)
		for _, item := range value.Values {
			for key, child := range suppressionMapping(item, anchors) {
				if _, exists := result[key]; !exists {
					result[key] = child
				}
			}
		}
		return result
	}
	result := make(map[string]ast.Node)
	for _, field := range fields {
		if !field.Key.IsMergeKey() {
			result[field.Key.GetToken().Value] = field.Value
		}
	}
	for _, field := range fields {
		if field.Key.IsMergeKey() {
			for key, child := range suppressionMapping(field.Value, anchors) {
				if _, exists := result[key]; !exists {
					result[key] = child
				}
			}
		}
	}
	return result
}

func (field *suppressionField) add(child suppressionField) {
	maps.Copy(field.paths, child.paths)
}
