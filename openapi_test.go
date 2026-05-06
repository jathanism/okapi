package openapi_test

import (
	"io"
	"net/http"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/jathanism/okapi"
	"github.com/jathanism/okapi/internal/testutil"
	"github.com/jathanism/okapi/request"
	"github.com/jathanism/okapi/spec"
)

const testSource = "file://./testdata/openapi.yaml"

func TestOpenApi(t *testing.T) {
	RegisterFailHandler(Fail)

	testutil.Debug()

	RunSpecs(t, "openapi")
}

var _ = Describe("OpenApi", func() {
	It("returns an error for endpoints not in spec", func() {
		err := openapi.NilEndpoint()
		Expect(err).To(HaveOccurred())
		Expect(err).To(MatchError(ContainSubstring("not present")))
	})

	Describe("NewFromSource", func() {
		It("works", func() {
			api, err := (*openapi.OpenApi)(nil).NewFromSource(testSource)
			Expect(err).ToNot(HaveOccurred())
			Expect(api.EndpointOperationNames()).To(ContainElement("accounts_change-password"))
		})
	})

	// Regression: header parameters must be passed via request.Header(...)
	// and reach the bound ApiClient as headers — not as query string params
	// on the URL. We exercise this through CallEndpoint directly because
	// the committed openapi_gen.go is just a placeholder; only the dynamic
	// endpoints from the parsed spec are guaranteed to exist in tests.
	Describe("CallEndpoint header routing", func() {
		var (
			called      bool
			seenURI     string
			seenHeaders map[string][]string
			endpoints   map[string]*spec.Endpoint
		)

		recorder := request.RequestJSON(func(method string, uri string, body io.Reader, result any, headers map[string][]string) (*http.Response, error) {
			called = true
			seenURI = uri
			seenHeaders = headers
			return nil, nil
		})

		BeforeEach(func() {
			called = false
			seenURI = ""
			seenHeaders = nil

			api, err := (*openapi.OpenApi)(nil).NewFromSource(testSource)
			Expect(err).ToNot(HaveOccurred())
			endpoints = api.Endpoints()
		})

		It("sends spec-declared header params on the headers map", func() {
			err := openapi.CallEndpoint(endpoints["users_create"],
				recorder,
				request.Header("Idempotency-Key", "abc-123"),
				request.Header("X-Trace-Id", "trace-xyz"),
				request.Data("email", "h@example.com"),
			)
			Expect(err).ToNot(HaveOccurred())
			Expect(seenHeaders).To(HaveKeyWithValue("Idempotency-Key", []string{"abc-123"}))
			Expect(seenHeaders).To(HaveKeyWithValue("X-Trace-Id", []string{"trace-xyz"}))
			Expect(seenURI).ToNot(ContainSubstring("Idempotency-Key"))
			Expect(seenURI).ToNot(ContainSubstring("X-Trace-Id"))
		})

		It("rejects spec-declared header params passed via Param()", func() {
			err := openapi.CallEndpoint(endpoints["users_create"],
				recorder,
				request.Param("Idempotency-Key", "abc-123"),
				request.Data("email", "h@example.com"),
			)
			Expect(err).To(HaveOccurred())
			Expect(err).To(MatchError(ContainSubstring("Idempotency-Key")))
			Expect(err).To(MatchError(ContainSubstring("request.Header")))
			Expect(called).To(BeFalse(), "client should not have been called")
		})
	})
})
