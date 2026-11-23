package hasprefixdotdotpathescape

import (
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"
)

func badRel(base, target string) bool {
	rel, err := filepath.Rel(base, target)
	if err != nil {
		return true
	}
	return strings.HasPrefix(rel, "..") // want `strings.HasPrefix\(rel, "\.\."\) also matches`
}

func badDirect(base, target string) bool {
	r, _ := filepath.Rel(base, target)
	_ = r
	return strings.HasPrefix(filepath.ToSlash(r), "..") // want `also matches`
}

func badClean(p string) bool {
	cleaned := path.Clean(p)
	return strings.HasPrefix(cleaned, "..") // want `also matches`
}

func badInlineClean(p string) bool {
	return strings.HasPrefix(filepath.Clean(p), "..") // want `also matches`
}

func badNamed(relPath string) bool {
	return strings.HasPrefix(relPath, "..") // want `also matches`
}

func badOrExact(rel string) bool {
	return rel == ".." || strings.HasPrefix(rel, "..") // want `also matches`
}

func suppressed(rel string) bool {
	return strings.HasPrefix(rel, "..") //nolint:hasprefixdotdotpathescape
}

func goodSeparator(rel string) bool {
	return rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func goodOSSeparator(rel string) bool {
	return rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator))
}

func goodSlash(p string) bool {
	c := path.Clean(p)
	return c == ".." || strings.HasPrefix(c, "../")
}

func goodIsLocal(base, target string) bool {
	rel, err := filepath.Rel(base, target)
	return err == nil && filepath.IsLocal(rel)
}

func unrelatedURL(u *url.URL) bool {
	return strings.HasPrefix(u.Path, "..")
}

func unrelatedString(s string) bool {
	return strings.HasPrefix(s, "..")
}

func otherPrefix(rel string) bool {
	return strings.HasPrefix(rel, "./")
}
