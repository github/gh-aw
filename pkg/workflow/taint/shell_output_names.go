package taint

import (
	"strings"

	"mvdan.cc/sh/v3/syntax"
)

func commandOutputName(command syntax.Command, markers scope) string {
	switch cmd := command.(type) {
	case *syntax.CallExpr:
		if len(cmd.Args) < 2 {
			return ""
		}
		nameWord, ok := sliceAt(cmd.Args, 0)
		if !ok {
			return ""
		}
		name := staticWord(nameWord)
		if name != "echo" && name != "printf" {
			return ""
		}
		for _, arg := range cmd.Args[1:] {
			text := staticWord(arg)
			key, _, found := strings.Cut(text, "=")
			if !found {
				key, _, found = strings.Cut(text, "<<")
			}
			if found {
				key = strings.TrimSpace(key)
				if strings.ContainsAny(key, "$%") || containsMarker(key, markers) {
					return ""
				}
				return key + "="
			}
		}
	case *syntax.Block:
		for _, stmt := range cmd.Stmts {
			if name := commandOutputName(stmt.Cmd, markers); name != "" {
				return name
			}
		}
	case *syntax.BinaryCmd:
		if cmd.X != nil {
			if name := commandOutputName(cmd.X.Cmd, markers); name != "" {
				return name
			}
		}
		if cmd.Y != nil {
			return commandOutputName(cmd.Y.Cmd, markers)
		}
	}
	return ""
}

func (s *shell) outputName(command syntax.Command, target string) string {
	if target != "$GITHUB_OUTPUT" {
		return commandOutputName(command, s.markers)
	}
	// Compound output may contain several distinct records. Track the union
	// under a wildcard rather than treating its first name as the entire write.
	call, ok := command.(*syntax.CallExpr)
	if !ok || len(call.Args) < 2 {
		return ""
	}
	commandWord, _ := sliceAt(call.Args, 0)
	first, _ := sliceAt(call.Args, 1)
	name := literalShellWord(commandWord)
	if name == "echo" && len(call.Args) == 2 && singleOutputWord(first) {
		return literalOutputName(first, s.markers)
	}
	if name == "printf" && len(call.Args) == 3 {
		second, _ := sliceAt(call.Args, 2)
		format := strings.TrimSuffix(literalShellWord(first), `\n`)
		if format == "%s" && singleOutputWord(second) {
			return literalOutputName(second, s.markers)
		}
		if key, ok := strings.CutSuffix(format, "=%s"); ok && outputIdentifier.MatchString(key) && !containsMarker(key, s.markers) && singleOutputWord(second) {
			return key + "="
		}
	}
	return ""
}

func singleOutputWord(word *syntax.Word) bool {
	if strings.ContainsAny(staticWord(word), "\r\n\\") {
		return false
	}
	known := true
	syntax.Walk(word, func(node syntax.Node) bool {
		if quoted, ok := node.(*syntax.SglQuoted); ok && quoted.Dollar {
			// ANSI-C escapes are raw in the AST and may introduce extra records
			// or change names. Do not infer transport names without decoding them.
			known = false
		}
		return known
	})
	return known
}

func (s *shell) outputMayBeMultiline(command syntax.Command) bool {
	call, ok := command.(*syntax.CallExpr)
	if !ok || len(call.Args) < 2 {
		return true
	}
	commandWord, exists := sliceAt(call.Args, 0)
	if !exists {
		return true
	}
	name := literalShellWord(commandWord)
	wordIndex := 1
	if name == "printf" {
		wordIndex = 2
	}
	word, exists := sliceAt(call.Args, wordIndex)
	if !exists {
		return true
	}
	uncertain := false
	syntax.Walk(word, func(node syntax.Node) bool {
		switch n := node.(type) {
		case *syntax.ParamExp, *syntax.CmdSubst:
			uncertain = true
		case *syntax.Lit:
			uncertain = uncertain || strings.ContainsAny(n.Value, "\\\r\n")
		case *syntax.SglQuoted:
			uncertain = uncertain || n.Dollar || strings.ContainsAny(n.Value, "\\\r\n")
		case *syntax.Word, *syntax.DblQuoted:
		default:
			uncertain = true
		}
		return !uncertain
	})
	return uncertain
}

func (s *shell) outputRecordComplete(command syntax.Command) bool {
	call, ok := command.(*syntax.CallExpr)
	if !ok || len(call.Args) < 2 {
		return false
	}
	commandWord, exists := sliceAt(call.Args, 0)
	if !exists {
		return false
	}
	switch literalShellWord(commandWord) {
	case "echo":
		return true
	case "printf":
		format, exists := sliceAt(call.Args, 1)
		if !exists {
			return false
		}
		texts, known := wordTexts(format, s.constants, s.constantValues, s.constantUnknown)
		text, hasText := sliceAt(texts, 0)
		return known && hasText && len(texts) == 1 && strings.HasSuffix(text, `\n`)
	default:
		return false
	}
}

// The name must precede every expansion. wordText alone is insufficient:
// command substitutions are deliberately omitted from its diagnostic text.

func literalOutputName(word *syntax.Word, markers scope) string {
	var prefix strings.Builder
	known, complete := true, false
	syntax.Walk(word, func(node syntax.Node) bool {
		if node == nil || !known || complete {
			return false
		}
		switch n := node.(type) {
		case *syntax.Word, *syntax.DblQuoted:
			return true
		case *syntax.Lit:
			prefix.WriteString(n.Value)
		case *syntax.SglQuoted:
			prefix.WriteString(n.Value)
		default:
			known = false
			return false
		}
		text := prefix.String()
		name, _, found := strings.Cut(text, "=")
		if !found {
			name, _, found = strings.Cut(text, "<<")
		}
		if found {
			complete = true
			prefix.Reset()
			prefix.WriteString(name)
		}
		return false
	})
	name := prefix.String()
	if !known || !complete || containsMarker(name, markers) || !outputIdentifier.MatchString(name) {
		return ""
	}
	return name + "="
}

func containsMarker(text string, markers scope) bool {
	for marker := range markers {
		if strings.Contains(text, marker) {
			return true
		}
	}
	return false
}

func heredocOutputName(word *syntax.Word, constants map[string]string, markers scope) string {
	text := wordText(word, constants)
	text, _, _ = strings.Cut(text, "\n")
	key, _, found := strings.Cut(text, "=")
	if !found {
		key, _, found = strings.Cut(text, "<<")
	}
	if !found {
		return ""
	}
	key = strings.TrimSpace(key)
	if strings.ContainsAny(key, "$%") || containsMarker(key, markers) {
		return ""
	}
	return key + "="
}
