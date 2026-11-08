package splitlinesonnewline

import "strings"

func intentionallySplit(input string) []string {
	return strings.Split(input, "\n") //nolint:splitlinesonnewline
}
