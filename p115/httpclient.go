package p115

import (
	"bytes"
	"io"
	"net"
	"net/http"
	"sync"
	"time"
)

// apiClient adapts http.Client for the 115 SDK.
//
// It exists for one reason: on timeout the SDK retries the same *http.Request.
// Its body comes from a one-shot reader with GetBody unset, so the retry sends
// nothing and fails with "ContentLength=N with Body length 0" — which masks the
// timeout that caused it. The body is cached and restored before each attempt.
//
// Deliberately, this client does not turn HTTP error statuses into Go errors. The
// SDK's Get/Post shadow the error where they call send:
//
//	if resp, err := c.send(req); err == nil { body = makeClientBody(resp) }
//	return   // the outer err is still nil
//
// A transport-level failure therefore yields (nil body, nil error), and CallApi
// passes that nil body straight to spec.Parse, which panics inside encoding/json.
// The default client only reaches that path on real network errors; converting a
// 502 into an error made it reachable and crashed the process during startup.
//
// A failing gateway is instead reported by what it returns — an HTML page the
// caller cannot decrypt — and explainP115Error describes that as an unexplained
// response rather than asserting a cause it cannot prove.
type apiClient struct {
	inner *http.Client

	mu    sync.Mutex
	bodys map[*http.Request][]byte
}

// maxCachedBodys bounds the replay cache so abandoned requests cannot grow it.
const maxCachedBodys = 64

func newAPIClient(inner *http.Client) *apiClient {
	return &apiClient{inner: inner, bodys: make(map[*http.Request][]byte)}
}

func (c *apiClient) Do(req *http.Request) (*http.Response, error) {
	c.restoreBody(req)

	resp, err := c.inner.Do(req)
	if err != nil {
		// Keep the cached body: the SDK may retry this very request.
		return nil, err
	}
	c.forget(req)
	return resp, nil
}

func (c *apiClient) restoreBody(req *http.Request) {
	if req.Body == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()

	if data, ok := c.bodys[req]; ok {
		req.Body = io.NopCloser(bytes.NewReader(data))
		return
	}
	if len(c.bodys) >= maxCachedBodys {
		c.bodys = make(map[*http.Request][]byte)
	}
	data, err := io.ReadAll(req.Body)
	if err != nil {
		return
	}
	c.bodys[req] = data
	req.Body = io.NopCloser(bytes.NewReader(data))
}

func (c *apiClient) forget(req *http.Request) {
	c.mu.Lock()
	delete(c.bodys, req)
	c.mu.Unlock()
}

// newTransportClient mirrors the SDK's default transport settings, so swapping in
// apiClient does not quietly change how connections behave.
func newTransportClient() *http.Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.MaxIdleConnsPerHost = 10
	transport.MaxIdleConns = 100
	transport.IdleConnTimeout = 60 * time.Second
	transport.DialContext = (&net.Dialer{
		Timeout:   10 * time.Second,
		KeepAlive: 60 * time.Second,
	}).DialContext
	transport.TLSHandshakeTimeout = 10 * time.Second
	transport.ResponseHeaderTimeout = 30 * time.Second
	return &http.Client{Transport: transport}
}
