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
	"log"

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

	// 2) ListItems — optional query params (cursor as *string).
	cursor := "page-1"
	limit := int64(10)
	list, err := c.ListItems(ctx, client.ListItemsParams{Cursor: &cursor, Limit: &limit})
	must("listItems", err)
	fmt.Printf("[2] listItems: %d item(s), first=%q\n", len(list.Items), list.Items[0].Name)

	// 3) CreateItem — typed body + required header. The compiler enforces
	// both; deleting the IdempotencyKey field below makes the build fail.
	created, err := c.CreateItem(ctx, client.CreateItemParams{
		IdempotencyKey: "key-1",
		Body:           client.CreateItemBody{Name: "Sprocket"},
	})
	must("createItem", err)
	fmt.Printf("[3] createItem: id=%d name=%q\n", created.Id, created.Name)

	// 4) GetItem — int64 path param.
	got, err := c.GetItem(ctx, client.GetItemParams{Id: created.Id})
	must("getItem", err)
	fmt.Printf("[4] getItem(%d): name=%q created_at=%q\n", got.Id, got.Name, got.CreatedAt)

	// 5) DeleteItem — multiple required headers (Idempotency-Key + If-Match).
	if err := c.DeleteItem(ctx, client.DeleteItemParams{
		Id:             created.Id,
		IdempotencyKey: "key-2",
		IfMatch:        "etag-abc",
	}); err != nil {
		log.Fatalf("deleteItem: %v", err)
	}
	fmt.Printf("[5] deleteItem(%d): 204 No Content\n", created.Id)

	// 6) Negative — server returns 422 when name is empty. Demonstrates
	// the typed *APIError surface for non-2xx responses.
	_, err = c.CreateItem(ctx, client.CreateItemParams{
		IdempotencyKey: "key-3",
		Body:           client.CreateItemBody{Name: ""},
	})
	var apiErr *client.APIError
	switch {
	case errors.As(err, &apiErr):
		fmt.Printf("[6] empty name → APIError(%d) %s\n", apiErr.StatusCode, string(apiErr.Body))
	case err == nil:
		log.Fatalf("[6] expected an error, got nil")
	default:
		log.Fatalf("[6] unexpected error type: %v", err)
	}

	fmt.Println("\nAll six interactions succeeded.")
}

func must(op string, err error) {
	if err != nil {
		log.Fatalf("%s: %v", op, err)
	}
}
