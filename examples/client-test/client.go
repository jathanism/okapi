package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// httpClient is a minimal reference implementation of okapi's
// request.OpenApiClient interface. It does three jobs:
//
//   - prepend a base URL to the path okapi hands it,
//   - set Accept / Content-Type and forward the per-call headers, and
//   - decode JSON responses into the caller-supplied result.
//
// Anything more (retries, auth, observability) belongs in a real client. This
// is the smallest thing that makes the example end-to-end runnable.
type httpClient struct {
	BaseURL string
	HTTP    *http.Client
}

func (c *httpClient) RequestJSON(
	method, uri string,
	body io.Reader,
	result any,
	headers map[string][]string,
) (*http.Response, error) {
	req, err := http.NewRequest(method, c.BaseURL+uri, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	for k, vs := range headers {
		for _, v := range vs {
			req.Header.Add(k, v)
		}
	}

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return resp, err
	}

	if resp.StatusCode >= 400 {
		return resp, fmt.Errorf("%s %s -> %d: %s", method, uri, resp.StatusCode, string(raw))
	}
	if result == nil || len(raw) == 0 {
		return resp, nil
	}

	ct := resp.Header.Get("Content-Type")
	if !strings.Contains(ct, "application/json") && !strings.Contains(ct, "+json") {
		// Non-JSON success body: surface raw text rather than fail to decode.
		if p, ok := result.(*any); ok {
			*p = map[string]any{"_content_type": ct, "_raw": string(raw)}
		}
		return resp, nil
	}
	if err := json.Unmarshal(raw, result); err != nil {
		return resp, fmt.Errorf("decode %s %s: %w (body: %s)", method, uri, err, string(raw))
	}
	return resp, nil
}
