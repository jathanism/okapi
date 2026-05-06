# Okapi

Okapi is a standalone OpenAPI 3.x client library for Go. Give it an OpenAPI spec, and it gives you typed Go endpoints — no code generation step required at runtime (unless you want compile-time safety with `go generate`).

It parses your spec, builds a `OpenApi` struct with endpoint methods, validates request params and bodies against the schema, and makes the HTTP call. It also ships with a CLI generator that turns your spec into a nested command tree automatically.

## Features

- **Spec-driven endpoints** — parse any OpenAPI 3.x spec (local file, `file://`, `http(s)://`) and call endpoints by name
- **Request validation** — parameters are type-checked, required params are enforced, request bodies are validated against JSON Schema
- **CLI generation** — `go generate` produces a typed `OpenApi` struct with endpoint methods as fields; the `cli` package builds a full command tree from the spec (using [urfave/cli](https://github.com/urfave/cli))
- **Flexible API client** — bring your own HTTP client via the `OpenApiClient` interface; Okapi doesn't couple you to any specific HTTP library
- **Glamour help output** — CLI help text is rendered with [glamour](https://github.com/charmbracelet/glamour) markdown styling, including inline request body schemas
- **Zero runtime dependencies on your app's context** — `CliContext` interface lets you bridge Okapi into any CLI framework

## Quick Start

### As a library

```go
package main

import (
    "fmt"
    "net/http"
    "io"

    "github.com/jathanism/okapi"
    "github.com/jathanism/okapi/request"
)

func main() {
    // Load an OpenAPI spec
    api, err := (*openapi.OpenApi)(nil).NewFromSource("file://openapi.yaml")
    if err != nil {
        panic(err)
    }

    // Bind an API client
    myClient := &MyHttpClient{}
    api = api.WithClient(myClient)

    // Call an endpoint
    err = api.UsersList(
        request.Param("limit", 10),
        request.Param("offset", 0),
        request.Result(&result),
    )
}
```

### CLI generation

```bash
# Generate the OpenApi struct from a spec
OKAPI_OPENAPI_SOURCE=https://api.example.com/openapi.json go generate ./...

# Or use flags
go run ./gen/gen.go --source https://api.example.com/openapi.json
```

This produces `openapi_gen.go` with typed endpoint fields:

```go
type OpenApi struct {
    internal

    AccountsChangePassword OpenApiEndpoint
    OrganizationsBySlug    OpenApiEndpoint
    OrganizationsUsersCreate OpenApiEndpoint
    OrganizationsUsersList OpenApiEndpoint
    UsersCreate            OpenApiEndpoint
    UsersList              OpenApiEndpoint
    SchemasList            OpenApiEndpoint
    // ...
}
```

## Project Structure

```
okapi/
├── openapi.go          # Core OpenApi struct — loads specs, builds endpoints, makes calls
├── openapi_gen.go      # Generated OpenApi struct (do not edit — use go generate)
├── openapi_test.go     # Integration tests (Ginkgo/Gomega)
├── error/              # Error types and helpers (OpenApiError, OpenApiValidationError)
├── request/            # Request building — params, body, headers, API client interface
├── spec/               # OpenAPI spec parsing, endpoint/param/body types, validation
├── cli/                # CLI generator — builds urfave/cli command tree from spec
├── gen/                # Code generator — reads spec, writes openapi_gen.go
├── internal/
│   ├── log/            # Structured logging (charmbracelet/log) with trace support
│   └── testutil/       # Test helpers
└── testdata/
    └── openapi.yaml    # Test fixture spec
```

## Packages

### `openapi` (root)

The core package. `OpenApi` is the main type — it loads a spec, builds endpoint callables, and dispatches requests.

Key types:
- `OpenApi` — spec-loaded API client with typed endpoint fields
- `OpenApiEndpoint` — `func(options ...RequestOption) error` — call an endpoint with options
- `OpenApiSpec` — alias for `spec.OpenApiSpec`

Key methods:
- `NewFromSource(source string)` — load from file path, URL, or raw string (cached)
- `NewFromBytes(source []byte)` — load from raw bytes (not cached)
- `WithClient(client OpenApiClient)` — bind an HTTP client to all endpoints
- `With(options ...RequestOption)` — clone with request options applied to all endpoints

### `spec`

Parses OpenAPI 3.x specs using [pb33f/libopenapi](https://github.com/pb33f/libopenapi). Handles parameter parsing, request body JSON Schema compilation, and URL building.

### `request`

Functional options for building requests. `Param()`, `Body()`, `Data()`, `Header()`, `Result()`. The `OpenApiClient` interface is what you implement to bring your own HTTP client.

### `cli`

Generates a nested CLI command tree from an OpenAPI spec using [urfave/cli](https://github.com/urfave/cli). Commands mirror the spec's operationId structure (e.g., `users list`, `organizations users create`). Help text includes inline JSON body schemas rendered with glamour.

The `CliContext` interface bridges Okapi into your app's CLI runtime — implement it to provide stdin/stdout, host resolution, and JSON output formatting.

### `gen`

Code generator invoked via `go generate`. Reads a spec and writes `openapi_gen.go` with the typed struct. Configure with `--source`, `--host`, or the `OKAPI_OPENAPI_SOURCE` env var.

### `error`

Custom error types with `OpenApiError` and `OpenApiValidationError` sentinels. Use `Error()`, `Errorf()`, and `ErrorFrom()` to create wrapped errors.

## Using as a Library

### Loading a spec

```go
// From a file
api, err := (*openapi.OpenApi)(nil).NewFromSource("file:///path/to/openapi.yaml")

// From a URL
api, err := (*openapi.OpenApi)(nil).NewFromSource("https://api.example.com/openapi.json")

// From raw bytes
api, err := (*openapi.OpenApi)(nil).NewFromBytes([]byte(yamlContent))
```

### Implementing the API client

Implement the `OpenApiClient` interface to provide HTTP transport:

```go
type OpenApiClient interface {
    RequestJSON(method string, uri string, body io.Reader, result any, headers map[string][]string) (*http.Response, error)
}
```

Then bind it:

```go
api = api.WithClient(myClient)
```

### Calling endpoints

```go
var result map[string]any

// Simple GET with query params
err := api.UsersList(
    request.Param("limit", 10),
    request.Result(&result),
)

// POST with body
err := api.UsersCreate(
    request.Body(map[string]any{"email": "user@example.com", "password": "secret"}),
    request.Result(&result),
)

// Path params, headers, and combined options
err := api.OrganizationsUsersList(
    request.Param("organization_id", "org-123"),
    request.Param("limit", 50),
    request.Header("Authorization", "Bearer token"),
    request.Result(&result),
)

// Endpoint chaining with .With()
customList := api.UsersList.With(
    request.Param("limit", 100),
    request.Header("Authorization", "Bearer token"),
)
err := customList(request.Result(&result))
```

### Dynamic dispatch (calling endpoints by name)

`WithClient` only binds the typed `OpenApiEndpoint` fields on the generated
`OpenApi` struct — it does **not** mutate the raw `*spec.Endpoint` values
returned by `api.Endpoints()`. If you fetch an endpoint from that map and try
to call it directly with `openapi.CallEndpoint(ep, ...)`, you'll get:

```
No ApiClient available, did you forget to call OpenApi.WithClient()?
```

`CallEndpoint` is the low-level dispatcher used internally — it doesn't know
about the client you bound to your `*OpenApi`. You have two pragmatic options:

> **Note:** `api.Endpoints()` is keyed by the spec's `operationId` (the raw
> name in the OpenAPI document, e.g. `usersList`). The matching field on the
> generated `*OpenApi` struct uses the CamelCased form returned by
> `(*spec.Endpoint).MethodName()` (e.g. `UsersList`). Use the raw name when
> looking up the endpoint, and `MethodName()` when looking up the field.

**1. Pass the client through per-call** (recommended — no `reflect`, works for any spec):

```go
err := openapi.CallEndpoint(ep,
    request.WithClient(myClient),
    request.Result(&result),
)
```

This is the simplest form and works whether or not you have a bound
`*OpenApi`. It's the right default for tooling that walks `api.Endpoints()`
or for tests that drive specs the generated struct doesn't match. See
[`examples/client-test`](examples/client-test) for a runnable end-to-end
example that uses this pattern.

**2. Reflective field lookup** (when you specifically need the options bound to your `*OpenApi`):

Look up the generated struct field by `MethodName()` and invoke it. This
inherits everything bound via `WithClient` / `With`, so it's the form to
reach for when you've layered on auth headers, defaults, etc., and want
each dynamic call to pick those up:

```go
api = api.WithClient(myClient).With(request.Header("Authorization", "Bearer "+token))

ep := api.Endpoints()["usersList"]  // keyed by spec operationId
name := ep.MethodName()             // CamelCased, e.g. "UsersList"

field := reflect.ValueOf(api).Elem().FieldByName(name)
fn := field.Interface().(openapi.OpenApiEndpoint)

err := fn(request.Result(&result))
```

### Params vs. headers

Okapi separates spec-declared parameters by location:

- `request.Param(name, value)` — for `path`, `query`, and `cookie` parameters
- `request.Header(name, value)` — for `header` parameters declared in the spec, **and** for ad-hoc HTTP headers (`Authorization`, `Content-Type`, etc.) that aren't in the spec at all

If a spec declares a parameter with `in: header` (e.g. `Idempotency-Key`), pass it through `request.Header(...)`. Passing it through `request.Param(...)` will fail validation with a message pointing you at the right helper, and vice versa:

```go
// Spec: Idempotency-Key is declared as `in: header`

// Correct:
err := api.UsersCreate(
    request.Header("Idempotency-Key", "abc-123"),
    request.Body(payload),
)

// Wrong — Validate returns:
//   "Parameter Idempotency-Key is a header — pass it with
//    request.Header(\"Idempotency-Key\", ...) instead of request.Param(...)"
err := api.UsersCreate(
    request.Param("Idempotency-Key", "abc-123"),
    request.Body(payload),
)
```

Headers that aren't declared in the spec (auth tokens, tracing IDs, etc.) pass through to the API client untouched.

## Logging

Okapi uses [charmbracelet/log](https://github.com/charmbracelet/log) internally. Enable debug or trace output with the `DEBUG` env var:

```bash
DEBUG=1      # Debug level
DEBUG=trace  # Trace level (verbose request/response details)
```

## Requirements

- Go 1.26.1+
- An OpenAPI 3.x spec (JSON or YAML)

## Development

```bash
# Run all tests (Ginkgo + Gomega)
go test ./...

# Run a specific package
go test ./spec/...
go test ./request/...

# Generate from a spec
OKAPI_OPENAPI_SOURCE=file://testdata/openapi.yaml go generate ./...

# Or with flags
go run ./gen/gen.go --source file://testdata/openapi.yaml
```

## Contributing

1. Fork the repo
2. Create a feature branch (`git checkout -b feat/my-feature`)
3. Write tests for your changes (tests use [Ginkgo](https://onsi.github.io/ginkgo/) + [Gomega](https://onsi.github.io/gomega/))
4. Make sure all tests pass (`go test ./...`)
5. Commit with conventional commits (`feat:`, `fix:`, `chore:`, etc.)
6. Push and open a pull request

## License

[Apache 2.0](LICENSE)
