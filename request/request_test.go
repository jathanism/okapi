package request_test

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/jathanism/okapi/internal/testutil"
	. "github.com/jathanism/okapi/request"
)

func TestRequest(t *testing.T) {
	RegisterFailHandler(Fail)

	testutil.Debug()

	RunSpecs(t, "request")
}

// mockClient is a minimal mock implementing request.OpenApiClient
type mockClient struct {
	requestJSON func(method string, uri string, body io.Reader, result any, headers map[string][]string) (*http.Response, error)
}

func (m *mockClient) RequestJSON(method string, uri string, body io.Reader, result any, headers map[string][]string) (*http.Response, error) {
	return m.requestJSON(method, uri, body, result, headers)
}

var _ = Describe("Request", func() {
	It("helps build a standalone request", func() {
		var i any
		r := RequestOptions(
			Param("param", "value"),
			Data("key", "value"),
			Result(&i),
		)
		Expect(r).ToNot(BeNil())
		req := NewRequest(r)
		Expect(req).ToNot(BeNil())
		Expect(req.Params).To(HaveKeyWithValue("param", []any{"value"}))
		Expect(req.Body).To(HaveKeyWithValue("key", "value"))
		Expect(req.Result).To(BeIdenticalTo(&i))
	})

	It("can serialize the request body into JSON", func() {
		r := NewRequest(Body(struct {
			Foo string `json:"foo"`
			Bar string `json:"bar"`
		}{
			Foo: "Fnord",
			Bar: "Banana",
		}))
		Expect(r.Json()).ToNot(BeNil())
		Expect(r.Json()).To(Equal(bytes.NewReader([]byte(`{"foo":"Fnord","bar":"Banana"}`))))
	})

	It("can bind additional options to an existing request", func() {
		r := NewRequest()
		Expect(r).ToNot(BeNil())
		Expect(r.Params).To(BeEmpty())
		r.With(Param("param", "value"))
		Expect(r.Params).To(HaveKeyWithValue("param", []any{"value"}))
		r.With(Param("param", "value2"))
		Expect(r.Params).To(HaveKeyWithValue("param", []any{"value", "value2"}))
	})

	It("can bind an api client", func() {
		client := &mockClient{}
		r := NewRequest()
		Expect(r).ToNot(BeNil())
		Expect(r.ApiClient).To(BeNil())
		r.With(WithClient(client))
		Expect(r.ApiClient).ToNot(BeNil())
	})

	It("can bind a RequestJSON method directly", func() {
		r := NewRequest(
			RequestJSON(
				func(m, u string, b io.Reader, r any, h map[string][]string) (*http.Response, error) {
					return nil, nil
				}),
		)
		Expect(r.ApiClient).ToNot(BeNil())
	})

	It("can remove a bound RequestJSON method", func() {
		r := NewRequest(
			RequestJSON(
				func(m, u string, b io.Reader, r any, h map[string][]string) (*http.Response, error) {
					return nil, nil
				}),
		)
		Expect(r.ApiClient).ToNot(BeNil())
		r.With(RequestJSON(nil))
		Expect(r.ApiClient).To(BeNil())
	})

	It("can bind multiple params in a single map", func() {
		r := NewRequest(
			Params(map[string][]any{
				"param1": {"value1"},
				"param2": {"value2"},
				"param3": {"value1", "value2"},
			}),
		)
		Expect(r.Params).To(HaveKeyWithValue("param1", []any{"value1"}))
		Expect(r.Params).To(HaveKeyWithValue("param2", []any{"value2"}))
		Expect(r.Params).To(HaveKeyWithValue("param3", []any{"value1", "value2"}))
	})

	var _ = Describe("NewRequest", func() {
		It("returns a standalone request", func() {
			r := NewRequest()
			Expect(r).ToNot(BeNil())
		})

		It("takes all sorts of options", func() {
			r := NewRequest(
				Param("param", "value"),
				Param("param", "value2"),
				Data("key", "value"),
				Result(nil),
			)
			Expect(r).ToNot(BeNil())
			Expect(r.Params).To(HaveKeyWithValue("param", []any{"value", "value2"}))
			Expect(r.Body).To(HaveKeyWithValue("key", "value"))
		})
	})
})

var _ = Describe("Header", func() {
	It("adds a single header to the request", func() {
		req := NewRequest()
		Header("Content-Type", "application/json")(req)
		Expect(req.Headers).To(HaveKeyWithValue("Content-Type", []string{"application/json"}))
	})

	It("appends multiple values for the same header", func() {
		req := NewRequest()
		Header("Accept", "text/html")(req)
		Header("Accept", "application/json")(req)
		Expect(req.Headers["Accept"]).To(Equal([]string{"text/html", "application/json"}))
	})
})

var _ = Describe("Headers", func() {
	It("adds multiple headers from a map", func() {
		req := NewRequest()
		headers := map[string][]string{
			"Content-Type":  {"application/json"},
			"Authorization": {"Bearer token123"},
		}
		Headers(headers)(req)
		Expect(req.Headers).To(HaveKeyWithValue("Content-Type", []string{"application/json"}))
		Expect(req.Headers).To(HaveKeyWithValue("Authorization", []string{"Bearer token123"}))
	})

	It("appends values to existing headers", func() {
		req := NewRequest()
		Header("Accept", "text/html")(req)
		headers := map[string][]string{
			"Accept":   {"application/json"},
			"X-Custom": {"value1", "value2"},
		}
		Headers(headers)(req)
		Expect(req.Headers["Accept"]).To(Equal([]string{"text/html", "application/json"}))
		Expect(req.Headers["X-Custom"]).To(Equal([]string{"value1", "value2"}))
	})
})

var _ = Describe("Call", func() {
	It("makes an API call with the request parameters", func() {
		called := false
		method := "GET"
		uri := "/test"
		req := NewRequest(
			RequestJSON(func(m string, u string, b io.Reader, r any, h map[string][]string) (*http.Response, error) {
				called = true
				Expect(m).To(Equal(method))
				Expect(u).To(Equal(uri))
				Expect(h).To(HaveKeyWithValue("Content-Type", []string{"application/json"}))
				return &http.Response{StatusCode: 200}, nil
			}),
			Header("Content-Type", "application/json"),
		)

		resp, err := req.Call(method, uri)
		Expect(err).ToNot(HaveOccurred())
		Expect(resp.StatusCode).To(Equal(200))
		Expect(called).To(BeTrue())
	})

	It("returns an error when ApiClient is not set", func() {
		req := NewRequest()
		resp, err := req.Call("GET", "/test")
		Expect(err).To(HaveOccurred())
		Expect(resp).To(BeNil())
	})

	It("passes through errors from ApiClient", func() {
		expectedErr := fmt.Errorf("api error")
		req := NewRequest(
			RequestJSON(func(m string, u string, b io.Reader, r any, h map[string][]string) (*http.Response, error) {
				return nil, expectedErr
			}),
		)

		resp, err := req.Call("GET", "/test")
		Expect(err).To(Equal(expectedErr))
		Expect(resp).To(BeNil())
	})
})
