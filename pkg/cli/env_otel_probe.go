package cli

import (
	"bytes"
	"context"
	"encoding/binary"
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
	invalidURL := errors.New("invalid OTLP endpoint: use an absolute HTTPS URL without credentials, fragments, whitespace, or control characters (HTTP allowed only on literal loopback)")
	if len(endpoint) > 8192 || strings.ContainsFunc(endpoint, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) }) {
		return nil, invalidURL
	}
	u, err := url.Parse(endpoint)
	if err != nil || u.Hostname() == "" || u.User != nil || u.Fragment != "" || strings.Contains(endpoint, "#") || u.Opaque != "" {
		return nil, invalidURL
	}
	decoded, err := url.PathUnescape(endpoint)
	if err != nil || strings.ContainsFunc(decoded, unicode.IsControl) {
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
	u.Path = strings.TrimRight(u.Path, "/")
	if !strings.HasSuffix(u.Path, "/v1/traces") {
		u.Path += "/v1/traces" //nolint:manualpathconcat // Match the exporter without cleaning collector-specific path segments.
		u.RawPath = ""
	}
	return u, nil
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
			case "apikey", "bearer", "basic", "digest", "dsn", "sentry":
				return nil, headerError
			}
		}
		if strings.Contains(strings.ToLower(u.Hostname()), "sentry") {
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
	if strings.Contains(strings.ToLower(u.Hostname()), "sentry") && headers.Get("Authorization") != "" {
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
	partial := response["partialSuccess"]
	if partial == nil || string(partial) == "null" {
		return nil
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(partial, &fields) != nil || fields == nil {
		return errors.New("invalid partial success")
	}
	count := fields["rejectedSpans"]
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
	if message := fields["errorMessage"]; message != nil && string(message) != "null" {
		var value string
		if json.Unmarshal(message, &value) != nil {
			return errors.New("invalid collector message")
		}
	}
	return nil
}

// Decode the small ExportTraceServiceResponse without adding a protobuf dependency.
func envOTELProtoResponse(data []byte) error {
	return envOTELProtoFields(data, false)
}

func envOTELProtoFields(data []byte, partial bool) error {
	for len(data) > 0 {
		tag, n := binary.Uvarint(data)
		if n <= 0 || tag>>3 == 0 || tag>>3 >= 1<<29 {
			return errors.New("invalid protobuf tag")
		}
		data = data[n:]
		field, wire := tag>>3, tag&7
		if partial && (field == 1 && wire != 0 || field == 2 && wire != 2) || !partial && field == 1 && wire != 2 {
			return errors.New("invalid protobuf field")
		}
		switch wire {
		case 0:
			value, n := binary.Uvarint(data)
			if n <= 0 || partial && field == 1 && value != 0 {
				return errors.New("invalid or rejected spans")
			}
			data = data[n:]
		case 1, 5:
			size := 8
			if wire == 5 {
				size = 4
			}
			if len(data) < size {
				return errors.New("truncated protobuf")
			}
			data = data[size:]
		case 2:
			size, n := binary.Uvarint(data)
			if n <= 0 || size > uint64(len(data)-n) {
				return errors.New("invalid protobuf length")
			}
			value := data[n : n+int(size)]
			data = data[n+int(size):]
			if !partial && field == 1 {
				if err := envOTELProtoFields(value, true); err != nil {
					return err
				}
			}
			if partial && field == 2 && !utf8.Valid(value) {
				return errors.New("invalid collector message")
			}
		default:
			return errors.New("unsupported protobuf wire type")
		}
	}
	return nil
}
