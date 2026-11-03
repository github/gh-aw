package httpnoctx

import (
	"context"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// BadClientGet calls (*http.Client).Get without context.
func BadClientGet(client *http.Client, rawURL string) (*http.Response, error) {
	return client.Get(rawURL) // want `\(\*http\.Client\)\.Get does not accept a context`
}

// BadClientHead calls (*http.Client).Head without context.
func BadClientHead(client *http.Client, rawURL string) (*http.Response, error) {
	return client.Head(rawURL) // want `\(\*http\.Client\)\.Head does not accept a context`
}

// BadClientPost calls (*http.Client).Post without context.
func BadClientPost(client *http.Client, rawURL string, body io.Reader) (*http.Response, error) {
	return client.Post(rawURL, "application/json", body) // want `\(\*http\.Client\)\.Post does not accept a context`
}

// BadClientPostForm calls (*http.Client).PostForm without context.
func BadClientPostForm(client *http.Client, rawURL string, data url.Values) (*http.Response, error) {
	return client.PostForm(rawURL, data) // want `\(\*http\.Client\)\.PostForm does not accept a context`
}

// BadPkgGet calls the package-level http.Get without context.
func BadPkgGet(rawURL string) (*http.Response, error) {
	return http.Get(rawURL) // want `http\.Get does not accept a context`
}

// BadPkgHead calls the package-level http.Head without context.
func BadPkgHead(rawURL string) (*http.Response, error) {
	return http.Head(rawURL) // want `http\.Head does not accept a context`
}

// BadPkgPost calls the package-level http.Post without context.
func BadPkgPost(rawURL string, body io.Reader) (*http.Response, error) {
	return http.Post(rawURL, "application/json", body) // want `http\.Post does not accept a context`
}

// BadPkgPostForm calls the package-level http.PostForm without context.
func BadPkgPostForm(rawURL string, data url.Values) (*http.Response, error) {
	return http.PostForm(rawURL, data) // want `http\.PostForm does not accept a context`
}

// BadParenPkgGet calls a parenthesized http.Get without context.
func BadParenPkgGet(rawURL string) (*http.Response, error) {
	return (http.Get)(rawURL) // want `http\.Get does not accept a context`
}

// GoodClientDo uses http.NewRequestWithContext + client.Do — not flagged.
func GoodClientDo(ctx context.Context, client *http.Client, rawURL string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	return client.Do(req)
}

// BadDefaultClientDo uses timeout-less http.DefaultClient.Do.
func BadDefaultClientDo(ctx context.Context, rawURL string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, rawURL, strings.NewReader("body"))
	if err != nil {
		return nil, err
	}
	return http.DefaultClient.Do(req) // want `http\.DefaultClient\.Do uses a timeout-less client`
}

// BadNewRequestWithContextInScope calls http.NewRequest where context.Context is in scope.
func BadNewRequestWithContextInScope(ctx context.Context, rawURL string) (*http.Request, error) {
	_ = ctx
	return http.NewRequest(http.MethodGet, rawURL, nil) // want `http\.NewRequest does not propagate context`
}

// GoodNewRequestNoContextInScope uses http.NewRequest in a function without context.Context.
func GoodNewRequestNoContextInScope(rawURL string) (*http.Request, error) {
	return http.NewRequest(http.MethodGet, rawURL, nil)
}

// GoodTimeoutClientDo uses a dedicated client with Timeout set.
func GoodTimeoutClientDo(ctx context.Context, rawURL string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	client := &http.Client{Timeout: time.Second}
	return client.Do(req)
}

// GoodNewRequestInPlainClosure calls http.NewRequest inside a plain closure that
// has no context parameter; the outer context must not be attributed to it.
func GoodNewRequestInPlainClosure(ctx context.Context, rawURL string) http.HandlerFunc {
	_ = ctx
	return func(w http.ResponseWriter, r *http.Request) {
		_, _ = http.NewRequest(http.MethodGet, rawURL, nil)
	}
}

// BadNewRequestInGoClosure calls http.NewRequest inside a go closure; the outer
// context is still in scope there so the call is flagged.
func BadNewRequestInGoClosure(ctx context.Context, rawURL string) {
	_ = ctx
	go func() {
		_, _ = http.NewRequest(http.MethodGet, rawURL, nil) // want `http\.NewRequest does not propagate context`
	}()
}

// BadNewRequestInDeferClosure calls http.NewRequest inside a defer closure.
func BadNewRequestInDeferClosure(ctx context.Context, rawURL string) {
	_ = ctx
	defer func() {
		_, _ = http.NewRequest(http.MethodGet, rawURL, nil) // want `http\.NewRequest does not propagate context`
	}()
}
