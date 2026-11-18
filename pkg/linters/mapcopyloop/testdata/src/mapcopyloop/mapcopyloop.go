package mapcopyloop

import "maps"

func clone(src map[string]int) map[string]int {
	return maps.Clone(src)
}

func bad() {
	src := map[string]int{"a": 1}
	dst := make(map[string]int)

	for k, v := range src { // want `range-copy loop can be replaced with maps.Copy\(dst, src\)`
		dst[k] = v
	}

	var k string
	var v int
	for k, v = range src { // want `range-copy loop can be replaced with maps.Copy\(dst, src\)`
		dst[k] = v
	}

	// A shadowed package name prevents an unsafe suggested fix.
	{
		maps := struct{}{}
		_ = maps
		for k, v := range src { // want `range-copy loop can be replaced with maps.Copy\(dst, src\)`
			dst[k] = v
		}
	}

	// Keep comments in the loop rather than dropping them in a suggested fix.
	for k, v := range src { // want `range-copy loop can be replaced with maps.Copy\(dst, src\)`
		// copied entry
		dst[k] = v
	}
}

func good() {
	src := map[string]int{"a": 1}
	dst := make(map[string]int)

	for _, v := range src {
		dst["a"] = v
	}

	for k, _ := range src {
		dst[k] = 1
	}

	for k, v := range src {
		dst[k] += v
	}

	for k, v := range src {
		dst[k] = v
		_ = k
	}

	for k, v := range src {
		src[k] = v
	}

	dstAny := make(map[any]any)
	for k, v := range src {
		dstAny[k] = v
	}

	for k, v := range src { //nolint:mapcopyloop
		dst[k] = v
	}

	ints := map[int]int{1: 1}
	dstSlice := make([]int, 2)
	for k, v := range ints {
		dstSlice[k] = v
	}

	values := []int{1}
	for k, v := range values {
		dst[string(rune(k))] = v
	}
}
