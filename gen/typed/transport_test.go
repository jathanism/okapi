package typed_test

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/jathanism/okapi/gen/typed"
)

// transportSpec is the canned spec the transport tests are generated
// against. It exercises every input/output shape we care about:
// - GET with no body and a typed response
// - GET with no body and 204 (no result)
// - POST with body, headers, path params, and a typed response
// - GET that returns 404 with a JSON error body (to test APIError)
// - POST with an application/octet-stream body (streamed upload)
// - GET with a text/csv response (streamed download)
// - GET with declared response headers (typed headers struct)
// - GET with an integer path param (non-string path formatting)
// - POST with a JSON body plus optional query params of every scalar
//   flavor: string, int64, and enum (the search shape)
// - POST with a streamed body, a Content-Type header param that
//   overrides the spec's media type, and a boolean query param
//   (the bulk-upsert shape)
const transportSpec = `
openapi: 3.1.0
info: {title: t, version: 0.0.1}
paths:
  /thing/import:
    post:
      operationId: importThings
      requestBody:
        required: true
        content:
          application/octet-stream: {}
      responses:
        '204': {description: accepted}
  /thing/export:
    get:
      operationId: exportThings
      responses:
        '200':
          description: csv stream
          content:
            text/csv: {}
  /thing/{id}:
    parameters:
      - {name: id, in: path, required: true, schema: {type: string}}
    get:
      operationId: getThing
      parameters:
        - {name: cursor, in: query, schema: {type: string}}
      responses:
        '200':
          description: ok
          content: {application/json: {schema: {$ref: '#/components/schemas/Thing'}}}
  /thing:
    post:
      operationId: createThing
      parameters:
        - {name: Idempotency-Key, in: header, required: true, schema: {type: string}}
        - {name: X-Trace, in: header, schema: {type: string}}
      requestBody:
        required: true
        content: {application/json: {schema: {$ref: '#/components/schemas/Thing'}}}
      responses:
        '201':
          description: created
          content: {application/json: {schema: {$ref: '#/components/schemas/Thing'}}}
  /thing/{id}/versioned:
    get:
      operationId: getVersionedThing
      parameters:
        - {name: id, in: path, required: true, schema: {type: string}}
      responses:
        '200':
          description: ok
          headers:
            ETag: {schema: {type: string}}
            X-Total-Count: {schema: {type: integer, format: int64}}
          content: {application/json: {schema: {$ref: '#/components/schemas/Thing'}}}
  /thing/{id}/wipe:
    parameters:
      - {name: id, in: path, required: true, schema: {type: string}}
    delete:
      operationId: wipeThing
      responses:
        '204': {description: no content}
  /thing/by-num/{num}:
    get:
      operationId: getThingByNum
      parameters:
        - {name: num, in: path, required: true, schema: {type: integer, format: int64}}
      responses:
        '200':
          description: ok
          content: {application/json: {schema: {$ref: '#/components/schemas/Thing'}}}
  /thing/search:
    post:
      operationId: searchThings
      parameters:
        - {name: cursor, in: query, schema: {type: string}}
        - {name: limit, in: query, schema: {type: integer, format: int64}}
        - {name: sort_by, in: query, schema: {type: string, enum: [name, created_at]}}
      requestBody:
        required: true
        content: {application/json: {schema: {type: object, additionalProperties: true}}}
      responses:
        '200':
          description: ok
          content: {application/json: {schema: {$ref: '#/components/schemas/Thing'}}}
  /thing/bulk:
    post:
      operationId: bulkThings
      parameters:
        - {name: Content-Type, in: header, schema: {type: string}}
        - {name: skip_existing, in: query, schema: {type: boolean}}
      requestBody:
        required: true
        content:
          application/octet-stream: {}
      responses:
        '204': {description: accepted}
  /thing/missing:
    get:
      operationId: getMissing
      responses:
        '200':
          description: ok
          content: {application/json: {schema: {$ref: '#/components/schemas/Thing'}}}
components:
  schemas:
    Thing:
      type: object
      required: [id, name]
      properties:
        id: {type: string}
        name: {type: string}
`

// runWithGenerated writes the generated client into a temp module
// alongside main.go (provided as `mainSrc`) and runs `go run .` with
// BASE set to baseURL. Returns combined stdout+stderr; fails the test
// on non-zero exit (unless allowFail is true, in which case the
// subprocess output is returned regardless).
func runWithGenerated(baseURL, mainSrc string, allowFail ...bool) string {
	dir, err := os.MkdirTemp("", "typedgen-tx-")
	Expect(err).ToNot(HaveOccurred())
	DeferCleanup(os.RemoveAll, dir)

	files, err := typed.Generate(typed.Options{
		PackageName: "main",
		SpecBytes:   []byte(transportSpec),
	})
	Expect(err).ToNot(HaveOccurred())
	for name, data := range files {
		Expect(os.WriteFile(filepath.Join(dir, name), data, 0o644)).To(Succeed())
	}
	Expect(os.WriteFile(filepath.Join(dir, "main.go"), []byte(mainSrc), 0o644)).To(Succeed())
	Expect(os.WriteFile(
		filepath.Join(dir, "go.mod"),
		[]byte("module run\n\ngo 1.21\n"),
		0o644,
	)).To(Succeed())

	cmd := exec.Command("go", "run", ".")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "BASE="+baseURL)
	out, err := cmd.CombinedOutput()
	if len(allowFail) > 0 && allowFail[0] {
		return string(out)
	}
	Expect(err).ToNot(HaveOccurred(), "go run failed:\n%s", out)
	return string(out)
}

var _ = Describe("Generated transport behavior", func() {
	It("encodes the body as JSON and sets default Content-Type and Accept", func() {
		var (
			gotMethod      string
			gotPath        string
			gotContentType string
			gotAccept      string
			gotIdem        string
			gotTrace       string
			gotBody        map[string]any
		)
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			gotMethod = r.Method
			gotPath = r.URL.Path
			gotContentType = r.Header.Get("Content-Type")
			gotAccept = r.Header.Get("Accept")
			gotIdem = r.Header.Get("Idempotency-Key")
			gotTrace = r.Header.Get("X-Trace")
			_ = json.NewDecoder(r.Body).Decode(&gotBody)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(201)
			_ = json.NewEncoder(w).Encode(map[string]string{"id": "abc", "name": "n"})
		}))
		DeferCleanup(srv.Close)

		out := runWithGenerated(srv.URL, `package main
import (
	"context"
	"fmt"
	"os"
)
func main() {
	c := NewClient(os.Getenv("BASE"))
	trace := "t1"
	// CreateThing(ctx, idempotencyKey, xTrace, body)
	r, err := c.CreateThing(context.Background(), "key-1", &trace, Thing{Id: "abc", Name: "n"})
	if err != nil { fmt.Println("ERR", err); os.Exit(1) }
	fmt.Println(r.Id, r.Name)
}
`)
		_ = out
		Expect(gotMethod).To(Equal("POST"))
		Expect(gotPath).To(Equal("/thing"))
		Expect(gotContentType).To(Equal("application/json"))
		Expect(gotAccept).To(Equal("application/json"))
		Expect(gotIdem).To(Equal("key-1"))
		Expect(gotTrace).To(Equal("t1"))
		Expect(gotBody).To(Equal(map[string]any{"id": "abc", "name": "n"}))
	})

	It("encodes path params with PathEscape and forwards query strings", func() {
		var (
			gotEscapedPath string
			gotDecodedPath string
			gotQuery       string
		)
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			gotEscapedPath = r.URL.EscapedPath()
			gotDecodedPath = r.URL.Path
			gotQuery = r.URL.RawQuery
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"id":"x","name":"y"}`))
		}))
		DeferCleanup(srv.Close)

		runWithGenerated(srv.URL, `package main
import (
	"context"
	"fmt"
	"os"
)
func main() {
	c := NewClient(os.Getenv("BASE"))
	cur := "page=2"
	// GetThing(ctx, id, cursor)
	if _, err := c.GetThing(context.Background(), "weird id/with slash", &cur); err != nil {
		fmt.Println("ERR", err); os.Exit(1)
	}
}
`)
		// Wire form keeps spaces and slash escaped — single path segment.
		Expect(gotEscapedPath).To(Equal("/thing/weird%20id%2Fwith%20slash"))
		// Decoded form is what reverse-proxies see as r.URL.Path.
		Expect(gotDecodedPath).To(Equal("/thing/weird id/with slash"))
		// Query value with reserved chars is escaped via Encode().
		Expect(gotQuery).To(Equal("cursor=page%3D2"))
	})

	It("returns *APIError carrying status and body for non-2xx responses", func() {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/problem+json")
			w.WriteHeader(404)
			_, _ = w.Write([]byte(`{"title":"not found"}`))
		}))
		DeferCleanup(srv.Close)

		out := runWithGenerated(srv.URL, `package main
import (
	"context"
	"errors"
	"fmt"
	"os"
)
func main() {
	c := NewClient(os.Getenv("BASE"))
	_, err := c.GetMissing(context.Background())
	var ae *APIError
	if errors.As(err, &ae) {
		fmt.Printf("APIError(%d) %s\n", ae.StatusCode, string(ae.Body))
		return
	}
	fmt.Println("UNEXPECTED", err)
	os.Exit(1)
}
`)
		Expect(strings.TrimSpace(out)).To(Equal(`APIError(404) {"title":"not found"}`))
	})

	It("decodes an application/problem+json error body into APIError.Problem", func() {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			// Media type parameters must be tolerated.
			w.Header().Set("Content-Type", "application/problem+json; charset=utf-8")
			w.WriteHeader(422)
			_, _ = w.Write([]byte(`{
				"type": "https://example.com/errors/validation",
				"title": "Unprocessable Entity",
				"status": 422,
				"detail": "name is required",
				"instance": "/thing/missing",
				"errors": [{"location": "body.name", "message": "required"}]
			}`))
		}))
		DeferCleanup(srv.Close)

		out := runWithGenerated(srv.URL, `package main
import (
	"context"
	"errors"
	"fmt"
	"os"
)
func main() {
	c := NewClient(os.Getenv("BASE"))
	_, err := c.GetMissing(context.Background())
	var ae *APIError
	if !errors.As(err, &ae) || ae.Problem == nil {
		fmt.Println("UNEXPECTED", err); os.Exit(1)
	}
	p := ae.Problem
	fmt.Printf("%s | %s | %d | %s | %s | %s\n",
		p.Type, p.Title, p.Status, p.Detail, p.Instance, string(p.Extensions["errors"]))
}
`)
		Expect(strings.TrimSpace(out)).To(Equal(
			`https://example.com/errors/validation | Unprocessable Entity | 422 | name is required | /thing/missing | [{"location": "body.name", "message": "required"}]`))
	})

	It("keeps the raw Body and a nil Problem when the problem body is malformed", func() {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/problem+json")
			w.WriteHeader(500)
			_, _ = w.Write([]byte(`<oops, not json>`))
		}))
		DeferCleanup(srv.Close)

		out := runWithGenerated(srv.URL, `package main
import (
	"context"
	"errors"
	"fmt"
	"os"
)
func main() {
	c := NewClient(os.Getenv("BASE"))
	_, err := c.GetMissing(context.Background())
	var ae *APIError
	if !errors.As(err, &ae) {
		fmt.Println("UNEXPECTED", err); os.Exit(1)
	}
	fmt.Printf("%d problem=%v body=%s\n", ae.StatusCode, ae.Problem, string(ae.Body))
}
`)
		Expect(strings.TrimSpace(out)).To(Equal(`500 problem=<nil> body=<oops, not json>`))
	})

	It("decodes plain application/json error objects the same way", func() {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(409)
			_, _ = w.Write([]byte(`{"title":"Conflict","code":"already_exists"}`))
		}))
		DeferCleanup(srv.Close)

		out := runWithGenerated(srv.URL, `package main
import (
	"context"
	"errors"
	"fmt"
	"os"
)
func main() {
	c := NewClient(os.Getenv("BASE"))
	_, err := c.GetMissing(context.Background())
	var ae *APIError
	if !errors.As(err, &ae) || ae.Problem == nil {
		fmt.Println("UNEXPECTED", err); os.Exit(1)
	}
	fmt.Printf("%s | %s\n", ae.Problem.Title, string(ae.Problem.Extensions["code"]))
}
`)
		Expect(strings.TrimSpace(out)).To(Equal(`Conflict | "already_exists"`))
	})

	It("leaves Problem nil for non-JSON error content types", func() {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "text/html")
			w.WriteHeader(502)
			_, _ = w.Write([]byte(`<html>bad gateway</html>`))
		}))
		DeferCleanup(srv.Close)

		out := runWithGenerated(srv.URL, `package main
import (
	"context"
	"errors"
	"fmt"
	"os"
)
func main() {
	c := NewClient(os.Getenv("BASE"))
	_, err := c.GetMissing(context.Background())
	var ae *APIError
	if !errors.As(err, &ae) {
		fmt.Println("UNEXPECTED", err); os.Exit(1)
	}
	fmt.Printf("%d problem=%v\n", ae.StatusCode, ae.Problem)
}
`)
		Expect(strings.TrimSpace(out)).To(Equal(`502 problem=<nil>`))
	})

	It("exposes the response headers on APIError", func() {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Retry-After", "17")
			w.WriteHeader(429)
			_, _ = w.Write([]byte(`slow down`))
		}))
		DeferCleanup(srv.Close)

		out := runWithGenerated(srv.URL, `package main
import (
	"context"
	"errors"
	"fmt"
	"os"
)
func main() {
	c := NewClient(os.Getenv("BASE"))
	_, err := c.GetMissing(context.Background())
	var ae *APIError
	if !errors.As(err, &ae) || ae.Header == nil {
		fmt.Println("UNEXPECTED", err); os.Exit(1)
	}
	fmt.Printf("%d retry-after=%s\n", ae.StatusCode, ae.Header.Get("Retry-After"))
}
`)
		Expect(strings.TrimSpace(out)).To(Equal(`429 retry-after=17`))
	})

	It("returns nil on a 204 with no result type", func() {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(204)
		}))
		DeferCleanup(srv.Close)

		out := runWithGenerated(srv.URL, `package main
import (
	"context"
	"fmt"
	"os"
)
func main() {
	c := NewClient(os.Getenv("BASE"))
	if err := c.WipeThing(context.Background(), "x"); err != nil {
		fmt.Println("ERR", err); os.Exit(1)
	}
	fmt.Println("OK")
}
`)
		Expect(strings.TrimSpace(out)).To(Equal("OK"))
	})

	It("streams an application/octet-stream body without buffering it", func() {
		var (
			gotContentType   string
			gotContentLength int64
			gotBody          string
		)
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			gotContentType = r.Header.Get("Content-Type")
			gotContentLength = r.ContentLength
			raw, _ := io.ReadAll(r.Body)
			gotBody = string(raw)
			w.WriteHeader(204)
		}))
		DeferCleanup(srv.Close)

		out := runWithGenerated(srv.URL, `package main
import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"
)

// opaque hides the concrete reader type so net/http cannot sniff a
// length from it — a buffered upload would have a Content-Length.
type opaque struct{ r io.Reader }

func (o opaque) Read(p []byte) (int, error) { return o.r.Read(p) }

func main() {
	c := NewClient(os.Getenv("BASE"))
	// ImportThings(ctx, body io.Reader)
	if err := c.ImportThings(context.Background(), opaque{strings.NewReader("raw,bytes\n1,2\n")}); err != nil {
		fmt.Println("ERR", err); os.Exit(1)
	}
	fmt.Println("OK")
}
`)
		Expect(strings.TrimSpace(out)).To(Equal("OK"))
		Expect(gotContentType).To(Equal("application/octet-stream"))
		Expect(gotBody).To(Equal("raw,bytes\n1,2\n"))
		// -1 means chunked transfer — the reader was streamed, not
		// buffered into memory first.
		Expect(gotContentLength).To(Equal(int64(-1)))
	})

	It("returns a non-JSON response body as an io.ReadCloser and sets Accept", func() {
		var gotAccept string
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			gotAccept = r.Header.Get("Accept")
			w.Header().Set("Content-Type", "text/csv")
			w.WriteHeader(200)
			_, _ = w.Write([]byte("id,name\n1,alice\n"))
		}))
		DeferCleanup(srv.Close)

		out := runWithGenerated(srv.URL, `package main
import (
	"context"
	"fmt"
	"io"
	"os"
)
func main() {
	c := NewClient(os.Getenv("BASE"))
	// ExportThings(ctx) (io.ReadCloser, error) — caller closes.
	body, err := c.ExportThings(context.Background())
	if err != nil { fmt.Println("ERR", err); os.Exit(1) }
	defer body.Close()
	raw, err := io.ReadAll(body)
	if err != nil { fmt.Println("ERR", err); os.Exit(1) }
	fmt.Print(string(raw))
}
`)
		Expect(out).To(Equal("id,name\n1,alice\n"))
		Expect(gotAccept).To(Equal("text/csv"))
	})

	It("decodes declared response headers into the typed headers struct", func() {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("ETag", `"v42"`)
			w.Header().Set("X-Total-Count", "17")
			w.WriteHeader(200)
			_, _ = w.Write([]byte(`{"id":"x","name":"y"}`))
		}))
		DeferCleanup(srv.Close)

		out := runWithGenerated(srv.URL, `package main
import (
	"context"
	"fmt"
	"os"
)
func main() {
	c := NewClient(os.Getenv("BASE"))
	// GetVersionedThing(ctx, id) (*Thing, GetVersionedThingResponseHeaders, error)
	thing, h, err := c.GetVersionedThing(context.Background(), "x")
	if err != nil { fmt.Println("ERR", err); os.Exit(1) }
	fmt.Printf("%s etag=%s total=%d\n", thing.Name, h.Etag, h.XTotalCount)
}
`)
		Expect(strings.TrimSpace(out)).To(Equal(`y etag="v42" total=17`))
	})

	It("leaves undeclared or malformed response header values at their zero value", func() {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			// No ETag; X-Total-Count is not an integer.
			w.Header().Set("X-Total-Count", "many")
			w.WriteHeader(200)
			_, _ = w.Write([]byte(`{"id":"x","name":"y"}`))
		}))
		DeferCleanup(srv.Close)

		out := runWithGenerated(srv.URL, `package main
import (
	"context"
	"fmt"
	"os"
)
func main() {
	c := NewClient(os.Getenv("BASE"))
	_, h, err := c.GetVersionedThing(context.Background(), "x")
	if err != nil { fmt.Println("ERR", err); os.Exit(1) }
	fmt.Printf("etag=%q total=%d\n", h.Etag, h.XTotalCount)
}
`)
		Expect(strings.TrimSpace(out)).To(Equal(`etag="" total=0`))
	})

	It("merges DefaultHeaders, with per-call headers overriding by Set semantics", func() {
		var (
			gotIdem string
			gotAuth string
		)
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			gotIdem = r.Header.Get("Idempotency-Key")
			gotAuth = r.Header.Get("Authorization")
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(201)
			_, _ = w.Write([]byte(`{"id":"x","name":"y"}`))
		}))
		DeferCleanup(srv.Close)

		runWithGenerated(srv.URL, `package main
import (
	"context"
	"fmt"
	"net/http"
	"os"
)
func main() {
	c := NewClient(os.Getenv("BASE"))
	c.DefaultHeaders = http.Header{
		"Authorization": []string{"Bearer top-level"},
		"Idempotency-Key": []string{"will-be-overridden"},
	}
	// CreateThing(ctx, idempotencyKey, xTrace, body)
	if _, err := c.CreateThing(context.Background(), "per-call", nil, Thing{Id: "x", Name: "y"}); err != nil {
		fmt.Println("ERR", err); os.Exit(1)
	}
}
`)
		Expect(gotAuth).To(Equal("Bearer top-level"))
		Expect(gotIdem).To(Equal("per-call"))
	})

	It("formats integer path params as base-10 in the path", func() {
		var gotPath string
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			gotPath = r.URL.Path
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"id":"x","name":"y"}`))
		}))
		DeferCleanup(srv.Close)

		runWithGenerated(srv.URL, `package main
import (
	"context"
	"fmt"
	"os"
)
func main() {
	c := NewClient(os.Getenv("BASE"))
	// GetThingByNum(ctx, num int64)
	if _, err := c.GetThingByNum(context.Background(), 42); err != nil {
		fmt.Println("ERR", err); os.Exit(1)
	}
}
`)
		Expect(gotPath).To(Equal("/thing/by-num/42"))
	})

	It("sends a JSON body and int64/enum query params on the same operation, omitting nil ones", func() {
		var (
			gotQuery url.Values
			gotBody  map[string]any
		)
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			gotQuery = r.URL.Query()
			_ = json.NewDecoder(r.Body).Decode(&gotBody)
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"id":"x","name":"y"}`))
		}))
		DeferCleanup(srv.Close)

		runWithGenerated(srv.URL, `package main
import (
	"context"
	"fmt"
	"os"
)
func main() {
	c := NewClient(os.Getenv("BASE"))
	limit := int64(25)
	sortBy := SearchThingsSortByCreatedAt
	// SearchThings(ctx, cursor, limit, sortBy, body map[string]any) —
	// a free-form (additionalProperties: true) body generates as
	// map[string]any; cursor stays nil to prove omission.
	filter := map[string]any{"and": []any{map[string]any{"field": "name", "op": "eq", "value": "x"}}}
	if _, err := c.SearchThings(context.Background(), nil, &limit, &sortBy, filter); err != nil {
		fmt.Println("ERR", err); os.Exit(1)
	}
}
`)
		Expect(gotQuery.Get("limit")).To(Equal("25"))
		Expect(gotQuery.Get("sort_by")).To(Equal("created_at"))
		Expect(gotQuery).NotTo(HaveKey("cursor"))
		Expect(gotBody).To(Equal(map[string]any{
			"and": []any{map[string]any{"field": "name", "op": "eq", "value": "x"}},
		}))
	})

	It("lets a Content-Type header param override the spec media type on a streamed upload", func() {
		var (
			gotContentType string
			gotQuery       url.Values
			gotBody        string
		)
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			gotContentType = r.Header.Get("Content-Type")
			gotQuery = r.URL.Query()
			raw, _ := io.ReadAll(r.Body)
			gotBody = string(raw)
			w.WriteHeader(204)
		}))
		DeferCleanup(srv.Close)

		runWithGenerated(srv.URL, `package main
import (
	"context"
	"fmt"
	"os"
	"strings"
)
func main() {
	c := NewClient(os.Getenv("BASE"))
	ct := "application/x-ndjson"
	skip := true
	// BulkThings(ctx, contentType, skipExisting, body io.Reader)
	if err := c.BulkThings(context.Background(), &ct, &skip, strings.NewReader("{\"a\":1}\n")); err != nil {
		fmt.Println("ERR", err); os.Exit(1)
	}
}
`)
		Expect(gotContentType).To(Equal("application/x-ndjson"))
		Expect(gotQuery.Get("skip_existing")).To(Equal("true"))
		Expect(gotBody).To(Equal("{\"a\":1}\n"))
	})

	It("falls back to the spec media type and omits nil optional params on a streamed upload", func() {
		var (
			gotContentType string
			gotQuery       url.Values
		)
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			gotContentType = r.Header.Get("Content-Type")
			gotQuery = r.URL.Query()
			w.WriteHeader(204)
		}))
		DeferCleanup(srv.Close)

		runWithGenerated(srv.URL, `package main
import (
	"context"
	"fmt"
	"os"
	"strings"
)
func main() {
	c := NewClient(os.Getenv("BASE"))
	if err := c.BulkThings(context.Background(), nil, nil, strings.NewReader("x")); err != nil {
		fmt.Println("ERR", err); os.Exit(1)
	}
}
`)
		Expect(gotContentType).To(Equal("application/octet-stream"))
		Expect(gotQuery).NotTo(HaveKey("skip_existing"))
	})

	It("propagates ctx cancellation through to the request", func() {
		// We don't observe the cancellation server-side; we just make sure
		// the call returns a non-nil error promptly when ctx is canceled.
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(204)
		}))
		DeferCleanup(srv.Close)

		out := runWithGenerated(srv.URL, `package main
import (
	"context"
	"errors"
	"fmt"
	"os"
)
func main() {
	c := NewClient(os.Getenv("BASE"))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := c.WipeThing(ctx, "x")
	if err == nil {
		fmt.Println("EXPECTED ERROR"); os.Exit(1)
	}
	if !errors.Is(err, context.Canceled) {
		fmt.Println("UNEXPECTED", err); os.Exit(1)
	}
	fmt.Println("OK")
}
`)
		Expect(strings.TrimSpace(out)).To(Equal("OK"))
	})
})
