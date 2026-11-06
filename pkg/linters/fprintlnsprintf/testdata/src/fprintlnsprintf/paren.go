package fprintlnsprintf

import (
	"fmt"
	"os"
)

func flaggedParenCallee(name string) {
	(fmt.Fprintln)(os.Stderr, fmt.Sprintf("hello %s", name)) // want "use fmt.Fprintf"
}

func flaggedParenSprintfCallee(name string) {
	fmt.Fprintln(os.Stderr, (fmt.Sprintf)("hello %s", name)) // want "use fmt.Fprintf"
}

func flaggedParenSprintfCall(name string) {
	fmt.Fprintln(os.Stderr, (fmt.Sprintf("hello %s", name))) // want "use fmt.Fprintf"
}

func flaggedParenFormat(name string) {
	fmt.Fprintln(os.Stderr, fmt.Sprintf(("hello %s"), name)) // want "use fmt.Fprintf"
}
