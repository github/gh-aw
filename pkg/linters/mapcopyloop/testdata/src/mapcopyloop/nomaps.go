package mapcopyloop

func noImport(dst, src map[string]int) {
	for k, v := range src { // want `range-copy loop over map can be replaced with maps\.Copy\(dst, src\)`
		dst[k] = v
	}
}

func shadowed(dst, src map[string]int) {
	maps := 1
	_ = maps
	for k, v := range src { // want `range-copy loop over map can be replaced with maps\.Copy\(dst, src\)`
		dst[k] = v
	}
}
