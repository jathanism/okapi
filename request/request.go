package request

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

type ApiClientCallable func(method string, uri string, body io.Reader, result any, headers map[string][]string) (*http.Response, error)

type OpenApiClient interface {
	RequestJSON(method string, uri string, body io.Reader, result any, headers map[string][]string) (*http.Response, error)
}

// request is a way to build the parameters and body of an OpenAPI request.
type request struct {
	Params    map[string][]any
	Headers   map[string][]string
	Body      any
	Result    any
	ApiClient ApiClientCallable
}

// With binds additional options to an existing request
func (r *request) With(options ...RequestOption) *request {
	RequestOptions(options...)(r)
	return r
}

// RequestOption is a function that modifies a request.
type RequestOption func(*request)

// NewRequest creates a new request, and optionally applies options.
func NewRequest(options ...RequestOption) *request {
	r := &request{
		Params:  make(map[string][]any),
		Headers: make(map[string][]string),
		Body:    nil,
		Result:  nil,
	}
	RequestOptions(options...)(r)
	return r
}

// RequestOptions creates a function that applies a list of options to a
// request.
// This allows an option set to be reused over multiple requests.
func RequestOptions(options ...RequestOption) RequestOption {
	return func(r *request) {
		for _, opt := range options {
			opt(r)
		}
	}
}

// WithClient sets the API client to use for the request.
func WithClient(client OpenApiClient) RequestOption {
	return func(r *request) {
		r.ApiClient = client.RequestJSON
	}
}

// RequestJSON specifies a function that will be used to make the request.
func RequestJSON(fn ApiClientCallable) RequestOption {
	return func(r *request) {
		r.ApiClient = fn
	}
}

// Param adds a parameter to the request.
// This can be used as a path parameter or a query parameter.
// The parameter key name and value type must match what is specified in the
// OpenApiSpec for the endpoint you are calling.
func Param(key string, value any) RequestOption {
	return func(r *request) {
		r.Params[key] = append(r.Params[key], value)
	}
}

// Params adds multiple parameters to the request from a map.
func Params(params map[string][]any) RequestOption {
	return func(r *request) {
		for k, v := range params {
			r.Params[k] = append(r.Params[k], v...)
		}
	}
}

// Data adds data to the request body payload.
// If the body has not been set it will be initialized to a map.
// If the body has been set and is not a map, this will panic.
// If the body is a map, the data will be added to it as a top level key.
func Data(key string, value any) RequestOption {
	return func(r *request) {
		if r.Body == nil {
			r.Body = make(map[string]any)
		}
		if _, ok := r.Body.(map[string]any); !ok {
			panic(fmt.Sprint("Data(", key, ", ", value, ") requires that Body is a map"))
		}
		r.Body.(map[string]any)[key] = value
	}
}

// Body sets the body of the request.
// This can take any JSON serializable value.
func Body(body any) RequestOption {
	return func(r *request) {
		r.Body = body
	}
}

// Result sets a pointer to a struct to use for unmarshalling the response body
// payload.
// This can be any JSON serializable value.
func Result(result any) RequestOption {
	return func(r *request) {
		r.Result = result
	}
}

// Json is a convenience helper to serialize the request body to JSON.
func (r *request) Json() io.Reader {
	null := bytes.NewReader([]byte{})
	if r.Body == nil {
		// If we have no data, return an empty reader to satisfy the interface
		return null
	}

	// Check for already marshalled data
	switch r.Body.(type) {
	case string:
		return strings.NewReader(r.Body.(string))
	case []byte:
		return bytes.NewReader(r.Body.([]byte))
	}

	// Try to marshal whatever we got left, which should work because we should
	// have already validated
	data, err := json.Marshal(r.Body)
	if err != nil {
		// We shouldn't fail to marshal, but data can be anything, so we return
		// an empty reader
		return null
	}

	return bytes.NewReader(data)
}

// Header adds a header to the request.
//
// Parameters:
//   - key: The header key
//   - value: The header value
//
// Example:
//
//	Header("Content-Type", "application/json")
func Header(key string, value string) RequestOption {
	return func(r *request) {
		r.Headers[key] = append(r.Headers[key], value)
	}
}

// Headers adds multiple headers to the request from a map.
//
// Parameters:
//   - headers: A map of header keys to a list of values
//
// Example:
//
//	headers := map[string][]string{
//		"Content-Type": {"application/json"},
//		"Authorization": {"Bearer <token>"},
//	}
func Headers(headers map[string][]string) RequestOption {
	return func(r *request) {
		for k, v := range headers {
			r.Headers[k] = append(r.Headers[k], v...)
		}
	}
}

// Make the actual API call
//
// Parameters:
//   - method: The HTTP method to use
//   - uri: The URI to call
//
// Returns:
//   - The response from the API
//   - An error if the request fails
//
// Example:
//
//	resp, err := req.Call("GET", "/test")
func (r *request) Call(method string, uri string) (*http.Response, error) {
	if r.ApiClient == nil {
		return nil, fmt.Errorf("ApiClient is not set")
	}
	return r.ApiClient(method, uri, r.Json(), r.Result, r.Headers)
}
