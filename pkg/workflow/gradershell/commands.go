package gradershell

import (
	"path"
	"strings"

	"mvdan.cc/sh/v3/syntax"
)

func (c *shellChecker) command(args []*syntax.Word, offset, depth int) error {
	if len(args) == 0 {
		return nil
	}
	line := int(args[0].Pos().Line()) + offset //nolint:uncheckedsliceindex // The preceding guard rejects empty argument slices.
	for wrappers := 0; wrappers < 16 && len(args) > 0; wrappers++ {
		name, static := LiteralWord(args[0]) //nolint:uncheckedsliceindex // The loop checks the potentially unwrapped slice is nonempty.
		if !static {
			c.add(line, uncheckedShell)
			return nil
		}
		name = path.Base(name)
		tail := args[1:]
		switch name {
		case "exec", "command", "builtin", "env", "sudo", "nice", "nohup", "busybox":
			unwrapped, query, known := unwrapCommand(name, tail)
			if !known {
				c.add(line, uncheckedShell)
				return nil
			}
			if query || len(unwrapped) == 0 {
				return nil
			}
			args = unwrapped
			continue
		case "bash", "sh", "dash", "ksh", "zsh":
			return c.nestedShell(tail, line, depth)
		case "find":
			return c.find(tail, line, offset, depth)
		default:
			if message := commandFinding(name, tail); message != "" {
				c.add(line, message)
			}
		}
		return nil
	}
	c.add(line, uncheckedShell)
	return nil
}

func commandFinding(name string, args []*syntax.Word) string {
	switch name {
	case "eval", "source", ".", "xargs":
		return uncheckedShell
	case "touch", "mkdir", "rmdir", "rm", "mv", "cp", "install", "truncate",
		"chmod", "chown", "chgrp", "ln", "link", "unlink", "mkfifo", "mknod",
		"mktemp", "sponge", "split", "csplit", "shred", "patch", "rsync", "zip":
		return writeCommand
	case "tee":
		if teeWrites(args) {
			return writeTee
		}
	case "dd":
		if ddWrites(args) {
			return writeCommand
		}
	case "sed", "perl":
		if inPlace(args, name) {
			return writeInPlace
		}
	case "sort":
		if outputOption(args, "-o", "--output") {
			return writeCommand
		}
	case "curl":
		if outputOption(args, "-o", "--output") || outputOption(args, "-O", "--remote-name") ||
			outputOption(args, "-D", "--dump-header") || outputOption(args, "-c", "--cookie-jar") ||
			outputOption(args, "", "--output-dir") {
			return writeCommand
		}
	case "wget":
		return writeCommand // Its default destination is a file.
	case "tar":
		if tarWrites(args) {
			return writeCommand
		}
	default:
		if strings.HasSuffix(name, ".sh") {
			return uncheckedShell
		}
	}
	return ""
}

func ddWrites(args []*syntax.Word) bool {
	for _, word := range args {
		value, static := LiteralWord(word)
		destination, output := strings.CutPrefix(value, "of=")
		if (!static && !ddInputOperand(word)) || (output && !sinkPath(destination)) {
			return true
		}
	}
	return false
}

func ddInputOperand(word *syntax.Word) bool {
	if word == nil || len(word.Parts) == 0 {
		return false
	}
	prefix, ok := word.Parts[0].(*syntax.Lit) //nolint:uncheckedsliceindex // The preceding guard requires a nonempty parts slice.
	if !ok {
		return false
	}
	key, _, assignment := strings.Cut(prefix.Value, "=")
	if !assignment {
		return false
	}
	for _, part := range word.Parts[1:] {
		switch part.(type) {
		case *syntax.Lit, *syntax.SglQuoted, *syntax.DblQuoted:
		default:
			return false // Unquoted expansion can introduce additional operands.
		}
	}
	switch key {
	case "if", "bs", "ibs", "obs", "count", "skip", "seek", "status", "conv", "iflag", "oflag":
		return true
	default:
		return false
	}
}

func (c *shellChecker) nestedShell(args []*syntax.Word, line, depth int) error {
	for i := 0; i < len(args); i++ {
		option, static := LiteralWord(args[i]) //nolint:uncheckedsliceindex // The loop rechecks the nonnegative cursor after option-operand skips.
		if !static {
			break
		}
		switch option {
		case "--help", "--version":
			return nil
		case "--noprofile", "--norc":
			continue
		case "-o", "+o", "-O", "+O":
			i++
			continue
		}
		if strings.HasPrefix(option, "-") && !strings.HasPrefix(option, "--") && strings.Contains(option, "c") {
			if i+1 < len(args) && depth < 8 {
				if source, known := LiteralWord(args[i+1]); known { //nolint:uncheckedsliceindex // The enclosing guard bounds the lookahead; i starts at zero.
					return c.source(source, line-1, depth+1)
				}
			}
			break
		}
		if !strings.HasPrefix(option, "-") && !strings.HasPrefix(option, "+") {
			break
		}
		// Startup files and option-dependent execution are outside the model.
		if strings.HasPrefix(option, "--") {
			break
		}
	}
	c.add(line, uncheckedShell)
	return nil
}

func teeWrites(args []*syntax.Word) bool {
	options := true
	for _, word := range args {
		value, static := LiteralWord(word)
		if options && static {
			if value == "--" {
				options = false
				continue
			}
			if strings.HasPrefix(value, "-") && value != "-" {
				continue
			}
		}
		if !outputSink(word) {
			return true
		}
	}
	return false
}

func inPlace(args []*syntax.Word, command string) bool {
	skip := false
	for _, word := range args {
		if skip {
			skip = false
			continue
		}
		value, static := LiteralWord(word)
		if !static {
			continue
		}
		if value == "--" {
			break
		}
		if value == "--in-place" || strings.HasPrefix(value, "--in-place=") {
			return true
		}
		if strings.HasPrefix(value, "--") {
			skip = value == "--expression" || value == "--file"
			continue
		}
		if !strings.HasPrefix(value, "-") {
			continue
		}
		flags := strings.TrimPrefix(value, "-")
		for i, flag := range flags {
			if flag == 'i' {
				return true
			}
			if flag == 'e' || flag == 'f' || (command == "perl" && strings.ContainsRune("EIMmF", flag)) {
				skip = i == len(flags)-1
				break // The remainder is an expression, file or module, not flags.
			}
		}
	}
	return false
}

func outputOption(args []*syntax.Word, short, long string) bool {
	for index, word := range args {
		value, static := LiteralWord(word)
		if !static {
			continue
		}
		if value == "--" {
			break
		}
		var operand string
		if value == long || (short != "" && value == short) {
			if long == "--remote-name" || index+1 >= len(args) {
				return true
			}
			if operand, static = LiteralWord(args[index+1]); !static { //nolint:uncheckedsliceindex // The preceding guard bounds the lookahead and range makes index nonnegative.
				return true
			}
		} else if after, found := strings.CutPrefix(value, long+"="); found {
			operand = after
		} else if after, found := strings.CutPrefix(value, short); short != "" && found {
			operand = after
		} else {
			continue
		}
		if !sinkPath(operand) && operand != "-" {
			return true
		}
	}
	return false
}

func tarWrites(args []*syntax.Word) bool {
	skip := false
	for index, word := range args {
		if skip {
			skip = false
			continue
		}
		value, static := LiteralWord(word)
		if !static {
			continue
		}
		if value == "--" {
			break
		}
		switch value {
		case "--extract", "--get", "--create", "--append", "--update", "--delete", "--concatenate", "--catenate":
			return true
		case "-f", "--file", "-C", "--directory", "-T", "--files-from":
			skip = true
			continue
		}
		if strings.HasPrefix(value, "--") || (!strings.HasPrefix(value, "-") && index != 0) {
			continue
		}
		for _, flag := range strings.TrimPrefix(value, "-") {
			if strings.ContainsRune("xcruA", flag) {
				return true
			}
			if strings.ContainsRune("fCT", flag) {
				// Attached names are data; with traditional unprefixed
				// option letters the operand follows the entire group.
				skip = !strings.HasPrefix(value, "-") || strings.HasSuffix(value, string(flag))
				break
			}
		}
	}
	return false
}

func (c *shellChecker) find(args []*syntax.Word, line, offset, depth int) error {
	for i, word := range args {
		value, static := LiteralWord(word)
		if !static {
			continue
		}
		switch value {
		case "-delete", "-fprint", "-fprint0", "-fprintf":
			c.add(line, writeCommand)
		case "-exec", "-execdir", "-ok", "-okdir":
			end := i + 1
			for ; end < len(args); end++ {
				value, known := LiteralWord(args[end]) //nolint:uncheckedsliceindex // end starts at nonnegative range index i+1 and is bounded by the loop.
				if known && (value == ";" || value == "+") {
					break
				}
			}
			if err := c.command(args[i+1:end], offset, depth); err != nil {
				return err
			}
		}
	}
	return nil
}
