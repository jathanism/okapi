package typed

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// Internal-package Ginkgo specs — registered into the same global
// suite as the external typed_test specs, run by TestTypedGen.

var _ = Describe("pascal", func() {
	cases := []struct {
		in, out string
	}{
		{"", ""},
		{"createContact", "CreateContact"},
		{"create_contact", "CreateContact"},
		{"create-contact", "CreateContact"},
		{"CreateContact", "CreateContact"},
		{"Idempotency-Key", "IdempotencyKey"},
		{"If-Match", "IfMatch"},
		{"x", "X"},
		{"X-Trace-ID", "XTraceId"},
		{"already_lower_snake", "AlreadyLowerSnake"},
		{"mixed_caseInput", "MixedCaseInput"},
	}
	for _, tc := range cases {
		tc := tc
		It("converts "+tc.in+" -> "+tc.out, func() {
			Expect(pascal(tc.in)).To(Equal(tc.out))
		})
	}
})

var _ = Describe("camel", func() {
	It("returns empty for empty", func() {
		Expect(camel("")).To(Equal(""))
	})
	It("lowercases the first letter", func() {
		Expect(camel("CreateContact")).To(Equal("createContact"))
		Expect(camel("create_contact")).To(Equal("createContact"))
	})
})

var _ = Describe("safeArgName", func() {
	It("escapes Go keywords and predeclared identifiers", func() {
		Expect(safeArgName("Type")).To(Equal("type_"))
		Expect(safeArgName("Error")).To(Equal("error_"))
		Expect(safeArgName("Ctx")).To(Equal("ctx_"))
	})

	It("escapes the generated method's own locals and receiver", func() {
		// Every generated method declares these; a param mangling to one
		// of them would redeclare it and fail to compile.
		for in, out := range map[string]string{
			"C":          "c_",
			"PathParams": "pathParams_",
			"Query":      "query_",
			"Headers":    "headers_",
			"Body":       "body_",
			"BodyReader": "bodyReader_",
			"Out":        "out_",
			"OutHeaders": "outHeaders_",
			"RespBody":   "respBody_",
			"ApiResp":    "apiResp_",
			"Err":        "err_",
		} {
			Expect(safeArgName(in)).To(Equal(out), "safeArgName(%q)", in)
		}
	})

	It("leaves ordinary names alone", func() {
		Expect(safeArgName("IfMatch")).To(Equal("ifMatch"))
		Expect(safeArgName("XTrace")).To(Equal("xTrace"))
	})
})

var _ = Describe("isGoIdent", func() {
	It("accepts valid identifiers", func() {
		for _, s := range []string{"foo", "_foo", "Foo1", "_x", "α"} {
			Expect(isGoIdent(s)).To(BeTrue(), "expected %q to be a valid ident", s)
		}
	})
	It("rejects invalid identifiers", func() {
		for _, s := range []string{"", "1foo", "foo-bar", "foo bar", "foo.bar"} {
			Expect(isGoIdent(s)).To(BeFalse(), "expected %q to be invalid", s)
		}
	})
})

var _ = Describe("goEnumIdent", func() {
	It("prefixes the type name", func() {
		Expect(goEnumIdent("Status", "open")).To(Equal("StatusOpen"))
		Expect(goEnumIdent("Status", "in_progress")).To(Equal("StatusInProgress"))
		Expect(goEnumIdent("Status", "FREEZING")).To(Equal("StatusFreezing"))
	})
})

var _ = Describe("goEnumIdents", func() {
	It("leaves non-colliding values untouched", func() {
		Expect(goEnumIdents("Status", []string{"open", "closed"})).
			To(Equal([]string{"StatusOpen", "StatusClosed"}))
	})

	It("suffixes a later value whose mangling collides", func() {
		Expect(goEnumIdents("SortBy", []string{"signed_up_at", "signedUpAt"})).
			To(Equal([]string{"SortBySignedUpAt", "SortBySignedUpAt2"}))
	})

	It("numbers a triple collision 2 then 3", func() {
		Expect(goEnumIdents("SortBy", []string{"full_name", "fullName", "FullName"})).
			To(Equal([]string{"SortByFullName", "SortByFullName2", "SortByFullName3"}))
	})

	It("skips a suffix another value would claim naturally", func() {
		// "signed_up_at_2" naturally mangles to SortBySignedUpAt2, so the
		// collider must jump to 3 instead of stealing it.
		Expect(goEnumIdents("SortBy", []string{"signed_up_at", "signedUpAt", "signed_up_at_2"})).
			To(Equal([]string{"SortBySignedUpAt", "SortBySignedUpAt3", "SortBySignedUpAt2"}))
	})

	It("is deterministic across runs", func() {
		values := []string{"signed_up_at", "signedUpAt", "full_name", "fullName"}
		first := goEnumIdents("SortBy", values)
		for i := 0; i < 10; i++ {
			Expect(goEnumIdents("SortBy", values)).To(Equal(first))
		}
	})
})

var _ = Describe("splitWords", func() {
	cases := []struct {
		in  string
		out []string
	}{
		{"", []string{}},
		{"foo", []string{"foo"}},
		{"fooBar", []string{"foo", "Bar"}},
		{"foo_bar", []string{"foo", "bar"}},
		{"foo-bar", []string{"foo", "bar"}},
		{"FOO", []string{"FOO"}}, // all-caps stays as one word — pascal lowercases the tail
	}
	for _, tc := range cases {
		tc := tc
		It("splits "+tc.in, func() {
			out := splitWords(tc.in)
			if len(tc.out) == 0 {
				Expect(out).To(BeEmpty())
			} else {
				Expect(out).To(Equal(tc.out))
			}
		})
	}
})
