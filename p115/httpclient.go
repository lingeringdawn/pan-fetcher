package p115

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"sync"
	"time"
)

// apiClient adapts http.Client for the 115 SDK.
//
// Two problems with the stock client show up only when 115 misbehaves:
//
//  1. When 115's gateway fails it answers with an HTML error page. The SDK tries
//     to decrypt that body, fails the checksum, and reports "invalid ec data" —
//     pointing at decryption instead of the outage that actually happened. The
//     status code is checked here so the real cause reaches the caller.
//
//  2. On timeout the SDK retries the same *http.Request. Its body is built from a
//     one-shot reader with GetBody unset, so the retry sends nothing and fails
//     with "ContentLength=N with Body length 0", masking the timeout. The body is
//     cached and restored before each attempt so a retry is a real retry.
type apiClient struct {
	inner *http.Client

	mu    sync.Mutex
	bodys map[*http.Request][]byte
}

// maxCachedBodys bounds the replay cache so abandoned requests cannot grow it
// without limit.
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

	switch resp.StatusCode {
	case http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout:
		io.Copy(io.Discard, io.LimitReader(resp.Body, 512))
		resp.Body.Close()
		return nil, &UpstreamError{Status: resp.StatusCode}
	}
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

// UpstreamError reports a 115-side outage rather than a problem with the request.
type UpstreamError struct {
	Status int
}

func (e *UpstreamError) Error() string {
	return fmt.Sprintf("115 服务暂时不可用（HTTP %d）", e.Status)
}

// isOutage reports whether err says 115 itself is down.
func isOutage(err error) bool {
	var up *UpstreamError
	return errors.As(err, &up)
}

// newTransportClient mirrors the SDK's default transport settings, so that
// swapping in apiClient does not quietly change how connections behave.
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
