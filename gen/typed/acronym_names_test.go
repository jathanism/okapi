package typed_test

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/jathanism/okapi/gen/typed"
)

// A component schema's declared name is the Go type name verbatim when it
// is already an exported identifier. Deriving references through pascal()
// would lowercase acronyms ("IDRange" -> "Idrange") and leave them
// pointing at a type that is never declared.
var _ = Describe("Component schema names with acronyms", func() {
	const acronymSpec = `
openapi: 3.1.0
info: {title: t, version: 0.0.1}
paths:
  /ranges:
    get:
      operationId: accountContactIDRange
      responses:
        '200':
          description: ok
          content:
            application/json:
              schema: {$ref: '#/components/schemas/AccountContactIDRangeResponseBody'}
components:
  schemas:
    AccountContactIDRangeResponseBody:
      type: object
      required: [min_id]
      properties:
        min_id: {type: integer, format: int64}
    my-hyphen-schema:
      type: object
      properties:
        n: {type: integer, format: int64}
`

	generate := func() typed.Files {
		files, err := typed.Generate(typed.Options{
			PackageName: "demo",
			ClientName:  "Client",
			SpecBytes:   []byte(acronymSpec),
		})
		Expect(err).ToNot(HaveOccurred())
		return files
	}

	It("declares the type under the schema's own name", func() {
		Expect(string(generate()["types.gen.go"])).
			To(ContainSubstring("type AccountContactIDRangeResponseBody struct"))
	})

	It("references the declared type from the client method", func() {
		client := string(generate()["client.gen.go"])
		Expect(client).To(ContainSubstring("*AccountContactIDRangeResponseBody"))
		Expect(client).NotTo(ContainSubstring("AccountContactIdrangeResponseBody"))
	})

	It("still normalizes names that are not exported Go identifiers", func() {
		Expect(string(generate()["types.gen.go"])).
			To(ContainSubstring("type MyHyphenSchema struct"))
	})
})
