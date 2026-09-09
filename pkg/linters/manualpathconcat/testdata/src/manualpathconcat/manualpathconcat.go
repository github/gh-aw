package manualpathconcat

import (
	"os"
	"path/filepath"
)

// badSimple builds a path with a manual "/" separator.
func badSimple(dir, file string) string {
	return dir + "/" + file // want `manual "/" path concatenation; use filepath\.Join\(dir, file\) \(or path\.Join\) instead`
}

// badChain concatenates three segments; only the outermost expression is
// reported so that a single chain produces a single diagnostic.
func badChain(a, b, c string) string {
	return a + "/" + b + "/" + c // want `manual "/" path concatenation; use filepath\.Join \(or path\.Join\) instead`
}

// badLiteralPrefix uses a literal base directory.
func badLiteralPrefix(name string) string {
	return "base" + "/" + name // want `manual "/" path concatenation; use filepath\.Join\("base", name\) \(or path\.Join\) instead`
}

// badCallOperand has an operand with side effects; it is still reported since
// no automatic fix is applied.
func badCallOperand(name string) string {
	return os.TempDir() + "/" + name // want `manual "/" path concatenation; use filepath\.Join\(os\.TempDir\(\), name\) \(or path\.Join\) instead`
}

// badNestedInCall reports a manual join passed as a call argument.
func badNestedInCall(dir, file string) ([]byte, error) {
	return os.ReadFile(dir + "/" + file) // want `manual "/" path concatenation; use filepath\.Join\(dir, file\) \(or path\.Join\) instead`
}

// badAddAssign builds a path with a manual "/" separator via +=.
func badAddAssign(absPath, relPath string) string {
	absPath += "/" + relPath // want `manual "/" path concatenation; use filepath\.Join\(absPath, relPath\) \(or path\.Join\) instead`
	return absPath
}

// bad2OpEmbeddedSlash uses the two-operand shape where the leading slash is
// embedded in a longer literal rather than being its own operand.
func bad2OpEmbeddedSlash(dir string) string {
	return dir + "/config.yml" // want `manual "/" path concatenation; use filepath\.Join\(dir, "config\.yml"\) \(or path\.Join\) instead`
}

// bad2OpEmbeddedSlashNested has a longer path after the leading slash.
func bad2OpEmbeddedSlashNested(home string) string {
	return home + "/.config/foo" // want `manual "/" path concatenation; use filepath\.Join\(home, "\.config/foo"\) \(or path\.Join\) instead`
}

// bad2OpEmbeddedSlashAddAssign builds a path with an embedded-slash literal
// via +=.
func bad2OpEmbeddedSlashAddAssign(home string) string {
	home += "/.config/foo" // want `manual "/" path concatenation; use filepath\.Join\(home, "\.config/foo"\) \(or path\.Join\) instead`
	return home
}

// badLongOperand has an operand too long to quote, so the diagnostic falls back
// to the generic message.
func badLongOperand(name string) string {
	return "a-very-long-literal-base-directory-name-goes-here" + "/" + name // want `manual "/" path concatenation; use filepath\.Join \(or path\.Join\) instead`
}

// goodNolint suppresses the diagnostic with a nolint directive.
func goodNolint(dir, file string) string {
	return dir + "/" + file //nolint:manualpathconcat
}

// goodJoin uses filepath.Join — not flagged.
func goodJoin(dir, file string) string {
	return filepath.Join(dir, file)
}

// goodOtherSeparator concatenates with a non-slash separator — not flagged.
func goodOtherSeparator(a, b string) string {
	return a + "-" + b
}

// goodSuffixOnly appends a trailing separator — not flagged.
func goodSuffixOnly(dir string) string {
	return dir + "/"
}

// goodPrefixOnly prepends a leading separator — not flagged.
func goodPrefixOnly(name string) string {
	return "/" + name
}

// goodBareSeparatorLeftOfEmbedded is a compile-time constant (both operands
// are literals), where filepath.Join is not valid — not flagged, matching
// goodConstant below.
func goodBareSeparatorLeftOfEmbedded() string {
	return "/" + "/config.yml"
}

// goodLiteralLeftOfEmbedded is also a compile-time constant (both operands
// are literals) — not flagged, matching goodConstant below.
func goodLiteralLeftOfEmbedded() string {
	return "prefix" + "/config.yml"
}

// badSuffixAfterJoinShape confirms the nested join shape is still reported.
func badSuffixAfterJoinShape(dir, file string) string {
	return dir + "/" + file + ".tmp" // want `manual "/" path concatenation; use filepath\.Join\(dir, file\) \(or path\.Join\) instead`
}

// badEmptyPrefix is treated as manual slash concatenation today.
func badEmptyPrefix(name string) string {
	return "" + "/" + name // want `manual "/" path concatenation; use filepath\.Join\("", name\) \(or path\.Join\) instead`
}

// goodConstant is a compile-time constant, where filepath.Join is not valid.
const goodConstant = "base" + "/" + "sub"

// goodNonString concatenation of numbers is unrelated.
func goodNonString(a, b int) int {
	return a + b
}
