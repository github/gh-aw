package logfatallibrary

import (
	"log"

	. "log"
)

func badParenthesized() {
	(log.Fatal)("boom")          // want `log\.Fatal called in library package`
	((log.Fatalf))("boom %d", 1) // want `log\.Fatalf called in library package`
}

func badDotImport() {
	Fatalln("dot import") // want `log\.Fatalln called in library package`
}

func suppressedParenthesized() {
	(log.Fatal)("suppressed") //nolint:logfatallibrary
}
