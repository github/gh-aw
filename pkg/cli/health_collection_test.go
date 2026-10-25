//go:build !integration

package cli

import (
	"context"
	"errors"
	"io"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHealthAPIHTMLRequestErrorsAreBoundedBeforeLogging(t *testing.T) {
	for _, rateLimited := range []bool{false, true} {
		name := "non_rate_limit"
		message := "Service unavailable"
		if rateLimited {
			name, message = "rate_limit", "API rate limit exceeded"
		}
		t.Run(name, func(t *testing.T) {
			original := healthRunGH
			t.Cleanup(func() { healthRunGH = original })
			collection := &HealthCollection{Host: "github.com", APIRateLimit: &GitHubAPIRateLimitState{Remaining: 4000}}
			body := []byte("<html>" + message + strings.Repeat("private error page content", 20000) + "</html>")
			healthRunGH = func(context.Context, string, ...string) ([]byte, error) {
				return body, errors.New("HTTP 502")
			}
			var err error
			debug := captureHealthDebugLog(t, func() {
				_, err = healthAPI(collection, "https://github.com/org/repo/actions/runs/1", "--header", "Accept: text/html")
			})
			require.Error(t, err)
			assert.LessOrEqual(t, len(err.Error()), 256)
			assert.LessOrEqual(t, len(collection.Warning), 256)
			assert.Less(t, len(debug), 512)
			for _, output := range []string{err.Error(), collection.Warning, debug} {
				assert.NotContains(t, output, "<html>")
				assert.NotContains(t, output, "private error page content")
			}
			assert.Equal(t, rateLimited, collection.stopped)
			assert.Equal(t, rateLimited, collection.Partial)
			if rateLimited {
				require.ErrorIs(t, err, errLogsAPIRateLimitReached)
				assert.Equal(t, "rate_limit", collection.Reason)
				if healthLog.Enabled() {
					assert.Contains(t, debug, "Health collection stopped:")
				}
			} else {
				assert.Empty(t, collection.Warning)
				assert.NotContains(t, debug, "Health collection stopped:")
			}
		})
	}
}

func TestHealthAPIHTMLShortErrorsAndRESTSemanticsArePreserved(t *testing.T) {
	for _, accept := range []string{"application/json", "text/html"} {
		t.Run(accept, func(t *testing.T) {
			original := healthRunGH
			t.Cleanup(func() { healthRunGH = original })
			originalErr := errors.New("HTTP 502")
			response := ""
			if accept == "application/json" {
				response = `{"message":"Bad Gateway"}`
			}
			healthRunGH = func(context.Context, string, ...string) ([]byte, error) {
				return []byte(response), originalErr
			}
			collection := &HealthCollection{Host: "github.com", APIRateLimit: &GitHubAPIRateLimitState{Remaining: 4000}}
			out, err := healthAPI(collection, "repos/org/repo", "--header", "Accept: "+accept)
			require.ErrorIs(t, err, originalErr)
			assert.Equal(t, "health API request failed: HTTP 502: "+response, err.Error())
			assert.Equal(t, response, string(out))
			assert.False(t, collection.stopped)
		})
	}
}

func TestHealthAPIHTMLCommandErrorIsBounded(t *testing.T) {
	for _, message := range []string{"<html>private command error</html>", strings.Repeat("private command error", 20000)} {
		t.Run(strconv.Itoa(len(message)), func(t *testing.T) {
			original := healthRunGH
			t.Cleanup(func() { healthRunGH = original })
			healthRunGH = func(context.Context, string, ...string) ([]byte, error) {
				return nil, errors.New(message)
			}
			collection := &HealthCollection{Host: "github.com", APIRateLimit: &GitHubAPIRateLimitState{Remaining: 4000}}
			_, err := healthAPI(collection, "https://github.com/org/repo/actions/runs/1", "--header", "Accept: text/html")
			require.Error(t, err)
			assert.LessOrEqual(t, len(err.Error()), 256)
			assert.NotContains(t, err.Error(), "private command error")
			assert.False(t, collection.stopped)
		})
	}
}

func captureHealthDebugLog(t *testing.T, fn func()) string {
	t.Helper()
	original := os.Stderr
	r, w, err := os.Pipe()
	require.NoError(t, err)
	os.Stderr = w
	t.Cleanup(func() {
		os.Stderr = original
		_ = w.Close()
		_ = r.Close()
	})
	var output []byte
	var readErr error
	done := make(chan struct{})
	go func() {
		output, readErr = io.ReadAll(r)
		close(done)
	}()
	fn()
	os.Stderr = original
	require.NoError(t, w.Close())
	<-done
	require.NoError(t, readErr)
	return string(output)
}
