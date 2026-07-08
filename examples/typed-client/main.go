// Command typed-client is the runnable example for okapi's
// statically typed client generator (`gen/typed`). It boots an
// in-process httptest server implementing the spec, instantiates the
// generated *Client, and round-trips every operation through it.
//
// Run:
//
//	go run ./examples/typed-client/
//
// Regenerate the client after a spec change:
//
//	go generate ./examples/typed-client/
package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"strings"

	"github.com/jathanism/okapi/examples/typed-client/client"
)

//go:generate go run ../../cmd/okapi-gen-typed/ --source file://./openapi.yaml --package client --out ./client

func main() {
	srv := newFakeServer()
	defer srv.Close()

	c := client.NewClient(srv.URL)
	ctx := context.Background()

	// 1) Healthz — no params, typed response.
	h, err := c.Healthz(ctx)
	must("healthz", err)
	fmt.Printf("[1] healthz: status=%q\n", h.Status)

	// 2) ListItems — optional query params as *T positional args.
	// Pass nil for any you want to omit; the method body drops the
	// query key entirely when the arg is nil.
	cursor := "page-1"
	limit := int64(10)
	list, err := c.ListItems(ctx, &cursor, &limit)
	must("listItems", err)
	fmt.Printf("[2] listItems: %d item(s), first=%q\n", len(list.Items), list.Items[0].Name)

	// 3) CreateItem(ctx, idempotencyKey, body) — required header + typed
	// body as positional args. Removing idempotencyKey makes the build
	// fail.
	created, err := c.CreateItem(ctx, "key-1", client.CreateItemBody{Name: "Sprocket"})
	must("createItem", err)
	fmt.Printf("[3] createItem: id=%d name=%q\n", created.Id, created.Name)

	// 4) GetItem(ctx, id) — int64 path param. The 200 response declares
	// ETag and Cache-Control headers, so the method also returns a typed
	// GetItemResponseHeaders struct.
	got, gotHeaders, err := c.GetItem(ctx, created.Id)
	must("getItem", err)
	fmt.Printf("[4] getItem(%d): name=%q etag=%s cache-control=%q\n",
		got.Id, got.Name, gotHeaders.Etag, gotHeaders.CacheControl)

	// 5) DeleteItem(ctx, id, idempotencyKey, ifMatch) — multiple required
	// headers, ordered alphabetically by Go name.
	if err := c.DeleteItem(ctx, created.Id, "key-2", "etag-abc"); err != nil {
		log.Fatalf("deleteItem: %v", err)
	}
	fmt.Printf("[5] deleteItem(%d): 204 No Content\n", created.Id)

	// 6) ExportItems(ctx) — a text/csv response is streamed back as an
	// io.ReadCloser instead of being decoded; the caller must close it.
	csvBody, err := c.ExportItems(ctx)
	must("exportItems", err)
	csvData, err := io.ReadAll(csvBody)
	_ = csvBody.Close()
	must("exportItems read", err)
	fmt.Printf("[6] exportItems: %d bytes of csv, header %q\n",
		len(csvData), strings.SplitN(string(csvData), "\n", 2)[0])

	// 7) ImportItems(ctx, body) — an application/octet-stream request
	// body is streamed from any io.Reader, not buffered.
	err = c.ImportItems(ctx, strings.NewReader("name\nGadget\n"))
	must("importItems", err)
	fmt.Println("[7] importItems: 204 No Content")

	// 8) Negative — server returns a 422 application/problem+json body
	// when name is empty. The RFC 7807 members are decoded onto
	// apiErr.Problem; apiErr.Body keeps the raw bytes either way.
	_, err = c.CreateItem(ctx, "key-3", client.CreateItemBody{Name: ""})
	var apiErr *client.APIError
	switch {
	case errors.As(err, &apiErr) && apiErr.Problem != nil:
		fmt.Printf("[8] empty name → APIError(%d) title=%q detail=%q\n",
			apiErr.StatusCode, apiErr.Problem.Title, apiErr.Problem.Detail)
	case err == nil:
		log.Fatalf("[8] expected an error, got nil")
	default:
		log.Fatalf("[8] unexpected error shape: %v", err)
	}

	fmt.Println("\nAll eight interactions succeeded.")
}

func must(op string, err error) {
	if err != nil {
		log.Fatalf("%s: %v", op, err)
	}
}
