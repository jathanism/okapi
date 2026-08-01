package typed_test

import (
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/jathanism/okapi/gen/typed"
)

// schemaSpec wraps a single component schema in a minimal complete
// OpenAPI document so we can drive the generator with one shape at a
// time and assert on the rendered types.gen.go.
func schemaSpec(name, schema string) []byte {
	return []byte(`
openapi: 3.1.0
info: {title: t, version: 0.0.1}
paths: {}
components:
  schemas:
    ` + name + `:
` + indent("      ", schema) + `
`)
}

func indent(prefix, s string) string {
	out := []string{}
	for _, line := range strings.Split(strings.TrimRight(s, "\n"), "\n") {
		out = append(out, prefix+line)
	}
	return strings.Join(out, "\n")
}

func generateTypes(specBytes []byte) string {
	files, err := typed.Generate(typed.Options{
		PackageName: "demo",
		SpecBytes:   specBytes,
	})
	Expect(err).ToNot(HaveOccurred())
	return string(files["types.gen.go"])
}

var _ = Describe("Schema translation", func() {
	Describe("primitives", func() {
		It("maps integer/format=int64 to int64", func() {
			out := generateTypes(schemaSpec("M", `
type: object
required: [n]
properties:
  n: {type: integer, format: int64}
`))
			Expect(out).To(MatchRegexp(`N\s+int64`))
		})

		It("maps integer/format=int32 to int32", func() {
			out := generateTypes(schemaSpec("M", `
type: object
required: [n]
properties:
  n: {type: integer, format: int32}
`))
			Expect(out).To(MatchRegexp(`N\s+int32`))
		})

		It("maps number to float64 by default", func() {
			out := generateTypes(schemaSpec("M", `
type: object
required: [x]
properties:
  x: {type: number}
`))
			Expect(out).To(MatchRegexp(`X\s+float64`))
		})

		It("maps number/format=float to float32", func() {
			out := generateTypes(schemaSpec("M", `
type: object
required: [x]
properties:
  x: {type: number, format: float}
`))
			Expect(out).To(MatchRegexp(`X\s+float32`))
		})

		It("maps boolean to bool", func() {
			out := generateTypes(schemaSpec("M", `
type: object
required: [b]
properties:
  b: {type: boolean}
`))
			Expect(out).To(MatchRegexp(`B\s+bool`))
		})

		It("maps string to string", func() {
			out := generateTypes(schemaSpec("M", `
type: object
required: [s]
properties:
  s: {type: string}
`))
			Expect(out).To(MatchRegexp(`S\s+string`))
		})
	})

	Describe("required vs optional", func() {
		It("required scalar is a value, no omitempty", func() {
			out := generateTypes(schemaSpec("M", `
type: object
required: [s]
properties:
  s: {type: string}
`))
			Expect(out).To(MatchRegexp(`S\s+string\s+` + "`" + `json:"s"` + "`"))
		})

		It("optional scalar is a pointer with omitempty", func() {
			out := generateTypes(schemaSpec("M", `
type: object
properties:
  s: {type: string}
`))
			Expect(out).To(MatchRegexp(`S\s+\*string\s+` + "`" + `json:"s,omitempty"` + "`"))
		})

		It("required nullable scalar (3.1 union) is a pointer without omitempty", func() {
			out := generateTypes(schemaSpec("M", `
type: object
required: [s]
properties:
  s: {type: [string, "null"]}
`))
			Expect(out).To(MatchRegexp(`S\s+\*string\s+` + "`" + `json:"s"` + "`"))
		})

		It("required array stays as a slice (nil = absent)", func() {
			out := generateTypes(schemaSpec("M", `
type: object
required: [xs]
properties:
  xs: {type: array, items: {type: string}}
`))
			Expect(out).To(MatchRegexp(`Xs\s+\[\]string\s+` + "`" + `json:"xs"` + "`"))
		})

		It("optional array is a pointer to a slice with omitempty", func() {
			out := generateTypes(schemaSpec("M", `
type: object
properties:
  xs: {type: array, items: {type: string}}
`))
			Expect(out).To(MatchRegexp(`Xs\s+\*\[\]string\s+` + "`" + `json:"xs,omitempty"` + "`"))
			Expect(out).ToNot(ContainSubstring("**"))
		})

		It("required nullable array (3.1 union) stays a bare slice (nil marshals as null)", func() {
			out := generateTypes(schemaSpec("M", `
type: object
required: [xs]
properties:
  xs: {type: [array, "null"], items: {type: string}}
`))
			Expect(out).To(MatchRegexp(`Xs\s+\[\]string\s+` + "`" + `json:"xs"` + "`"))
			Expect(out).ToNot(ContainSubstring("*[]string"))
		})

		It("required nullable map (3.1 union) stays a bare map (nil marshals as null)", func() {
			out := generateTypes(schemaSpec("M", `
type: object
required: [tags]
properties:
  tags:
    type: [object, "null"]
    additionalProperties: {type: string}
`))
			Expect(out).To(MatchRegexp(`Tags\s+map\[string\]string\s+` + "`" + `json:"tags"` + "`"))
			Expect(out).ToNot(ContainSubstring("*map["))
		})

		It("required map stays as a map (no pointer)", func() {
			out := generateTypes(schemaSpec("M", `
type: object
required: [tags]
properties:
  tags:
    type: object
    additionalProperties: {type: string}
`))
			Expect(out).To(MatchRegexp(`Tags\s+map\[string\]string`))
			Expect(out).ToNot(ContainSubstring("*map["))
		})

		It("optional map is a pointer to a map with omitempty", func() {
			out := generateTypes(schemaSpec("M", `
type: object
properties:
  tags:
    type: object
    additionalProperties: {type: string}
`))
			Expect(out).To(MatchRegexp(`Tags\s+\*map\[string\]string\s+` + "`" + `json:"tags,omitempty"` + "`"))
		})

		It("optional free-form object is a pointer to map[string]any with omitempty", func() {
			out := generateTypes(schemaSpec("M", `
type: object
properties:
  meta: {type: object}
`))
			Expect(out).To(MatchRegexp(`Meta\s+\*map\[string\]any\s+` + "`" + `json:"meta,omitempty"` + "`"))
		})
	})

	Describe("enums", func() {
		It("synthesizes a named enum from an inline string enum", func() {
			out := generateTypes(schemaSpec("Order", `
type: object
required: [status]
properties:
  status:
    type: string
    enum: [pending, paid, refunded]
`))
			Expect(out).To(ContainSubstring("type OrderStatus string"))
			Expect(out).To(ContainSubstring(`OrderStatusPending  OrderStatus = "pending"`))
			Expect(out).To(ContainSubstring(`OrderStatusPaid     OrderStatus = "paid"`))
			Expect(out).To(ContainSubstring(`OrderStatusRefunded OrderStatus = "refunded"`))
		})

		It("uses the schema name when the enum is the schema itself", func() {
			out := generateTypes([]byte(`
openapi: 3.1.0
info: {title: t, version: 0.0.1}
paths: {}
components:
  schemas:
    Status:
      type: string
      enum: [open, closed]
`))
			Expect(out).To(ContainSubstring("type Status string"))
			Expect(out).To(ContainSubstring(`StatusOpen   Status = "open"`))
			Expect(out).To(ContainSubstring(`StatusClosed Status = "closed"`))
		})

		It("does NOT collapse non-string enums into a named type", func() {
			out := generateTypes(schemaSpec("M", `
type: object
required: [n]
properties:
  n:
    type: integer
    enum: [1, 2, 3]
`))
			// Integer enums fall through to the bare primitive — we don't
			// generate const blocks for them in the MVP.
			Expect(out).To(MatchRegexp(`N\s+int64`))
			Expect(out).ToNot(ContainSubstring("type MN integer"))
		})
	})

	Describe("references and composition", func() {
		It("$ref resolves to the named type", func() {
			out := generateTypes([]byte(`
openapi: 3.1.0
info: {title: t, version: 0.0.1}
paths: {}
components:
  schemas:
    Inner:
      type: object
      required: [name]
      properties:
        name: {type: string}
    Outer:
      type: object
      required: [inner]
      properties:
        inner: {$ref: '#/components/schemas/Inner'}
`))
			Expect(out).To(ContainSubstring("type Inner struct"))
			Expect(out).To(ContainSubstring("type Outer struct"))
			Expect(out).To(MatchRegexp(`Inner\s+Inner\s+`))
		})

		It("optional $ref becomes a pointer to the named type", func() {
			out := generateTypes([]byte(`
openapi: 3.1.0
info: {title: t, version: 0.0.1}
paths: {}
components:
  schemas:
    Inner:
      type: object
      properties: {name: {type: string}}
    Outer:
      type: object
      properties:
        inner: {$ref: '#/components/schemas/Inner'}
`))
			Expect(out).To(MatchRegexp(`Inner\s+\*Inner`))
		})

		It("array of $ref produces []Type", func() {
			out := generateTypes([]byte(`
openapi: 3.1.0
info: {title: t, version: 0.0.1}
paths: {}
components:
  schemas:
    Item:
      type: object
      required: [id]
      properties: {id: {type: integer, format: int64}}
    Bag:
      type: object
      required: [items]
      properties:
        items:
          type: array
          items: {$ref: '#/components/schemas/Item'}
`))
			Expect(out).To(MatchRegexp(`Items\s+\[\]Item`))
		})

		It("single-member allOf is unwrapped to the inner schema", func() {
			out := generateTypes([]byte(`
openapi: 3.1.0
info: {title: t, version: 0.0.1}
paths: {}
components:
  schemas:
    Inner:
      type: object
      required: [x]
      properties: {x: {type: string}}
    Wrapped:
      allOf:
        - {$ref: '#/components/schemas/Inner'}
`))
			Expect(out).To(ContainSubstring("type Wrapped Inner"))
		})

		It("multi-member allOf falls back to any", func() {
			out := generateTypes([]byte(`
openapi: 3.1.0
info: {title: t, version: 0.0.1}
paths: {}
components:
  schemas:
    A: {type: object, properties: {a: {type: string}}}
    B: {type: object, properties: {b: {type: string}}}
    Combo:
      allOf:
        - {$ref: '#/components/schemas/A'}
        - {$ref: '#/components/schemas/B'}
`))
			Expect(out).To(ContainSubstring("type Combo = any"))
		})

		It("oneOf falls back to any (MVP)", func() {
			out := generateTypes([]byte(`
openapi: 3.1.0
info: {title: t, version: 0.0.1}
paths: {}
components:
  schemas:
    A: {type: object, properties: {a: {type: string}}}
    B: {type: object, properties: {b: {type: string}}}
    Either:
      oneOf:
        - {$ref: '#/components/schemas/A'}
        - {$ref: '#/components/schemas/B'}
`))
			Expect(out).To(ContainSubstring("type Either = any"))
		})

		It("anyOf falls back to any (MVP)", func() {
			out := generateTypes([]byte(`
openapi: 3.1.0
info: {title: t, version: 0.0.1}
paths: {}
components:
  schemas:
    A: {type: object, properties: {a: {type: string}}}
    B: {type: object, properties: {b: {type: string}}}
    Many:
      anyOf:
        - {$ref: '#/components/schemas/A'}
        - {$ref: '#/components/schemas/B'}
`))
			Expect(out).To(ContainSubstring("type Many = any"))
		})
	})

	Describe("objects without properties", func() {
		It("empty object becomes map[string]any", func() {
			out := generateTypes(schemaSpec("Bag", `
type: object
`))
			Expect(out).To(ContainSubstring("type Bag map[string]any"))
		})

		It("additionalProperties: <schema> becomes map[string]<T>", func() {
			out := generateTypes(schemaSpec("Bag", `
type: object
additionalProperties: {type: integer, format: int64}
`))
			Expect(out).To(ContainSubstring("type Bag map[string]int64"))
		})
	})

	Describe("arrays", func() {
		It("array of strings becomes []string", func() {
			out := generateTypes(schemaSpec("M", `
type: object
required: [tags]
properties:
  tags:
    type: array
    items: {type: string}
`))
			Expect(out).To(MatchRegexp(`Tags\s+\[\]string`))
		})

		It("array without items becomes []any", func() {
			out := generateTypes(schemaSpec("M", `
type: object
required: [xs]
properties:
  xs: {type: array}
`))
			Expect(out).To(MatchRegexp(`Xs\s+\[\]any`))
		})
	})
})
