# client-test

A minimal end-to-end example: load an OpenAPI spec at runtime, dispatch every operation through okapi, and round-trip each call against an in-process HTTP server.

This is the "I want to see okapi working" example — useful when you're new to the library, when you're integrating it against a spec that doesn't match the generated `openapi_gen.go`, or when you're writing tooling that walks `api.Endpoints()` and dispatches by name.

## What's in here

| File | Purpose |
| --- | --- |
| [`openapi.yaml`](openapi.yaml) | Tiny five-endpoint Items API. Covers query params, path params, header params (`Idempotency-Key`, `If-Match`), required body fields, and a 422 problem response. |
| [`server.go`](server.go) | A canned `httptest` server implementing the spec — just enough to round-trip every operation. |
| [`client.go`](client.go) | A reference `request.OpenApiClient` (~50 lines): prepend base URL, set headers, decode JSON, surface non-JSON bodies. |
| [`main.go`](main.go) | The runner. Loads the spec, asserts the call table covers every operation, dispatches each call, and runs one negative validation test. |

## Run it

```bash
cd examples/client-test
go run .
```

Expected output:

```
[PASS] healthz      GET    /healthz             out: {"status":"ok"}
[PASS] listItems    GET    /items               out: {"items":[{...}]}
[PASS] createItem   POST   /items               out: {...}
[PASS] getItem      GET    /items/{id}          out: {...}
[PASS] deleteItem   DELETE /items/{id}          out: <no body>
[PASS] negative     validation rejected as expected: ... expected string, but got number

6 passed, 0 failed
```

## How it works

The runner uses **dynamic dispatch** — the per-call `request.WithClient(...)` form — rather than the typed bound fields:

```go
api, _ := (*openapi.OpenApi)(nil).NewFromSource("file://openapi.yaml")

ep := api.Endpoints()["createItem"] // keyed by spec operationId

err := openapi.CallEndpoint(ep,
    request.WithClient(client),
    request.Header("Idempotency-Key", key),
    request.Body(map[string]any{"name": "Widget"}),
    request.Result(&out),
)
```

This pattern works for any spec at runtime — no codegen needed, no requirement that the spec match the `OpenApi` struct. See the top-level README's [Dynamic dispatch](../../README.md#dynamic-dispatch-calling-endpoints-by-name) section for the full picture, including when to prefer typed bound fields instead.

## Things this example demonstrates

- **Loading a spec from a `file://` URL** at runtime.
- **Schema-driven request validation** — the negative test sends `{"name": 123}` and okapi rejects it before any HTTP call leaves the process.
- **Spec-declared header params** (`Idempotency-Key`, `If-Match`) flow through `request.Header(...)`, not `request.Param(...)`.
- **A complete, ~50-line `OpenApiClient` implementation** you can copy into your own project as a starting point.
- **Coverage assertions**: the runner refuses to start if any spec-declared operation isn't in the call table, so spec drift surfaces immediately.
