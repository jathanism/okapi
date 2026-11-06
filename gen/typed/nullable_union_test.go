package typed_test

import (
	"os"
	"os/exec"
	"path/filepath"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/jathanism/okapi/gen/typed"
)

// nullableUnionSpec declares a component M with one property p whose
// schema is the given YAML, required or not, plus a referenced struct
// Brand for $ref members.
func nullableUnionSpec(prop string, required bool) []byte {
	req := ""
	if required {
		req = "      required: [p]\n"
	}
	return []byte(`
openapi: 3.1.0
info: {title: t, version: 0.0.1}
paths: {}
components:
  schemas:
    Brand:
      type: object
      required: [id]
      properties:
        id: {type: string}
    M:
      type: object
` + req + `      properties:
        p:
` + indent("          ", prop) + `
`)
}

var _ = Describe("Nullable unions (anyOf/oneOf [X, null])", func() {
	DescribeTable("render X made nilable",
		func(prop string, required bool, want string) {
			out := generateTypes(nullableUnionSpec(prop, required))
			Expect(out).To(MatchRegexp(want))
			Expect(out).ToNot(ContainSubstring("P any"))
			Expect(out).ToNot(ContainSubstring("**"))
		},
		Entry("anyOf, $ref first, required",
			`anyOf: [{$ref: '#/components/schemas/Brand'}, {type: "null"}]`, true,
			`P\s+\*Brand\s+`+"`"+`json:"p"`+"`"),
		Entry("anyOf, null first, required",
			`anyOf: [{type: "null"}, {$ref: '#/components/schemas/Brand'}]`, true,
			`P\s+\*Brand\s+`+"`"+`json:"p"`+"`"),
		Entry("oneOf, $ref first, required",
			`oneOf: [{$ref: '#/components/schemas/Brand'}, {type: "null"}]`, true,
			`P\s+\*Brand\s+`+"`"+`json:"p"`+"`"),
		Entry("oneOf, null first, required",
			`oneOf: [{type: "null"}, {$ref: '#/components/schemas/Brand'}]`, true,
			`P\s+\*Brand\s+`+"`"+`json:"p"`+"`"),
		Entry("anyOf $ref, optional",
			`anyOf: [{$ref: '#/components/schemas/Brand'}, {type: "null"}]`, false,
			`P\s+\*Brand\s+`+"`"+`json:"p,omitempty"`+"`"),
		Entry("anyOf $ref with a sibling description",
			"description: The brand.\nanyOf: [{$ref: '#/components/schemas/Brand'}, {type: \"null\"}]", true,
			`(?s)// The brand\.\s+P\s+\*Brand\s+`+"`"+`json:"p"`+"`"),
		Entry("anyOf inline object, required",
			"anyOf:\n  - type: object\n    properties:\n      id: {type: string}\n  - type: \"null\"", true,
			`P\s+\*Mp\s+`+"`"+`json:"p"`+"`"),
		Entry("anyOf inline object, null first",
			"anyOf:\n  - type: \"null\"\n  - type: object\n    properties:\n      id: {type: string}", true,
			`P\s+\*Mp\s+`+"`"+`json:"p"`+"`"),
		Entry("anyOf string, required",
			`anyOf: [{type: string}, {type: "null"}]`, true,
			`P\s+\*string\s+`+"`"+`json:"p"`+"`"),
		Entry("anyOf integer, null first, required",
			`anyOf: [{type: "null"}, {type: integer, format: int32}]`, true,
			`P\s+\*int32\s+`+"`"+`json:"p"`+"`"),
		Entry("anyOf string, optional",
			`anyOf: [{type: string}, {type: "null"}]`, false,
			`P\s+\*string\s+`+"`"+`json:"p,omitempty"`+"`"),
	)

	// Slices and maps are already nil-able, so nullability adds no
	// pointer: a required one is a bare slice/map. An optional one keeps
	// the optional-collection rule (pointer, so omit and [] / {} stay
	// distinct), exactly as `type: [array, "null"]` renders.
	DescribeTable("slices and maps take no pointer for nullability",
		func(prop string, required bool, want string) {
			out := generateTypes(nullableUnionSpec(prop, required))
			Expect(out).To(MatchRegexp(want))
			Expect(out).ToNot(ContainSubstring("P any"))
		},
		Entry("anyOf array, required",
			`anyOf: [{type: array, items: {type: string}}, {type: "null"}]`, true,
			`P\s+\[\]string\s+`+"`"+`json:"p"`+"`"),
		Entry("anyOf array, null first, required",
			`anyOf: [{type: "null"}, {type: array, items: {$ref: '#/components/schemas/Brand'}}]`, true,
			`P\s+\[\]Brand\s+`+"`"+`json:"p"`+"`"),
		Entry("anyOf map, required",
			`anyOf: [{type: object, additionalProperties: {type: string}}, {type: "null"}]`, true,
			`P\s+map\[string\]string\s+`+"`"+`json:"p"`+"`"),
		Entry("oneOf map, null first, required",
			`oneOf: [{type: "null"}, {type: object, additionalProperties: {type: integer}}]`, true,
			`P\s+map\[string\]int64\s+`+"`"+`json:"p"`+"`"),
		Entry("anyOf array, optional (same as type: [array, null])",
			`anyOf: [{type: array, items: {type: string}}, {type: "null"}]`, false,
			`P\s+\*\[\]string\s+`+"`"+`json:"p,omitempty"`+"`"),
		Entry("type: [object, null] with additionalProperties, optional, stays a map",
			"type: [object, \"null\"]\nadditionalProperties: {type: string}", false,
			`P\s+\*map\[string\]string\s+`+"`"+`json:"p,omitempty"`+"`"),
	)

	DescribeTable("every other union stays any",
		func(prop string) {
			out := generateTypes(nullableUnionSpec(prop, true))
			Expect(out).To(MatchRegexp(`P\s+any\s+` + "`" + `json:"p"` + "`"))
		},
		Entry("anyOf of two refs",
			`anyOf: [{$ref: '#/components/schemas/Brand'}, {$ref: '#/components/schemas/M'}]`),
		Entry("oneOf of two scalars",
			`oneOf: [{type: string}, {type: integer}]`),
		Entry("anyOf with three members including null",
			`anyOf: [{$ref: '#/components/schemas/Brand'}, {type: string}, {type: "null"}]`),
		Entry("anyOf of null and a multi-type member",
			`anyOf: [{type: "null"}, {type: [string, integer]}]`),
		Entry("anyOf of null and a nested union",
			`anyOf: [{type: "null"}, {anyOf: [{type: string}, {type: integer}]}]`),
		Entry("anyOf of null and an untyped member",
			`anyOf: [{type: "null"}, {description: anything}]`),
		Entry("anyOf of two nulls",
			`anyOf: [{type: "null"}, {type: "null"}]`),
		Entry("anyOf with a single member",
			`anyOf: [{$ref: '#/components/schemas/Brand'}]`),
		Entry("anyOf [X, null] alongside a oneOf",
			"anyOf: [{$ref: '#/components/schemas/Brand'}, {type: \"null\"}]\noneOf: [{type: string}, {type: integer}]"),
		Entry("anyOf [X, null] alongside allOf",
			"anyOf: [{$ref: '#/components/schemas/Brand'}, {type: \"null\"}]\nallOf: [{type: string}, {type: integer}]"),
	)
})

// The fixture spec is shaped like the local-numbers service's: response
// structs whose object members are `anyOf: [{$ref}, {type: "null"}]`, the
// form that service sends as null, alongside a nullable map and request
// strings declared `type: [string, "null"]`.
var _ = Describe("Nullable reference golden fixture", func() {
	const fixtureDir = "testdata/nullable_ref"

	generateFixture := func(pkg string) typed.Files {
		files, err := typed.Generate(typed.Options{
			PackageName: pkg,
			Source:      "file://" + filepath.Join(fixtureDir, "openapi.yaml"),
		})
		Expect(err).ToNot(HaveOccurred())
		return files
	}

	It("matches the golden output (UPDATE_GOLDEN=1 rewrites it)", func() {
		files := generateFixture("localnumbers")
		for _, name := range []string{"types.gen.go", "client.gen.go"} {
			golden := filepath.Join(fixtureDir, name+".golden")
			if os.Getenv("UPDATE_GOLDEN") != "" {
				Expect(os.WriteFile(golden, files[name], 0o644)).To(Succeed())
			}
			want, err := os.ReadFile(golden)
			Expect(err).ToNot(HaveOccurred())
			Expect(string(files[name])).To(Equal(string(want)), "%s differs from %s", name, golden)
		}
	})

	It("decodes null and an absent member to a nil pointer", func() {
		dir, err := os.MkdirTemp("", "typedgen-nullable-")
		Expect(err).ToNot(HaveOccurred())
		DeferCleanup(os.RemoveAll, dir)

		for name, data := range generateFixture("main") {
			Expect(os.WriteFile(filepath.Join(dir, name), data, 0o644)).To(Succeed())
		}
		Expect(os.WriteFile(filepath.Join(dir, "go.mod"),
			[]byte("module demo\n\ngo 1.21\n"), 0o644)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(dir, "main.go"), []byte(`package main

import (
	"encoding/json"
	"fmt"
	"os"
)

func main() {
	for _, in := range []string{
		`+"`"+`{"id":"n1","adoption":null,"placement":null}`+"`"+`,
		`+"`"+`{"id":"n1"}`+"`"+`,
		`+"`"+`{"id":"n1","adoption":{"id":"a1","state":"pending"},"placement":{"campaignId":"c1"}}`+"`"+`,
	} {
		var n Number
		if err := json.Unmarshal([]byte(in), &n); err != nil {
			fmt.Println("error:", err)
			os.Exit(1)
		}
		adoption, placement := "nil", "nil"
		if n.Adoption != nil {
			adoption = n.Adoption.Id + "/" + n.Adoption.State
		}
		if n.Placement != nil {
			placement = n.Placement.CampaignId
		}
		fmt.Printf("%s %s\n", adoption, placement)
	}
	out, _ := json.Marshal(Number{Id: "n1"})
	fmt.Println(string(out))
}
`), 0o644)).To(Succeed())

		cmd := exec.Command("go", "run", ".")
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		Expect(err).ToNot(HaveOccurred(), "go run failed:\n%s", out)
		Expect(string(out)).To(Equal(
			"nil nil\n" +
				"nil nil\n" +
				"a1/pending c1\n" +
				// Required nullable members send an explicit null.
				`{"id":"n1","adoption":null,"placement":null}` + "\n"))
	})
})
