package openapi_test

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/jathanism/okapi"
	"github.com/jathanism/okapi/internal/testutil"
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
})
