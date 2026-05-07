package typed_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
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
const transportSpec = `
openapi: 3.1.0
info: {title: t, version: 0.0.1}
paths:
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
  /thing/{id}/wipe:
    parameters:
      - {name: id, in: path, required: true, schema: {type: string}}
    delete:
      operationId: wipeThing
      responses:
        '204': {description: no content}
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

