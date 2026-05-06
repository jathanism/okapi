package spec_test

import (
	"testing"
	"unsafe"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/jathanism/okapi/internal/testutil"
	. "github.com/jathanism/okapi/request"
	"github.com/jathanism/okapi/spec"
)

const testSource = "file://../testdata/openapi.yaml"

func TestOpenApiSpec(t *testing.T) {
	RegisterFailHandler(Fail)

	testutil.Debug()

	RunSpecs(t, "spec")
}

var _ = Describe("OpenApiSpec", func() {
	Describe("New", func() {
		It("works with a file uri", func() {
			err := (&spec.OpenApiSpec{
				Source: testSource,
			}).New()
			Expect(err).To(BeNil())
		})

		It("errors with a bad source", func() {
			err := (&spec.OpenApiSpec{
				Source: "file://bogus",
			}).New()
			Expect(err).ToNot(BeNil())
			Expect(err).To(MatchError(ContainSubstring("OpenApiError")))
			Expect(err).To(MatchError(ContainSubstring("bogus")))
		})
	})

	Describe("NewFromSource", func() {
		It("works", func() {
			_, err := (*spec.OpenApiSpec)(nil).NewFromSource(testSource)
			Expect(err).To(BeNil())
		})

		It("caches for repeat calls", func() {
			api1, err := (*spec.OpenApiSpec)(nil).NewFromSource(testSource)
			Expect(err).To(BeNil())

			api2, err := (*spec.OpenApiSpec)(nil).NewFromSource(testSource)
			Expect(err).To(BeNil())
			Expect(unsafe.Pointer(api1)).To(Equal(unsafe.Pointer(api2)))
		})
	})

	Context("when parsing", func() {
		api, _ := (*spec.OpenApiSpec)(nil).NewFromSource(testSource)

		It("parses a bunch of params", func() {
			e := api.Endpoints["organizations_users_list"]
			Expect(e.Params).To(HaveKey("organization_id"))
			Expect(e.Params["organization_id"].Type).To(Equal("string"))
			Expect(e.Params["organization_id"].Required).To(BeTrue())
			Expect(e.Params["organization_id"].In).To(Equal("path"))
			Expect(e.Params).To(HaveKey("limit"))
			Expect(e.Params["limit"].Type).To(Equal("integer"))
			Expect(e.Params["limit"].Required).To(BeFalse())
			Expect(e.Params["limit"].In).To(Equal("query"))
		})

		It("properly modifies required param lists for request body", func() {
			e := api.Endpoints["users_create"]
			Expect(e.Body.MapSchema).To(HaveKey("required"))
			Expect(e.Body.MapSchema["required"]).To(ContainElements("email"))
		})
	})

	Context("when validating", func() {
		api, _ := (*spec.OpenApiSpec)(nil).NewFromSource(testSource)

		It("handles params", func() {
			r := NewRequest(
				Param("organization_id", "123"),
				Param("limit", 123),
			)
			endpoint := api.Endpoints["organizations_users_list"]
			err := endpoint.Validate(r.Params, r.Headers, nil)
			Expect(err).To(BeNil())
			url := endpoint.MustMakeUrl(r.Params)
			Expect(url).To(ContainSubstring("/api/organizations/123/users/?"))
			Expect(url).To(ContainSubstring("limit=123"))
		})

		It("handles bytes for string params", func() {
			r := NewRequest(
				Param("organization_id", []byte("123")),
			)
			endpoint := api.Endpoints["organizations_users_list"]
			err := endpoint.Validate(r.Params, r.Headers, nil)
			Expect(err).To(BeNil())
			url := endpoint.MustMakeUrl(r.Params)
			Expect(url).To(Equal("/api/organizations/123/users/"))
		})

		It("handles request body", func() {
			r := NewRequest(
				Param("organization_id", "123"),
				Data("email", "test@example.com"),
				Data("password", "secret"),
			)
			endpoint := api.Endpoints["organizations_users_create"]
			err := endpoint.Validate(r.Params, r.Headers, r.Body)
			Expect(err).To(BeNil())
			url := endpoint.MustMakeUrl(r.Params)
			Expect(url).To(Equal("/api/organizations/123/users/"))
		})

		// Regression: spec-declared header params used to be silently
		// migrated from r.Params into r.Headers by openapi.CallEndpoint,
		// which made the header-vs-param distinction invisible to callers.
		// Validate now requires headers to be supplied via request.Header()
		// and rejects misuse with a pointer-message error.
		Context("header parameter routing", func() {
			It("accepts headers via the headers map", func() {
				r := NewRequest(
					Header("Idempotency-Key", "abc-123"),
					Data("email", "h@example.com"),
				)
				endpoint := api.Endpoints["users_create"]
				err := endpoint.Validate(r.Params, r.Headers, r.Body)
				Expect(err).To(BeNil())
			})

			It("rejects header params passed via Param() with a pointer message", func() {
				r := NewRequest(
					Param("Idempotency-Key", "abc-123"),
					Data("email", "h@example.com"),
				)
				endpoint := api.Endpoints["users_create"]
				err := endpoint.Validate(r.Params, r.Headers, r.Body)
				Expect(err).To(HaveOccurred())
				Expect(err).To(MatchError(ContainSubstring("Idempotency-Key")))
				Expect(err).To(MatchError(ContainSubstring("request.Header")))
			})

			It("rejects non-header params passed via Header() with a pointer message", func() {
				r := NewRequest(
					Param("organization_id", "123"),
					Header("limit", "5"),
				)
				endpoint := api.Endpoints["organizations_users_list"]
				err := endpoint.Validate(r.Params, r.Headers, nil)
				Expect(err).To(HaveOccurred())
				Expect(err).To(MatchError(ContainSubstring("limit")))
				Expect(err).To(MatchError(ContainSubstring("request.Param")))
			})

			It("requires a header param marked required", func() {
				r := NewRequest(
					Data("email", "h@example.com"),
				)
				endpoint := api.Endpoints["users_create"]
				err := endpoint.Validate(r.Params, r.Headers, r.Body)
				Expect(err).To(HaveOccurred())
				Expect(err).To(MatchError(ContainSubstring("Idempotency-Key")))
				Expect(err).To(MatchError(ContainSubstring("required")))
			})

			It("ignores ad-hoc headers not declared in the spec", func() {
				r := NewRequest(
					Header("Idempotency-Key", "abc-123"),
					Header("X-Random-Header", "anything"),
					Data("email", "h@example.com"),
				)
				endpoint := api.Endpoints["users_create"]
				err := endpoint.Validate(r.Params, r.Headers, r.Body)
				Expect(err).To(BeNil())
			})
		})
	})
})

var _ = Describe("Endpoint", func() {
	api, _ := (*spec.OpenApiSpec)(nil).NewFromSource(testSource)

	Describe("MethodName", func() {
		It("handles hyphenated names correctly", func() {
			endpoint := api.Endpoints["accounts_change-password"]
			name := endpoint.MethodName()
			Expect(name).To(Equal("AccountsChangePassword"))
		})

		// Regression: the previous regex only matched lowercase runs, so
		// camelCase / PascalCase operationIds had every uppercase letter
		// that started an internal word silently dropped — e.g.
		// "ackAudience" became "AckUdience" and "bulkDeleteContacts"
		// became "BulkEleteOntacts" in the generated openapi_gen.go.
		DescribeTable("normalizes various input shapes to CamelCase",
			func(input, want string) {
				e := &spec.Endpoint{Name: input}
				Expect(e.MethodName()).To(Equal(want))
			},
			Entry("snake_case", "accounts_verify_email", "AccountsVerifyEmail"),
			Entry("kebab-case", "accounts-verify-email", "AccountsVerifyEmail"),
			Entry("mixed snake and kebab", "accounts_verify-email", "AccountsVerifyEmail"),
			Entry("camelCase (regression)", "ackAudience", "AckAudience"),
			Entry("camelCase three words (regression)", "bulkDeleteContacts", "BulkDeleteContacts"),
			Entry("camelCase three words, second variant (regression)", "segmentCountContacts", "SegmentCountContacts"),
			Entry("camelCase four words (regression)", "upsertContactByPhone", "UpsertContactByPhone"),
			Entry("PascalCase (regression)", "CreateTag", "CreateTag"),
			Entry("single word lowercase", "healthz", "Healthz"),
			Entry("single word digits", "metrics2", "Metrics2"),
			Entry("camelCase with digits", "getV2Resource", "GetV2Resource"),
			Entry("already CamelCase passes through", "AccountsVerifyEmail", "AccountsVerifyEmail"),
		)
	})

	Describe("ParamNames", func() {
		It("gives back the param names", func() {
			endpoint := api.Endpoints["organizations_users_list"]
			paramNames := endpoint.ParamNames()
			Expect(paramNames).To(ConsistOf("organization_id", "limit", "offset"))
		})
	})

	Describe("MustMakeUrl", func() {
		It("encodes query params correctly", func() {
			endpoint := api.Endpoints["schemas_list"]
			url := endpoint.MustMakeUrl(map[string][]any{
				"slug": {"foo"},
			})
			Expect(url).To(Equal("/api/schemas/?slug=foo"))
		})

		It("encodes multiple query params correctly", func() {
			endpoint := api.Endpoints["schemas_list"]
			url := endpoint.MustMakeUrl(map[string][]any{
				"slug": {"foo", "bar"},
			})
			Expect(url).To(Equal("/api/schemas/?slug=foo&slug=bar"))
		})
	})
})
