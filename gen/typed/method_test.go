package typed_test

import (
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/jathanism/okapi/gen/typed"
)

func generateAll(specBytes []byte) (typesSrc, clientSrc string) {
	files, err := typed.Generate(typed.Options{
		PackageName: "demo",
		SpecBytes:   specBytes,
	})
	Expect(err).ToNot(HaveOccurred())
	return string(files["types.gen.go"]), string(files["client.gen.go"])
}

var _ = Describe("Operation translation", func() {
	Describe("parameter merging", func() {
		It("inherits path-level path params into each operation", func() {
			_, client := generateAll([]byte(`
openapi: 3.1.0
info: {title: t, version: 0.0.1}
paths:
  /users/{aid}:
    parameters:
      - {name: aid, in: path, required: true, schema: {type: string}}
    get:
      operationId: getUser
      responses:
        '200':
          description: ok
          content:
            application/json: {schema: {type: object}}
    delete:
      operationId: deleteUser
      responses: {'204': {description: ok}}
`))
			// Path param appears in both methods' signatures.
			Expect(client).To(ContainSubstring(
				"func (c *Client) GetUser(ctx context.Context, aid string)"))
			Expect(client).To(ContainSubstring(
				"func (c *Client) DeleteUser(ctx context.Context, aid string)"))
		})

		It("operation-level params override path-level on (name,in) collision", func() {
			_, client := generateAll([]byte(`
openapi: 3.1.0
info: {title: t, version: 0.0.1}
paths:
  /things/{id}:
    parameters:
      - {name: id, in: path, required: true, schema: {type: string}}
    get:
      operationId: getThing
      parameters:
        - {name: id, in: path, required: true, schema: {type: integer, format: int64}}
      responses:
        '200':
          description: ok
          content:
            application/json: {schema: {type: object}}
`))
			// Op-level wins: id is int64, not string.
			Expect(client).To(ContainSubstring(
				"func (c *Client) GetThing(ctx context.Context, id int64)"))
		})

		It("required header param is a value; optional is a pointer", func() {
			_, client := generateAll([]byte(`
openapi: 3.1.0
info: {title: t, version: 0.0.1}
paths:
  /x:
    get:
      operationId: getX
      parameters:
        - {name: If-Match, in: header, required: true, schema: {type: string}}
        - {name: X-Trace, in: header, schema: {type: string}}
      responses: {'204': {description: ok}}
`))
			// Required header → value; optional → pointer. Header args
			// are emitted alphabetically by Go name (IfMatch before XTrace).
			Expect(client).To(ContainSubstring(
				"func (c *Client) GetX(ctx context.Context, ifMatch string, xTrace *string)"))
		})

		It("query params with format=int64 become int64", func() {
			_, client := generateAll([]byte(`
openapi: 3.1.0
info: {title: t, version: 0.0.1}
paths:
  /list:
    get:
      operationId: listX
      parameters:
        - {name: limit, in: query, schema: {type: integer, format: int64}}
      responses: {'204': {description: ok}}
`))
			Expect(client).To(ContainSubstring(
				"func (c *Client) ListX(ctx context.Context, limit *int64)"))
		})
	})

	Describe("operation skipping", func() {
		It("skips operations without operationId", func() {
			_, client := generateAll([]byte(`
openapi: 3.1.0
info: {title: t, version: 0.0.1}
paths:
  /noid:
    get:
      responses: {'204': {description: ok}}
  /ok:
    get:
      operationId: getOk
      responses: {'204': {description: ok}}
`))
			Expect(client).To(ContainSubstring("func (c *Client) GetOk("))
			// No method should be generated for the unnamed op.
			Expect(strings.Count(client, "func (c *Client) ")).To(Equal(5)) // send, do, doStream, httpClient, GetOk
		})
	})

	Describe("response selection", func() {
		It("picks the lowest 2xx with application/json", func() {
			_, client := generateAll([]byte(`
openapi: 3.1.0
info: {title: t, version: 0.0.1}
paths:
  /x:
    post:
      operationId: createX
      responses:
        '202':
          description: accepted
          content: {application/json: {schema: {type: object, properties: {a: {type: string}}}}}
        '201':
          description: created
          content: {application/json: {schema: {$ref: '#/components/schemas/Result'}}}
components:
  schemas:
    Result:
      type: object
      required: [id]
      properties: {id: {type: integer, format: int64}}
`))
			// 201 < 202 lexicographically too; we should land on Result.
			Expect(client).To(ContainSubstring(
				"func (c *Client) CreateX(ctx context.Context) (*Result, *APIResponse, error)"))
		})

		It("returns no result when 2xx has no application/json content", func() {
			_, client := generateAll([]byte(`
openapi: 3.1.0
info: {title: t, version: 0.0.1}
paths:
  /x:
    delete:
      operationId: deleteX
      responses:
        '204': {description: no content}
`))
			Expect(client).To(ContainSubstring("func (c *Client) DeleteX(ctx context.Context) (*APIResponse, error)"))
		})

		It("treats an empty 2xx schema (any) as no result", func() {
			_, client := generateAll([]byte(`
openapi: 3.1.0
info: {title: t, version: 0.0.1}
paths:
  /x:
    get:
      operationId: getX
      responses:
        '200':
          description: ok
          content: {application/json: {schema: {}}}
`))
			Expect(client).To(ContainSubstring("func (c *Client) GetX(ctx context.Context) (*APIResponse, error)"))
		})

		It("streams a non-JSON 2xx response as io.ReadCloser", func() {
			_, client := generateAll([]byte(`
openapi: 3.1.0
info: {title: t, version: 0.0.1}
paths:
  /x/export:
    get:
      operationId: exportX
      responses:
        '200':
          description: csv stream
          content:
            text/csv: {}
`))
			Expect(client).To(ContainSubstring(
				"func (c *Client) ExportX(ctx context.Context) (io.ReadCloser, *APIResponse, error)"))
			// Streamed via doStream, with the declared media type as Accept.
			Expect(client).To(ContainSubstring(
				`c.doStream(ctx, "GET", "/x/export", pathParams, query, headers, nil, "", "text/csv")`))
			// The caller-must-close contract is documented on the method.
			Expect(client).To(ContainSubstring("the caller must close it."))
		})

		It("streams application/octet-stream 2xx responses with a binary schema", func() {
			_, client := generateAll([]byte(`
openapi: 3.1.0
info: {title: t, version: 0.0.1}
paths:
  /x/blob:
    get:
      operationId: getBlob
      responses:
        '200':
          description: raw bytes
          content:
            application/octet-stream: {schema: {type: string, format: binary}}
`))
			Expect(client).To(ContainSubstring(
				"func (c *Client) GetBlob(ctx context.Context) (io.ReadCloser, *APIResponse, error)"))
		})

		It("prefers application/json over non-JSON when a 2xx declares both", func() {
			_, client := generateAll([]byte(`
openapi: 3.1.0
info: {title: t, version: 0.0.1}
paths:
  /x:
    get:
      operationId: getX
      responses:
        '200':
          description: ok
          content:
            application/json: {schema: {$ref: '#/components/schemas/Result'}}
            text/csv: {}
components:
  schemas:
    Result:
      type: object
      required: [id]
      properties: {id: {type: integer, format: int64}}
`))
			Expect(client).To(ContainSubstring(
				"func (c *Client) GetX(ctx context.Context) (*Result, *APIResponse, error)"))
		})
	})

	Describe("response headers", func() {
		It("declared headers become a typed struct returned alongside the body", func() {
			_, client := generateAll([]byte(`
openapi: 3.1.0
info: {title: t, version: 0.0.1}
paths:
  /items/{id}:
    get:
      operationId: getItem
      parameters:
        - {name: id, in: path, required: true, schema: {type: string}}
      responses:
        '200':
          description: ok
          headers:
            ETag:
              description: Entity tag for the returned item
              schema: {type: string}
            Cache-Control: {schema: {type: string}}
          content: {application/json: {schema: {$ref: '#/components/schemas/Item'}}}
components:
  schemas:
    Item:
      type: object
      required: [id]
      properties: {id: {type: string}}
`))
			// Struct fields are alphabetical by Go name, derived like params.
			Expect(client).To(ContainSubstring("type GetItemResponseHeaders struct {"))
			Expect(client).To(MatchRegexp(`CacheControl\s+string`))
			Expect(client).To(MatchRegexp(`Etag\s+string`))
			// The method returns the struct as an additional value.
			Expect(client).To(ContainSubstring(
				"func (c *Client) GetItem(ctx context.Context, id string) (*Item, GetItemResponseHeaders, *APIResponse, error)"))
			// Wire names keep their original case.
			Expect(client).To(ContainSubstring(`outHeaders.Etag = apiResp.Header.Get("ETag")`))
			Expect(client).To(ContainSubstring(`outHeaders.CacheControl = apiResp.Header.Get("Cache-Control")`))
		})

		It("non-string header schemas parse through the param type mapping", func() {
			_, client := generateAll([]byte(`
openapi: 3.1.0
info: {title: t, version: 0.0.1}
paths:
  /items:
    get:
      operationId: listItems
      responses:
        '200':
          description: ok
          headers:
            X-Total-Count: {schema: {type: integer, format: int64}}
          content: {application/json: {schema: {type: array, items: {type: string}}}}
`))
			Expect(client).To(MatchRegexp(`XTotalCount\s+int64`))
			Expect(client).To(ContainSubstring(
				`if v, err := strconv.ParseInt(apiResp.Header.Get("X-Total-Count"), 10, 64); err == nil {`))
		})

		It("headers on a body-less success response still change the signature", func() {
			_, client := generateAll([]byte(`
openapi: 3.1.0
info: {title: t, version: 0.0.1}
paths:
  /x:
    delete:
      operationId: deleteX
      responses:
        '204':
          description: no content
          headers:
            X-Request-Id: {schema: {type: string}}
`))
			Expect(client).To(ContainSubstring(
				"func (c *Client) DeleteX(ctx context.Context) (DeleteXResponseHeaders, *APIResponse, error)"))
		})

		It("headers on a streamed response ride alongside the io.ReadCloser", func() {
			_, client := generateAll([]byte(`
openapi: 3.1.0
info: {title: t, version: 0.0.1}
paths:
  /x/export:
    get:
      operationId: exportX
      responses:
        '200':
          description: csv stream
          headers:
            ETag: {schema: {type: string}}
          content:
            text/csv: {}
`))
			Expect(client).To(ContainSubstring(
				"func (c *Client) ExportX(ctx context.Context) (io.ReadCloser, ExportXResponseHeaders, *APIResponse, error)"))
		})

		It("ignores a declared Content-Type header, per OpenAPI 3", func() {
			_, client := generateAll([]byte(`
openapi: 3.1.0
info: {title: t, version: 0.0.1}
paths:
  /x:
    get:
      operationId: getX
      responses:
        '200':
          description: ok
          headers:
            Content-Type: {schema: {type: string}}
          content: {application/json: {schema: {type: object, properties: {a: {type: string}}}}}
`))
			// Content-Type was the only declared header, so the signature
			// keeps its unadorned shape.
			Expect(client).ToNot(ContainSubstring("GetXResponseHeaders"))
		})

		It("operations without declared headers keep their signatures", func() {
			_, client := generateAll([]byte(`
openapi: 3.1.0
info: {title: t, version: 0.0.1}
paths:
  /x:
    get:
      operationId: getX
      responses:
        '200':
          description: ok
          content: {application/json: {schema: {$ref: '#/components/schemas/Item'}}}
components:
  schemas:
    Item:
      type: object
      required: [id]
      properties: {id: {type: string}}
`))
			Expect(client).To(ContainSubstring(
				"func (c *Client) GetX(ctx context.Context) (*Item, *APIResponse, error)"))
		})
	})

	Describe("request body", func() {
		It("required JSON body becomes a typed Body field (value)", func() {
			_, client := generateAll([]byte(`
openapi: 3.1.0
info: {title: t, version: 0.0.1}
paths:
  /x:
    post:
      operationId: createX
      requestBody:
        required: true
        content: {application/json: {schema: {$ref: '#/components/schemas/Req'}}}
      responses: {'201': {description: ok}}
components:
  schemas:
    Req:
      type: object
      required: [name]
      properties: {name: {type: string}}
`))
			Expect(client).To(ContainSubstring(
				"func (c *Client) CreateX(ctx context.Context, body Req)"))
		})

		It("optional JSON body becomes *Type", func() {
			_, client := generateAll([]byte(`
openapi: 3.1.0
info: {title: t, version: 0.0.1}
paths:
  /x:
    post:
      operationId: createX
      requestBody:
        content: {application/json: {schema: {$ref: '#/components/schemas/Req'}}}
      responses: {'201': {description: ok}}
components:
  schemas:
    Req:
      type: object
      properties: {n: {type: string}}
`))
			Expect(client).To(ContainSubstring(
				"func (c *Client) CreateX(ctx context.Context, body *Req)"))
		})

		It("non-JSON, non-binary body content is ignored", func() {
			_, client := generateAll([]byte(`
openapi: 3.1.0
info: {title: t, version: 0.0.1}
paths:
  /x:
    post:
      operationId: createX
      requestBody:
        required: true
        content:
          text/csv: {schema: {type: string}}
      responses: {'201': {description: ok}}
`))
			// No Body field — only ctx in the signature.
			Expect(client).To(ContainSubstring("func (c *Client) CreateX(ctx context.Context) (*APIResponse, error)"))
		})

		It("application/octet-stream body becomes a streamed io.Reader", func() {
			_, client := generateAll([]byte(`
openapi: 3.1.0
info: {title: t, version: 0.0.1}
paths:
  /x:
    post:
      operationId: uploadX
      requestBody:
        required: true
        content:
          application/octet-stream: {}
      responses: {'204': {description: ok}}
`))
			Expect(client).To(ContainSubstring(
				"func (c *Client) UploadX(ctx context.Context, body io.Reader) (*APIResponse, error)"))
			// The reader is handed to the transport untouched, with the
			// declared media type as Content-Type.
			Expect(client).To(ContainSubstring(
				`headers, body, "application/octet-stream", "application/json", nil)`))
			// The streaming contract is documented on the method.
			Expect(client).To(ContainSubstring(
				"// The request body is streamed as application/octet-stream; it is not buffered."))
		})

		It("a binary-schema body (type string, format binary) becomes a streamed io.Reader", func() {
			_, client := generateAll([]byte(`
openapi: 3.1.0
info: {title: t, version: 0.0.1}
paths:
  /x:
    post:
      operationId: uploadX
      requestBody:
        required: true
        content:
          text/csv: {schema: {type: string, format: binary}}
      responses: {'204': {description: ok}}
`))
			Expect(client).To(ContainSubstring(
				"func (c *Client) UploadX(ctx context.Context, body io.Reader) (*APIResponse, error)"))
			Expect(client).To(ContainSubstring(`body, "text/csv",`))
		})

		It("prefers application/json when both JSON and binary bodies are declared", func() {
			_, client := generateAll([]byte(`
openapi: 3.1.0
info: {title: t, version: 0.0.1}
paths:
  /x:
    post:
      operationId: createX
      requestBody:
        required: true
        content:
          application/json: {schema: {$ref: '#/components/schemas/Req'}}
          application/octet-stream: {}
      responses: {'201': {description: ok}}
components:
  schemas:
    Req:
      type: object
      required: [name]
      properties: {name: {type: string}}
`))
			Expect(client).To(ContainSubstring(
				"func (c *Client) CreateX(ctx context.Context, body Req) (*APIResponse, error)"))
		})
	})

	Describe("path templating in generated method bodies", func() {
		It("substitutes path params via formatPathValue", func() {
			_, client := generateAll([]byte(`
openapi: 3.1.0
info: {title: t, version: 0.0.1}
paths:
  /x/{id}:
    get:
      operationId: getX
      parameters:
        - {name: id, in: path, required: true, schema: {type: integer, format: int64}}
      responses: {'204': {description: ok}}
`))
			Expect(client).To(ContainSubstring(`pathParams["id"] = formatPathValue(id)`))
		})

		It("encodes optional query params behind a nil-check", func() {
			_, client := generateAll([]byte(`
openapi: 3.1.0
info: {title: t, version: 0.0.1}
paths:
  /list:
    get:
      operationId: listX
      parameters:
        - {name: cursor, in: query, schema: {type: string}}
      responses: {'204': {description: ok}}
`))
			Expect(client).To(ContainSubstring("if cursor != nil"))
			Expect(client).To(ContainSubstring(
				`query.Set("cursor", formatPathValue(*cursor))`))
		})

		It("uses headers.Set for header params, preserving case", func() {
			_, client := generateAll([]byte(`
openapi: 3.1.0
info: {title: t, version: 0.0.1}
paths:
  /x:
    post:
      operationId: createX
      parameters:
        - {name: Idempotency-Key, in: header, required: true, schema: {type: string}}
      requestBody:
        required: true
        content: {application/json: {schema: {type: object}}}
      responses: {'201': {description: ok}}
`))
			Expect(client).To(ContainSubstring(
				`headers.Set("Idempotency-Key", formatPathValue(idempotencyKey))`))
		})
	})
})
