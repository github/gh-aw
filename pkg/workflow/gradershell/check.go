package gradershell

import (
	"context"
	"errors"
	"slices"
	"strings"

	"mvdan.cc/sh/v3/syntax"
)

// Finding identifies a direct write or unchecked shell execution. Messages are
// static and never contain source values.
type Finding struct {
	Line    int
	Message string
}

const (
	writeRedirection = "Shell grader redirection may write a file; emit results on stdout instead"
	writeCommand     = "Shell grader command may modify files; compute from the session file and emit results on stdout instead"
	writeTee         = "Shell grader tee destination may write a file; emit results on stdout instead"
	writeInPlace     = "Shell grader in-place editing may modify files; compute without changing the session file"
	uncheckedShell   = "Shell grader dynamic or external shell execution cannot be checked for file writes"
)

// Check parses Bash without executing it. All branches, function bodies and
// substitutions are inspected, including statically known shell -c strings.
// The analysis is deliberately finite: it does not model arbitrary programs,
// interpreter languages, aliases, or runtime-generated command contents.
func Check(ctx context.Context, source string) ([]Finding, error) {
	checker := &shellChecker{ctx: ctx}
	err := checker.source(source, 0, 0)
	slices.SortFunc(checker.findings, func(a, b Finding) int {
		if a.Line != b.Line {
			return a.Line - b.Line
		}
		return strings.Compare(a.Message, b.Message)
	})
	checker.findings = slices.Compact(checker.findings)
	return checker.findings, err
}

type shellChecker struct {
	ctx      context.Context
	findings []Finding
}

func (c *shellChecker) source(source string, offset, depth int) error {
	if err := c.ctx.Err(); err != nil {
		return err
	}
	if len(source) > maxSourceBytes {
		return errors.New("shell grader source exceeds the 64 KiB analysis limit")
	}
	tree, err := syntax.NewParser(syntax.Variant(syntax.LangBash)).Parse(strings.NewReader(source), "")
	if err != nil {
		// Parser errors may quote source text.
		return errors.New("cannot analyze shell grader: invalid Bash syntax")
	}
	var failure error
	syntax.Walk(tree, func(node syntax.Node) bool {
		if failure != nil {
			return false
		}
		if err := c.ctx.Err(); err != nil {
			failure = err
			return false
		}
		switch node := node.(type) {
		case *syntax.Redirect:
			if writingRedirect(node) {
				c.add(int(node.Pos().Line())+offset, writeRedirection)
			}
		case *syntax.CallExpr:
			failure = c.command(node.Args, offset, depth)
		}
		return true
	})
	return failure
}

func (c *shellChecker) add(line int, message string) {
	c.findings = append(c.findings, Finding{Line: line, Message: message})
}

func writingRedirect(redir *syntax.Redirect) bool {
	switch redir.Op {
	case syntax.RdrOut, syntax.AppOut, syntax.RdrClob, syntax.RdrAll, syntax.AppAll, syntax.RdrInOut:
		return !outputSink(redir.Word)
	case syntax.DplOut:
		value, static := LiteralWord(redir.Word)
		if static && (value == "-" || numericFD(strings.TrimSuffix(value, "-"))) {
			return false
		}
		return !outputSink(redir.Word)
	default:
		return false
	}
}

func numericFD(value string) bool {
	if value == "" {
		return false
	}
	for _, c := range value {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

func outputSink(word *syntax.Word) bool {
	if word != nil && len(word.Parts) == 1 {
		if pipe, ok := word.Parts[0].(*syntax.ProcSubst); ok && pipe.Op == syntax.CmdOut { //nolint:uncheckedsliceindex // The enclosing guard requires exactly one part.
			return true
		}
	}
	value, static := LiteralWord(word)
	return static && sinkPath(value)
}

func sinkPath(value string) bool {
	switch value {
	case "/dev/null", "/dev/stdout", "/dev/stderr", "/dev/fd/1", "/dev/fd/2", "/proc/self/fd/1", "/proc/self/fd/2":
		return true
	default:
		return false
	}
}
