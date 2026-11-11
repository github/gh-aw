package connectors

import (
	"errors"
	"regexp"
	"strings"
)

var metadataHeaderPattern = regexp.MustCompile("^[A-Za-z0-9!#$%&'*+.^_`|~-]+$")

func validateRequestHeaders(request Request) error {
	if len(request.Headers) > 16 {
		return errors.New("request headers exceed the 16-header bound")
	}
	seen := map[string]struct{}{}
	for name, value := range request.Headers {
		key := strings.ToLower(name)
		_, duplicate := seen[key]
		if !metadataHeaderPattern.MatchString(name) || duplicate || len(value) > 1024 || strings.ContainsAny(value, "\r\n") || strings.Contains(value, "${") {
			return errors.New("request headers require unique HTTP field names and bounded literal values without expressions or line breaks")
		}
		seen[key] = struct{}{}
		switch key {
		case "authorization", "proxy-authorization", "host", "cookie", "set-cookie", "connection", "content-length", "transfer-encoding", "te", "trailer", "upgrade", "keep-alive", "private-token", "api-key":
			return errors.New("request metadata headers cannot override authentication, authority, or transport")
		}
		for _, suffix := range []string{"-key", "-token", "-secret"} {
			if strings.HasSuffix(key, suffix) {
				return errors.New("request metadata headers cannot contain credential fields")
			}
		}
		if strings.EqualFold(name, "content-type") {
			expected := "application/json"
			if len(request.Patch)+len(request.AdditionalPatch) != 0 {
				expected = "application/json-patch+json"
			}
			if request.Binary != nil {
				expected = "application/octet-stream"
			}
			if value != expected {
				return errors.New("request Content-Type must match the JSON body encoding")
			}
		}
	}
	return nil
}
