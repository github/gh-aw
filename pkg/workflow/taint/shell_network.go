package taint

import (
	"strings"

	"mvdan.cc/sh/v3/syntax"
)

func (s *shell) network(command string, args []*syntax.Word, line int, stdin value) {
	var request value
	for index := 0; index < len(args); index++ {
		word, ok := sliceAt(args, index)
		if !ok {
			continue
		}
		arg := staticWord(word)
		if arg == "-H" || arg == "--header" {
			if header, ok := sliceAt(args, index+1); ok {
				index++
				if !authenticationHeader(staticWord(header)) {
					request = union(request, s.networkHeader(command, header, stdin, ""))
				}
			}
			continue
		}
		if header, found := strings.CutPrefix(arg, "--header="); found {
			if !authenticationHeader(header) {
				request = union(request, s.networkHeader(command, word, stdin, "--header="))
			}
			continue
		}
		if arg == "-u" || arg == "--user" || arg == "--oauth2-bearer" ||
			arg == "--password" || arg == "--http-password" {
			index++
			continue
		}
		if arg == "--post-file" || arg == "-T" || arg == "--upload-file" {
			if file, ok := sliceAt(args, index+1); ok {
				index++
				request = union(request, s.networkFileBody(file, stdin, arg != "--post-file", ""))
			}
			continue
		}
		if option, _, found := strings.Cut(arg, "="); found && (option == "--post-file" || option == "--upload-file") {
			request = union(request, s.networkFileBody(word, stdin, option != "--post-file", option+"="))
			continue
		}
		if strings.HasPrefix(arg, "--user=") || strings.HasPrefix(arg, "--oauth2-bearer=") {
			continue
		}
		if arg == "-d" || arg == "--data" || arg == "--data-binary" ||
			arg == "--data-urlencode" || arg == "--json" || arg == "-F" || arg == "--form" ||
			arg == "--data-raw" || arg == "--form-string" {
			if body, ok := sliceAt(args, index+1); ok {
				index++
				request = union(request, s.networkBody(body, stdin, arg, ""))
			}
			continue
		}
		if option, _, found := strings.Cut(arg, "="); found {
			switch option {
			case "--data", "--data-binary", "--data-urlencode", "--json", "--form", "--data-raw", "--form-string":
				request = union(request, s.networkBody(word, stdin, option, option+"="))
				continue
			}
		}
		request = union(request, s.word(word))
	}
	s.a.emit(Site{Kind: Network, Path: s.path, Line: line}, request)
}

func (s *shell) networkHeader(command string, word *syntax.Word, stdin value, prefix string) value {
	if command == "curl" {
		return s.networkBody(word, stdin, "--header", prefix)
	}
	return s.word(word)
}

func (s *shell) networkBody(word *syntax.Word, stdin value, option, prefix string) value {
	direct := s.word(word)
	contents := direct
	if option == "--data-raw" || option == "--form-string" {
		return contents
	}
	texts, known := wordTexts(word, s.constants, s.constantValues, s.constantUnknown)
	for _, text := range texts {
		text = strings.TrimPrefix(text, prefix)
		if containsMarker(text, s.markers) {
			known = false
			continue
		}
		var targets []string
		switch option {
		case "-F", "--form":
			_, body, found := strings.Cut(text, "=")
			if found {
				if target, upload := strings.CutPrefix(body, "@"); upload {
					targets = strings.Split(target, ",")
				} else if target, file := strings.CutPrefix(body, "<"); file {
					targets = []string{target}
				}
				for index, target := range targets {
					targets[index], _, _ = strings.Cut(target, ";")
				}
			}
		case "--data-urlencode":
			// An '=' before '@' selects literal content, not a filename.
			if name, target, found := strings.Cut(text, "@"); found && !strings.Contains(name, "=") {
				targets = []string{target}
			}
		default:
			if target, found := strings.CutPrefix(text, "@"); found {
				targets = []string{target}
			}
		}
		for _, target := range targets {
			// Curl's quoted multipart filenames need their own escaping parser.
			if (option == "-F" || option == "--form") && strings.ContainsAny(target, "\"\\") {
				s.a.gap(s.path, "quoted multipart upload files are not modeled")
				continue
			}
			if target == "-" {
				contents = union(contents, stdin)
				continue
			}
			contents = union(contents, s.networkFileBodyValue(target, stdin, false))
		}
	}
	if !known && ((len(direct) == 0 && (s.files.hasSources() || len(stdin) > 0)) || strings.Contains(staticWord(word), "@")) {
		s.a.gap(s.path, "dynamic network upload files are not modeled")
	}
	return contents
}

func (s *shell) networkFileBody(word *syntax.Word, stdin value, stdinUpload bool, prefix string) value {
	texts, known := wordTexts(word, s.constants, s.constantValues, s.constantUnknown)
	var contents value
	for _, text := range texts {
		if containsMarker(text, s.markers) {
			known = false
			continue
		}
		contents = union(contents, s.networkFileBodyValue(strings.TrimPrefix(text, prefix), stdin, stdinUpload))
	}
	if !known {
		s.a.gap(s.path, "dynamic network upload files are not modeled")
	}
	return contents
}

func (s *shell) networkFileBodyValue(path string, stdin value, stdinUpload bool) value {
	// Curl accepts "-" and "." as stdin; wget --post-file requires a regular file.
	if stdinUpload && (path == "-" || path == ".") {
		return stdin
	}
	if path == "" {
		return nil
	}
	files, modeled := s.readPaths(s.pathFor(path))
	if !modeled {
		s.a.gap(s.path, "dynamic network upload files are not modeled")
	}
	var contents value
	for _, file := range files {
		contents = union(contents, s.files[file])
	}
	return contents
}

func authenticationHeader(header string) bool {
	name, _, _ := strings.Cut(strings.ToLower(strings.TrimSpace(header)), ":")
	switch name {
	case "authorization", "proxy-authorization", "x-api-key", "api-key", "x-goog-api-key", "private-token":
		return true
	default:
		return false
	}
}
