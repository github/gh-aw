package blankassigncomma

func good() {
	// Single blank is OK (common pattern for ignoring single return value)
	_ = someFunction()

	// Assignment to real variables is OK
	a, b := someFunction2()
	_ = a
	_ = b

	// Mixed blank and real variable is OK
	_, err := someFunction2()
	if err != nil {
		// handle error
	}

	// Selective assignment with a trailing non-blank is OK, even though the
	// leading identifiers are blank.
	_, _, err2 := someFunction3()
	if err2 != nil {
		// handle error
	}

	// Selective assignment with a blank in the middle is OK.
	x, _, z := someFunction3()
	_ = x
	_ = z
}

func bad() {
	// Multiple consecutive blanks - code smell
	_, _ = someFunction2() // want `assignment with 2 blank identifiers`

	// Three blanks is also bad
	_, _, _ = someFunction3() // want `assignment with 3 blank identifiers`
}

func someFunction() interface{} {
	return nil
}

func someFunction2() (interface{}, interface{}) {
	return nil, nil
}

func someFunction3() (interface{}, interface{}, interface{}) {
	return nil, nil, nil
}
