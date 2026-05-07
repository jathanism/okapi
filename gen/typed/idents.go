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
