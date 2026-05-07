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
