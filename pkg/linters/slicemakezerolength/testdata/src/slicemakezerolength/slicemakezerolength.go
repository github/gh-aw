package slicemakezerolength

func badZeroLengthNoCapacity() {
	s := make([]string, 0) // want `make\(\[\]string, 0\) without capacity can be optimized`
	_ = s
}

func badZeroLengthInt() {
	s := make([]int, 0) // want `make\(\[\]int, 0\) without capacity can be optimized`
	_ = s
}

func badZeroLengthByte() {
	s := make([]byte, 0) // want `make\(\[\]byte, 0\) without capacity can be optimized`
	_ = s
}

func badZeroLengthCustomType() {
	type MyType struct {
		name string
	}
	s := make([]MyType, 0) // want `make\(\[\]MyType, 0\) without capacity can be optimized`
	_ = s
}

func goodWithCapacity() {
	s := make([]string, 0, 10)
	_ = s
}

func goodWithLength() {
	s := make([]string, 5)
	_ = s
}

func goodWithLengthAndCapacity() {
	s := make([]string, 5, 10)
	_ = s
}

func goodArrayType() {
	s := [10]string{}
	_ = s
}

func goodArrayLiteral() {
	s := []string{"a", "b"}
	_ = s
}

func suppressed() {
	//nolint:slicemakezerolength
	s := make([]string, 0)
	_ = s
}
