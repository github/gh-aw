package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"golang.org/x/net/http/httpguts"
	"google.golang.org/protobuf/encoding/protowire"
)

const envOTELResponseLimit = 64 * 1024

func envOTELValidate(endpoint, protocol, rawHeaders string) (*url.URL, http.Header, error) {
	if protocol != "http/json" && protocol != "http/protobuf" {
		return nil, nil, errors.New("--protocol must be http/json or http/protobuf")
	}
	if err := validateActionsStorageValue(endpoint); err != nil {
		return nil, nil, err
	}
	u, err := envOTELURL(endpoint)
	if err != nil {
		return nil, nil, err
	}
	headers, err := envOTELHeaders(u, rawHeaders)
	return u, headers, err
}

func envOTELURL(endpoint string) (*url.URL, error) {
	invalidURL := errors.New("invalid OTLP endpoint: use an absolute HTTPS URL without credentials, fragments, whitespace, or control characters; percent-encode quotes and backslashes (HTTP allowed only on literal loopback)")
	if len(endpoint) > 8192 || strings.ContainsAny(endpoint, `"\\`) || strings.ContainsFunc(endpoint, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) }) {
		return nil, invalidURL
	}
	u, err := url.Parse(endpoint)
	if err != nil || u.Hostname() == "" || u.User != nil || u.Fragment != "" || strings.Contains(endpoint, "#") || u.Opaque != "" {
		return nil, invalidURL
	}
	decodedQuery, err := url.QueryUnescape(u.RawQuery)
	if err != nil || validateActionsStorageValue(decodedQuery) != nil {
		return nil, invalidURL
	}
	decoded, err := url.PathUnescape(endpoint)
	if err != nil || validateActionsStorageValue(decoded) != nil || strings.ContainsFunc(decoded, unicode.IsControl) {
		return nil, invalidURL
	}
	if port := u.Port(); port != "" {
		n, err := strconv.Atoi(port)
		if err != nil || n < 1 || n > 65535 {
			return nil, invalidURL
		}
	}
	ip := net.ParseIP(u.Hostname())
	allowed := u.Scheme == "https" || u.Scheme == "http" && ip != nil && ip.IsLoopback()
	if !allowed {
		return nil, invalidURL
	}
	escapedPath := u.EscapedPath()
	if !strings.HasSuffix(escapedPath, "/v1/traces") {
		escapedPath = strings.TrimRight(escapedPath, "/") + "/v1/traces" //nolint:manualpathconcat // Match the exporter without cleaning collector-specific path segments.
		u.Path, err = url.PathUnescape(escapedPath)
		if err != nil {
			return nil, invalidURL
		}
		if escapedPath == u.Path {
			u.RawPath = ""
		} else {
			u.RawPath = escapedPath
		}
	}
	return u, nil
}

func envOTELSentryDomain(host string) bool {
	host = strings.ToLower(strings.TrimSuffix(host, "."))
	return host == "sentry.io" || strings.HasSuffix(host, ".sentry.io")
}

func envOTELHeaders(u *url.URL, rawHeaders string) (http.Header, error) {
	headers := http.Header{}
	headerError := errors.New("invalid OTLP headers: provide nonempty comma-separated key=value pairs; percent-encode commas; transport headers and control characters are not allowed")
	if rawHeaders == "" || len(rawHeaders) > 48*1024 || strings.ContainsFunc(rawHeaders, unicode.IsControl) {
		return nil, headerError
	}
	for pair := range strings.SplitSeq(rawHeaders, ",") {
		name, value, found := strings.Cut(pair, "=")
		if !found {
			return nil, headerError
		}
		name, err := url.PathUnescape(name)
		if err != nil {
			return nil, headerError
		}
		value, err = url.PathUnescape(value)
		if err != nil || strings.ContainsFunc(name, unicode.IsControl) || strings.ContainsFunc(value, unicode.IsControl) {
			return nil, headerError
		}
		name, value = strings.TrimSpace(name), strings.TrimSpace(value)
		if err := validateActionsStorageValue(value); err != nil {
			return nil, err
		}
		if err != nil || !httpguts.ValidHeaderFieldName(name) || value == "" || !httpguts.ValidHeaderFieldValue(value) || strings.ContainsFunc(value, unicode.IsControl) {
			return nil, headerError
		}
		switch strings.ToLower(name) {
		case "host", "content-type", "content-length", "transfer-encoding", "connection", "trailer", "upgrade", "proxy-authorization", "proxy-connection", "accept", "accept-encoding":
			return nil, headerError
		}
		if strings.EqualFold(name, "Authorization") || strings.EqualFold(name, "x-sentry-auth") {
			switch strings.ToLower(value) {
			case "api-key", "apikey", "basic", "bearer", "concealed", "digest", "dpop", "dsn", "gnap", "hoba", "mutual", "negotiate", "oauth", "privatetoken", "scram-sha-1", "scram-sha-256", "sentry", "vapid":
				return nil, headerError
			}
		}
		if envOTELSentryDomain(u.Hostname()) {
			switch strings.ToLower(name) {
			case "sentry_key", "sentry_version", "sentry_secret", "sentry_client":
				return nil, headerError
			}
		}
		if headers.Get(name) != "" {
			return nil, headerError
		}
		headers.Set(name, value)
	}
	if envOTELSentryDomain(u.Hostname()) && headers.Get("Authorization") != "" {
		if headers.Get("x-sentry-auth") != "" {
			return nil, headerError
		}
		headers.Set("x-sentry-auth", headers.Get("Authorization"))
		headers.Del("Authorization")
	}
	return headers, nil
}

func envOTELProbe(ctx context.Context, endpoint *url.URL, headers http.Header, protocol string) error {
	body, contentType := []byte(`{"resourceSpans":[]}`), "application/json"
	if protocol == "http/protobuf" {
		body, contentType = nil, "application/x-protobuf"
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint.String(), bytes.NewReader(body))
	if err != nil {
		return errors.New("could not create OTLP test request")
	}
	req.Header = headers.Clone()
	req.Header.Set("Content-Type", contentType)
	req.Header.Set("Accept", contentType)
	// Never forward arbitrary credential headers to a redirect destination.
	client := &http.Client{
		Timeout: 10 * time.Second,
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	response, err := client.Do(req)
	if err != nil {
		return errors.New("OTLP endpoint test failed (network, TLS, or timeout); no settings saved; details withheld")
	}
	defer func() {
		if err := response.Body.Close(); err != nil {
			envCmdLog.Print("OTLP probe response cleanup failed; details withheld")
		}
	}()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("OTLP endpoint test returned HTTP %d; no settings saved; response withheld", response.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, envOTELResponseLimit+1))
	if err != nil || len(data) > envOTELResponseLimit {
		return errors.New("OTLP endpoint test response unreadable or exceeds 64 KiB; no settings saved")
	}
	if len(data) == 0 {
		return nil
	}
	responseType, _, err := mime.ParseMediaType(response.Header.Get("Content-Type"))
	if err != nil || responseType != contentType {
		return errors.New("OTLP endpoint test returned an unexpected response encoding; no settings saved")
	}
	if protocol == "http/protobuf" {
		err = envOTELProtoResponse(data)
	} else {
		err = envOTELJSONResponse(data)
	}
	if err != nil {
		return errors.New("OTLP endpoint test returned malformed or rejected partial success; no settings saved; response withheld")
	}
	return nil
}

func envOTELJSONResponse(data []byte) error {
	var response map[string]json.RawMessage
	if json.Unmarshal(data, &response) != nil || response == nil {
		return errors.New("invalid OTLP response")
	}
	partial, err := envOTELJSONAlias(response, "partialSuccess", "partial_success")
	if err != nil {
		return err
	}
	if partial == nil || string(partial) == "null" {
		return nil
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(partial, &fields) != nil || fields == nil {
		return errors.New("invalid partial success")
	}
	count, err := envOTELJSONAlias(fields, "rejectedSpans", "rejected_spans")
	if err != nil {
		return err
	}
	if count != nil && string(count) != "null" {
		var value string
		if json.Unmarshal(count, &value) != nil {
			value = string(count)
		}
		n, err := strconv.ParseInt(value, 10, 64)
		if err != nil || n != 0 {
			return errors.New("rejected spans")
		}
	}
	message, err := envOTELJSONAlias(fields, "errorMessage", "error_message")
	if err != nil {
		return err
	}
	if message != nil && string(message) != "null" {
		var value string
		if json.Unmarshal(message, &value) != nil {
			return errors.New("invalid collector message")
		}
	}
	return nil
}

func envOTELJSONAlias(fields map[string]json.RawMessage, camel, snake string) (json.RawMessage, error) {
	first, second := fields[camel], fields[snake]
	if first != nil && second != nil && !bytes.Equal(bytes.TrimSpace(first), bytes.TrimSpace(second)) {
		return nil, errors.New("conflicting OTLP response aliases")
	}
	if first != nil {
		return first, nil
	}
	return second, nil
}

func envOTELProtoResponse(data []byte) error {
	return envOTELProtoFields(data, false)
}

func envOTELProtoFields(data []byte, partial bool) error {
	for len(data) > 0 {
		field, wire, n := protowire.ConsumeTag(data)
		if n < 0 {
			return protowire.ParseError(n)
		}
		data = data[n:]
		if !field.IsValid() || partial && (field == 1 && wire != protowire.VarintType || field == 2 && wire != protowire.BytesType) || !partial && field == 1 && wire != protowire.BytesType {
			return errors.New("invalid protobuf field")
		}
		if wire == protowire.VarintType && partial && field == 1 {
			value, n := protowire.ConsumeVarint(data)
			if n < 0 || value != 0 {
				return errors.New("invalid or rejected spans")
			}
			data = data[n:]
			continue
		}
		if wire == protowire.BytesType {
			value, n := protowire.ConsumeBytes(data)
			if n < 0 {
				return protowire.ParseError(n)
			}
			data = data[n:]
			if !partial && field == 1 {
				if err := envOTELProtoFields(value, true); err != nil {
					return err
				}
			}
			if partial && field == 2 && !utf8.Valid(value) {
				return errors.New("invalid collector message")
			}
			continue
		}
		n = protowire.ConsumeFieldValue(field, wire, data)
		if n < 0 {
			return protowire.ParseError(n)
		}
		data = data[n:]
	}
	return nil
}
