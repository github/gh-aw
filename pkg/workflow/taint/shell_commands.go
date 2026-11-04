package taint

import (
	"strings"

	"mvdan.cc/sh/v3/syntax"
)

func knownShellCommand(name string) bool {
	switch name {
	case "echo", "printf", "env", "printenv", "cat", "head", "tail", "base64", "openssl",
		"jq", "sed", "awk", "tr", "cut", "tee", "curl", "wget", "set", "true", "false", ":", "test", "[":
		return true
	default:
		return false
	}
}

func isMaskRegistration(cmd *syntax.CallExpr) bool {
	if len(cmd.Args) < 2 {
		return false
	}
	command, ok := sliceAt(cmd.Args, 0)
	if !ok {
		return false
	}
	switch staticWord(command) {
	case "echo":
		argument, ok := sliceAt(cmd.Args, 1)
		if !ok {
			return false
		}
		text := staticWord(argument)
		return len(cmd.Args) == 2 && strings.HasPrefix(text, "::add-mask::") &&
			!strings.ContainsAny(text, "\r\n")
	case "printf":
		if len(cmd.Args) != 3 {
			return false
		}
		formatWord, ok := sliceAt(cmd.Args, 1)
		if !ok {
			return false
		}
		valueWord, ok := sliceAt(cmd.Args, 2)
		if !ok {
			return false
		}
		format := staticWord(formatWord)
		return (format == "::add-mask::%s" || format == "::add-mask::%s\\n") &&
			!strings.ContainsAny(staticWord(valueWord), "\r\n")
	default:
		return false
	}
}

func shellInvocationTraces(shellName string) bool {
	fields := strings.Fields(shellName)
	tracing := false
	for index := 1; index < len(fields); index++ {
		flag, ok := sliceAt(fields, index)
		if !ok {
			break
		}
		if flag == "-o" || flag == "+o" {
			if next, ok := sliceAt(fields, index+1); ok && next == "xtrace" {
				tracing = flag == "-o"
				index++
			}
		} else if strings.HasPrefix(flag, "-") && strings.Contains(flag[1:], "x") {
			tracing = true
		} else if strings.HasPrefix(flag, "+") && strings.Contains(flag[1:], "x") {
			tracing = false
		}
	}
	return tracing
}

func applyTraceOptions(current bool, args []*syntax.Word) bool {
	for index := 0; index < len(args); index++ {
		word, ok := sliceAt(args, index)
		if !ok {
			break
		}
		flag := staticWord(word)
		if flag == "-o" || flag == "+o" {
			if next, ok := sliceAt(args, index+1); ok && staticWord(next) == "xtrace" {
				current = flag == "-o"
			}
			index++
		} else if strings.HasPrefix(flag, "-") && strings.Contains(flag[1:], "x") {
			current = true
		} else if strings.HasPrefix(flag, "+") && strings.Contains(flag[1:], "x") {
			current = false
		}
	}
	return current
}

func specialTarget(target string) bool {
	return target == "$GITHUB_STEP_SUMMARY" || target == "$GITHUB_OUTPUT" || target == "$GITHUB_ENV"
}
