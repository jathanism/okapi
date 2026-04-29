package error_test

import (
	"errors"
	"fmt"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	. "github.com/jathanism/okapi/error"
	"github.com/jathanism/okapi/internal/testutil"
)

func TestOpenApiError(t *testing.T) {
	RegisterFailHandler(Fail)

	testutil.Debug()

	RunSpecs(t, "error")
}

var _ = Describe("OpenAPIError", func() {
	It("returns OpenAPIError", func() {
		err := Error()
		Expect(err).To(MatchError(OpenApiError, "OpenAPIError"))
	})

	It("identifies as OpenAPIError", func() {
		err := Error("testing")
		Expect(errors.Is(err, OpenApiError)).To(BeTrue())
	})

	It("makes an error work good", func() {
		otherErr := fmt.Errorf("other error")
		err := ErrorFrom(otherErr)
		Expect(errors.Is(err, OpenApiError)).To(BeTrue())
	})

	It("handles multiple errors", func() {
		a := errors.New("first error")
		b := errors.New("second error")
		err := Error(a, b)
		Expect(errors.Is(err, OpenApiError)).To(BeTrue())
		Expect(err).To(MatchError(OpenApiError, "OpenAPIError"))
		Expect(err).To(MatchError(ContainSubstring("first error")))
		Expect(err).To(MatchError(ContainSubstring("second error")))
	})

	It("handles multiple errors with a nice message", func() {
		a := errors.New("first error")
		b := errors.New("second error")
		err := Error("oh gods we're in trouble now", a, b)
		Expect(errors.Is(err, OpenApiError)).To(BeTrue())
		Expect(err).To(MatchError(OpenApiError, "OpenAPIError"))
		Expect(err).To(MatchError(ContainSubstring("oh gods")))
		Expect(err).To(MatchError(ContainSubstring("first error")))
		Expect(err).To(MatchError(ContainSubstring("second error")))
	})

	It("handles multiple errors with mixed in strings", func() {
		a := errors.New("first error")
		b := errors.New("second error")
		err := Error("oh gods we're in trouble now", a, "we messed up", b)
		Expect(errors.Is(err, OpenApiError)).To(BeTrue())
		Expect(err).To(MatchError(OpenApiError, "OpenAPIError"))
		Expect(err).To(MatchError(ContainSubstring("oh gods")))
		Expect(err).To(MatchError(ContainSubstring("first error")))
		Expect(err).To(MatchError(ContainSubstring("messed up")))
		Expect(err).To(MatchError(ContainSubstring("second error")))
	})

	It("lets you make more complicated format strings", func() {
		err := Errorf("there has been %d times this should have worked", 42)
		Expect(errors.Is(err, OpenApiError)).To(BeTrue())
		Expect(err).To(MatchError(OpenApiError, "OpenAPIError"))
		Expect(err).To(MatchError(ContainSubstring("been 42 times")))
	})
})
