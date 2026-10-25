package githubapi

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/github/gh-aw/pkg/logger"
)

var publicGetLog = logger.New("githubapi:public_get")

const maxPublicResponseBytes int64 = 10 << 20

// HTTPStatusError reports a non-200 response from the public GitHub API.
type HTTPStatusError struct {
	StatusCode int
	Body       string
}

func (e *HTTPStatusError) Error() string {
	return fmt.Sprintf("GitHub API returned %d: %s", e.StatusCode, e.Body)
}

// ResponseTooLargeError reports a public GitHub API response that exceeds its size limit.
type ResponseTooLargeError struct {
	Limit int64
}

func (e *ResponseTooLargeError) Error() string {
	return fmt.Sprintf("public GitHub API response exceeds %d bytes", e.Limit)
}

// GetPublic performs an unauthenticated GitHub JSON GET using the supplied client.
// It returns the response bytes only for HTTP 200 and always closes the response body.
// Responses larger than 10 MiB return a *ResponseTooLargeError.
func GetPublic(ctx context.Context, client *http.Client, endpointURL string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpointURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			publicGetLog.Printf("Failed to close public GitHub API response: %v", err)
		}
	}()

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxPublicResponseBytes+1))
	if int64(len(body)) > maxPublicResponseBytes {
		return nil, &ResponseTooLargeError{Limit: maxPublicResponseBytes}
	}
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, &HTTPStatusError{
			StatusCode: resp.StatusCode,
			Body:       strings.TrimSpace(string(body)),
		}
	}
	return body, nil
}
