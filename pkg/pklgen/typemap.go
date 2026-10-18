package pklgen

import (
	"errors"
	"go/ast"
	"strings"
	"unicode"
	"unicode/utf8"
)

// A pklType is the Pkl type that Options.TypeMap gives a Go type, split
// around the names that it refers to, so that the names hidden by module
// properties can be referenced through the base module.
type pklType struct {
	parts []pklPart
}

// pklPart is literal text, or a name that a pklType refers to.
type pklPart struct {
	text string
	name bool
}

// parsePklType splits the text of a Pkl type. The names it refers to are
// the capitalized identifiers outside of strings that are neither members,
// such as the name in x.Name, nor functions called in a constraint, which
// properties do not hide. parsePklType only checks that strings end and
// parentheses balance; pkl reports other errors.
func parsePklType(text string) (*pklType, error) {
	if strings.TrimSpace(text) == "" {
		return nil, errors.New("empty type")
	}
	var (
		t      pklType
		lit    int  // start of the literal text not yet added
		depth  int  // of parentheses
		member bool // the last token was a dot
	)
	for i := 0; i < len(text); {
		r, size := utf8.DecodeRuneInString(text[i:])
		next := i + size
		switch {
		case r == '"' || r == '#':
			n, ok := stringLen(text[i:])
			if !ok {
				return nil, errors.New("unterminated string")
			}
			next = i + n
		case r == '`':
			n := strings.IndexByte(text[next:], '`')
			if n < 0 {
				return nil, errors.New("unterminated identifier")
			}
			next += n + 1
		case unicode.IsLetter(r) || r == '_' || r == '$':
			next = i + identLen(text[i:])
			call := depth > 0 && strings.HasPrefix(strings.TrimLeft(text[next:], " \t"), "(")
			if unicode.IsUpper(r) && !member && !call {
				t.add(text[lit:i], false)
				t.add(text[i:next], true)
				lit = next
			}
		case unicode.IsDigit(r):
			next = i + identLen(text[i:]) // such as 1_000, 0x1F or 1e3
		case r == '(':
			depth++
		case r == ')':
			if depth == 0 {
				return nil, errors.New("unbalanced parentheses")
			}
			depth--
		}
		if !unicode.IsSpace(r) {
			member = r == '.'
		}
		i = next
	}
	if depth > 0 {
		return nil, errors.New("unbalanced parentheses")
	}
	t.add(text[lit:], false)
	return &t, nil
}

func (t *pklType) add(text string, name bool) {
	if text != "" {
		t.parts = append(t.parts, pklPart{text, name})
	}
}

// names returns the names that t refers to.
func (t *pklType) names() []string {
	var names []string
	for _, p := range t.parts {
		if p.name {
			names = append(names, p.text)
		}
	}
	return names
}

// render returns the text of t, with the hidden names qualified by the
// base module.
func (t *pklType) render(hidden map[string]bool) string {
	var b strings.Builder
	for _, p := range t.parts {
		if p.name && hidden[p.text] {
			b.WriteString("base.")
		}
		b.WriteString(p.text)
	}
	return b.String()
}

// stringLen returns the length of the Pkl string literal at the start of s,
// which may be delimited by pounds, as in #"…"#, or span lines, as in """…""".
func stringLen(s string) (int, bool) {
	n := len(s) - len(strings.TrimLeft(s, "#"))
	pounds := s[:n]
	quote := `"`
	if strings.HasPrefix(s[n:], `"""`) {
		quote = `"""`
	} else if !strings.HasPrefix(s[n:], quote) {
		return 0, false
	}
	escape, end := `\`+pounds, quote+pounds
	for i := n + len(quote); i < len(s); {
		switch {
		case strings.HasPrefix(s[i:], escape):
			i += len(escape) + 1
		case strings.HasPrefix(s[i:], end):
			return i + len(end), true
		default:
			i++
		}
	}
	return 0, false
}

// identLen returns the length of the identifier or number at the start of s.
func identLen(s string) int {
	for i, r := range s {
		if !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '_' && r != '$' {
			return i
		}
	}
	return len(s)
}

// typeKey returns the name of the type that d declares in Options.TypeMap:
// the import path of its package, or the package name for added files, a
// dot and its name. It returns "" for anonymous structs.
func typeKey(d *decl) string {
	if d.pkg.types[d.name] != d {
		return ""
	}
	q := d.pkg.path
	if q == "" {
		q = d.pkg.name
	}
	return q + "." + d.name
}

// mapping returns the Pkl type that Options.TypeMap gives the type that d
// declares, if any.
func (s *state) mapping(d *decl) *pklType {
	return s.typeMap[typeKey(d)]
}

// mappedRef returns a reference to the type of Options.TypeMap that x, a
// possibly qualified type name in file f, names, or nil if it names none.
// The packages of qualified names are not imported.
func (s *state) mappedRef(f *file, x ast.Expr) *ptype {
	switch x := x.(type) {
	case *ast.Ident:
		if d := f.pkg.types[x.Name]; d != nil && s.mapping(d) != nil {
			return s.ref(d, x)
		}
	case *ast.SelectorExpr:
		q, ok := x.X.(*ast.Ident)
		if !ok {
			return nil
		}
		importPath := f.importPath(q.Name)
		key := importPath + "." + x.Sel.Name
		if s.typeMap[key] == nil {
			return nil
		}
		d := s.mapped[key]
		if p := s.byPath[importPath]; d == nil && p != nil {
			d = p.types[x.Sel.Name]
		}
		if d == nil {
			// Declare the type in a stand-in for its package.
			p := &pkg{path: importPath, name: q.Name, types: map[string]*decl{}}
			d = &decl{name: x.Sel.Name, pkg: p, pos: x.Pos()}
			p.types[d.name] = d
		}
		return s.ref(d, x)
	}
	return nil
}
