package taint

import (
	"maps"
	"strings"

	"mvdan.cc/sh/v3/syntax"
)

func (s *shell) env(cmd *syntax.CallExpr, stdin value) value {
	child := *s
	child.vars = maps.Clone(s.vars)
	child.constants = maps.Clone(s.constants)
	child.constantValues = cloneConstantValues(s.constantValues)
	child.constantUnknown = maps.Clone(s.constantUnknown)
	args := cmd.Args[1:]
	commandIndex := 0
	for commandIndex < len(args) {
		word, ok := sliceAt(args, commandIndex)
		if !ok {
			break
		}
		text := staticWord(word)
		switch text {
		case "-i", "--ignore-environment":
			clear(child.vars)
			clear(child.constants)
			clear(child.constantValues)
			clear(child.constantUnknown)
		case "-u", "--unset":
			commandIndex = unsetEnvironmentVariable(&child, args, commandIndex)
		case "--":
			commandIndex++
			goto command
		default:
			if name, _, ok := strings.Cut(text, "="); ok && name != "" {
				child.setEnvironmentArgument(name, word)
			} else if strings.HasPrefix(text, "-") {
				s.a.gap(s.path, "env options are not fully modeled")
			} else {
				goto command
			}
		}
		commandIndex++
	}
	return child.environmentOutput()

command:
	if commandIndex >= len(args) {
		return child.environmentOutput()
	}
	commandArgs := &syntax.CallExpr{Args: args[commandIndex:]}
	child.expandedArgs = maps.Clone(s.expandedArgs)
	for _, arg := range commandArgs.Args[1:] {
		child.expandedArgs[arg] = s.word(arg)
	}
	commandWord, ok := sliceAt(commandArgs.Args, 0)
	if ok && !knownShellCommand(literalShellWord(commandWord)) {
		for _, sources := range child.vars {
			if len(sources) > 0 {
				s.a.gap(s.path, "env command environment data flow is not modeled")
				break
			}
		}
	}
	return child.call(commandArgs, true, stdin)
}

func unsetEnvironmentVariable(child *shell, args []*syntax.Word, index int) int {
	index++
	if nameWord, ok := sliceAt(args, index); ok {
		name := staticWord(nameWord)
		delete(child.vars, name)
		delete(child.constants, name)
		delete(child.constantValues, name)
		delete(child.constantUnknown, name)
	}
	return index
}

func (s *shell) setEnvironmentArgument(name string, word *syntax.Word) {
	s.vars[name] = s.word(word)
	literals, known := wordTexts(word, s.constants, s.constantValues, s.constantUnknown)
	values := make([]string, 0, len(literals))
	for _, literal := range literals {
		if _, value, found := strings.Cut(literal, "="); found {
			values = append(values, value)
		}
	}
	if len(s.vars[name]) > 0 {
		s.setConstants(name, nil, true)
	} else if known {
		s.setConstants(name, values, false)
	} else {
		s.setConstants(name, literals, true)
	}
}

func (s *shell) environmentOutput() value {
	var out value
	for _, sources := range s.vars {
		out = union(out, sources)
	}
	return out
}
