package typed_test

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/jathanism/okapi/gen/typed"
)

const tinySpec = `
openapi: 3.1.0
info: {title: t, version: 0.0.1}
paths:
  /x:
    get:
      operationId: getX
      responses: {'204': {description: ok}}
`

var _ = Describe("Source loading", func() {
	It("reads a file:// URL", func() {
		dir, err := os.MkdirTemp("", "typedgen-src-")
		Expect(err).ToNot(HaveOccurred())
		DeferCleanup(os.RemoveAll, dir)
		path := filepath.Join(dir, "spec.yaml")
		Expect(os.WriteFile(path, []byte(tinySpec), 0o644)).To(Succeed())

		files, err := typed.Generate(typed.Options{
			PackageName: "demo",
			Source:      "file://" + path,
		})
		Expect(err).ToNot(HaveOccurred())
		Expect(string(files["client.gen.go"])).To(ContainSubstring("GetX"))
	})

	It("reads a bare path (no scheme)", func() {
		dir, err := os.MkdirTemp("", "typedgen-src-")
		Expect(err).ToNot(HaveOccurred())
		DeferCleanup(os.RemoveAll, dir)
		path := filepath.Join(dir, "spec.yaml")
		Expect(os.WriteFile(path, []byte(tinySpec), 0o644)).To(Succeed())

		files, err := typed.Generate(typed.Options{
			PackageName: "demo",
			Source:      path,
		})
		Expect(err).ToNot(HaveOccurred())
		Expect(string(files["client.gen.go"])).To(ContainSubstring("GetX"))
	})

	It("reads an http URL", func() {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/yaml")
			_, _ = w.Write([]byte(tinySpec))
		}))
		DeferCleanup(srv.Close)

		files, err := typed.Generate(typed.Options{
			PackageName: "demo",
			Source:      srv.URL + "/openapi.yaml",
		})
		Expect(err).ToNot(HaveOccurred())
		Expect(string(files["client.gen.go"])).To(ContainSubstring("GetX"))
	})

	It("returns a clear error for a missing file", func() {
		_, err := typed.Generate(typed.Options{
			PackageName: "demo",
			Source:      "file:///does/not/exist/spec.yaml",
		})
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("read spec"))
	})

	It("returns a clear error for a non-200 HTTP response", func() {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		}))
		DeferCleanup(srv.Close)
		_, err := typed.Generate(typed.Options{
			PackageName: "demo",
			Source:      srv.URL,
		})
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("status 500"))
	})

	It("returns a parse error for malformed YAML", func() {
		_, err := typed.Generate(typed.Options{
			PackageName: "demo",
			SpecBytes:   []byte("not: a: valid: yaml: structure: ::::"),
		})
		Expect(err).To(HaveOccurred())
		// libopenapi surfaces parse failures; we just check we got an error.
		Expect(err.Error()).ToNot(BeEmpty())
	})

	It("returns an error for a YAML doc that isn't an OpenAPI spec", func() {
		_, err := typed.Generate(typed.Options{
			PackageName: "demo",
			SpecBytes:   []byte("just_some: yaml\nnothing: openapi"),
		})
		Expect(err).To(HaveOccurred())
		Expect(strings.ToLower(err.Error())).To(SatisfyAny(
			ContainSubstring("openapi"),
			ContainSubstring("parse"),
			ContainSubstring("build"),
		))
	})
})
