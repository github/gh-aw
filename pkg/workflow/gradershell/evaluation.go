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
	word, exists := evaluationArgument(args, 0)
	if !exists || depth > 16 {
		return depth > 16
	}
	if isDynamic(word) {
		return true
	}
	name, literal := LiteralWord(word)
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

func evaluationArgument(args []*syntax.Word, index int) (*syntax.Word, bool) {
	if index < 0 || index >= len(args) {
		return nil, false
	}
	return args[index], true
}

func argumentIsDynamic(args []*syntax.Word, index int, isDynamic func(*syntax.Word) bool) bool {
	word, exists := evaluationArgument(args, index)
	return !exists || isDynamic(word)
}

func scriptArgumentReadsCode(args []*syntax.Word, index int, isDynamic func(*syntax.Word) bool) bool {
	word, exists := evaluationArgument(args, index)
	if !exists {
		return true
	}
	script, literal := LiteralWord(word)
	return !literal || isDynamic(word) || scriptUsesStandardInput(script)
}

func powershellHasCodeFlag(args []*syntax.Word, isDynamic func(*syntax.Word) bool) bool {
	optionsWithOperands := map[string]struct{}{
		"-configurationname": {}, "-configurationfile": {}, "-executionpolicy": {},
		"-inputformat": {}, "-outputformat": {}, "-settingsfile": {},
		"-workingdirectory": {}, "-windowstyle": {}, "-custompipename": {},
	}
	for i := 0; ; i++ {
		word, exists := evaluationArgument(args, i)
		if !exists {
			break
		}
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
			return scriptArgumentReadsCode(args, i+1, isDynamic)
		}
		if _, found := optionsWithOperands[strings.ToLower(option)]; found {
			i++
			if argumentIsDynamic(args, i, isDynamic) {
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
	for i := 0; ; i++ {
		word, exists := evaluationArgument(args, i)
		if !exists {
			break
		}
		if isDynamic(word) {
			return true
		}
		arg, literal := LiteralWord(word)
		if !literal {
			return true
		}
		if arg == "--" {
			return scriptArgumentReadsCode(args, i+1, isDynamic)
		}
		switch arg {
		case "-c", "--command":
			return true
		case "-s":
			return true
		case "-o", "+o", "-O", "+O", "--rcfile", "--init-file":
			i++
			if argumentIsDynamic(args, i, isDynamic) {
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
			if shellShortOptionsEvaluateCode(arg) {
				return true
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

func shellShortOptionsEvaluateCode(option string) bool {
	for _, flag := range strings.TrimPrefix(option, "-") {
		switch flag {
		case 'a', 'e', 'f', 'h', 'i', 'm', 'n', 'p', 'r', 'u', 'v', 'x', 'B', 'C', 'E', 'H', 'P', 'T', 'l', 'D':
		default:
			return true
		}
	}
	return false
}

func interpreterHasCode(name string, args []*syntax.Word, isDynamic func(*syntax.Word) bool) bool {
	if !codeInterpreter(name) {
		return false
	}
	for i := 0; ; i++ {
		word, exists := evaluationArgument(args, i)
		if !exists {
			break
		}
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
			return scriptArgumentReadsCode(args, i+1, isDynamic)
		}
		if interpreterOptionEvaluatesCode(name, option) {
			return true
		}
		if interpreterOptionHasOperand(name, option) {
			i++
			if argumentIsDynamic(args, i, isDynamic) {
				return true
			}
			continue
		}
		if name == "deno" && option == "run" {
			return denoRunReadsCode(args[i+1:], isDynamic)
		}
		if strings.HasPrefix(option, "-") {
			continue
		}
		if isAwk(name) {
			return true
		}
		// A static script-file operand is data; following words are script args.
		return scriptUsesStandardInput(option)
	}
	// Without a script-file operand, these interpreters consume program text
	// from stdin (or enter an interactive mode that evaluates stdin).
	return true
}

func interpreterOptionEvaluatesCode(name, option string) bool {
	switch name {
	case "python", "python2", "python3", "pypy", "pypy3":
		return option == "-c" || option == "--command" || option == "-m" || option == "--module"
	case "ruby":
		return option == "-e" || option == "--eval"
	case "perl":
		return option == "-e" || option == "-E"
	case "php":
		return option == "-r"
	case "node", "bun":
		return option == "-e" || option == "--eval" || option == "-p" || option == "--print"
	case "deno":
		return option == "eval" || option == "--eval"
	case "lua", "qjs", "js":
		return option == "-e" || option == "--eval"
	case "R", "Rscript":
		return option == "-e" || option == "--expression"
	}
	return false
}

func interpreterOptionHasOperand(name, option string) bool {
	switch name {
	case "python", "python2", "python3", "pypy", "pypy3":
		return option == "-W" || option == "-X"
	case "awk", "gawk", "mawk", "nawk":
		return option == "-F" || option == "-v" || option == "--field-separator" || option == "--assign"
	default:
		return false
	}
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
	optionsWithOperands := map[string]struct{}{
		"--config": {}, "--import-map": {}, "--lock": {}, "--seed": {},
		"--watch-exclude": {}, "--cert": {}, "--ext": {},
	}
	for i := 0; ; i++ {
		word, exists := evaluationArgument(args, i)
		if !exists {
			break
		}
		if isDynamic(word) {
			return true
		}
		option, literal := LiteralWord(word)
		if !literal {
			return true
		}
		if option == "--" {
			return scriptArgumentReadsCode(args, i+1, isDynamic)
		}
		if _, found := optionsWithOperands[option]; found {
			i++
			if argumentIsDynamic(args, i, isDynamic) {
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
	word, exists := evaluationArgument(args, 0)
	if !exists {
		return false
	}
	if option, literal := LiteralWord(word); literal && option == "--" {
		word, exists = evaluationArgument(args, 1)
		if !exists {
			return false
		}
	}
	if isDynamic(word) {
		return true
	}
	source, literal := LiteralWord(word)
	return !literal || scriptUsesStandardInput(source)
}

func trapEvaluatesCode(args []*syntax.Word, isDynamic func(*syntax.Word) bool) bool {
	word, exists := evaluationArgument(args, 0)
	if !exists {
		return false
	}
	action, literal := LiteralWord(word)
	if !literal || isDynamic(word) {
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
		word, exists := evaluationArgument(args, 0)
		if !exists {
			return nil, true
		}
		if arg, literal := LiteralWord(word); literal && arg == "--" {
			return args[1:], true
		}
		return args, true
	case "nohup":
		return unwrapSimpleOptions(args, isDynamic, map[string]struct{}{"--": {}, "--help": {}, "--version": {}}, nil)
	case "setsid":
		return unwrapSimpleOptions(args, isDynamic, map[string]struct{}{
			"--": {}, "-c": {}, "--ctty": {}, "-f": {}, "--fork": {},
			"-w": {}, "--wait": {}, "-n": {}, "--no-stdin": {},
		}, map[string]struct{}{"-k": {}, "--kill-after": {}})
	case "timeout":
		return unwrapTimeout(args, isDynamic)
	case "time":
		return unwrapSimpleOptions(args, isDynamic, map[string]struct{}{
			"--": {}, "-p": {}, "--portability": {}, "-a": {}, "--append": {},
			"--verbose": {}, "--quiet": {},
		}, map[string]struct{}{"-f": {}, "--format": {}, "-o": {}, "--output": {}})
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

func unwrapSimpleOptions(args []*syntax.Word, isDynamic func(*syntax.Word) bool, flags, operands map[string]struct{}) ([]*syntax.Word, bool) {
	for i := 0; ; i++ {
		word, exists := evaluationArgument(args, i)
		if !exists {
			break
		}
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
		if _, found := flags[value]; found {
			continue
		}
		if _, found := operands[value]; found {
			i++
			if argumentIsDynamic(args, i, isDynamic) {
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
	operandOptions := map[string]struct{}{
		"-E": {}, "-I": {}, "-L": {}, "-n": {}, "-P": {}, "-s": {},
		"-a": {}, "-d": {}, "-e": {}, "--eof": {}, "--replace": {},
		"--max-lines": {}, "--max-args": {}, "--max-procs": {}, "--max-chars": {},
		"--arg-file": {}, "--delimiter": {}, "--process-slot-var": {},
	}
	for i := 0; ; i++ {
		word, exists := evaluationArgument(args, i)
		if !exists {
			break
		}
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
		if _, found := operandOptions[value]; found {
			i++
			if argumentIsDynamic(args, i, isDynamic) {
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
	optionsWithOperands := map[string]struct{}{"-k": {}, "--kill-after": {}, "-s": {}, "--signal": {}}
	for i := 0; ; i++ {
		word, exists := evaluationArgument(args, i)
		if !exists {
			break
		}
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
		if _, found := optionsWithOperands[value]; found {
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
	for i := 0; ; i++ {
		word, exists := evaluationArgument(args, i)
		if !exists {
			break
		}
		value, literal := LiteralWord(word)
		if !literal {
			continue
		}
		switch value {
		case "-exec", "-execdir", "-ok", "-okdir":
			start := i + 1
			end := start
			for ; end < len(args); end++ {
				word, exists := evaluationArgument(args, end)
				if !exists {
					break
				}
				delimiter, known := LiteralWord(word)
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
	word, exists := evaluationArgument(args, 0)
	if !exists {
		return false
	}
	subcommand, literal := LiteralWord(word)
	if !literal || isDynamic(word) {
		return true
	}
	if subcommand != "run" && subcommand != "exec" {
		return false
	}
	start, entrypoint, known := containerCommandOptions(args, isDynamic)
	if !known {
		return true
	}
	if start >= len(args) {
		return entrypoint != "" && commandReevaluatesCode([]*syntax.Word{{Parts: []syntax.WordPart{&syntax.Lit{Value: entrypoint}}}}, isDynamic, depth+1) ||
			subcommand == "run" && entrypoint == ""
	}
	start++ // The image or container name precedes the command.
	command := args[start:]
	if entrypoint != "" {
		command = append([]*syntax.Word{{Parts: []syntax.WordPart{&syntax.Lit{Value: entrypoint}}}}, command...)
	}
	if len(command) == 0 {
		return subcommand == "run"
	}
	return commandReevaluatesCode(command, isDynamic, depth+1)
}

func containerCommandOptions(args []*syntax.Word, isDynamic func(*syntax.Word) bool) (int, string, bool) {
	start := 1
	var entrypoint string
	for {
		word, exists := evaluationArgument(args, start)
		if !exists {
			break
		}
		option, literal := LiteralWord(word)
		if !literal {
			return start, entrypoint, false
		}
		if option == "--" {
			start++
			break
		}
		if !strings.HasPrefix(option, "-") {
			break
		}
		var known bool
		start, entrypoint, known = consumeContainerOption(args, start, word, option, entrypoint, isDynamic)
		if !known {
			return start, entrypoint, false
		}
	}
	return start, entrypoint, true
}

func consumeContainerOption(args []*syntax.Word, start int, word *syntax.Word, option, entrypoint string, isDynamic func(*syntax.Word) bool) (int, string, bool) {
	if value, found := strings.CutPrefix(option, "--entrypoint="); found {
		return start + 1, value, !isDynamic(word)
	}
	if strings.HasPrefix(option, "--env=") || strings.HasPrefix(option, "--volume=") ||
		strings.HasPrefix(option, "--publish=") || strings.HasPrefix(option, "--workdir=") ||
		strings.HasPrefix(option, "--name=") {
		return start + 1, entrypoint, true
	}
	if isDynamic(word) {
		return start, entrypoint, false
	}
	switch option {
	case "-e", "--env", "-v", "--volume", "-p", "--publish", "-w", "--workdir", "--name", "--entrypoint", "-u", "--user", "--network":
		start++
		operand, exists := evaluationArgument(args, start)
		if !exists {
			return start, entrypoint, false
		}
		if option == "--entrypoint" {
			var static bool
			entrypoint, static = LiteralWord(operand)
			if !static || isDynamic(operand) {
				return start, entrypoint, false
			}
		}
	case "--rm", "-i", "-t", "-d", "--detach", "--interactive", "--tty", "--privileged", "--read-only":
	default:
		if len(option) <= 2 || !strings.HasPrefix(option, "-") || strings.HasPrefix(option, "--") ||
			strings.Trim(option[1:], "itd") != "" {
			return start, entrypoint, false
		}
	}
	return start + 1, entrypoint, true
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
	word, exists := evaluationArgument(args, 0)
	if !exists || depth > 16 || isDynamic(word) {
		return false
	}
	name, literal := LiteralWord(word)
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
	for i := 0; ; i++ {
		word, exists := evaluationArgument(args, i)
		if !exists {
			break
		}
		if isDynamic(word) {
			return true
		}
		option, literal := LiteralWord(word)
		if !literal {
			return true
		}
		if option == "--" {
			return scriptArgumentReadsCode(args, i+1, isDynamic)
		}
		if option == "-s" {
			return true
		}
		switch option {
		case "-o", "+o", "-O", "+O", "--rcfile", "--init-file":
			i++
			if argumentIsDynamic(args, i, isDynamic) {
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
