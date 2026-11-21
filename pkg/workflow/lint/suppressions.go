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
var suppressionMarkerPattern = regexp.MustCompile(`#\s*gh-aw:`)

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

type suppressionAnchor struct {
	base  string
	paths map[string]struct{}
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
	if input.Workflow != nil && suppressionMarkerPattern.MatchString(input.Workflow.OriginalSource()) {
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

func (s lintSuppressions) contains(input Input, rule Rule, diagnostic Diagnostic) bool {
	scopes := s.aw
	owner := diagnostic.Source
	if owner == 0 {
		owner = rule.Targets
		if owner == 0 {
			owner = ActionsAST
		}
		if input.Workflow == nil {
			owner = AWAST
		}
	}
	if owner&ActionsAST != 0 {
		scopes = s.actions
	}
	return slices.ContainsFunc(scopes, func(scope lintSuppression) bool {
		_, ruleMatches := scope.rules[rule.ID]
		_, pathMatches := scope.paths[diagnostic.Path]
		return ruleMatches && pathMatches
	})
}

func parseLintSuppressions(source string, known map[string]struct{}) ([]lintSuppression, error) {
	if !suppressionMarkerPattern.MatchString(source) {
		return nil, nil
	}
	source = strings.ReplaceAll(source, "\r\n", "\n")
	tokens := lexer.Tokenize(source)
	file, err := parser.Parse(tokens, parser.ParseComments)
	if err != nil {
		return nil, errors.New("cannot parse YAML comments for lint suppressions")
	}
	fields := make(map[int][]suppressionField)
	anchors := make(map[string]suppressionAnchor)
	for _, document := range file.Docs {
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
		text := strings.TrimSpace(strings.TrimPrefix(item.Value, "#"))
		if !strings.HasPrefix(text, "gh-aw:") {
			continue
		}
		directive := strings.TrimSpace(strings.TrimPrefix(text, "gh-aw:"))
		if !strings.HasPrefix(directive, "ignore") {
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

func collectSuppressionFields(node ast.Node, path string, fields map[int][]suppressionField, anchors map[string]suppressionAnchor) suppressionField {
	result := suppressionField{paths: map[string]struct{}{path: {}}}
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
		fields[key.Position.Line] = append(fields[key.Position.Line], descendants)
		result.add(descendants)
	case *ast.SequenceNode:
		for index, item := range value.Values {
			result.add(collectSuppressionFields(item, path+"["+strconv.Itoa(index)+"]", fields, anchors))
		}
	case *ast.AnchorNode:
		descendants := collectSuppressionFields(value.Value, path, fields, anchors)
		result.add(descendants)
		if value.Name != nil && value.Name.GetToken() != nil {
			anchors[strings.Trim(value.Name.GetToken().Value, "& ")] = suppressionAnchor{base: path, paths: descendants.paths}
		}
	case *ast.TagNode:
		result.add(collectSuppressionFields(value.Value, path, fields, anchors))
	case *ast.AliasNode:
		name := ""
		if value.GetToken() != nil {
			name = strings.Trim(value.GetToken().Value, "* ")
		}
		if target, ok := anchors[name]; ok {
			for targetPath := range target.paths {
				if targetPath == target.base {
					result.paths[path] = struct{}{}
				} else if strings.HasPrefix(targetPath, target.base+".") {
					result.paths[path+strings.TrimPrefix(targetPath, target.base)] = struct{}{}
				} else if strings.HasPrefix(targetPath, target.base+"[") {
					result.paths[path+strings.TrimPrefix(targetPath, target.base)] = struct{}{}
				}
			}
		} else {
			result.paths[path] = struct{}{}
			result.paths[path+".image"] = struct{}{}
		}
	}
	return result
}

func (field *suppressionField) add(child suppressionField) {
	maps.Copy(field.paths, child.paths)
	field.aliases = append(field.aliases, child.aliases...)
}
