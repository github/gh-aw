package workqueue

import (
	"net/url"
	"regexp"
	"strings"

	"github.com/github/gh-aw/pkg/envutil"
)

var resourceHostPattern = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?(?:\.[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?)*$`)

func expectedResourceHost() (string, error) {
	serverURL := envutil.GetStringFromEnv("GITHUB_SERVER_URL", "", nil)
	if serverURL == "" {
		return "github.com", nil
	}
	server, err := url.Parse(serverURL)
	if err != nil {
		return "", queueError("resource_invalid", "invalid GITHUB_SERVER_URL")
	}
	host := strings.ToLower(server.Hostname())
	authority := serverURL
	if index := strings.Index(authority, "://"); index >= 0 {
		authority = authority[index+3:]
	}
	if index := strings.IndexAny(authority, "/?#"); index >= 0 {
		authority = authority[:index]
	}
	if !strings.EqualFold(server.Scheme, "https") || server.User != nil || server.Port() != "" ||
		server.Path != "" && server.Path != "/" || server.RawQuery != "" || server.ForceQuery ||
		server.Fragment != "" || server.RawFragment != "" || !strings.EqualFold(authority, host) ||
		!resourceHostPattern.MatchString(host) {
		return "", queueError("resource_invalid", "invalid GITHUB_SERVER_URL")
	}
	return host, nil
}
