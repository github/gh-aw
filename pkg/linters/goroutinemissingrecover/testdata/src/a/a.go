// Package a is the test fixture for the goroutinemissingrecover analyzer.
package a

// safeGoroutine has a top-level defer/recover — no diagnostic expected.
func safeGoroutine() {
	go func() {
		defer func() {
			if r := recover(); r != nil {
				_ = r
			}
		}()
		panic("oops")
	}()
}

// unsafeGoroutine has no recover — should be flagged.
func unsafeGoroutine() {
	go func() { // want `goroutine launched via a function literal without a top-level defer/recover`
		panic("oops")
	}()
}

// namedFuncGoroutine calls a named function — out of scope, not flagged.
func namedFuncHelper() {}

func namedFuncGoroutine() {
	go namedFuncHelper()
}

// localClosureGoroutine launches a local literal without recover — should be flagged.
func localClosureGoroutine() {
	fn := func() {
		panic("oops")
	}
	go fn() // want `goroutine launched via a function literal without a top-level defer/recover`
}

// safeLocalClosureGoroutine installs recovery inside the local literal.
func safeLocalClosureGoroutine() {
	fn := func() {
		defer func() {
			recover()
		}()
		panic("oops")
	}
	go fn()
}

// varLocalClosureGoroutine also resolves a parenthesized var initializer and call.
func varLocalClosureGoroutine() {
	var unused, fn = 1, (func() {
		panic("oops")
	})
	_ = unused
	go (fn)() // want `goroutine launched via a function literal without a top-level defer/recover`
}

// shadowedLocalClosureGoroutine distinguishes variables with the same name.
func shadowedLocalClosureGoroutine() {
	fn := func() {
		defer namedRecoverHelper()
	}
	{
		fn := func() {
			panic("oops")
		}
		go fn() // want `goroutine launched via a function literal without a top-level defer/recover`
	}
	go fn()
}

// suppressedLocalClosureGoroutine respects suppression at the launch site.
func suppressedLocalClosureGoroutine() {
	fn := func() {
		panic("oops")
	}
	//nolint:goroutinemissingrecover
	go fn()
}

// packageClosureGoroutine and function values without local literal initializers are out of scope.
var packageClosure = func() {}

func packageClosureGoroutine(fn func()) {
	go packageClosure()
	go fn()
	named := namedFuncHelper
	go named()
}

// reassignedLocalClosureGoroutine must not inspect a stale literal initializer.
func reassignedLocalClosureGoroutine() {
	fn := func() {
		panic("oops")
	}
	(fn) = namedFuncHelper
	go fn()
}

// rangeAssignedLocalClosureGoroutine also has a mutable launch target.
func rangeAssignedLocalClosureGoroutine() {
	fn := func() {
		panic("oops")
	}
	for _, fn = range []func(){namedFuncHelper} {
		go fn()
	}
}

// addressTakenLocalClosureGoroutine can change its target through a pointer.
func addressTakenLocalClosureGoroutine() {
	fn := func() {
		panic("oops")
	}
	ptr := &fn
	*ptr = namedFuncHelper
	go fn()
}

// unrelatedDeferGoroutine has a defer but no recover — should be flagged.
func unrelatedDeferGoroutine() {
	go func() { // want `goroutine launched via a function literal without a top-level defer/recover`
		defer func() {
			_ = 1
		}()
		panic("oops")
	}()
}

// suppressedGoroutine is suppressed via nolint — no diagnostic expected.
func suppressedGoroutine() {
	//nolint:goroutinemissingrecover
	go func() {
		panic("oops")
	}()
}

// parenthesizedGoroutine uses the parenthesised literal syntax — should be flagged.
func parenthesizedGoroutine() {
	go (func() { // want `goroutine launched via a function literal without a top-level defer/recover`
		panic("oops")
	})()
}

// nestedClosureRecoverGoroutine has recover() buried in a nested closure inside the
// defer literal. recover() only stops a panic in the same stack frame, so the
// nested recover does NOT protect the goroutine — should be flagged.
func nestedClosureRecoverGoroutine() {
	go func() { // want `goroutine launched via a function literal without a top-level defer/recover`
		defer func() {
			// recover() is inside a nested closure — it only guards the nested call,
			// not the outer goroutine.
			func() { recover() }()
		}()
		panic("oops")
	}()
}

// nestedRecoverOnlyGoroutine — outer goroutine has no recover; inner func does.
// The outer goroutine is unprotected — should be flagged.
func nestedRecoverOnlyGoroutine() {
	go func() { // want `goroutine launched via a function literal without a top-level defer/recover`
		func() {
			defer func() {
				if r := recover(); r != nil {
					_ = r
				}
			}()
			panic("inner")
		}()
		panic("outer") // this panic is unrecovered
	}()
}

// namedRecoverHelper calls recover() directly, so deferring it protects the caller.
func namedRecoverHelper() {
	if r := recover(); r != nil {
		_ = r
	}
}

// namedNoRecoverHelper does not call recover().
func namedNoRecoverHelper() {}

type recoverer struct{}

func (recoverer) recoverMethod() {
	if r := recover(); r != nil {
		_ = r
	}
}

// namedRecoverDeferGoroutine defers a named helper that calls recover() — no diagnostic expected.
func namedRecoverDeferGoroutine() {
	go func() {
		defer namedRecoverHelper()
		panic("oops")
	}()
}

// methodRecoverDeferGoroutine defers a method that calls recover() — no diagnostic expected.
func methodRecoverDeferGoroutine() {
	var r recoverer
	go func() {
		defer r.recoverMethod()
		panic("oops")
	}()
}

// namedNoRecoverDeferGoroutine defers a named helper without recover — should be flagged.
func namedNoRecoverDeferGoroutine() {
	go func() { // want `goroutine launched via a function literal without a top-level defer/recover`
		defer namedNoRecoverHelper()
		panic("oops")
	}()
}

// funcValueDeferGoroutine defers a func value whose body cannot be resolved — flagged conservatively.
func funcValueDeferGoroutine() {
	f := namedRecoverHelper
	go func() { // want `goroutine launched via a function literal without a top-level defer/recover`
		defer f()
		panic("oops")
	}()
}

// nestedRecoverHelper only calls recover() inside a nested closure — does not protect its caller.
func nestedRecoverHelper() {
	func() { recover() }()
}

// namedNestedRecoverDeferGoroutine defers a helper whose recover is nested — should be flagged.
func namedNestedRecoverDeferGoroutine() {
	go func() { // want `goroutine launched via a function literal without a top-level defer/recover`
		defer nestedRecoverHelper()
		panic("oops")
	}()
}
