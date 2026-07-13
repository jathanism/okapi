# typed-client

A minimal end-to-end example: feed an OpenAPI spec to
[`okapi-gen-typed`](../../cmd/okapi-gen-typed), get a statically
typed `*Client` with one method per `operationId`, and round-trip
every operation against an in-process HTTP server.

This is the "I want to see okapi typed gen working" example —
useful when you're new to the static gen, when you're wiring it up
in your own service, or when you want to see what the generated
output looks like for a tiny but realistic spec.

The dynamic-dispatch counterpart (no codegen, runtime spec parsing)
is in [`../client-test`](../client-test). This spec is a superset of
the `client-test` one (it adds streamed CSV export/import), so you
can still diff the shared call shapes side-by-side.

## What's in here

| File | Purpose |
| --- | --- |
| [`openapi.yaml`](openapi.yaml) | Tiny seven-operation Items API. Covers query, path, and header params, required body fields, streamed CSV export/import, and a 422 problem response. |
| [`client/`](client) | **Generator output** — `types.gen.go` (typed structs) + `client.gen.go` (typed `*Client` with one method per operation). Committed; regenerate via `go generate`. |
| [`server.go`](server.go) | A canned `httptest` server implementing the spec. |
| [`main.go`](main.go) | The runner. Drives `Healthz`, `ListItems`, `CreateItem`, `GetItem`, `DeleteItem`, `ExportItems`, `ImportItems`, and a 422 negative path through the typed client. |

## Run it

```sh
go run ./examples/typed-client/
```

Expected output:

```text
[1] healthz: status="ok"
[2] listItems: 1 item(s), first="Widget"
[3] createItem: id=2 name="Sprocket"
[4] getItem(2): name="Widget" etag="item-2-v1" cache-control="max-age=60"
[5] deleteItem(2): 204 No Content
[6] exportItems: 17 bytes of csv, header "id,name"
[7] importItems: 204 No Content
[8] empty name → APIError(422) title="Unprocessable Entity" detail="name is required"

All eight interactions succeeded.
```

## Regenerate after a spec change

```sh
go generate ./examples/typed-client/
```

That re-invokes `okapi-gen-typed` against `./openapi.yaml` and
overwrites the two `.gen.go` files in [`client/`](client). Commit
the diff alongside the spec change — that's the API surface change
your callers will see.

The `//go:generate` directive in [`main.go`](main.go) is the source
of truth for the regen command:

```go
//go:generate go run ../../cmd/okapi-gen-typed/ --source file://./openapi.yaml --package client --out ./client
```

In a downstream consumer (not in this repo) you'd typically pin
the generator version with `go run pkg@version`:

```go
//go:generate go run github.com/jathanism/okapi/cmd/okapi-gen-typed@v0.X.Y --source file://./openapi.yaml --package client --out ./client
```

That way the generator is fetched on demand and the version is
explicit in the commit history.

## What the generated surface looks like

For the spec in `openapi.yaml`, `okapi-gen-typed` produces:

```go
// types.gen.go — body / response payloads only
type Item struct {
    Id        int64  `json:"id"`
    Name      string `json:"name"`
    CreatedAt string `json:"created_at"`
}

type CreateItemBody struct {
    Name string `json:"name"`
}

// (... Health, ItemList, Problem ...)

// client.gen.go — Client + per-op methods with positional args
type Client struct { /* BaseURL, *http.Client, DefaultHeaders */ }

func NewClient(baseURL string) *Client

func (c *Client) Healthz(ctx context.Context) (*Health, error)

// Optional query params are *T — pass nil to omit.
func (c *Client) ListItems(ctx context.Context, cursor *string, limit *int64) (*ItemList, error)

// Required header + body. Removing an arg fails to compile.
func (c *Client) CreateItem(ctx context.Context, idempotencyKey string, body CreateItemBody) (*Item, error)

// int64 path param. The 200 declares ETag and Cache-Control headers,
// so a typed headers struct rides alongside the body.
type GetItemResponseHeaders struct {
    CacheControl string
    Etag         string
}
func (c *Client) GetItem(ctx context.Context, id int64) (*Item, GetItemResponseHeaders, error)

// Multiple required headers — alphabetical order by Go name.
func (c *Client) DeleteItem(ctx context.Context, id int64, idempotencyKey string, ifMatch string) error

// text/csv response — streamed back as an io.ReadCloser (caller closes).
func (c *Client) ExportItems(ctx context.Context) (io.ReadCloser, error)

// application/octet-stream request body — streamed from any io.Reader.
func (c *Client) ImportItems(ctx context.Context, body io.Reader) error

type APIError struct {
    StatusCode int
    Status     string
    Method     string
    URL        string
    Body       []byte
    Problem    *APIProblem // decoded RFC 7807 body, when present
    Header     http.Header // response headers, e.g. Retry-After on 429
}
func (e *APIError) Error() string
```

The argument order is canonical and stable across regens:
**ctx → path params (URL-template order) → header params (alphabetical
by Go name) → query params (alphabetical) → body**. Required scalars
are values; optional scalars are pointers.

Compile-time guarantees:

- the path param `id` on `GetItem` is `int64`, not a generic
  stringly-typed request param
- `CreateItem` won't compile without `idempotencyKey` — you can't
  silently zero-value it the way a struct field would let you
- the `Idempotency-Key` casing is preserved on the wire
- `Healthz` returns `*Health`, not `any` — IDE autocomplete works

Non-2xx server responses surface as `*APIError`; transport-level
failures (DNS, connection refused, ctx cancellation) come back as
plain wrapped errors. Use `errors.As` to discriminate. When the error
body is `application/problem+json` (or plain JSON), the RFC 7807
members are decoded onto `apiErr.Problem` — the raw bytes stay on
`apiErr.Body` either way:

```go
var apiErr *client.APIError
if errors.As(err, &apiErr) {
    if apiErr.Problem != nil {
        log.Printf("api %d %s: %s", apiErr.StatusCode, apiErr.Problem.Title, apiErr.Problem.Detail)
    } else {
        log.Printf("api %d: %s", apiErr.StatusCode, apiErr.Body)
    }
}
```

## Why use this over dynamic dispatch?

Use the typed gen (this example) when you're a **production
consumer** of the API: the surface is SemVer-tracked, spec changes
show up as compile-time errors at call sites, and there's no
runtime spec parsing or reflection on the hot path.

Use [dynamic dispatch](../client-test) when you're a **tool** that
needs to adapt to arbitrary specs at runtime, or when you want a
single binary that can drive whatever endpoint you point it at
without a regen step.

The two approaches are not mutually exclusive — the typed gen
imports nothing from okapi at runtime, so consumers can use both
in the same binary if it makes sense.
