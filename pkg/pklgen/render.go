package pklgen

import (
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"
)

// keywords are the Pkl words that cannot be used as plain identifiers.
var keywords = map[string]bool{
	"abstract": true, "amends": true, "as": true, "case": true, "class": true,
	"const": true, "delete": true, "else": true, "extends": true,
	"external": true, "false": true, "fixed": true, "for": true,
	"function": true, "hidden": true, "if": true, "import": true, "in": true,
	"is": true, "let": true, "local": true, "module": true, "new": true,
	"nothing": true, "null": true, "open": true, "out": true, "outer": true,
	"override": true, "protected": true, "read": true, "record": true,
	"super": true, "switch": true, "this": true, "throw": true, "trace": true,
	"true": true, "typealias": true, "unknown": true, "vararg": true,
	"when": true,
}

// ident returns name as a Pkl identifier, quoted with backticks if needed.
func ident(name string) string {
	if keywords[name] {
		return "`" + name + "`"
	}
	for i, r := range name {
		if !(unicode.IsLetter(r) || r == '_' || r == '$' || i > 0 && unicode.IsDigit(r)) {
			return "`" + name + "`"
		}
	}
	return name
}

// pklString returns s as a Pkl string literal.
func pklString(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range s {
		switch r {
		case '\\':
			b.WriteString(`\\`)
		case '"':
			b.WriteString(`\"`)
		case '\n':
			b.WriteString(`\n`)
		case '\r':
			b.WriteString(`\r`)
		case '\t':
			b.WriteString(`\t`)
		default:
			if unicode.IsControl(r) {
				fmt.Fprintf(&b, `\u{%x}`, r)
			} else {
				b.WriteRune(r)
			}
		}
	}
	b.WriteByte('"')
	return b.String()
}

// maxWidth is the line width of pkl format.
const maxWidth = 100

// writeUnion writes a typealias of a union of string literals, wrapped the
// way pkl format wraps it when it is too wide.
func writeUnion(b *strings.Builder, name string, lits []string) {
	union := strings.Join(lits, " | ")
	if line := "typealias " + name + " = " + union; utf8.RuneCountInString(line) <= maxWidth {
		b.WriteString(line + "\n")
		return
	}
	fmt.Fprintf(b, "typealias %s =\n", name)
	if 2+utf8.RuneCountInString(union) <= maxWidth {
		fmt.Fprintf(b, "  %s\n", union)
		return
	}
	fmt.Fprintf(b, "  %s\n", lits[0])
	for _, lit := range lits[1:] {
		fmt.Fprintf(b, "    | %s\n", lit)
	}
}

// writeDoc writes doc as a Pkl doc comment.
func writeDoc(b *strings.Builder, indent, doc string) {
	doc = strings.TrimRight(doc, "\n")
	if doc == "" {
		return
	}
	for _, line := range strings.Split(doc, "\n") {
		if line == "" {
			fmt.Fprintf(b, "%s///\n", indent)
		} else {
			fmt.Fprintf(b, "%s/// %s\n", indent, line)
		}
	}
}

// writeProps writes property declarations, setting documented ones apart
// with blank lines.
func writeProps(b *strings.Builder, indent string, props []prop) {
	for i, p := range props {
		if i > 0 && (p.doc != "" || props[i-1].doc != "") {
			b.WriteString("\n")
		}
		writeDoc(b, indent, p.doc)
		fmt.Fprintf(b, "%s%s: %s\n", indent, ident(p.name), p.typ)
	}
}
