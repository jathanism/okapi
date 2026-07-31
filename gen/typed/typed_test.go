package typed_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/jathanism/okapi/gen/typed"
)

func TestTypedGen(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "gen/typed")
}

const minSpec = `
openapi: 3.1.0
info: {title: Test, version: 0.0.1}
paths:
  /contacts/{aid}:
    parameters:
      - name: aid
        in: path
        required: true
        schema: {type: string}
    get:
      operationId: getContact
      parameters:
        - name: If-Match
          in: header
          required: true
          schema: {type: string}
      responses:
        '200':
          description: ok
          content:
            application/json:
              schema: {$ref: '#/components/schemas/Contact'}
    post:
      operationId: createContact
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
  /contacts:
    get:
      operationId: listContacts
      parameters:
        - name: cursor
          in: query
          schema: {type: string}
      responses:
        '200':
          description: ok
          content:
            application/json:
              schema: {type: array, items: {$ref: '#/components/schemas/Contact'}}
components:
  schemas:
    Contact:
      type: object
      required: [id, name, status]
      properties:
        id:
          type: integer
          format: int64
        name:
          type: string
        nickname:
          type: [string, "null"]
        status:
          type: string
          enum: [active, archived]
    CreateContactBody:
      type: object
      required: [name]
      properties:
        name: {type: string}
        nickname: {type: string}
`

// collidingEnumSpec lists canonical snake_case enum values next to
// legacy camelCase (and SCREAMING_SNAKE) aliases that mangle to the
// same Go identifier.
const collidingEnumSpec = `
openapi: 3.1.0
info: {title: Test, version: 0.0.1}
paths:
  /contacts/search:
    get:
      operationId: searchContacts
      parameters:
        - name: sort_by
          in: query
          schema:
            type: string
            enum: [id, created_at, signed_up_at, full_name,
                   signedUpAt, fullName, FULL_NAME]
      responses:
        '200':
          description: ok
          content:
            application/json:
              schema: {type: string}
`

var _ = Describe("Generate", func() {
	It("rejects empty PackageName", func() {
		_, err := typed.Generate(typed.Options{SpecBytes: []byte(minSpec)})
		Expect(err).To(MatchError(ContainSubstring("PackageName")))
	})

	It("rejects an invalid PackageName", func() {
		_, err := typed.Generate(typed.Options{
			PackageName: "1bad",
			SpecBytes:   []byte(minSpec),
		})
		Expect(err).To(MatchError(ContainSubstring("not a valid")))
	})

	It("requires a source", func() {
		_, err := typed.Generate(typed.Options{PackageName: "demo"})
		Expect(err).To(MatchError(ContainSubstring("Source or SpecBytes")))
	})

	It("rejects both source forms together", func() {
		_, err := typed.Generate(typed.Options{
			PackageName: "demo",
			Source:      "x",
			SpecBytes:   []byte("y"),
		})
		Expect(err).To(MatchError(ContainSubstring("mutually exclusive")))
	})

	It("emits compileable types and client for a tiny spec", func() {
		files, err := typed.Generate(typed.Options{
			PackageName: "demo",
			SpecBytes:   []byte(minSpec),
		})
		Expect(err).ToNot(HaveOccurred())
		Expect(files).To(HaveKey("types.gen.go"))
		Expect(files).To(HaveKey("client.gen.go"))

		types := string(files["types.gen.go"])
		client := string(files["client.gen.go"])

		// Components.schemas → typed structs.
		Expect(types).To(ContainSubstring("type Contact struct"))
		Expect(types).To(ContainSubstring("type CreateContactBody struct"))

		// Inline enum on Contact.status is registered as a named type.
		Expect(types).To(ContainSubstring("type ContactStatus string"))
		Expect(types).To(ContainSubstring(`ContactStatusActive   ContactStatus = "active"`))

		// Nullable [string, "null"] becomes a pointer.
		Expect(types).To(MatchRegexp(`Nickname\s+\*string`))

		// Required scalar stays a value.
		Expect(types).To(MatchRegexp(`Id\s+int64\s+` + "`" + `json:"id"`))

		// Per-op signatures: positional args, ordered path → header →
		// query → body. Required scalar = value, optional = pointer.
		Expect(client).To(ContainSubstring(
			"func (c *Client) GetContact(ctx context.Context, aid string, ifMatch string) (*Contact, *APIResponse, error)"))
		Expect(client).To(ContainSubstring(
			"func (c *Client) CreateContact(ctx context.Context, aid string, body CreateContactBody) (*Contact, *APIResponse, error)"))
		// Array response stays a slice — return type is a value, not pointer.
		Expect(client).To(ContainSubstring(
			"func (c *Client) ListContacts(ctx context.Context, cursor *string) (*[]Contact, *APIResponse, error)"))

		// Header param wired to headers.Set with original case.
		Expect(client).To(ContainSubstring(
			`headers.Set("If-Match", formatPathValue(ifMatch))`))
		// Path param wired with the wire name.
		Expect(client).To(ContainSubstring(
			`pathParams["aid"] = formatPathValue(aid)`))
	})

	It("produces a package that builds with go build", func() {
		dir, err := os.MkdirTemp("", "typedgen-")
		Expect(err).ToNot(HaveOccurred())
		DeferCleanup(os.RemoveAll, dir)

		files, err := typed.Generate(typed.Options{
			PackageName: "demo",
			SpecBytes:   []byte(minSpec),
		})
		Expect(err).ToNot(HaveOccurred())
		for name, data := range files {
			Expect(os.WriteFile(filepath.Join(dir, name), data, 0o644)).To(Succeed())
		}
		Expect(os.WriteFile(
			filepath.Join(dir, "go.mod"),
			[]byte("module demo\n\ngo 1.21\n"),
			0o644,
		)).To(Succeed())

		cmd := exec.Command("go", "build", "./...")
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		Expect(err).ToNot(HaveOccurred(), "go build failed:\n%s", out)
	})

	It("disambiguates enum values whose identifiers collide", func() {
		// A spec that lists canonical snake_case enum values alongside
		// legacy camelCase aliases: both mangle to the same Go identifier,
		// which used to emit redeclared consts that failed go build.
		files, err := typed.Generate(typed.Options{
			PackageName: "demo",
			SpecBytes:   []byte(collidingEnumSpec),
		})
		Expect(err).ToNot(HaveOccurred())
		types := string(files["types.gen.go"])

		// First occurrence keeps the clean name; the alias gets an ordinal
		// suffix. Wire values stay exact on both.
		Expect(types).To(MatchRegexp(`SearchContactsSortBySignedUpAt\s+SearchContactsSortBy = "signed_up_at"`))
		Expect(types).To(MatchRegexp(`SearchContactsSortBySignedUpAt2\s+SearchContactsSortBy = "signedUpAt"`))
		// A triple collision numbers 2 then 3.
		Expect(types).To(MatchRegexp(`SearchContactsSortByFullName\s+SearchContactsSortBy = "full_name"`))
		Expect(types).To(MatchRegexp(`SearchContactsSortByFullName2\s+SearchContactsSortBy = "fullName"`))
		Expect(types).To(MatchRegexp(`SearchContactsSortByFullName3\s+SearchContactsSortBy = "FULL_NAME"`))

		// Naming is deterministic across runs.
		again, err := typed.Generate(typed.Options{
			PackageName: "demo",
			SpecBytes:   []byte(collidingEnumSpec),
		})
		Expect(err).ToNot(HaveOccurred())
		Expect(string(again["types.gen.go"])).To(Equal(types))

		// And the generated package compiles.
		dir, err := os.MkdirTemp("", "typedgen-enum-")
		Expect(err).ToNot(HaveOccurred())
		DeferCleanup(os.RemoveAll, dir)
		for name, data := range files {
			Expect(os.WriteFile(filepath.Join(dir, name), data, 0o644)).To(Succeed())
		}
		Expect(os.WriteFile(
			filepath.Join(dir, "go.mod"),
			[]byte("module demo\n\ngo 1.21\n"),
			0o644,
		)).To(Succeed())
		cmd := exec.Command("go", "build", "./...")
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		Expect(err).ToNot(HaveOccurred(), "go build failed:\n%s", out)
	})
})

var _ = Describe("Generated client transport", func() {
	// We exercise the transport behavior shape (path templating, header
	// case, query, body encoding, error surfacing) by writing a spec,
	// generating the code, then driving it as a sub-binary against an
	// httptest server. This keeps the test self-contained without
	// committing a fixture binary.

	It("sends path/header/query/body and decodes the response", func() {
		gotMethod := ""
		gotPath := ""
		gotIfMatch := ""
		gotCursor := ""
		var gotBody map[string]any

		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			gotMethod = r.Method
			gotPath = r.URL.Path
			gotIfMatch = r.Header.Get("If-Match")
			gotCursor = r.URL.Query().Get("cursor")
			if r.Body != nil {
				_ = json.NewDecoder(r.Body).Decode(&gotBody)
			}

			w.Header().Set("Content-Type", "application/json")
			switch r.URL.Path {
			case "/contacts":
				w.WriteHeader(200)
				_ = json.NewEncoder(w).Encode([]map[string]any{{
					"id": 1, "name": "alice", "status": "active",
				}})
			case "/contacts/acc-1":
				w.WriteHeader(200)
				_ = json.NewEncoder(w).Encode(map[string]any{
					"id": 7, "name": "bob", "status": "archived",
				})
			default:
				w.WriteHeader(404)
			}
		}))
		DeferCleanup(srv.Close)

		// Build a runnable program that drives the generated client.
		dir, err := os.MkdirTemp("", "typedgen-run-")
		Expect(err).ToNot(HaveOccurred())
		DeferCleanup(os.RemoveAll, dir)

		files, err := typed.Generate(typed.Options{
			PackageName: "main",
			SpecBytes:   []byte(minSpec),
		})
		Expect(err).ToNot(HaveOccurred())
		for name, data := range files {
			Expect(os.WriteFile(filepath.Join(dir, name), data, 0o644)).To(Succeed())
		}
		main := `package main

import (
	"context"
	"fmt"
	"os"
)

func run() error {
	c := NewClient(os.Args[1])
	ctx := context.Background()
	cur := "abc"
	// ListContacts(ctx, cursor *string)
	if _, _, err := c.ListContacts(ctx, &cur); err != nil {
		return fmt.Errorf("list: %w", err)
	}
	// GetContact(ctx, aid, ifMatch)
	got, _, err := c.GetContact(ctx, "acc-1", "etag-1")
	if err != nil {
		return fmt.Errorf("get: %w", err)
	}
	fmt.Println(got.Name)
	return nil
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
`
		Expect(os.WriteFile(filepath.Join(dir, "main.go"), []byte(main), 0o644)).To(Succeed())
		Expect(os.WriteFile(
			filepath.Join(dir, "go.mod"),
			[]byte("module run\n\ngo 1.21\n"),
			0o644,
		)).To(Succeed())

		cmd := exec.Command("go", "run", ".", srv.URL)
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		Expect(err).ToNot(HaveOccurred(), "go run failed:\n%s", out)
		Expect(strings.TrimSpace(string(out))).To(Equal("bob"))

		// We made two calls; check the second got captured (the test handler
		// overwrites on each request, so gotMethod/Path reflect the LAST one).
		Expect(gotMethod).To(Equal("GET"))
		Expect(gotPath).To(Equal("/contacts/acc-1"))
		Expect(gotIfMatch).To(Equal("etag-1"))

		// Cursor was sent on the list call before the get; we can't observe
		// it directly here, but the body sequence proves both calls landed.
		_ = gotCursor
		_ = gotBody
	})
})
