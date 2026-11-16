package httpclientnotimeout

import (
	"net/http"
	"time"
)

type Client struct{}

func pointerLiteral() *http.Client {
	return &http.Client{} // want "http.Client literal has no Timeout"
}

func transportOnly(t http.RoundTripper) *http.Client {
	return &http.Client{Transport: t} // want "http.Client literal has no Timeout"
}

func valueLiteral() http.Client {
	return http.Client{} // want "http.Client literal has no Timeout"
}

func withTimeout() *http.Client {
	return &http.Client{Timeout: 5 * time.Second}
}

func explicitZero() *http.Client {
	return &http.Client{Timeout: 0}
}

func otherClient() *Client {
	return &Client{}
}

func suppressed() *http.Client {
	return &http.Client{} //nolint:httpclientnotimeout
}
