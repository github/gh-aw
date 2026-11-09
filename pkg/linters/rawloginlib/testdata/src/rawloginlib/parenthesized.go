package rawloginlib

import (
	"log"

	. "log"
)

func badParenthesized() {
	(log.Printf)("hello %s", "world") // want `log\.Printf called in library package`
	((log.Println))("oops")           // want `log\.Println called in library package`
}

func badDotImport() {
	Println("dot import") // want `log\.Println called in library package`
}

func suppressedParenthesized() {
	(log.Printf)("suppressed") //nolint:rawloginlib
}
