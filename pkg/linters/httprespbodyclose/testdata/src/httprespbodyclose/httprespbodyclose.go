package httprespbodyclose

import (
	"io"
	"net/http"
)

// BadManualClose calls resp.Body.Close() directly instead of deferring it.
func BadManualClose(client *http.Client, req *http.Request) ([]byte, error) {
	resp, err := client.Do(req) // want `HTTP response Body\.Close\(\) should be deferred immediately after receiving the response to prevent resource leaks`
	if err != nil {
		return nil, err
	}
	data, readErr := io.ReadAll(resp.Body)
	resp.Body.Close()
	return data, readErr
}

// GoodDeferClose uses defer resp.Body.Close() immediately after receiving — not flagged.
func GoodDeferClose(client *http.Client, req *http.Request) ([]byte, error) {
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	return io.ReadAll(resp.Body)
}

// GoodNoClose returns the response to the caller, which is responsible for closing — not flagged.
func GoodNoClose(client *http.Client, req *http.Request) (*http.Response, error) {
	return client.Do(req)
}

// GoodCloseInsideClosure closes the outer response inside a closure — not flagged.
func GoodCloseInsideClosure(client *http.Client, req *http.Request) {
	resp, err := client.Do(req)
	if err != nil {
		return
	}
	go func() {
		_ = resp.Body.Close()
	}()
}

// BadManualCloseInsideIIFE reports manual close when assignment and close are in the same closure.
func BadManualCloseInsideIIFE(client *http.Client, req *http.Request) {
	func() {
		resp, err := client.Do(req) // want `HTTP response Body\.Close\(\) should be deferred immediately after receiving the response to prevent resource leaks`
		if err != nil {
			return
		}
		_, _ = io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
	}()
}

// BadManualCloseInsideGoClosure reports manual close when assignment and close are in a go closure.
func BadManualCloseInsideGoClosure(client *http.Client, req *http.Request) {
	go func() {
		resp, err := client.Do(req) // want `HTTP response Body\.Close\(\) should be deferred immediately after receiving the response to prevent resource leaks`
		if err != nil {
			return
		}
		_, _ = io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
	}()
}

// BadManualCloseInsideHandler reports manual close when assignment and close are in a handler closure.
func BadManualCloseInsideHandler(client *http.Client, req *http.Request) {
	mux := http.NewServeMux()
	mux.HandleFunc("/x", func(http.ResponseWriter, *http.Request) {
		resp, err := client.Do(req) // want `HTTP response Body\.Close\(\) should be deferred immediately after receiving the response to prevent resource leaks`
		if err != nil {
			return
		}
		_, _ = io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
	})
}

// GoodDeferCloseInsideClosure uses defer close inside closure — not flagged.
func GoodDeferCloseInsideClosure(client *http.Client, req *http.Request) {
	func() {
		resp, err := client.Do(req)
		if err != nil {
			return
		}
		defer resp.Body.Close()
		_, _ = io.Copy(io.Discard, resp.Body)
	}()
}

// GoodShadowedResp closes both outer and inner shadowed responses with defer — not flagged.
func GoodShadowedResp(client *http.Client, req1, req2 *http.Request) error {
	resp, err := client.Do(req1)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if true {
		resp, err := client.Do(req2)
		if err != nil {
			return err
		}
		defer resp.Body.Close()
	}
	return nil
}

// BadReopenManualCloseThenDefer reports the first assignment when resp is reused.
func BadReopenManualCloseThenDefer(client *http.Client, req1, req2 *http.Request) error {
	resp, err := client.Do(req1) // want `HTTP response Body\.Close\(\) should be deferred immediately after receiving the response to prevent resource leaks`
	if err != nil {
		return err
	}
	resp.Body.Close()

	resp, err = client.Do(req2)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return nil
}

// BadManualCloseAssigned reports Body.Close() used on assignment RHS.
func BadManualCloseAssigned(client *http.Client, req *http.Request) ([]byte, error) {
	resp, err := client.Do(req) // want `HTTP response Body\.Close\(\) should be deferred immediately after receiving the response to prevent resource leaks`
	if err != nil {
		return nil, err
	}
	data, readErr := io.ReadAll(resp.Body)
	closeErr := resp.Body.Close()
	if readErr != nil {
		return nil, readErr
	}
	return data, closeErr
}

// SuppressedOnAssignment uses nolint on assignment, so no diagnostic should be reported.
func SuppressedOnAssignment(client *http.Client, req *http.Request) ([]byte, error) {
	resp, err := client.Do(req) //nolint:httprespbodyclose
	if err != nil {
		return nil, err
	}
	data, readErr := io.ReadAll(resp.Body)
	resp.Body.Close()
	return data, readErr
}

// SuppressedAcrossReassignment suppresses the prior-assignment report path.
func SuppressedAcrossReassignment(client *http.Client, req1, req2 *http.Request) error {
	resp, err := client.Do(req1) //nolint:httprespbodyclose
	if err != nil {
		return err
	}

	resp.Body.Close()

	resp, err = client.Do(req2)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return nil
}

func BadSiblingClose(client *http.Client, req *http.Request, fast bool) error {
	resp, err := client.Do(req) // want `HTTP response Body\.Close\(\) should be deferred immediately after receiving the response to prevent resource leaks`
	if err != nil {
		return err
	}
	if fast {
		resp.Body.Close()
	} else {
		defer resp.Body.Close()
	}
	return nil
}

func DifferentBranchResponses(client *http.Client, req *http.Request, deferred bool) {
	var resp *http.Response
	if deferred {
		resp, _ = client.Do(req)
		defer resp.Body.Close()
	} else {
		resp, _ = client.Do(req) // want `HTTP response Body\.Close\(\) should be deferred immediately after receiving the response to prevent resource leaks`
		resp.Body.Close()
	}
}

func BothBranchResponsesManual(client *http.Client, req *http.Request, first bool) {
	var resp *http.Response
	if first {
		resp, _ = client.Do(req) // want `HTTP response Body\.Close\(\) should be deferred immediately after receiving the response to prevent resource leaks`
	} else {
		resp, _ = client.Do(req) // want `HTTP response Body\.Close\(\) should be deferred immediately after receiving the response to prevent resource leaks`
	}
	resp.Body.Close()
}
