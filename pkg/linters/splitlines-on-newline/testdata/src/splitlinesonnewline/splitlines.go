package splitlinesonnewline

import (
	"strings"
	stringAlias "strings"
)

const newline = "\n"
const namedNewline = newline

func badLiteral(input string) []string {
	return strings.Split(input, "\n") // want `strings.Split with a newline separator can retain carriage returns`
}

func badNamedConstant(input string) []string {
	return strings.Split(input, namedNewline) // want `strings.Split with a newline separator can retain carriage returns`
}

func badAliasedImport(input string) []string {
	return stringAlias.Split(input, newline) // want `strings.Split with a newline separator can retain carriage returns`
}

func goodOtherSeparators(input string, separator string) {
	strings.Split(input, "\r\n")
	strings.Split(input, ",")
	strings.Split(input, separator)
}

type customSplitter struct{}

func (customSplitter) Split(string, string) []string {
	return nil
}

func goodUserDefinedSplit(input string) {
	customSplitter{}.Split(input, "\n")
}

func goodShadowedPackageName(input string) {
	strings := customSplitter{}
	strings.Split(input, "\n")
}
