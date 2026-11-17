package mapcopyloop

import "maps"

var _ = maps.Clone[map[string]int]

type holder struct{ m map[string]int }

func bad(dst, src map[string]int, h holder) {
	for k, v := range src { // want `range-copy loop over map can be replaced with maps\.Copy\(dst, src\)`
		dst[k] = v
	}

	for key, val := range src { // want `range-copy loop over map can be replaced with maps\.Copy\(h\.m, src\)`
		h.m[key] = val
	}
}

func good(dst, src map[string]int, sl []int, other map[string]int) {
	for _, v := range src {
		dst[v2k(v)] = v
	}
	for k, _ := range src {
		dst[k] = 1
	}
	for k, v := range src {
		dst[k] = v
		_ = k
	}
	for k, v := range src {
		src[k] = v
	}
	for k, v := range src {
		dst[k] = v + 1
	}
	for k, v := range src {
		dst[v2k(v)] = v
		_ = k
	}
	for i, v := range sl {
		_ = i
		_ = v
	}
	for k, v := range src { //nolint:mapcopyloop
		dst[k] = v
	}
	for k, v := range src {
		dst[k] += v
	}
	for k, v := range src {
		other[k] = dst[k]
		_ = v
	}
}

func v2k(int) string { return "" }
