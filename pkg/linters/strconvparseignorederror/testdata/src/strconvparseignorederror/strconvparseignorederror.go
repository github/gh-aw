package strconvparseignorederror

import "strconv"

func bad() {
	n, _ := strconv.Atoi("42") // want `error return from strconv\.Atoi is discarded`
	_ = n
	x, _ := strconv.ParseInt("42", 10, 64) // want `error return from strconv\.ParseInt is discarded`
	_ = x
	f, _ := strconv.ParseFloat("3.14", 64) // want `error return from strconv\.ParseFloat is discarded`
	_ = f
	b, _ := strconv.ParseBool("true") // want `error return from strconv\.ParseBool is discarded`
	_ = b
	u, _ := strconv.ParseUint("42", 10, 64) // want `error return from strconv\.ParseUint is discarded`
	_ = u
}

func badBareCalls() {
	strconv.Atoi("42") // want `error return from strconv\.Atoi is discarded`
}

func good() {
	n, err := strconv.Atoi("42")
	if err != nil {
		return
	}
	_ = n

	x, err2 := strconv.ParseInt("42", 10, 64)
	if err2 != nil {
		return
	}
	_ = x
}

func suppressed() {
	//nolint:strconvparseignorederror
	n, _ := strconv.Atoi("42")
	_ = n
	x, _ := strconv.ParseInt("42", 10, 64) //nolint:strconvparseignorederror
	_ = x
}
