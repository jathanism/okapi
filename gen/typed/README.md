# gen/typed — Statically Typed Go Client Generator

`gen/typed` produces a self-contained, statically typed Go client from
an OpenAPI 3.x spec. The output is two `.gen.go` files in a target
package:

| File | Contents |
| --- | --- |
| `types.gen.go` | Typed structs for everything in `components.schemas`, plus named string enums for inline `enum:` schemas. |
| `client.gen.go` | A `Client` struct with one method per `operationId`, each taking `(ctx, params <Op>Params)` and returning `(*<Response>, error)`. |

The generated package depends only on the standard library — no okapi
runtime import. This is the static counterpart to okapi's dynamic
dispatch (the top-level `OpenApi` struct in the root package): use
typed gen when you want compile-time guarantees on parameter shapes,
request bodies, and response types, and a SemVer-tracked surface that
only changes via deliberate regen.

## Usage

```sh
go install github.com/jathanism/okapi/cmd/okapi-gen-typed@latest

okapi-gen-typed \
  --source file://./openapi.yaml \
  --package myapi \
  --out    ./internal/myapi
```

`--source` accepts `file://`, `http(s)://`, or a bare path. `--package`
must be a valid Go identifier. `--out` is created if missing.

## What you get

For an operation declared as

```yaml
paths:
  /accounts/{aid}/contacts:
    parameters:
      - {name: aid, in: path, required: true, schema: {type: string}}
    post:
      operationId: createContact
      parameters:
        - {name: Idempotency-Key, in: header, required: true, schema: {type: string}}
      requestBody:
        required: true
        content:
          application/json:
            schema: {$ref: '#/components/schemas/CreateContactBody'}
      responses:
        '201':
          description: created
          content:
            application/json:
              schema: {$ref: '#/components/schemas/Contact'}
```

you get:

```go
type CreateContactParams struct {
    Aid            string          `json:"-" path:"aid"`
    IdempotencyKey string          `json:"-" header:"Idempotency-Key"`
    Body           CreateContactBody `json:"-"`
}

func (c *Client) CreateContact(
    ctx context.Context,
    params CreateContactParams,
) (*Contact, error) {
    // ...
}
```

The `Idempotency-Key` casing is preserved on the wire (the generated
code calls `headers.Set("Idempotency-Key", ...)`). Path templates are
substituted with `url.PathEscape`.

## What's supported

- `components.schemas` → typed Go structs with JSON tags
- `$ref: '#/components/schemas/Foo'` → Go type reference
- `type: [string, "null"]` (OpenAPI 3.1 nullable) → pointer field
- `format: int32 | int64 | float | double` → matching Go primitive
- Inline `enum:` on a string field → named `type Foo string` + const block
- `required: [...]` → controls pointer vs value, and `omitempty` JSON tag
- Path-item-level parameters merged into operation-level (op-level wins
  on collisions, per OpenAPI 3)
- Request bodies: `application/json` (typed struct), plus binary media
  streamed from an `io.Reader` (see below)
- Responses: first 2xx with `application/json`; non-JSON 2xx content is
  streamed as an `io.ReadCloser`; non-2xx returns a typed `*APIError`
  carrying status code, raw body, response headers (e.g. `Retry-After`
  on 429), and decoded RFC 7807 problem details
- Response headers declared on the success response → a typed
  `<Op>ResponseHeaders` struct returned alongside the body

### Non-JSON request and response bodies

Binary request bodies — `application/octet-stream`, or any non-JSON
media type whose schema is `type: string, format: binary` — become an
`io.Reader` argument. The reader is handed straight to the transport
(no buffering) and the declared media type is sent as `Content-Type`:

```go
// requestBody: {content: {application/octet-stream: {}}}
func (c *Client) ImportItems(ctx context.Context, body io.Reader) error
```

When an operation's 2xx response declares only non-JSON content (e.g.
`text/csv`, `application/octet-stream` — including the bare
`text/csv: {}` empty-schema form), the method returns the raw response
body instead of decoding it. The caller must close it:

```go
// responses: {'200': {content: {text/csv: {}}}}
func (c *Client) ExportItems(ctx context.Context) (io.ReadCloser, error)
```

The declared media type is also sent as the default `Accept` header.
Operations whose 2xx declares both `application/json` and non-JSON
content keep the decoded-JSON shape.

### Typed response headers

When an operation's success response declares `headers:`, the generator
emits a per-operation struct (fields alphabetical by Go name, derived
the same way parameter names are) and the method returns it as an
additional value, just before the error:

```yaml
responses:
  '200':
    headers:
      ETag: {schema: {type: string}}
      Cache-Control: {schema: {type: string}}
    content:
      application/json:
        schema: {$ref: '#/components/schemas/Item'}
```

```go
type GetItemResponseHeaders struct {
    CacheControl string
    Etag         string
}

func (c *Client) GetItem(ctx context.Context, id int64) (*Item, GetItemResponseHeaders, error)
```

Header values map through the same schema→Go-type logic as parameters;
non-string primitives (e.g. `type: integer`) parse best-effort, and a
missing or malformed value leaves the field at its zero value. Only
operations that declare response headers change shape — everything
else keeps the plain `(*T, error)` / `error` signatures.

### RFC 7807 problem details

Non-2xx responses whose `Content-Type` is `application/problem+json`
(media type parameters tolerated) or `application/json` are decoded
into `APIError.Problem`:

```go
type APIProblem struct {
    Type     string
    Title    string
    Status   int
    Detail   string
    Instance string
    // Non-standard members (e.g. a per-field "errors" array), raw.
    Extensions map[string]json.RawMessage
}
```

Decoding is best-effort: a malformed problem body never masks the HTTP
error — `Problem` stays nil and `APIError.Body` always carries the raw
bytes.

```go
var apiErr *client.APIError
if errors.As(err, &apiErr) && apiErr.Problem != nil {
    log.Printf("%d %s: %s", apiErr.StatusCode, apiErr.Problem.Title, apiErr.Problem.Detail)
}
```

## What's not supported (yet)

- `oneOf` / `anyOf` / multi-member `allOf` — these collapse to `any`.
  Single-member `allOf` is unwrapped.
- `multipart/form-data` request bodies
- Authentication helpers — set `Client.DefaultHeaders` or wrap
  `*http.Client` for auth, tracing, and retries.

These are deliberate scope cuts for the MVP, not architectural blocks.
File issues with concrete spec snippets if you hit them.

## Regen workflow

The intended workflow when the upstream service ships a new release:

1. Fetch the new spec to a known location (e.g. `openapi.yaml`).
2. Run `okapi-gen-typed` with the same `--package` and `--out`.
3. Commit the diff. Compile errors at call sites = the API changed.
4. SemVer-bump your client package if downstream consumers depend on it.

The two generated files together are the SemVer surface. If you don't
regenerate, the API surface is fixed and your callers see no drift.

## Library vs CLI

The CLI is a thin wrapper around `typed.Generate(opts) (Files, error)`.
Use the library directly if you want to:

- Pipe spec bytes from a non-URL source
- Post-process generated files (linting, custom file headers, etc.)
- Generate inside `go:generate` without invoking a separate binary
