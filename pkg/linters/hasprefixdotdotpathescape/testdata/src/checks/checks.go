package checks

import (
	"path/filepath"
	s "strings"
)

const parent = "." + "."

func checks(path, prefix string) {
	_ = s.HasPrefix(path, "..")                 // want `strings.HasPrefix\(value, "\.\."\) also matches`
	_ = !s.HasPrefix(path, (`..`))              // want `strings.HasPrefix\(value, "\.\."\) also matches`
	_ = (s.HasPrefix)(path, parent)             // want `strings.HasPrefix\(value, "\.\."\) also matches`
	_ = s.HasPrefix(path, "\x2e\x2e")           // want `strings.HasPrefix\(value, "\.\."\) also matches`
	_ = s.HasPrefix(filepath.Clean(path), "..") // want `strings.HasPrefix\(value, "\.\."\) also matches`
	_ = path == ".." || s.HasPrefix(path, "../")
	_ = path == ".." || s.HasPrefix(path, ".."+string(filepath.Separator))
	_ = s.HasPrefix(path, prefix)
	_ = filepath.IsLocal(path)
	//nolint:hasprefixdotdotpathescape
	_ = s.HasPrefix(path, "..")
}

func nonPathText(text, tokenPrefix, profile string) {
	_ = s.HasPrefix(text, "..")
	_ = s.HasPrefix(tokenPrefix, "..")
	_ = s.HasPrefix(profile, "..")
	_ = s.HasPrefix(filepath.Clean(text), "..") // want `strings.HasPrefix\(value, "\.\."\) also matches`
}

type stringLike struct{}

func (stringLike) HasPrefix(string, string) bool { return false }

func unrelated(path string) {
	strings := stringLike{}
	_ = strings.HasPrefix(path, "..")
}
