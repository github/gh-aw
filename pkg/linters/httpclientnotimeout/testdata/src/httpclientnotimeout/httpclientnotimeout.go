package httpclientnotimeout

import "net/http"

type Client struct{}

func missingPointerTimeout() *http.Client {
	return &http.Client{} // want `net/http\.Client literal has no Timeout field`
}

func missingValueTimeout() http.Client {
	return http.Client{} // want `net/http\.Client literal has no Timeout field`
}

func timeoutSet() *http.Client {
	return &http.Client{Timeout: 0}
}

func unrelatedClient() Client {
	return Client{}
}

func suppressed() *http.Client {
	return &http.Client{} //nolint:httpclientnotimeout // exercise suppression
}
