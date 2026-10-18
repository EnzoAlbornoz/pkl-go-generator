package pklgen

import (
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"reflect"
	"strconv"
	"strings"
)

// state holds the progress of one Generate call.
type state struct {
	*Generator
	queue  []*decl // declarations to generate, in discovery order
	defs   map[*decl]*def
	anon   map[*ast.StructType]*decl
	mapped map[string]*decl // declarations of the types of TypeMap, by key
}

// def is the Pkl definition of a declaration: a typealias of a union of
// string literals, of another type or of the type that TypeMap gives it,
// or else a class.
type def struct {
	union []string
	alias *ptype
	text  *pklType
	props []prop
}

// prop is a Pkl property generated from a struct field.
type prop struct {
	name  string // the encoded key
	typ   *ptype
	doc   string
	pos   token.Pos
	depth int // embedding depth of the field
}

// ptype is a Pkl type. User types are referenced through their declaration
// because their names are assigned once every type is known.
type ptype struct {
	name     string
	ref      *decl
	args     []*ptype
	nullable bool
}

func builtin(name string, args ...*ptype) *ptype {
	return &ptype{name: name, args: args}
}

func (t *ptype) String() string {
	s := t.name
	if t.ref != nil {
		s = ident(t.ref.pklName)
	}
	if len(t.args) > 0 {
		args := make([]string, len(t.args))
		for i, a := range t.args {
			args[i] = a.String()
		}
		s += "<" + strings.Join(args, ", ") + ">"
	}
	if t.nullable {
		s += "?"
	}
	return s
}

// qualify references the built-in types of t that are hidden through the
// base module.
func qualify(t *ptype, hidden map[string]bool) {
	if t.ref == nil && hidden[t.name] {
		t.name = "base." + t.name
	}
	for _, a := range t.args {
		qualify(a, hidden)
	}
}

// builtins are the Pkl types that generated code may reference; user types
// are renamed so that they do not shadow them.
var builtins = []string{
	"Any", "Boolean", "Float", "Int", "Int8", "Int16", "Int32", "Listing",
	"Mapping", "Number", "String", "UInt", "UInt8", "UInt16", "UInt32",
}

var basics = map[string]string{
	"bool":    "Boolean",
	"string":  "String",
	"int":     "Int",
	"int8":    "Int8",
	"int16":   "Int16",
	"int32":   "Int32",
	"rune":    "Int32",
	"int64":   "Int",
	"uint":    "UInt",
	"uint8":   "UInt8",
	"byte":    "UInt8",
	"uint16":  "UInt16",
	"uint32":  "UInt32",
	"uint64":  "UInt",
	"uintptr": "UInt",
	"float32": "Float",
	"float64": "Float",
	"any":     "Any",
}

// use schedules d for generation, and returns the declaration to refer to:
// d or, if TypeMap maps its type, the declaration of the type used first,
// which differs from d when d stands in for a package not yet imported.
func (s *state) use(d *decl) *decl {
	if k := typeKey(d); s.typeMap[k] != nil {
		if m := s.mapped[k]; m != nil {
			return m
		}
		s.mapped[k] = d
	}
	if _, ok := s.defs[d]; !ok {
		s.defs[d] = nil
		s.queue = append(s.queue, d)
	}
	return d
}

// useStruct schedules the struct named "Name" or "pkg.Name" for generation.
// A type that TypeMap maps may be of any kind.
func (s *state) useStruct(name string) error {
	d, err := s.lookup(name)
	if err != nil {
		return err
	}
	if s.mapping(d) != nil {
		s.use(d)
		return nil
	}
	if _, st := s.structOf(d); st == nil {
		return fmt.Errorf("%s is not a struct type", name)
	}
	if d.generic {
		return fmt.Errorf("%s is generic; generic types map to Any", name)
	}
	if t := s.marshaled(d); t != nil {
		return fmt.Errorf("%s has a custom marshaler; it maps to %s", name, t)
	}
	s.use(d)
	return nil
}

// drain generates the scheduled declarations, and those they reference.
func (s *state) drain() {
	for i := 0; i < len(s.queue); i++ {
		d := s.queue[i]
		s.defs[d] = s.define(d)
	}
}

func (s *state) define(d *decl) *def {
	if t := s.mapping(d); t != nil {
		return &def{text: t}
	}
	switch e := ast.Unparen(d.expr).(type) {
	case *ast.StructType:
		return &def{props: s.fields(d, e)}
	case *ast.Ident:
		if values := d.pkg.enums[d.name]; e.Name == "string" && len(values) > 0 {
			lits := make([]string, len(values))
			for i, v := range values {
				lits[i] = pklString(v)
			}
			return &def{union: lits}
		}
	}
	t := s.typeOf(d.file, d.expr, d.name)
	if t == nil {
		s.warnf(d.pos, "type %s cannot be encoded; using Any", d.name)
		t = builtin("Any")
	}
	return &def{alias: t}
}

// fields returns the properties for the fields of st, declared by d, in the
// order that the encoders write them.
func (s *state) fields(d *decl, st *ast.StructType) []prop {
	props := s.collect(d, st, 0, map[*decl]bool{d: true})

	// Of several fields with the same key, the least embedded one wins.
	depth := map[string]int{}
	for _, p := range props {
		if n, ok := depth[p.name]; !ok || p.depth < n {
			depth[p.name] = p.depth
		}
	}
	var out []prop
	kept := map[string]bool{}
	for _, p := range props {
		if p.depth != depth[p.name] {
			continue
		}
		if kept[p.name] {
			s.warnf(p.pos, "duplicate key %q; keeping the first", p.name)
			continue
		}
		kept[p.name] = true
		out = append(out, p)
	}
	return out
}

func (s *state) collect(d *decl, st *ast.StructType, depth int, seen map[*decl]bool) []prop {
	var props []prop
	for _, fld := range st.Fields.List {
		key, opts, ok := s.tag(fld)
		if !ok {
			continue
		}
		names := fld.Names
		embedded := len(names) == 0
		if embedded {
			names = []*ast.Ident{typeName(fld.Type)}
		}
		if opts.has("inline") || (embedded && key == "" && s.opts.Tag == "json") {
			owner, est := s.structOf(s.resolve(d.file, fld.Type))
			if est == nil {
				s.warnf(fld.Pos(), "cannot inline %s; skipping", types.ExprString(fld.Type))
				continue
			}
			if !seen[owner] {
				seen[owner] = true
				props = append(props, s.collect(owner, est, depth+1, seen)...)
				delete(seen, owner)
			}
			continue
		}
		for _, n := range names {
			if n == nil || !ast.IsExported(n.Name) {
				continue
			}
			k := key
			if k == "" {
				k = n.Name
				if s.opts.Tag == "yaml" {
					k = strings.ToLower(k)
				}
			}
			t := s.typeOf(d.file, fld.Type, d.name+n.Name)
			if t == nil {
				s.warnf(n.Pos(), "field %s cannot be encoded; skipping", n.Name)
				continue
			}
			if s.opts.Tag == "json" && opts.has("string") && s.scalar(d.file, fld.Type) {
				t = &ptype{name: "String", nullable: t.nullable}
			}
			if opts.has("omitempty") || opts.has("omitzero") {
				t.nullable = true
			}
			doc := fld.Doc.Text()
			if doc == "" {
				doc = fld.Comment.Text()
			}
			props = append(props, prop{name: k, typ: t, doc: doc, pos: n.Pos(), depth: depth})
		}
	}
	return props
}

// tagOptions is the option list of a struct tag, such as "omitempty,string".
type tagOptions string

func (o tagOptions) has(name string) bool {
	for s := string(o); s != ""; {
		var opt string
		opt, s, _ = strings.Cut(s, ",")
		if opt == name {
			return true
		}
	}
	return false
}

// tag returns the key and options of the field's struct tag; ok is false if
// the tag excludes the field.
func (s *state) tag(fld *ast.Field) (key string, opts tagOptions, ok bool) {
	var raw string
	if fld.Tag != nil {
		raw, _ = strconv.Unquote(fld.Tag.Value)
	}
	v := reflect.StructTag(raw).Get(s.opts.Tag)
	if v == "-" {
		return "", "", false
	}
	key, o, _ := strings.Cut(v, ",")
	return key, tagOptions(o), true
}

// typeName returns the name of a possibly pointer or generic type.
func typeName(e ast.Expr) *ast.Ident {
	switch e := e.(type) {
	case *ast.Ident:
		return e
	case *ast.StarExpr:
		return typeName(e.X)
	case *ast.SelectorExpr:
		return e.Sel
	case *ast.IndexExpr:
		return typeName(e.X)
	case *ast.IndexListExpr:
		return typeName(e.X)
	}
	return nil
}

// typeOf converts the Go type e, written in file f, to a Pkl type. It
// returns nil for types that the encoders cannot represent. Anonymous
// structs become classes named hint.
func (s *state) typeOf(f *file, e ast.Expr, hint string) *ptype {
	switch e := e.(type) {
	case *ast.ParenExpr:
		return s.typeOf(f, e.X, hint)
	case *ast.StarExpr:
		t := s.typeOf(f, e.X, hint)
		if t != nil {
			t.nullable = true
		}
		return t
	case *ast.Ident:
		if d := f.pkg.types[e.Name]; d != nil {
			return s.ref(d, e)
		}
		if name, ok := basics[e.Name]; ok {
			return builtin(name)
		}
		if e.Name == "error" || strings.HasPrefix(e.Name, "complex") {
			return nil
		}
		s.warnf(e.Pos(), "unknown type %s; using Any", e.Name)
		return builtin("Any")
	case *ast.SelectorExpr:
		if t := s.mappedRef(f, e); t != nil {
			return t
		}
		q, ok := e.X.(*ast.Ident)
		if !ok {
			return nil
		}
		importPath := f.importPath(q.Name)
		if t := s.wellKnown(importPath, e.Sel.Name); t != nil {
			return t
		}
		if d := s.find(f.pkg, importPath, e.Sel.Name); d != nil {
			return s.ref(d, e)
		}
		s.warnf(e.Pos(), "unknown type %s; using Any", types.ExprString(e))
		return builtin("Any")
	case *ast.ArrayType:
		if id, ok := e.Elt.(*ast.Ident); ok && e.Len == nil && (id.Name == "byte" || id.Name == "uint8") && f.pkg.types[id.Name] == nil {
			return builtin("String") // encoded as base64 or text
		}
		elem := s.typeOf(f, e.Elt, hint)
		if elem == nil {
			return nil
		}
		elem.nullable = false
		return builtin("Listing", elem)
	case *ast.MapType:
		key, val := s.typeOf(f, e.Key, hint), s.typeOf(f, e.Value, hint)
		if key == nil || val == nil {
			return nil
		}
		if s.opts.Tag == "json" {
			key = builtin("String") // JSON object keys are strings
		}
		key.nullable, val.nullable = false, false
		return builtin("Mapping", key, val)
	case *ast.StructType:
		d := s.anon[e]
		if d == nil {
			d = &decl{name: hint, pkg: f.pkg, file: f, expr: e, pos: e.Pos()}
			s.anon[e] = d
		}
		return s.ref(d, e)
	case *ast.InterfaceType:
		return builtin("Any")
	case *ast.IndexExpr:
		return s.instance(f, e, e.X)
	case *ast.IndexListExpr:
		return s.instance(f, e, e.X)
	}
	return nil // channels and functions
}

// instance converts e, an instance of the generic type x, to Any unless
// TypeMap maps x.
func (s *state) instance(f *file, e, x ast.Expr) *ptype {
	if t := s.mappedRef(f, x); t != nil {
		return t
	}
	s.warnf(e.Pos(), "generic type %s; using Any", types.ExprString(e))
	return builtin("Any")
}

// ref returns a reference to the type that d declares. TypeMap takes
// precedence over custom marshalers and type parameters.
func (s *state) ref(d *decl, at ast.Node) *ptype {
	if s.mapping(d) == nil {
		if t := s.marshaled(d); t != nil {
			return t
		}
		if d.generic {
			s.warnf(at.Pos(), "generic type %s; using Any", d.name)
			return builtin("Any")
		}
	}
	return &ptype{ref: s.use(d)}
}

// marshaled returns the type that a declared type with a custom marshaler
// encodes to: String for text marshalers, and Any for the others, whose
// output is unknown. Custom unmarshalers alone, which often just set
// defaults, keep the declared form.
func (s *state) marshaled(d *decl) *ptype {
	if d.pkg.types[d.name] != d {
		return nil // anonymous struct
	}
	methods := d.pkg.methods[d.name]
	switch {
	case methods["Marshal"+strings.ToUpper(s.opts.Tag)]:
		return builtin("Any")
	case methods["MarshalText"]:
		return builtin("String")
	}
	return nil
}

// wellKnown maps standard library types that encode to a fixed form.
func (s *state) wellKnown(importPath, name string) *ptype {
	switch importPath + "." + name {
	case "time.Time":
		return builtin("String") // RFC 3339
	case "net.IP", "net/netip.Addr", "net/netip.AddrPort", "net/netip.Prefix":
		return builtin("String") // text form, such as "192.0.2.1"
	case "time.Duration":
		if s.opts.Tag == "yaml" {
			return builtin("String") // such as "1m30s"
		}
		return builtin("Int") // nanoseconds
	case "encoding/json.Number":
		return builtin("Number")
	case "encoding/json.RawMessage":
		return builtin("Any")
	}
	return nil
}

// scalar reports whether e is a boolean, number or string type, the types
// that the json ",string" option applies to.
func (s *state) scalar(f *file, e ast.Expr) bool {
	if star, ok := e.(*ast.StarExpr); ok {
		e = star.X
	}
	for range 10 {
		id, ok := ast.Unparen(e).(*ast.Ident)
		if !ok {
			return false
		}
		d := f.pkg.types[id.Name]
		if d == nil {
			name, ok := basics[id.Name]
			return ok && name != "Any"
		}
		f, e = d.file, d.expr
	}
	return false
}
