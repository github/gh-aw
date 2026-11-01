package stringbytesroundtrip

// Named types to verify the analyzer checks underlying types.
type myString string
type myBytes []byte

func good() {
	s := "hello"
	b := []byte("world")

	// These are valid, non-redundant conversions.
	_ = string(b)
	_ = []byte(s)

	// Named types: single-step conversions are fine.
	var ms myString = "hello"
	var mb myBytes = []byte("world")
	_ = string(mb)
	_ = []byte(ms)
}

func bad() {
	s := "hello"
	b := []byte{104, 101, 108, 108, 111}

	_ = string([]byte(s)) // want `string\(\[\]byte\(s\)\) is a redundant round-trip`
	_ = []byte(string(b)) // want `\[\]byte\(string\(b\)\) makes two copies to clone b`
}

func badNamedTypes() string {
	var ms myString = "hello"
	var mb myBytes = []byte("world")

	// Named-type round-trips: underlying types still match, so these are flagged.
	_ = myString([]byte(ms))  // want `myString\(\[\]byte\(ms\)\) is a redundant round-trip; replace it with myString\(ms\)`
	_ = []byte(string(mb))    // want `\[\]byte\(string\(mb\)\) makes two copies to clone mb`
	return string([]byte(ms)) // want `string\(\[\]byte\(ms\)\) is a redundant round-trip; replace it with string\(ms\)`
}

// aliasString is an alias for the predeclared string type, so round-trips
// through it are fully redundant.
type aliasString = string

// namedAlias is an alias for a named string type, so an outer conversion is
// still required even though its underlying type is string.
type namedAlias = myString

func badMixedNamedTypes() {
	s := "hello"
	var ms myString = "hello"
	var as aliasString = "hello"
	var na namedAlias = "hello"

	// Outer conversion is a named string type: the outer conversion must stay.
	_ = myString([]byte(s)) // want `myString\(\[\]byte\(s\)\) is a redundant round-trip; replace it with myString\(s\)`
	// Argument is a named string type: an outer conversion is still needed.
	_ = string([]byte(ms)) // want `string\(\[\]byte\(ms\)\) is a redundant round-trip; replace it with string\(ms\)`
	// Alias of the predeclared string type: both conversions can be removed.
	_ = aliasString([]byte(as)) // want `aliasString\(\[\]byte\(as\)\) is a redundant round-trip; both conversions can be removed`
	// Alias of a named string type: the outer conversion must stay.
	_ = namedAlias([]byte(na)) // want `namedAlias\(\[\]byte\(na\)\) is a redundant round-trip; replace it with namedAlias\(na\)`
}

func badParenInner() {
	s := "hello"
	b := []byte{104, 101, 108, 108, 111}

	_ = string(([]byte(s))) // want `string\(\[\]byte\(s\)\) is a redundant round-trip`
	_ = []byte((string(b))) // want `\[\]byte\(string\(b\)\) makes two copies to clone b`
}

// helperString is a regular function, not a type conversion — must not be flagged.
func helperString(b []byte) string { return string(b) }

func notAConversion() {
	b := []byte("world")
	// Calling helperString (a real function) with []byte(s) is not a round-trip.
	_ = helperString([]byte("x"))
	_ = helperString(b)
}
