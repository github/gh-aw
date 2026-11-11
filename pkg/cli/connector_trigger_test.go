//go:build !integration

package cli

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestConnectorDevelopmentOfflineTrigger(t *testing.T) {
	source := strings.Replace(developmentConnectorFixture, "    tools:\n", `    triggers:
      issue-closed:
        verificationProfile: example.test/hmac
        nativeEvents: [update]
        resourceOrigins: [https://api.example.test]
        match: {/event: update}
        normalize: {subject: /id, url: /url}
        resources: {/id: issues}
        when:
          path: /state/new
          other: /state/old
          op: enters
          values: [Done]
    tools:
`, 1)
	declaration := loadDevelopmentConnector(t, source, map[string][]string{"issues": {"APP-1"}})
	options := developmentOptions("trigger", "issue-closed", `{"event":"update","id":"APP-1","url":"https://api.example.test/issues/APP-1","state":{"old":"Active","new":"Done"}}`)
	options.nativeEvent = "update"
	var output bytes.Buffer
	getenv := func(string) string {
		t.Fatal("offline trigger projection accessed a credential")
		return ""
	}
	require.NoError(t, testDevelopmentConnector(context.Background(), &output, declaration, options, getenv, nil))
	require.Contains(t, output.String(), `"operational":false`)
	require.Contains(t, output.String(), `"subject":"APP-1"`)
	require.NotContains(t, output.String(), `"state"`)
	for _, args := range []string{
		`{"event":"update","id":"APP-1","url":"https://api.example.test/issues/APP-1","state":{"new":"Done"}}`,
		`{"event":"update","id":"OTHER","url":"https://api.example.test/issues/OTHER","state":{"old":"Active","new":"Done"}}`,
	} {
		options.arguments = args
		require.Error(t, testDevelopmentConnector(context.Background(), &output, declaration, options, getenv, nil))
	}
	options.nativeEvent = ""
	require.ErrorContains(t, testDevelopmentConnector(context.Background(), &output, declaration, options, getenv, nil), "--native-event")
}
