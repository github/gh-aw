package httpclientnotimeout

import "net/http"

type Client struct{}

type HTTPClient = http.Client
type HTTPClientAlias = HTTPClient
type UnrelatedClient = Client

func aliasedClients() {
	_ = &HTTPClient{}              // want `net/http\.Client literal has no Timeout field`
	_ = HTTPClient{}               // want `net/http\.Client literal has no Timeout field`
	_ = &HTTPClientAlias{}         // want `net/http\.Client literal has no Timeout field`
	_ = HTTPClient{Transport: nil} // want `net/http\.Client literal has no Timeout field`
	_ = &HTTPClient{Timeout: 0}
	_ = HTTPClient{Timeout: 0}
	_ = &HTTPClient{nil, nil, nil, 0}
	_ = HTTPClient{nil, nil, nil, 0}
	_ = UnrelatedClient{}
}

func missingPointerTimeout() *http.Client {
	return &http.Client{} // want `net/http\.Client literal has no Timeout field`
}

func missingValueTimeout() http.Client {
	return http.Client{} // want `net/http\.Client literal has no Timeout field`
}

func timeoutSet() *http.Client {
	return &http.Client{Timeout: 0}
}

func unrelatedClient() Client {
	return Client{}
}

func suppressed() *http.Client {
	return &http.Client{} //nolint:httpclientnotimeout // exercise suppression
}
