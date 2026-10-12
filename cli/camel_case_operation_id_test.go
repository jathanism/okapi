package cli_test

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/jathanism/okapi"
	"github.com/jathanism/okapi/cli"
)

var _ = Describe("OpenApiCli with camelCase operationIds", func() {
	const spec = `openapi: 3.0.3
info: {title: t, version: "1"}
paths:
  /thing:
    get:
      operationId: getThing
      responses: {"200": {description: ok}}
`

	It("builds a command for an operationId with no underscore instead of panicking", func() {
		api, err := (*openapi.OpenApi)(nil).NewFromBytes([]byte(spec))
		Expect(err).ToNot(HaveOccurred())

		var openApiCli *cli.OpenApiCli
		Expect(func() {
			openApiCli = (&cli.OpenApiCli{Api: api, AppName: "okapi"}).New()
		}).ToNot(Panic())

		names := make([]string, 0)
		for _, c := range openApiCli.Commands() {
			names = append(names, c.Name)
		}
		Expect(names).To(ContainElement("getThing"))
	})
})
