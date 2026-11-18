package mapcopyloopnoimport

func copyEntries(src, dst map[string]int) {
	for key, value := range src { // want `range-copy loop can be replaced with maps.Copy\(dst, src\)`
		dst[key] = value
	}
}
