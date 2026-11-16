package shellanalysis

import (
	"strings"

	"mvdan.cc/sh/v3/syntax"
)

func unwrapCommand(name string, args []*syntax.Word) (tail []*syntax.Word, query, known bool) {
	for i := 0; i < len(args); i++ {
		value, static := LiteralWord(args[i])            //nolint:uncheckedsliceindex // The loop bounds the nonnegative index.
		if name == "env" && envAssignmentWord(args[i]) { //nolint:uncheckedsliceindex // The loop bounds the nonnegative index.
			continue
		}
		if !static {
			return args[i:], false, true
		}
		if value == "--" {
			return args[i+1:], false, true
		}
		if !strings.HasPrefix(value, "-") || value == "-" {
			return args[i:], false, true
		}
		switch name {
		case "command":
			if value == "-v" || value == "-V" {
				return nil, true, true
			}
			if value != "-p" {
				return nil, false, false
			}
		case "exec":
			switch value {
			case "-a":
				i++
			case "-c", "-l", "-cl", "-lc":
			default:
				return nil, false, false
			}
		case "env":
			switch {
			case value == "-i", value == "--ignore-environment":
			case value == "-u", value == "--unset", value == "-C", value == "--chdir":
				i++
			case strings.HasPrefix(value, "--unset="), strings.HasPrefix(value, "--chdir="):
			default:
				return nil, false, false
			}
		case "sudo":
			switch value {
			case "-u", "--user", "-g", "--group", "-h", "--host":
				i++
			case "-n", "-E", "-H", "--non-interactive", "--preserve-env", "--set-home":
			default:
				return nil, false, false
			}
		case "nice":
			if value == "-n" || value == "--adjustment" {
				i++
			} else if !strings.HasPrefix(value, "--adjustment=") {
				return nil, false, false
			}
		default:
			return nil, false, false
		}
	}
	return nil, false, true
}

func envAssignmentWord(word *syntax.Word) bool {
	if word == nil || len(word.Parts) == 0 {
		return false
	}
	first, ok := word.Parts[0].(*syntax.Lit) //nolint:uncheckedsliceindex // The preceding guard requires a nonempty parts slice.
	if !ok {
		return false
	}
	name, _, assignment := strings.Cut(first.Value, "=")
	if !assignment || name == "" {
		return false
	}
	for i, char := range name {
		if i == 0 {
			if (char < 'A' || char > 'Z') && (char < 'a' || char > 'z') && char != '_' {
				return false
			}
			continue
		}
		if (char < 'A' || char > 'Z') && (char < 'a' || char > 'z') &&
			(char < '0' || char > '9') && char != '_' {
			return false
		}
	}
	return true
}

func numericFD(value string) bool {
	if value == "" {
		return false
	}
	for _, char := range value {
		if char < '0' || char > '9' {
			return false
		}
	}
	return true
}
