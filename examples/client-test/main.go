// Package main demonstrates how to drive every endpoint in an OpenAPI spec
// dynamically through okapi, against a real HTTP round-trip.
//
// The example spins up an in-process httptest server that implements
// openapi.yaml, loads the spec at runtime, and dispatches each operation
// using `openapi.CallEndpoint(ep, request.WithClient(client), ...)`. No
// codegen step is needed — useful when the spec you call against doesn't
// match the generated `openapi_gen.go` (the typical case for `examples/`,
// or for tooling that walks `api.Endpoints()`).
//
// For the typed-field flow (api.UsersList(...) etc.) see the top-level
// README "Quick Start" section.
package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	openapi "github.com/jathanism/okapi"
	"github.com/jathanism/okapi/request"
	"github.com/jathanism/okapi/spec"
)

// call describes one request to make. Driver-style so each operation reads
// top-to-bottom and you can see exactly which inputs go with which endpoint.
type call struct {
	op   string // operationId in the spec
	opts []request.RequestOption
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "FAIL: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	srv := newFakeServer()
	defer srv.Close()

	specPath, err := filepath.Abs("openapi.yaml")
	if err != nil {
		return fmt.Errorf("abs: %w", err)
	}
	api, err := (*openapi.OpenApi)(nil).NewFromSource("file://" + specPath)
	if err != nil {
		return fmt.Errorf("load spec: %w", err)
	}

	client := &httpClient{
		BaseURL: srv.URL,
		HTTP:    &http.Client{Timeout: 5 * time.Second},
	}

	calls := []call{
		{op: "healthz"},
		{op: "listItems", opts: []request.RequestOption{
			request.Param("limit", 10),
		}},
		{op: "createItem", opts: []request.RequestOption{
			request.Header("Idempotency-Key", fmt.Sprintf("create-%d", time.Now().UnixNano())),
			request.Body(map[string]any{"name": "Widget"}),
		}},
		{op: "getItem", opts: []request.RequestOption{
			request.Param("id", int64(1)),
		}},
		{op: "deleteItem", opts: []request.RequestOption{
			request.Param("id", int64(1)),
			request.Header("Idempotency-Key", "delete-1"),
			request.Header("If-Match", "*"),
		}},
	}

	// Coverage check: every operation declared in the spec must be in the
	// call table. If you add a new path to openapi.yaml without wiring it
	// here, this fails loudly at startup rather than silently skipping.
	if err := assertCoversAllOps(api, calls); err != nil {
		return err
	}

	endpoints := api.Endpoints()
	pass, fail := 0, 0

	for _, c := range calls {
		ep, ok := endpoints[c.op]
		if !ok {
			fmt.Printf("[FAIL] %-12s (no such operationId in spec)\n", c.op)
			fail++
			continue
		}

		// Dynamic dispatch: pass the client per-call rather than binding it
		// to the OpenApi struct via WithClient. This works for any spec at
		// runtime, including specs that don't match the generated struct.
		var out any
		opts := append([]request.RequestOption{
			request.WithClient(client),
			request.Result(&out),
		}, c.opts...)

		err := openapi.CallEndpoint(ep, opts...)
		if err != nil {
			fmt.Printf("[FAIL] %-12s %-6s %-20s err: %v\n", c.op, strings.ToUpper(ep.Method), ep.Path, err)
			fail++
			continue
		}
		pass++
		fmt.Printf("[PASS] %-12s %-6s %-20s out: %s\n", c.op, strings.ToUpper(ep.Method), ep.Path, summarize(out))
	}

	// Negative path: body with a wrong-typed field is rejected by okapi
	// before the HTTP call is made (schema validation).
	negativeOpts := []request.RequestOption{
		request.WithClient(client),
		request.Header("Idempotency-Key", "negative-1"),
		request.Body([]byte(`{"name": 123}`)),
	}
	err = openapi.CallEndpoint(endpoints["createItem"], negativeOpts...)
	if err == nil {
		fmt.Println("[FAIL] negative createItem: expected validation error, got none")
		fail++
	} else {
		pass++
		fmt.Printf("[PASS] %-12s validation rejected as expected: %v\n", "negative", err)
	}

	fmt.Printf("\n%d passed, %d failed\n", pass, fail)
	if fail > 0 {
		return fmt.Errorf("%d failures", fail)
	}
	return nil
}

func assertCoversAllOps(api *openapi.OpenApi, calls []call) error {
	declared := map[string]*spec.Endpoint{}
	for _, ep := range api.Endpoints() {
		declared[ep.Name] = ep
	}
	covered := map[string]bool{}
	for _, c := range calls {
		covered[c.op] = true
	}
	var missing []string
	for op := range declared {
		if !covered[op] {
			missing = append(missing, op)
		}
	}
	if len(missing) > 0 {
		sort.Strings(missing)
		return fmt.Errorf("call table is missing operations: %v", missing)
	}
	return nil
}

func summarize(v any) string {
	if v == nil {
		return "<no body>"
	}
	b, err := json.Marshal(v)
	if err != nil {
		return fmt.Sprintf("<unmarshalable: %v>", err)
	}
	s := string(b)
	if len(s) > 120 {
		s = s[:120] + "…"
	}
	return s
}
