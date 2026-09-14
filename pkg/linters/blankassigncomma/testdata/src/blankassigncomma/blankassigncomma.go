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
}

func bad() {
	// Multiple consecutive blanks - code smell
	_, _ = someFunction2() // want `assignment with 2 consecutive blank identifiers`
	
	// Three blanks is also bad
	_, _, _ = someFunction3() // want `assignment with 3 consecutive blank identifiers`
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
