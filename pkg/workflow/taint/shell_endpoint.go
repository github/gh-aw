package taint

import (
	"context"
	"strings"

	"github.com/rhysd/actionlint"
	"mvdan.cc/sh/v3/syntax"
)

// Safe-output jobs consume endpoint credentials at a sink, without sanitization.
// Only direct step-env secret bindings that the script never mutates qualify.
func (s *shell) endpointContracts(tree *syntax.File, fields map[string]any) map[string]struct{} {
	endpoints := make(map[string]struct{})
	env, ok := fields["env"].(map[string]any)
	if !s.a.safeOutputJob || !ok {
		return nil
	}
	for name := range env {
		if s.directEndpointBinding(name, env) {
			endpoints[name] = struct{}{}
		}
	}
	syntax.Walk(tree, func(node syntax.Node) bool {
		switch n := node.(type) {
		case *syntax.Assign:
			if n.Name != nil {
				delete(endpoints, n.Name.Value)
			}
		case *syntax.WordIter:
			delete(endpoints, n.Name.Value)
		case *syntax.ParamExp:
			if n.Param != nil && n.Exp != nil && (n.Exp.Op.String() == "=" || n.Exp.Op.String() == ":=") {
				delete(endpoints, n.Param.Value)
			}
		case *syntax.ArithmExp, *syntax.ArithmCmd, *syntax.CStyleLoop, *syntax.DeclClause:
			clear(endpoints)
		case *syntax.CallExpr:
			command, ok := sliceAt(n.Args, 0)
			if !ok {
				break
			}
			switch literalShellWord(command) {
			case "", ".", "source", "eval", "read", "readarray", "mapfile", "unset", "alias", "env",
				"builtin", "command", "trap", "let", "getopts", "enable", "fc", "bind":
				clear(endpoints)
			case "printf":
				format, ok := sliceAt(n.Args, 1)
				if !ok || literalShellWord(format) == "" {
					clear(endpoints)
				}
				for _, arg := range n.Args[1:] {
					if staticWord(arg) == "-v" {
						clear(endpoints)
					}
				}
			}
		}
		return true
	})
	return endpoints
}

func (s *shell) directEndpointBinding(name string, env map[string]any) bool {
	text, ok := env[name].(string)
	if !ok || !syntax.ValidName(name) || len(s.vars[name]) == 0 {
		return false
	}
	direct := false
	err := VisitInterpolations(context.Background(), text, s.path, func(start, end, _ int, node actionlint.ExprNode) error {
		ref, known := reference(node)
		direct = start == 0 && end == len(text) && known &&
			strings.HasPrefix(ref, "secrets.") && !strings.Contains(ref, "*")
		return nil
	})
	return direct && err == nil
}

// A deliberately finite curl grammar excludes redirects, config files, proxies,
// tracing, URL rewriting, and multiple destinations from the endpoint contract.
func (s *shell) authenticatedEndpoint(args []*syntax.Word) *syntax.Word {
	var endpoint *syntax.Word
	for index := 0; index < len(args); index++ {
		word, ok := sliceAt(args, index)
		if !ok {
			return nil
		}
		arg := staticWord(word)
		switch arg {
		case "--fail", "--silent", "--show-error", "-f", "-s", "-S", "-sS", "-fsS", "-q", "--disable":
			continue
		case "-H", "--header", "-d", "--data", "--data-binary", "--data-urlencode", "--json",
			"-F", "--form", "-T", "--upload-file", "-u", "--user", "--oauth2-bearer":
			index++
			if index >= len(args) {
				return nil
			}
			continue
		case "--url":
			index++
			word, ok = sliceAt(args, index)
			if !ok {
				return nil
			}
		default:
			option, _, inline := strings.Cut(arg, "=")
			if inline {
				switch option {
				case "--header", "--data", "--data-binary", "--data-urlencode", "--json",
					"--form", "--upload-file", "--user", "--oauth2-bearer":
					continue
				}
			}
			if strings.HasPrefix(arg, "-") {
				return nil
			}
		}
		name := quotedEndpointVariable(word)
		_, trusted := s.credentialEndpoints[name]
		if endpoint != nil || !trusted {
			return nil
		}
		endpoint = word
	}
	return endpoint
}

func quotedEndpointVariable(word *syntax.Word) string {
	part, ok := sliceAt(word.Parts, 0)
	if !ok || len(word.Parts) != 1 {
		return ""
	}
	quoted, ok := part.(*syntax.DblQuoted)
	if !ok || len(quoted.Parts) != 1 || quoted.Dollar {
		return ""
	}
	part, ok = sliceAt(quoted.Parts, 0)
	if !ok {
		return ""
	}
	param, ok := part.(*syntax.ParamExp)
	if !ok || param.Param == nil || param.Excl || param.Length || param.Width ||
		param.Index != nil || param.Slice != nil || param.Repl != nil || param.Exp != nil {
		return ""
	}
	return param.Param.Value
}
