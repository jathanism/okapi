package typed

import (
	"strings"
	"unicode"
)

// pascal converts identifiers like "create_contact", "create-contact",
// "createContact", or "Idempotency-Key" into "CreateContact" /
// "IdempotencyKey". It splits on _ and -, and at lowercase->uppercase
// boundaries, then upcases the first rune of each word.
func pascal(s string) string {
	if s == "" {
		return ""
	}
	parts := splitWords(s)
	var b strings.Builder
	for _, p := range parts {
		if p == "" {
			continue
		}
		// Preserve all-uppercase initialisms when possible; otherwise
		// upper-first + lower-rest is the simple correct form.
		runes := []rune(p)
		runes[0] = unicode.ToUpper(runes[0])
		for i := 1; i < len(runes); i++ {
			runes[i] = unicode.ToLower(runes[i])
		}
		b.WriteString(string(runes))
	}
	return b.String()
}

// camel is pascal but with the first letter lowercased.
func camel(s string) string {
	p := pascal(s)
	if p == "" {
		return p
	}
	r := []rune(p)
	r[0] = unicode.ToLower(r[0])
	return string(r)
}

// splitWords breaks an identifier into word parts, treating _ and - as
// separators and inserting splits at lowercase->uppercase boundaries.
func splitWords(s string) []string {
	// First normalize separators to spaces.
	mapped := strings.Map(func(r rune) rune {
		switch r {
		case '_', '-':
			return ' '
		}
		return r
	}, s)
	// Walk, breaking on case boundaries.
	var out []string
	var cur strings.Builder
	prev := rune(0)
	for _, r := range mapped {
		if r == ' ' {
			if cur.Len() > 0 {
				out = append(out, cur.String())
				cur.Reset()
			}
			prev = 0
			continue
		}
		if cur.Len() > 0 && unicode.IsLower(prev) && unicode.IsUpper(r) {
			out = append(out, cur.String())
			cur.Reset()
		}
		cur.WriteRune(r)
		prev = r
	}
	if cur.Len() > 0 {
		out = append(out, cur.String())
	}
	return out
}

// isGoIdent returns true if s is a valid Go identifier.
func isGoIdent(s string) bool {
	if s == "" {
		return false
	}
	for i, r := range s {
		if i == 0 {
			if !unicode.IsLetter(r) && r != '_' {
				return false
			}
			continue
		}
		if !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '_' {
			return false
		}
	}
	return true
}

// goEnumIdent renders an enum value into a valid Go identifier. We
// prefix it with the enum's type name so the consts don't collide.
func goEnumIdent(typeName, value string) string {
	return typeName + pascal(value)
}

// goKeywords are reserved Go identifiers that cannot be used as
// variable or parameter names. A spec param named "type" or "range"
// would clash; safeArgName escapes those.
var goKeywords = map[string]bool{
	"break": true, "case": true, "chan": true, "const": true, "continue": true,
	"default": true, "defer": true, "else": true, "fallthrough": true, "for": true,
	"func": true, "go": true, "goto": true, "if": true, "import": true,
	"interface": true, "map": true, "package": true, "range": true, "return": true,
	"select": true, "struct": true, "switch": true, "type": true, "var": true,
}

// goPredeclared are predeclared identifiers we don't want to shadow.
// Shadowing them is legal but produces warnings in tools and confusing code.
var goPredeclared = map[string]bool{
	"any": true, "bool": true, "byte": true, "comparable": true,
	"complex64": true, "complex128": true, "error": true, "float32": true,
	"float64": true, "int": true, "int8": true, "int16": true, "int32": true,
	"int64": true, "rune": true, "string": true, "uint": true, "uint8": true,
	"uint16": true, "uint32": true, "uint64": true, "uintptr": true,
	"true": true, "false": true, "iota": true, "nil": true,
	"append": true, "cap": true, "close": true, "complex": true, "copy": true,
	"delete": true, "imag": true, "len": true, "make": true, "new": true,
	"panic": true, "print": true, "println": true, "real": true, "recover": true,
	"ctx": true, // we always have ctx context.Context in scope
}

// safeArgName takes an already-PascalCased Go name (like "IfMatch")
// and returns the camelCase form ("ifMatch") for use as a function
// argument name. Reserved-word and predeclared-identifier collisions
// get a "_" suffix. Returns "_" for empty input.
//
// We lowercase only the first rune, preserving the rest verbatim, so
// "XTrace" becomes "xTrace" — running camel(pascal(name)) here would
// double-pascal and destroy the internal capitalization.
func safeArgName(pascalName string) string {
	if pascalName == "" {
		return "_"
	}
	runes := []rune(pascalName)
	runes[0] = unicode.ToLower(runes[0])
	s := string(runes)
	if goKeywords[s] || goPredeclared[s] {
		return s + "_"
	}
	return s
}
