package fileclosenotdeferred

import "os"

// flagged: file.Close() not deferred
func ReadFileManualClose() error {
	file, err := os.Open("test.txt") // want `file Close\(\) should be deferred immediately after successful open to prevent resource leaks`
	if err != nil {
		return err
	}
	// ... code that might return early ...
	file.Close()
	return nil
}

// not flagged: defer used correctly
func ReadFileDeferClose() error {
	file, err := os.Open("test.txt")
	if err != nil {
		return err
	}
	defer file.Close()
	// ... rest of code ...
	return nil
}

// flagged: os.Create with manual close
func CreateFileManualClose() error {
	f, err := os.Create("output.txt") // want `file Close\(\) should be deferred immediately after successful open to prevent resource leaks`
	if err != nil {
		return err
	}
	f.Close()
	return nil
}

// flagged: os.Open with Close() assigned to error variable
func ReadFileCloseWithErrAssign() error {
	file, err := os.Open("test.txt") // want `file Close\(\) should be deferred immediately after successful open to prevent resource leaks`
	if err != nil {
		return err
	}
	// ... code ...
	closeErr := file.Close()
	if closeErr != nil {
		return closeErr
	}
	return nil
}

// not flagged: blank identifier
func IgnoredFile() error {
	_, err := os.Open("test.txt")
	return err
}

// not flagged: os.Open inside a closure — the closure is a separate execution
// context and must not be attributed to the outer function's fileVars.
func OpenInClosure() {
	fn := func() error {
		f, err := os.Open("test.txt")
		if err != nil {
			return err
		}
		defer f.Close()
		return nil
	}
	_ = fn()
}

// not flagged: inner variable named 'f' shadows outer 'f'; the inner open
// is deferred, so neither binding triggers the diagnostic.
func ShadowedVar() error {
	f, err := os.Open("outer.txt")
	if err != nil {
		return err
	}
	defer f.Close()
	if true {
		f, err := os.Open("inner.txt") // shadows outer f
		if err != nil {
			return err
		}
		defer f.Close()
		_ = f
	}
	return nil
}

// flagged: same variable reassigned via plain = after manual close; the first
// open's violation must not be hidden by the second open's defer.
func ReopenWithManualCloseThenDefer() error {
	f, err := os.Open("first.txt") // want `file Close\(\) should be deferred immediately after successful open to prevent resource leaks`
	if err != nil {
		return err
	}
	f.Close() // manual close — violation for first open
	f, err = os.Open("second.txt")
	if err != nil {
		return err
	}
	defer f.Close() // deferred — ok for second open
	return nil
}

// not flagged: suppression directive on violating line.
func SuppressedManualClose() error {
	f, err := os.Open("suppressed.txt") //nolint:fileclosenotdeferred
	if err != nil {
		return err
	}
	f.Close()
	return nil
}

// not flagged: suppression directive suppresses the reassignment-path violation
// (exercises the nolint check inside trackFileOpenAssignment).
func SuppressedReopenManualClose() error {
	f, err := os.Open("first.txt") //nolint:fileclosenotdeferred
	if err != nil {
		return err
	}
	f.Close()
	f, err = os.Open("second.txt")
	if err != nil {
		return err
	}
	defer f.Close()
	return nil
}

func BadSiblingClose(deferred bool) error {
	f, err := os.Open("test.txt") // want `file Close\(\) should be deferred immediately after successful open to prevent resource leaks`
	if err != nil {
		return err
	}
	if deferred {
		defer f.Close()
	} else {
		f.Close()
	}
	return nil
}
