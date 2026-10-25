//go:build !integration

package githubapi

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"testing/iotest"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

type trackingBody struct {
	io.Reader
	closed bool
}

func (b *trackingBody) Close() error {
	b.closed = true
	return nil
}

func TestGetPublicResponse(t *testing.T) {
	t.Parallel()
	readErr := errors.New("body read failed")
	for _, tt := range []struct {
		name       string
		statusCode int
		readErr    error
	}{
		{"success", http.StatusOK, nil},
		{"not found", http.StatusNotFound, nil},
		{"rate limited", http.StatusForbidden, nil},
		{"other success status is rejected", http.StatusCreated, nil},
		{"read failure", http.StatusOK, readErr},
		{"read failure before status validation", http.StatusForbidden, readErr},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			const payload = " \n{\"message\":\"response\"}\n "
			body := &trackingBody{Reader: strings.NewReader(payload)}
			if tt.readErr != nil {
				body.Reader = iotest.ErrReader(tt.readErr)
			}
			ctx := t.Context()
			const endpoint = "https://api.github.com/repos/owner/repo?ref=feature%2Fbranch"
			client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				if req.Method != http.MethodGet || req.URL.String() != endpoint {
					t.Errorf("unexpected request: %s %s", req.Method, req.URL)
				}
				if req.Header.Get("Accept") != "application/vnd.github+json" {
					t.Errorf("unexpected Accept header: %q", req.Header.Get("Accept"))
				}
				if req.Header.Get("Authorization") != "" {
					t.Error("public request must not set Authorization")
				}
				if req.Context() != ctx {
					t.Error("request must use the caller context")
				}
				return &http.Response{StatusCode: tt.statusCode, Body: body}, nil
			})}

			got, err := GetPublic(ctx, client, endpoint)
			if !body.closed {
				t.Error("response body was not closed")
			}
			switch {
			case tt.readErr != nil:
				if !errors.Is(err, tt.readErr) {
					t.Fatalf("error = %v, want %v", err, tt.readErr)
				}
			case tt.statusCode != http.StatusOK:
				var statusErr *HTTPStatusError
				if !errors.As(err, &statusErr) {
					t.Fatalf("error = %v, want *HTTPStatusError", err)
				}
				if statusErr.StatusCode != tt.statusCode || statusErr.Body != strings.TrimSpace(payload) {
					t.Fatalf("unexpected status error: %+v", statusErr)
				}
				wantError := fmt.Sprintf("GitHub API returned %d: %s", tt.statusCode, strings.TrimSpace(payload))
				if err.Error() != wantError {
					t.Fatalf("unexpected error message: %v", err)
				}
			default:
				if err != nil || string(got) != payload {
					t.Fatalf("GetPublic() = %q, %v; want %q, nil", got, err, payload)
				}
				return
			}
			if got != nil {
				t.Errorf("GetPublic() returned body %q on error", got)
			}
		})
	}
}

func TestGetPublicResponseTooLarge(t *testing.T) {
	t.Parallel()
	body := &trackingBody{Reader: strings.NewReader(strings.Repeat("x", int(maxPublicResponseBytes)+2))}
	client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Body: body}, nil
	})}

	got, err := GetPublic(t.Context(), client, "https://api.github.com/repos/owner/repo")
	var sizeErr *ResponseTooLargeError
	if !errors.As(err, &sizeErr) {
		t.Fatalf("error = %v, want *ResponseTooLargeError", err)
	}
	if sizeErr.Limit != maxPublicResponseBytes {
		t.Errorf("response size limit = %d, want %d", sizeErr.Limit, maxPublicResponseBytes)
	}
	if got != nil {
		t.Errorf("GetPublic() returned %d bytes on error", len(got))
	}
	if !body.closed {
		t.Error("response body was not closed")
	}
	if remaining := body.Reader.(*strings.Reader).Len(); remaining != 1 {
		t.Errorf("read %d bytes beyond the limit, want exactly %d", maxPublicResponseBytes+1-int64(remaining), maxPublicResponseBytes+1)
	}
}

func TestGetPublicInvalidURL(t *testing.T) {
	t.Parallel()
	client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		t.Fatal("invalid URL must not reach the transport")
		return nil, nil
	})}
	body, err := GetPublic(t.Context(), client, "https://api.github.com/%zz")
	if err == nil || body != nil {
		t.Fatalf("GetPublic() = %q, %v; want nil body and error", body, err)
	}
}

func TestGetPublicRequestFailure(t *testing.T) {
	t.Parallel()
	requestErr := errors.New("network unavailable")
	client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return nil, requestErr
	})}
	body, err := GetPublic(t.Context(), client, "https://api.github.com/repos/owner/repo")
	if !errors.Is(err, requestErr) || body != nil {
		t.Fatalf("GetPublic() = %q, %v; want nil body and %v", body, err, requestErr)
	}
}

func TestGetPublicCancellation(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		cancel()
		if err := req.Context().Err(); err != nil {
			return nil, err
		}
		t.Fatal("request context was not canceled")
		return nil, nil
	})}
	body, err := GetPublic(ctx, client, "https://api.github.com/repos/owner/repo")
	if !errors.Is(err, context.Canceled) || body != nil {
		t.Fatalf("GetPublic() = %q, %v; want nil body and context.Canceled", body, err)
	}
}
