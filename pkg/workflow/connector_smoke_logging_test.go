//go:build !integration

package workflow

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"testing"
	"testing/iotest"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"
)

const connectorSmokeCaptureLimit = 65536

type connectorSmokeDiagnostics struct {
	log      func(string)
	values   []string
	redactor *strings.Replacer
	verbose  bool
}

func newConnectorSmokeDiagnostics(log func(string), secrets []string, verbose bool) *connectorSmokeDiagnostics {
	unique := map[string]bool{}
	for _, secret := range secrets {
		if secret == "" {
			continue
		}
		for _, value := range append([]string{secret, url.QueryEscape(secret), url.PathEscape(secret)}, strings.FieldsFunc(secret, func(r rune) bool { return r == '\r' || r == '\n' })...) {
			if value != "" {
				unique[value] = true
			}
		}
	}
	var values []string
	for value := range unique {
		values = append(values, value)
	}
	slices.SortFunc(values, func(a, b string) int { return len(b) - len(a) })
	replacements := make([]string, 0, len(values)*2)
	for _, value := range values {
		replacements = append(replacements, value, "***")
	}
	return &connectorSmokeDiagnostics{log: log, values: values, redactor: strings.NewReplacer(replacements...), verbose: verbose}
}

func (d *connectorSmokeDiagnostics) addMasks(writer io.Writer) error {
	for _, value := range d.values {
		escaped := strings.NewReplacer("%", "%25", "\r", "%0D", "\n", "%0A").Replace(value)
		if _, err := fmt.Fprintf(writer, "::add-mask::%s\n", escaped); err != nil {
			return err
		}
	}
	return nil
}

func (d *connectorSmokeDiagnostics) emit(message string) {
	message = d.redactor.Replace(message)
	if len(message) > 4096 {
		message = message[:4096] + " [truncated]"
	}
	// Quoting blocks control characters; neutralize commands even inside a line.
	message = strings.ReplaceAll(message, "::", `\u003a\u003a`)
	d.log(strconv.Quote(message))
}

func (d *connectorSmokeDiagnostics) transport(base http.RoundTripper) http.RoundTripper {
	return connectorSmokeTransport(func(request *http.Request) (*http.Response, error) {
		start := time.Now()
		target := request.URL.Scheme + "://" + request.URL.Host + request.URL.EscapedPath()
		d.emit(fmt.Sprintf("HTTP request method=%s endpoint=%s%s", request.Method, target, connectorSmokeRPCLabel(request)))
		response, err := base.RoundTrip(request)
		if err != nil {
			d.emit("HTTP transport failed: " + err.Error())
			return nil, err
		}
		contentType, _, parseErr := mime.ParseMediaType(response.Header.Get("Content-Type"))
		if parseErr != nil {
			contentType = "absent-or-invalid"
		}
		d.emit(fmt.Sprintf("HTTP response status=%d content-type=%s elapsed=%s", response.StatusCode, contentType, time.Since(start).Round(time.Millisecond)))
		if d.verbose && response.Body != nil {
			response.Body = &connectorSmokeDiagnosticBody{
				ReadCloser: response.Body, diagnostics: d, status: response.StatusCode,
				eventStream: contentType == "text/event-stream",
			}
		}
		return response, nil
	})
}

func connectorSmokeRPCLabel(request *http.Request) string {
	if request.GetBody == nil {
		return ""
	}
	body, err := request.GetBody()
	if err != nil {
		return " request-metadata=unavailable"
	}
	var rpc struct {
		Method string
		Params struct{ Name string }
	}
	err = json.NewDecoder(io.LimitReader(body, 8192)).Decode(&rpc)
	closeErr := body.Close()
	if err != nil || closeErr != nil {
		return " request-metadata=unavailable"
	}
	return fmt.Sprintf(" rpc=%s tool=%s", rpc.Method, rpc.Params.Name)
}

type connectorSmokeDiagnosticBody struct {
	io.ReadCloser
	diagnostics *connectorSmokeDiagnostics
	status      int
	eventStream bool
	buffer      []byte
	captured    int
	done        bool
}

func (body *connectorSmokeDiagnosticBody) Read(output []byte) (int, error) {
	count, err := body.ReadCloser.Read(output)
	if body.done {
		return count, err
	}
	if count > connectorSmokeCaptureLimit-body.captured {
		body.done = true
		body.buffer = nil
		body.diagnostics.emit("Response diagnostic capture limit exceeded; remaining content withheld")
		return count, err
	}
	body.captured += count
	body.buffer = append(body.buffer, output[:count]...)
	if body.eventStream {
		body.buffer = bytes.ReplaceAll(body.buffer, []byte("\r\n"), []byte("\n"))
		for {
			at := bytes.Index(body.buffer, []byte("\n\n"))
			if at < 0 {
				break
			}
			var data []string
			for line := range strings.SplitSeq(string(body.buffer[:at]), "\n") {
				if value, ok := strings.CutPrefix(line, "data:"); ok {
					data = append(data, strings.TrimPrefix(value, " "))
				}
			}
			body.diagnostics.packet(body.status, []byte(strings.Join(data, "\n")))
			body.buffer = body.buffer[at+2:]
		}
	} else if json.Valid(body.buffer) || err != nil {
		body.diagnostics.packet(body.status, body.buffer)
		body.done = true
		body.buffer = nil
	}
	return count, err
}

func (d *connectorSmokeDiagnostics) packet(status int, data []byte) {
	if len(data) == 0 {
		return
	}
	var packet struct {
		Error            json.RawMessage
		Result           json.RawMessage
		Errors           json.RawMessage
		ErrorMessages    []string `json:"errorMessages"`
		Message          string
		ErrorDescription string `json:"error_description"`
	}
	if err := json.Unmarshal(data, &packet); err != nil {
		if status >= 400 {
			d.emit("Unrecognized error response body withheld")
		}
		return
	}
	if len(packet.Error) > 0 && string(packet.Error) != "null" {
		var rpcError struct {
			Code    int
			Message string
		}
		if err := json.Unmarshal(packet.Error, &rpcError); err == nil {
			d.emit(fmt.Sprintf("JSON-RPC error code=%d message=%s", rpcError.Code, rpcError.Message))
		} else {
			var message string
			if err := json.Unmarshal(packet.Error, &message); err == nil {
				d.emit("Remote error: " + message)
			}
		}
	}
	if status >= 400 {
		d.emit("HTTP error message=" + packet.Message + " description=" + packet.ErrorDescription)
		for _, message := range packet.ErrorMessages[:min(len(packet.ErrorMessages), 4)] {
			d.emit("HTTP error: " + message)
		}
	}
	var graphqlErrors []struct{ Message string }
	if len(packet.Errors) > 0 {
		if err := json.Unmarshal(packet.Errors, &graphqlErrors); err != nil {
			d.emit("Non-GraphQL error details withheld")
		}
	}
	for _, entry := range graphqlErrors[:min(len(graphqlErrors), 4)] {
		d.emit("GraphQL error: " + entry.Message)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(packet.Result, &fields); err != nil {
		return
	}
	if fields["content"] == nil && fields["isError"] == nil && fields["inputRequests"] == nil {
		return
	}
	var result mcp.CallToolResult
	if err := json.Unmarshal(packet.Result, &result); err != nil {
		d.emit("MCP result diagnostics could not be decoded; content withheld")
		return
	}
	d.emit(fmt.Sprintf("MCP result is_error=%t needs_input=%t content_items=%d", result.IsError, result.NeedsInput(), len(result.Content)))
	if result.IsError {
		for _, content := range result.Content[:min(len(result.Content), 4)] {
			if text, ok := content.(*mcp.TextContent); ok {
				d.emit("MCP tool error: " + text.Text)
			}
		}
	}
}

func TestConnectorSmokeDiagnosticsMaskAndEscape(t *testing.T) {
	const secret = "private%\r\n::error::injected"
	var commands bytes.Buffer
	var lines []string
	diagnostics := newConnectorSmokeDiagnostics(func(line string) { lines = append(lines, line) }, []string{secret, "derived-basic-value"}, true)
	require.NoError(t, diagnostics.addMasks(&commands))
	require.Contains(t, commands.String(), "::add-mask::private%25%0D%0A::error::injected\n")
	for command := range strings.SplitSeq(strings.TrimSpace(commands.String()), "\n") {
		require.True(t, strings.HasPrefix(command, "::add-mask::"))
		require.NotContains(t, command, "\r")
	}
	diagnostics.emit(secret + " derived-basic-value ::warning::untrusted\nnext-line")
	require.Len(t, lines, 1)
	require.NotContains(t, lines[0], "private")
	require.NotContains(t, lines[0], "derived-basic-value")
	require.NotContains(t, lines[0], "::")
	require.NotContains(t, lines[0], "\n")
}

func TestConnectorSmokeDiagnosticsPreserveResponses(t *testing.T) {
	for _, eventStream := range []bool{false, true} {
		t.Run(strconv.FormatBool(eventStream), func(t *testing.T) {
			var lines []string
			diagnostics := newConnectorSmokeDiagnostics(func(line string) { lines = append(lines, line) }, []string{"private-token", "derived-basic-value"}, true)
			payload := `{"jsonrpc":"2.0","id":2,"result":{"isError":true,"content":[{"type":"text","text":"Permission denied: private-token derived-basic-value ::error::not-a-command\nnew line"}]}}`
			contentType := "application/json"
			if eventStream {
				contentType = "Text/Event-Stream; charset=utf-8"
				payload = "data: {\"jsonrpc\":\"2.0\",\"id\":1,\"result\":{\"content\":[{\"type\":\"text\",\"text\":\"private-success-body\"}]}}\r\n\r\n" +
					"event: message\r\ndata: " + payload + "\r\n\r\n"
			}
			request, err := http.NewRequest(http.MethodPost, "https://example.test/mcp?token=private-query", strings.NewReader(`{"method":"tools/call","params":{"name":"getJiraIssue","arguments":{"issue":"private-issue"}}}`))
			require.NoError(t, err)
			request.Header.Set("Authorization", "Basic derived-basic-value")
			transport := diagnostics.transport(connectorSmokeTransport(func(received *http.Request) (*http.Response, error) {
				data, err := io.ReadAll(received.Body)
				require.NoError(t, err)
				require.Contains(t, string(data), "private-issue", "logging must not consume the request")
				return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {contentType}}, Body: io.NopCloser(iotest.OneByteReader(strings.NewReader(payload)))}, nil
			}))
			response, err := transport.RoundTrip(request)
			require.NoError(t, err)
			data, err := io.ReadAll(response.Body)
			require.NoError(t, err)
			require.NoError(t, response.Body.Close())
			require.Equal(t, payload, string(data))
			logs := strings.Join(lines, "\n")
			require.Contains(t, logs, "rpc=tools/call tool=getJiraIssue")
			require.Contains(t, logs, "is_error=true needs_input=false")
			require.Contains(t, logs, "Permission denied")
			for _, private := range []string{"private-token", "derived-basic-value", "private-query", "private-issue", "private-success-body", "::"} {
				require.NotContains(t, logs, private)
			}
		})
	}
}

func TestConnectorSmokeDiagnosticsWithholdSuccessAndOversizedData(t *testing.T) {
	for _, payload := range []string{
		`{"jsonrpc":"2.0","result":{"content":[{"type":"text","text":"private issue body"}]}}`,
		strings.Repeat("x", connectorSmokeCaptureLimit+1),
	} {
		var lines []string
		diagnostics := newConnectorSmokeDiagnostics(func(line string) { lines = append(lines, line) }, nil, true)
		body := connectorSmokeDiagnosticBody{ReadCloser: io.NopCloser(strings.NewReader(payload)), diagnostics: diagnostics}
		data, err := io.ReadAll(&body)
		require.NoError(t, err)
		require.Equal(t, payload, string(data))
		require.NoError(t, body.Close())
		require.NotContains(t, strings.Join(lines, "\n"), "private issue body")
		require.Less(t, len(strings.Join(lines, "\n")), 1024)
	}
}

func TestConnectorSmokeDiagnosticsMaskFailure(t *testing.T) {
	reader, writer := io.Pipe()
	require.NoError(t, reader.Close())
	diagnostics := newConnectorSmokeDiagnostics(func(string) {}, []string{"fixture-secret"}, true)
	require.ErrorIs(t, diagnostics.addMasks(writer), io.ErrClosedPipe)
	require.NoError(t, writer.Close())
}

func TestConnectorSmokeDiagnosticsHTTPAndProtocolErrors(t *testing.T) {
	var lines []string
	diagnostics := newConnectorSmokeDiagnostics(func(line string) { lines = append(lines, line) }, []string{"credential"}, true)
	diagnostics.packet(http.StatusBadRequest, []byte(`{"errorMessages":["Missing scope for credential"],"errors":{"field":"private-value"}}`))
	diagnostics.packet(http.StatusOK, []byte(`{"jsonrpc":"2.0","error":{"code":-32000,"message":"Denied credential","data":{"private":"withheld"}}}`))
	diagnostics.packet(http.StatusOK, []byte(`{"errors":[{"message":"GraphQL denied credential"}]}`))
	logs := strings.Join(lines, "\n")
	require.Contains(t, logs, "Missing scope")
	require.Contains(t, logs, "JSON-RPC error code=-32000")
	require.Contains(t, logs, "GraphQL denied")
	require.NotContains(t, logs, "credential")
	require.NotContains(t, logs, "private-value")
	require.NotContains(t, logs, `"private"`)
}
