package readallunboundedhttpbody

import (
	"io"
	"net/http"
	"strings"
)

type fakeResp struct {
	Body io.Reader
}

func BadPointer(resp *http.Response) ([]byte, error) {
	return io.ReadAll(resp.Body) // want `io\.ReadAll on an HTTP response Body is unbounded`
}

func BadValue(resp http.Response) ([]byte, error) {
	return io.ReadAll(resp.Body) // want `io\.ReadAll on an HTTP response Body is unbounded`
}

func BadParen(resp *http.Response) ([]byte, error) {
	return io.ReadAll((resp.Body)) // want `io\.ReadAll on an HTTP response Body is unbounded`
}

func GoodLimited(resp *http.Response) ([]byte, error) {
	return io.ReadAll(io.LimitReader(resp.Body, 1<<20))
}

func GoodOtherStruct(f fakeResp) ([]byte, error) {
	return io.ReadAll(f.Body)
}

func GoodStrings() ([]byte, error) {
	return io.ReadAll(strings.NewReader("x"))
}

func GoodNolint(resp *http.Response) ([]byte, error) {
	return io.ReadAll(resp.Body) //nolint:readallunboundedhttpbody
}
