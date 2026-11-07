package httprespbodyclose

import (
	"io"
	"net/http"
)

// BadManualParenClose closes the body through a parenthesized callee; it is
// still a manual, non-deferred close.
func BadManualParenClose(client *http.Client, req *http.Request) ([]byte, error) {
	resp, err := client.Do(req) // want `HTTP response Body\.Close\(\) should be deferred immediately after receiving the response to prevent resource leaks`
	if err != nil {
		return nil, err
	}
	data, readErr := io.ReadAll(resp.Body)
	(resp.Body.Close)()
	return data, readErr
}

// BadManualParenReceiverClose closes the body through a parenthesized
// receiver and assigns the error.
func BadManualParenReceiverClose(client *http.Client, req *http.Request) error {
	resp, err := client.Do(req) // want `HTTP response Body\.Close\(\) should be deferred immediately after receiving the response to prevent resource leaks`
	if err != nil {
		return err
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	closeErr := (resp.Body).Close()
	return closeErr
}

// GoodParenDeferClose defers a parenthesized Body.Close callee; the extra
// early manual close on the error branch must not be reported because the
// response is also closed by the deferred call.
func GoodParenDeferClose(client *http.Client, req *http.Request) ([]byte, error) {
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer (resp.Body.Close)()
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, nil
	}
	return io.ReadAll(resp.Body)
}
