package taint

import (
	"maps"
	"slices"
	"strings"

	"mvdan.cc/sh/v3/syntax"
)

func (s *shell) assign(assign *syntax.Assign) {
	if assign.Name == nil {
		return
	}
	if assign.Value == nil && assign.Array == nil {
		return
	}
	assigned := s.word(assign.Value)
	s.vars[assign.Name.Value] = union(s.vars[assign.Name.Value], assigned)
	texts, known := wordTexts(assign.Value, s.constants, s.constantValues, s.constantUnknown)
	if known && len(assigned) == 0 && assign.Array == nil {
		s.setConstants(assign.Name.Value, texts, false)
	} else if len(assigned) == 0 {
		s.setConstants(assign.Name.Value, texts, true)
	} else {
		s.setConstants(assign.Name.Value, nil, true)
	}
	if assign.Array != nil {
		for _, elem := range assign.Array.Elems {
			s.vars[assign.Name.Value] = union(s.vars[assign.Name.Value], s.word(elem.Value))
		}
	}
}

func (s *shell) setConstants(name string, values []string, unknown bool) {
	unique := make(map[string]struct{}, len(values))
	for _, value := range values {
		unique[value] = struct{}{}
	}
	if len(unique) == 0 {
		delete(s.constantValues, name)
		delete(s.constants, name)
	} else {
		s.constantValues[name] = unique
		if len(unique) == 1 && !unknown {
			for value := range unique {
				s.constants[name] = value
			}
		} else {
			delete(s.constants, name)
		}
	}
	if unknown {
		s.constantUnknown[name] = true
	} else {
		delete(s.constantUnknown, name)
	}
}

func (s *shell) constantState() constantState {
	return constantState{
		values:  cloneConstantValues(s.constantValues),
		unknown: maps.Clone(s.constantUnknown),
	}
}

func (s *shell) restoreConstants(state constantState) {
	s.constantValues = cloneConstantValues(state.values)
	s.constantUnknown = maps.Clone(state.unknown)
	s.constants = make(map[string]string)
	for name, values := range state.values {
		if len(values) == 1 && !state.unknown[name] {
			for value := range values {
				s.constants[name] = value
			}
		}
	}
}

func mergeConstantStates(states ...constantState) constantState {
	merged := constantState{
		values: make(map[string]map[string]struct{}), unknown: make(map[string]bool),
	}
	names := make(map[string]struct{})
	for _, state := range states {
		for name := range state.values {
			names[name] = struct{}{}
		}
		for name := range state.unknown {
			names[name] = struct{}{}
		}
	}
	for name := range names {
		values := make(map[string]struct{})
		unknown := false
		for _, state := range states {
			current := state.values[name]
			if len(current) == 0 || state.unknown[name] {
				unknown = true
			}
			for value := range current {
				values[value] = struct{}{}
			}
		}
		if len(values) > 0 {
			merged.values[name] = values
		}
		if unknown {
			merged.unknown[name] = true
		}
	}
	return merged
}

func cloneConstantValues(values map[string]map[string]struct{}) map[string]map[string]struct{} {
	cloned := make(map[string]map[string]struct{}, len(values))
	for name, candidates := range values {
		cloned[name] = maps.Clone(candidates)
	}
	return cloned
}

func wordTexts(word *syntax.Word, constants map[string]string, alternatives map[string]map[string]struct{}, unknownValues map[string]bool) ([]string, bool) {
	if word == nil {
		return nil, false
	}
	known := true
	texts := []string{""}
	appendValue := func(values []string) {
		if len(texts) > 0 && len(values) > 64/len(texts) {
			known = false
			texts = nil
			return
		}
		combined := make([]string, 0, len(texts)*len(values))
		for _, prefix := range texts {
			for _, value := range values {
				combined = append(combined, prefix+value)
			}
		}
		texts = combined
	}
	syntax.Walk(word, func(node syntax.Node) bool {
		switch n := node.(type) {
		case *syntax.Lit:
			appendValue([]string{n.Value})
		case *syntax.SglQuoted:
			appendValue([]string{n.Value})
		case *syntax.ParamExp:
			if n.Param == nil {
				known = false
				return false
			}
			name := n.Param.Value
			var candidates []string
			for value := range alternatives[name] {
				candidates = append(candidates, value)
			}
			if len(candidates) == 0 {
				if value, ok := constants[name]; ok {
					candidates = []string{value}
				} else {
					known = false
					texts = nil
					return false
				}
			}
			if unknownValues[name] || n.Excl {
				known = false
			}
			slices.Sort(candidates)
			appendValue(candidates)
			return false
		case *syntax.CmdSubst:
			known = false
			texts = nil
			return false
		}
		return known
	})
	return texts, known
}

func sliceAt[T any](values []T, index int) (T, bool) {
	if index >= 0 && index < len(values) {
		return values[index], true
	}
	var zero T
	return zero, false
}

func staticWord(word *syntax.Word) string {
	return wordText(word, nil)
}

func wordText(word *syntax.Word, constants map[string]string) string {
	if word == nil {
		return ""
	}
	var text strings.Builder
	syntax.Walk(word, func(node syntax.Node) bool {
		switch n := node.(type) {
		case *syntax.Lit:
			text.WriteString(n.Value)
		case *syntax.SglQuoted:
			text.WriteString(n.Value)
		case *syntax.ParamExp:
			if n.Param != nil {
				if literal, exists := constants[n.Param.Value]; exists {
					text.WriteString(literal)
				} else {
					text.WriteString("$" + n.Param.Value)
				}
			}
			return false
		case *syntax.CmdSubst:
			return false
		}
		return true
	})
	return text.String()
}
