package connectors

import (
	"fmt"
	"net/url"
	"path"
	"strings"
)

func (s *ConnectorSpec) validateInference() error {
	for name, profile := range s.Inference {
		if !namePattern.MatchString(name) || !namePattern.MatchString(profile.Models) {
			return fmt.Errorf("inference %q requires a named profile and model resource collection", name)
		}
		if _, ok := s.Endpoints[profile.Endpoint]; !ok {
			return fmt.Errorf("inference %q references an unknown endpoint", name)
		}
		if profile.Protocol != "openai-responses" && profile.Protocol != "openai-chat-completions" {
			return fmt.Errorf("inference %q protocol must be openai-responses or openai-chat-completions", name)
		}
		_, pathErr := url.ParseRequestURI(profile.Path)
		if pathErr != nil || len(profile.Path) > 2048 || !strings.HasPrefix(profile.Path, "/") ||
			strings.HasPrefix(profile.Path, "//") || path.Clean(profile.Path) != profile.Path ||
			strings.ContainsAny(profile.Path, "?#%\\${} \t\r\n") {
			return fmt.Errorf("inference %q path must be a fixed absolute SDK base path without traversal, escaping, expressions, query or fragment", name)
		}
		credential, ok := s.Credentials[profile.Credential]
		if !ok || credential.Phase != "inference" || credential.Header != "Authorization" || credential.Scheme != "bearer" {
			return fmt.Errorf("inference %q requires its own inference-phase Authorization/bearer credential", name)
		}
	}
	return nil
}
