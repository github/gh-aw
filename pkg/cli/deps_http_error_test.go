package cli

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

type depsTestRoundTripper func(*http.Request) (*http.Response, error)

func (f depsTestRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

type depsTestErrorBody struct {
	err error
}

func (b depsTestErrorBody) Read([]byte) (int, error) {
	return 0, b.err
}

func (depsTestErrorBody) Close() error {
	return nil
}

func TestDependencyQueriesReportResponseDrainErrors(t *testing.T) {
	bodyErr := errors.New("response body read failed")
	tests := []struct {
		name string
		run  func() error
	}{
		{
			name: "Go proxy",
			run: func() error {
				_, _, err := getLatestVersion(context.Background(), "example.com/module", "v1.0.0", false)
				return err
			},
		},
		{
			name: "GitHub advisory API",
			run: func() error {
				_, err := querySecurityAdvisories(context.Background(), map[string]string{}, false)
				return err
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			originalTransport := http.DefaultTransport
			t.Cleanup(func() {
				http.DefaultTransport = originalTransport
			})
			http.DefaultTransport = depsTestRoundTripper(func(req *http.Request) (*http.Response, error) {
				return &http.Response{
					StatusCode: http.StatusServiceUnavailable,
					Header:     make(http.Header),
					Body:       depsTestErrorBody{err: bodyErr},
					Request:    req,
				}, nil
			})

			err := test.run()
			require.ErrorIs(t, err, bodyErr)
			require.ErrorContains(t, err, "failed to drain response body")
		})
	}
}
