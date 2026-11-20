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

var suppressionPattern = regexp.MustCompile(`^gh-aw:\s*ignore\[([^\]]*)\]\s+--\s+(\S.*)$`)

type lintSuppression struct {
	rules   map[string]struct{}
	paths   map[string]struct{}
	aliases []string
}

type lintSuppressions struct {
	actions []lintSuppression
	aw      []lintSuppression
}

type suppressionField struct {
	column  int
	paths   map[string]struct{}
	aliases []string
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
	if input.Workflow != nil && strings.Contains(input.Workflow.OriginalSource(), "gh-aw:") {
		source, err := actions.Emit(input.Workflow)
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

func (s lintSuppressions) contains(input Input, rule Rule, path string) bool {
	scopes := s.aw
	if input.Workflow != nil && rule.Targets != AWAST {
		scopes = s.actions
	}
	return slices.ContainsFunc(scopes, func(scope lintSuppression) bool {
		_, ruleMatches := scope.rules[rule.ID]
		_, pathMatches := scope.paths[path]
		pathMatches = pathMatches || slices.ContainsFunc(scope.aliases, func(alias string) bool {
			return strings.HasPrefix(path, alias+".") || strings.HasPrefix(path, alias+"[")
		})
		return ruleMatches && pathMatches
	})
}

func parseLintSuppressions(source string, known map[string]struct{}) ([]lintSuppression, error) {
	if !strings.Contains(source, "gh-aw:") {
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
		collectSuppressionFields(document.Body, "", fields)
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
		text := strings.TrimSpace(strings.TrimPrefix(item.Value, "#"))
		if !strings.HasPrefix(text, "gh-aw:") {
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
		return lintSuppression{paths: field.paths, aliases: field.aliases, rules: rules}, nil
	}
	return lintSuppression{}, invalid
}

func suppressionIndent(line string) int {
	return len(line) - len(strings.TrimLeft(line, " \t"))
}

func collectSuppressionFields(node ast.Node, path string, fields map[int][]suppressionField) suppressionField {
	result := suppressionField{paths: map[string]struct{}{path: {}}}
	switch value := node.(type) {
	case *ast.MappingNode:
		for _, field := range value.Values {
			result.add(collectSuppressionFields(field, path, fields))
		}
	case *ast.MappingValueNode:
		key := value.Key.GetToken()
		child := key.Value
		if path != "" {
			child = path + "." + child
		}
		descendants := collectSuppressionFields(value.Value, child, fields)
		descendants.column = key.Position.Column
		fields[key.Position.Line] = append(fields[key.Position.Line], descendants)
		result.add(descendants)
	case *ast.SequenceNode:
		for index, item := range value.Values {
			result.add(collectSuppressionFields(item, path+"["+strconv.Itoa(index)+"]", fields))
		}
	case *ast.AnchorNode:
		result.add(collectSuppressionFields(value.Value, path, fields))
	case *ast.TagNode:
		result.add(collectSuppressionFields(value.Value, path, fields))
	case *ast.AliasNode:
		result.aliases = append(result.aliases, path)
	}
	return result
}

func (field *suppressionField) add(child suppressionField) {
	maps.Copy(field.paths, child.paths)
	field.aliases = append(field.aliases, child.aliases...)
}
