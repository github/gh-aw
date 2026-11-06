package gradershell

import (
	"path"
	"strings"

	"mvdan.cc/sh/v3/syntax"
)

// HasShellCodeEvaluation reports whether a parsed Bash script contains a
// command or input context that can reinterpret an expanded value as code.
// dynamicWord identifies GitHub expressions that were masked before parsing.
func HasShellCodeEvaluation(file *syntax.File, dynamicWord func(*syntax.Word) bool) bool {
	if file == nil {
		return false
	}
	isDynamic := func(word *syntax.Word) bool {
		if dynamicWord != nil && dynamicWord(word) {
			return true
		}
		_, literal := LiteralWord(word)
		return !literal
	}

	found := false
	syntax.Walk(file, func(node syntax.Node) bool {
		switch node := node.(type) {
		case *syntax.CallExpr:
			found = commandReevaluatesCode(node.Args, isDynamic, 0)
		case *syntax.BinaryCmd:
			found = strings.HasPrefix(node.Op.String(), "|") && commandTreeReadsShellStdin(node.Y.Cmd, isDynamic)
		case *syntax.Stmt:
			found = hasInputRedirection(node.Redirs) && commandTreeReadsShellStdin(node.Cmd, isDynamic)
		}
		return !found
	})
	return found
}

func commandReevaluatesCode(args []*syntax.Word, isDynamic func(*syntax.Word) bool, depth int) bool {
	if len(args) == 0 || depth > 16 {
		return depth > 16
	}
	if isDynamic(args[0]) {
		return true
	}
	name, literal := LiteralWord(args[0])
	if !literal {
		return true
	}
	name = path.Base(name)

	switch {
	case name == "eval":
		return true
	case name == "source" || name == ".":
		return sourceCommandEvaluatesCode(args[1:], isDynamic)
	case name == "pwsh" || name == "powershell":
		return powershellHasCodeFlag(args[1:], isDynamic)
	case shellCommand(name):
		return shellHasCodeFlag(args[1:], isDynamic)
	case interpreterHasCode(name, args[1:], isDynamic):
		return true
	case name == "trap":
		return trapEvaluatesCode(args[1:], isDynamic)
	case isCommandWrapper(name):
		if name == "env" && envUsesSplitString(args[1:]) {
			return true
		}
		nested, known := unwrapEvaluationWrapper(name, args[1:], isDynamic)
		if !known {
			return true
		}
		return commandReevaluatesCode(nested, isDynamic, depth+1)
	case name == "find":
		return findExecReevaluatesCode(args[1:], isDynamic, depth+1)
	case isContainerCommand(name):
		return containerReevaluatesCode(name, args[1:], isDynamic, depth+1)
	default:
		return false
	}
}

func shellCommand(name string) bool {
	switch name {
	case "bash", "sh", "dash", "ash", "zsh", "ksh", "mksh", "fish", "csh", "tcsh", "yash":
		return true
	default:
		return false
	}
}

func powershellHasCodeFlag(args []*syntax.Word, isDynamic func(*syntax.Word) bool) bool {
	optionsWithOperands := map[string]bool{
		"-configurationname": true, "-configurationfile": true, "-executionpolicy": true,
		"-inputformat": true, "-outputformat": true, "-settingsfile": true,
		"-workingdirectory": true, "-windowstyle": true, "-custompipename": true,
	}
	for i := 0; i < len(args); i++ {
		word := args[i] //nolint:uncheckedsliceindex // The loop bounds i before every iteration.
		if isDynamic(word) {
			return true
		}
		option, literal := LiteralWord(word)
		if !literal {
			return true
		}
		switch strings.ToLower(option) {
		case "-command", "-c", "-encodedcommand", "-e", "-commandwithargs":
			return true
		case "-file", "-f":
			if i+1 >= len(args) {
				return true
			}
			file, static := LiteralWord(args[i+1])
			return !static || isDynamic(args[i+1]) || scriptUsesStandardInput(file)
		}
		if optionsWithOperands[strings.ToLower(option)] {
			i++
			if i >= len(args) || isDynamic(args[i]) {
				return true
			}
			continue
		}
		switch strings.ToLower(option) {
		case "-noexit", "-nologo", "-noprofile", "-noninteractive", "-sta", "-mta", "-help", "-?", "-version":
			continue
		}
		if strings.HasPrefix(option, "-") {
			return true
		}
		return scriptUsesStandardInput(option)
	}
	return true
}

func shellHasCodeFlag(args []*syntax.Word, isDynamic func(*syntax.Word) bool) bool {
	for i := 0; i < len(args); i++ {
		word := args[i] //nolint:uncheckedsliceindex // The loop bounds i before every iteration.
		if isDynamic(word) {
			return true
		}
		arg, literal := LiteralWord(word)
		if !literal {
			return true
		}
		if arg == "--" {
			if i+1 == len(args) {
				return true
			}
			operand, literal := LiteralWord(args[i+1])
			return !literal || isDynamic(args[i+1]) || scriptUsesStandardInput(operand)
		}
		switch arg {
		case "-c", "--command":
			return true
		case "-s":
			return true
		case "-o", "+o", "-O", "+O", "--rcfile", "--init-file":
			i++
			if i >= len(args) || isDynamic(args[i]) {
				return true
			}
			continue
		}
		if strings.HasPrefix(arg, "--command=") {
			return true
		}
		if strings.HasPrefix(arg, "--rcfile=") || strings.HasPrefix(arg, "--init-file=") {
			continue
		}
		if strings.HasPrefix(arg, "-") && !strings.HasPrefix(arg, "--") {
			for _, flag := range strings.TrimPrefix(arg, "-") {
				switch flag {
				case 'c':
					return true
				case 's':
					return true
				case 'a', 'e', 'f', 'h', 'i', 'm', 'n', 'p', 'r', 'u', 'v', 'x', 'B', 'C', 'E', 'H', 'P', 'T', 'l', 'D':
				default:
					return true
				}
			}
			continue
		}
		if strings.HasPrefix(arg, "--") {
			switch arg {
			case "--help", "--version", "--noprofile", "--norc", "--posix", "--restricted", "--login", "--verbose", "--debugger", "--noediting":
				continue
			default:
				return true
			}
		}
		// The first non-option word is the shell script operand. Later words
		// belong to that script and are not shell options.
		return scriptUsesStandardInput(arg)
	}
	// With no script operand, the shell reads its program from standard input.
	return true
}

func interpreterHasCode(name string, args []*syntax.Word, isDynamic func(*syntax.Word) bool) bool {
	if !codeInterpreter(name) {
		return false
	}
	for i := 0; i < len(args); i++ {
		word := args[i] //nolint:uncheckedsliceindex // The loop bounds i before every iteration.
		if isDynamic(word) {
			return true
		}
		option, literal := LiteralWord(word)
		if !literal {
			return true
		}
		if option == "--" {
			if isAwk(name) {
				return true
			}
			if i+1 == len(args) {
				return true
			}
			file, static := LiteralWord(args[i+1])
			return !static || isDynamic(args[i+1]) || scriptUsesStandardInput(file)
		}
		switch name {
		case "python", "python2", "python3", "pypy", "pypy3":
			if option == "-c" || option == "--command" {
				return true
			}
			if option == "-m" || option == "--module" {
				return true
			}
			if option == "-W" || option == "-X" {
				i++
				if i >= len(args) || isDynamic(args[i]) {
					return true
				}
				continue
			}
		case "ruby":
			if option == "-e" || option == "--eval" {
				return true
			}
		case "perl":
			if option == "-e" || option == "-E" {
				return true
			}
		case "php":
			if option == "-r" {
				return true
			}
		case "node", "bun":
			if option == "-e" || option == "--eval" || option == "-p" || option == "--print" {
				return true
			}
		case "deno":
			if option == "eval" || option == "--eval" {
				return true
			}
			if option == "run" {
				return denoRunReadsCode(args[i+1:], isDynamic)
			}
		case "lua", "qjs", "js":
			if option == "-e" || option == "--eval" {
				return true
			}
		case "R", "Rscript":
			if option == "-e" || option == "--expression" {
				return true
			}
		case "awk", "gawk", "mawk", "nawk":
			if option == "-F" || option == "-v" || option == "--field-separator" || option == "--assign" {
				i++
				if i >= len(args) || isDynamic(args[i]) {
					return true
				}
				continue
			}
			if strings.HasPrefix(option, "-") {
				continue
			}
			return true
		}
		if strings.HasPrefix(option, "-") {
			continue
		}
		// A static script-file operand is data; following words are script args.
		return scriptUsesStandardInput(option)
	}
	// Without a script-file operand, these interpreters consume program text
	// from stdin (or enter an interactive mode that evaluates stdin).
	return true
}

func codeInterpreter(name string) bool {
	switch name {
	case "python", "python2", "python3", "pypy", "pypy3", "ruby", "perl", "php", "node", "bun", "deno", "lua", "qjs", "js", "awk", "gawk", "mawk", "nawk", "R", "Rscript":
		return true
	default:
		return false
	}
}

func isAwk(name string) bool {
	switch name {
	case "awk", "gawk", "mawk", "nawk":
		return true
	default:
		return false
	}
}

func denoRunReadsCode(args []*syntax.Word, isDynamic func(*syntax.Word) bool) bool {
	optionsWithOperands := map[string]bool{
		"--config": true, "--import-map": true, "--lock": true, "--seed": true,
		"--watch-exclude": true, "--cert": true, "--ext": true,
	}
	for i := 0; i < len(args); i++ {
		word := args[i] //nolint:uncheckedsliceindex // The loop bounds i before every iteration.
		if isDynamic(word) {
			return true
		}
		option, literal := LiteralWord(word)
		if !literal {
			return true
		}
		if option == "--" {
			if i+1 == len(args) {
				return true
			}
			script, static := LiteralWord(args[i+1])
			return !static || isDynamic(args[i+1]) || scriptUsesStandardInput(script)
		}
		if optionsWithOperands[option] {
			i++
			if i >= len(args) || isDynamic(args[i]) {
				return true
			}
			continue
		}
		if strings.HasPrefix(option, "-") {
			continue
		}
		return scriptUsesStandardInput(option)
	}
	return true
}

func sourceCommandEvaluatesCode(args []*syntax.Word, isDynamic func(*syntax.Word) bool) bool {
	if len(args) == 0 {
		return false
	}
	if option, literal := LiteralWord(args[0]); literal && option == "--" {
		args = args[1:]
		if len(args) == 0 {
			return false
		}
	}
	if isDynamic(args[0]) {
		return true
	}
	source, literal := LiteralWord(args[0])
	return !literal || scriptUsesStandardInput(source)
}

func trapEvaluatesCode(args []*syntax.Word, isDynamic func(*syntax.Word) bool) bool {
	if len(args) == 0 {
		return false
	}
	action, literal := LiteralWord(args[0])
	if !literal || isDynamic(args[0]) {
		return true
	}
	if action == "--" {
		return len(args) > 1
	}
	return action != "-l" && action != "-p" && action != "-P"
}

func isCommandWrapper(name string) bool {
	switch name {
	case "builtin", "command", "env", "exec", "nice", "nohup", "setsid", "sudo", "time", "timeout", "xargs", "busybox":
		return true
	default:
		return false
	}
}

func envUsesSplitString(args []*syntax.Word) bool {
	for _, word := range args {
		value, literal := LiteralWord(word)
		if !literal {
			return false
		}
		if value == "-S" || value == "--split-string" ||
			strings.HasPrefix(value, "-S") && len(value) > 2 ||
			strings.HasPrefix(value, "--split-string=") {
			return true
		}
		if !strings.HasPrefix(value, "-") && !strings.Contains(value, "=") {
			return false
		}
	}
	return false
}

func unwrapEvaluationWrapper(name string, args []*syntax.Word, isDynamic func(*syntax.Word) bool) ([]*syntax.Word, bool) {
	switch name {
	case "command", "exec", "env", "nice", "sudo":
		nested, _, known := unwrapCommand(name, args)
		return nested, known
	case "builtin":
		if len(args) == 0 {
			return nil, true
		}
		if arg, literal := LiteralWord(args[0]); literal && arg == "--" {
			return args[1:], true
		}
		return args, true
	case "nohup":
		return unwrapSimpleOptions(args, isDynamic, map[string]bool{"--": true, "--help": true, "--version": true}, nil)
	case "setsid":
		return unwrapSimpleOptions(args, isDynamic, map[string]bool{
			"--": true, "-c": true, "--ctty": true, "-f": true, "--fork": true,
			"-w": true, "--wait": true, "-n": true, "--no-stdin": true,
		}, map[string]bool{"-k": true, "--kill-after": true})
	case "timeout":
		return unwrapTimeout(args, isDynamic)
	case "time":
		return unwrapSimpleOptions(args, isDynamic, map[string]bool{
			"--": true, "-p": true, "--portability": true, "-a": true, "--append": true,
			"--verbose": true, "--quiet": true,
		}, map[string]bool{"-f": true, "--format": true, "-o": true, "--output": true})
	case "busybox":
		for i, word := range args {
			if isDynamic(word) {
				return nil, false
			}
			value, literal := LiteralWord(word)
			if !literal {
				return nil, false
			}
			if value == "--" {
				return args[i+1:], true
			}
			if !strings.HasPrefix(value, "-") {
				return args[i:], true
			}
		}
		return nil, true
	case "xargs":
		return unwrapXargs(args, isDynamic)
	default:
		return nil, false
	}
}

func unwrapSimpleOptions(args []*syntax.Word, isDynamic func(*syntax.Word) bool, flags, operands map[string]bool) ([]*syntax.Word, bool) {
	for i := 0; i < len(args); i++ {
		word := args[i] //nolint:uncheckedsliceindex // The loop bounds i before every iteration.
		if isDynamic(word) {
			return nil, false
		}
		value, literal := LiteralWord(word)
		if !literal {
			return nil, false
		}
		if value == "--" {
			return args[i+1:], true
		}
		if !strings.HasPrefix(value, "-") || value == "-" {
			return args[i:], true
		}
		if flags[value] {
			continue
		}
		if operands[value] {
			i++
			if i >= len(args) || isDynamic(args[i]) {
				return nil, false
			}
			continue
		}
		if strings.HasPrefix(value, "--") {
			knownAttached := false
			for option := range operands {
				if strings.HasPrefix(value, option+"=") {
					knownAttached = true
					break
				}
			}
			if knownAttached {
				continue
			}
		}
		return nil, false
	}
	return nil, true
}

func unwrapXargs(args []*syntax.Word, isDynamic func(*syntax.Word) bool) ([]*syntax.Word, bool) {
	operandOptions := map[string]bool{
		"-E": true, "-I": true, "-L": true, "-n": true, "-P": true, "-s": true,
		"-a": true, "-d": true, "-e": true, "--eof": true, "--replace": true,
		"--max-lines": true, "--max-args": true, "--max-procs": true, "--max-chars": true,
		"--arg-file": true, "--delimiter": true, "--process-slot-var": true,
	}
	for i := 0; i < len(args); i++ {
		word := args[i] //nolint:uncheckedsliceindex // The loop bounds i before every iteration.
		if isDynamic(word) {
			return nil, false
		}
		value, literal := LiteralWord(word)
		if !literal {
			return nil, false
		}
		if value == "--" {
			return args[i+1:], true
		}
		if !strings.HasPrefix(value, "-") || value == "-" {
			return args[i:], true
		}
		if operandOptions[value] {
			i++
			if i >= len(args) || isDynamic(args[i]) {
				return nil, false
			}
			continue
		}
		if strings.HasPrefix(value, "--") {
			knownAttached := false
			for option := range operandOptions {
				if strings.HasPrefix(value, option+"=") {
					knownAttached = true
					break
				}
			}
			if knownAttached {
				continue
			}
			switch value {
			case "--null", "--no-run-if-empty", "--verbose", "--exit", "--interactive", "--open-tty":
				continue
			default:
				return nil, false
			}
		}
		if value == "-0" || value == "-r" || value == "-t" || value == "-x" {
			continue
		}
		return nil, false
	}
	return nil, true
}

func unwrapTimeout(args []*syntax.Word, isDynamic func(*syntax.Word) bool) ([]*syntax.Word, bool) {
	optionsWithOperands := map[string]bool{"-k": true, "--kill-after": true, "-s": true, "--signal": true}
	for i := 0; i < len(args); i++ {
		word := args[i] //nolint:uncheckedsliceindex // The loop bounds i before every iteration.
		value, literal := LiteralWord(word)
		if !literal {
			return nil, false
		}
		if value == "--" {
			duration := i + 1
			if duration >= len(args) {
				return nil, false
			}
			return args[duration+1:], true
		}
		if !strings.HasPrefix(value, "-") || value == "-" {
			// The first positional argument is the duration, not the command.
			return args[i+1:], true
		}
		if isDynamic(word) {
			return nil, false
		}
		if optionsWithOperands[value] {
			i++
			if i >= len(args) {
				return nil, false
			}
			continue
		}
		if strings.HasPrefix(value, "--kill-after=") || strings.HasPrefix(value, "--signal=") {
			continue
		}
		switch value {
		case "--preserve-status", "--foreground", "-v", "--verbose":
			continue
		default:
			return nil, false
		}
	}
	return nil, false
}

func findExecReevaluatesCode(args []*syntax.Word, isDynamic func(*syntax.Word) bool, depth int) bool {
	for i := 0; i < len(args); i++ {
		value, literal := LiteralWord(args[i])
		if !literal {
			continue
		}
		switch value {
		case "-exec", "-execdir", "-ok", "-okdir":
			start := i + 1
			end := start
			for ; end < len(args); end++ {
				delimiter, known := LiteralWord(args[end]) //nolint:uncheckedsliceindex // The range bounds end.
				if known && (delimiter == ";" || delimiter == "+") {
					break
				}
			}
			if end == len(args) {
				return true
			}
			if commandReevaluatesCode(args[start:end], isDynamic, depth) {
				return true
			}
			i = end
		}
	}
	return false
}

func isContainerCommand(name string) bool {
	switch name {
	case "docker", "podman", "nerdctl":
		return true
	default:
		return false
	}
}

func containerReevaluatesCode(name string, args []*syntax.Word, isDynamic func(*syntax.Word) bool, depth int) bool {
	if len(args) == 0 {
		return false
	}
	subcommand, literal := LiteralWord(args[0])
	if !literal || isDynamic(args[0]) {
		return true
	}
	if subcommand != "run" && subcommand != "exec" {
		return false
	}
	start := 1
	var entrypoint string
	for start < len(args) {
		option, literal := LiteralWord(args[start]) //nolint:uncheckedsliceindex // The loop bounds start.
		if !literal {
			return true
		}
		if option == "--" {
			start++
			break
		}
		if !strings.HasPrefix(option, "-") {
			break
		}
		if value, found := strings.CutPrefix(option, "--entrypoint="); found {
			if isDynamic(args[start]) {
				return true
			}
			entrypoint = value
			start++
			continue
		}
		if strings.HasPrefix(option, "--env=") || strings.HasPrefix(option, "--volume=") ||
			strings.HasPrefix(option, "--publish=") || strings.HasPrefix(option, "--workdir=") ||
			strings.HasPrefix(option, "--name=") {
			start++
			continue
		}
		if isDynamic(args[start]) {
			return true
		}
		switch option {
		case "-e", "--env", "-v", "--volume", "-p", "--publish", "-w", "--workdir", "--name", "--entrypoint", "-u", "--user", "--network":
			start++
			if start >= len(args) {
				return true
			}
			if option == "--entrypoint" {
				var static bool
				entrypoint, static = LiteralWord(args[start])
				if !static || isDynamic(args[start]) {
					return true
				}
			}
		case "--rm", "-i", "-t", "-d", "--detach", "--interactive", "--tty", "--privileged", "--read-only":
		default:
			if len(option) > 2 && strings.HasPrefix(option, "-") && !strings.HasPrefix(option, "--") &&
				strings.Trim(option[1:], "itd") == "" {
				break
			}
			return true
		}
		start++
	}
	if start >= len(args) {
		return entrypoint != "" && commandReevaluatesCode([]*syntax.Word{{Parts: []syntax.WordPart{&syntax.Lit{Value: entrypoint}}}}, isDynamic, depth+1) ||
			subcommand == "run" && entrypoint == ""
	}
	if subcommand == "run" {
		start++ // The image name precedes its optional command.
	} else {
		start++ // The container name precedes the command for exec.
	}
	command := args[start:]
	if entrypoint != "" {
		command = append([]*syntax.Word{{Parts: []syntax.WordPart{&syntax.Lit{Value: entrypoint}}}}, command...)
	}
	if len(command) == 0 {
		return subcommand == "run"
	}
	return commandReevaluatesCode(command, isDynamic, depth+1)
}

func hasInputRedirection(redirections []*syntax.Redirect) bool {
	for _, redirection := range redirections {
		switch redirection.Op {
		case syntax.RdrIn, syntax.RdrInOut, syntax.DplIn, syntax.Hdoc, syntax.DashHdoc, syntax.WordHdoc:
			return true
		}
	}
	return false
}

func commandTreeReadsShellStdin(command syntax.Command, isDynamic func(*syntax.Word) bool) bool {
	reads := false
	syntax.Walk(command, func(node syntax.Node) bool {
		if call, ok := node.(*syntax.CallExpr); ok && commandReadsShellStdin(call.Args, isDynamic, 0) {
			reads = true
			return false
		}
		return !reads
	})
	return reads
}

func commandReadsShellStdin(args []*syntax.Word, isDynamic func(*syntax.Word) bool, depth int) bool {
	if len(args) == 0 || depth > 16 || isDynamic(args[0]) {
		return false
	}
	name, literal := LiteralWord(args[0])
	if !literal {
		return false
	}
	name = path.Base(name)
	if shellCommand(name) {
		return shellReadsStdin(args[1:], isDynamic)
	}
	if !isCommandWrapper(name) {
		return false
	}
	nested, known := unwrapEvaluationWrapper(name, args[1:], isDynamic)
	return known && commandReadsShellStdin(nested, isDynamic, depth+1)
}

func shellReadsStdin(args []*syntax.Word, isDynamic func(*syntax.Word) bool) bool {
	for i := 0; i < len(args); i++ {
		if isDynamic(args[i]) {
			return true
		}
		option, literal := LiteralWord(args[i])
		if !literal {
			return true
		}
		if option == "--" {
			if i+1 == len(args) {
				return true
			}
			operand, literal := LiteralWord(args[i+1])
			return !literal || isDynamic(args[i+1]) || scriptUsesStandardInput(operand)
		}
		if option == "-s" {
			return true
		}
		switch option {
		case "-o", "+o", "-O", "+O", "--rcfile", "--init-file":
			i++
			if i >= len(args) || isDynamic(args[i]) {
				return true
			}
			continue
		}
		if strings.HasPrefix(option, "-") {
			if strings.ContainsRune(option[1:], 'c') {
				return scriptUsesStandardInput(option)
			}
			continue
		}
		return false
	}
	return true
}

func scriptUsesStandardInput(value string) bool {
	switch value {
	case "-", "/dev/stdin":
		return true
	default:
		fd, isDeviceFD := strings.CutPrefix(value, "/dev/fd/")
		if !isDeviceFD {
			fd, isDeviceFD = strings.CutPrefix(value, "/proc/self/fd/")
		}
		return isDeviceFD && numericFD(fd)
	}
}
