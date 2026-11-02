package connectors

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"
)

func validateRetryPolicy(policy *RetryPolicy, request Request, phase string) error {
	if policy == nil {
		return nil
	}
	if policy.MaxAttempts < 1 || policy.MaxAttempts > 4 || policy.BackoffMS < 1 || policy.BackoffMS > 1000 {
		return errors.New("retry requires maxAttempts between 1 and 4 and backoffMs between 1 and 1000")
	}
	if len(policy.RetryOn) == 0 || len(policy.RetryOn) > 5 {
		return errors.New("retryOn requires explicit transient HTTP statuses")
	}
	seen := map[int]bool{}
	for _, status := range policy.RetryOn {
		if !slices.Contains([]int{429, 500, 502, 503, 504}, status) || seen[status] {
			return errors.New("retryOn must contain unique statuses from 429, 500, 502, 503, 504")
		}
		seen[status] = true
	}
	if phase == "safe-output" && policy.MaxAttempts > 1 &&
		(!policy.Idempotent || !slices.Contains([]string{"PUT", "PATCH", "DELETE"}, request.Method)) {
		return errors.New("safe-output retries require an explicitly idempotent PUT, PATCH or DELETE operation; create and append operations cannot retry")
	}
	return nil
}

func connectorHTTP(ctx context.Context, client *http.Client, request *http.Request, maximum int, policy *RetryPolicy) ([]byte, int, error) {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	attempts := 1
	if policy != nil {
		attempts = policy.MaxAttempts
	}
	for attempt := range attempts {
		current := request.Clone(ctx)
		if request.GetBody != nil {
			body, err := request.GetBody()
			if err != nil {
				return nil, 0, errors.New("connector request body could not be replayed")
			}
			current.Body = body
		}
		data, status, retryAfter, err := connectorHTTPAttempt(client, current, maximum)
		if err != nil {
			return nil, 0, err
		}
		if status >= 200 && status < 300 {
			return data, status, nil
		}
		if policy == nil {
			return nil, 0, fmt.Errorf("connector HTTP status %d; no retry performed", status)
		}
		if attempt+1 == attempts || !slices.Contains(policy.RetryOn, status) {
			return nil, 0, fmt.Errorf("connector HTTP status %d after %d attempt(s)", status, attempt+1)
		}
		delay := time.Duration(policy.BackoffMS) * time.Millisecond * time.Duration(1<<attempt)
		if suggested := connectorRetryAfter(retryAfter); suggested > delay {
			delay = suggested
		}
		deadline, _ := ctx.Deadline()
		if delay >= time.Until(deadline) {
			return nil, 0, errors.New("connector retry delay exceeds the bounded execution window")
		}
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, 0, errors.New("connector retry cancelled before another request")
		case <-timer.C:
		}
	}
	return nil, 0, errors.New("connector retry attempts exhausted")
}

func connectorHTTPAttempt(client *http.Client, request *http.Request, maximum int) ([]byte, int, string, error) {
	response, err := client.Do(request)
	if err != nil {
		return nil, 0, "", errors.New("connector HTTP request failed; outcome may be unknown, no retry performed")
	}
	defer func() {
		if err := response.Body.Close(); err != nil {
			log.Printf("close connector response: %v", err)
		}
	}()
	data, err := io.ReadAll(io.LimitReader(response.Body, int64(maximum)+1))
	if err != nil || len(data) > maximum {
		return nil, 0, "", errors.New("connector response could not be read within its byte bound")
	}
	return data, response.StatusCode, response.Header.Get("Retry-After"), nil
}

func connectorRetryAfter(value string) time.Duration {
	value = strings.TrimSpace(value)
	if value != "" && strings.Trim(value, "0123456789") == "" {
		seconds, err := strconv.ParseUint(value, 10, 64)
		if err != nil || seconds > 20 {
			return 21 * time.Second
		}
		return time.Duration(seconds) * time.Second
	}
	if deadline, err := http.ParseTime(value); err == nil {
		return time.Until(deadline)
	}
	return 0
}
