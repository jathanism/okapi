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
- Request bodies: `application/json` only
- Responses: first 2xx with `application/json`; non-2xx returns a typed
  `*APIError` carrying status code and raw body

## What's not supported (yet)

- `oneOf` / `anyOf` / multi-member `allOf` — these collapse to `any`.
  Single-member `allOf` is unwrapped.
- Non-JSON request/response content (e.g. `text/csv`, `multipart`)
- Header parameters in responses (we only decode the body)
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
