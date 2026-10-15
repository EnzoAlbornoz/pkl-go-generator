// Package pklgen generates Pkl schemas from Go type declarations.
//
// Property names follow the struct tags of one encoding, json or yaml, so
// that data rendered from the schema by Pkl decodes into the Go types.
package pklgen

import (
	"cmp"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

// Options configures a Generator.
type Options struct {
	// Tag is the struct tag that names fields: "json" (the default) or
	// "yaml".
	Tag string

	// Root names a struct whose fields become module properties, so that
	// configurations can amend the generated module. Only the types it
	// reaches are generated. The form "pkg.Name" selects between packages.
	Root string

	// Module is the name in the module clause. It defaults to Root.
	Module string

	// Warn, if non-nil, receives non-fatal diagnostics.
	Warn func(msg string)
}

// A Generator collects Go source files and generates a Pkl module from the
// types they declare.
type Generator struct {
	opts Options
	fset *token.FileSet
	pkgs []*pkg
}

// pkg holds the declarations of one Go package.
type pkg struct {
	dir, name string
	types     map[string]*decl
	decls     []*decl             // in source order
	enums     map[string][]string // string constants by type name
}

// file holds the imports of one source file.
type file struct {
	pkg     *pkg
	imports map[string]string // import path by local name
}

// decl is a type declaration, or an anonymous struct that becomes a class.
type decl struct {
	name    string
	pkg     *pkg
	file    *file
	expr    ast.Expr
	doc     string
	pos     token.Pos
	generic bool

	pklName string // assigned by Generate
}

var moduleName = regexp.MustCompile(`^[\pL_$][\pL\pN_$]*(\.[\pL_$][\pL\pN_$]*)*$`)

// New returns a Generator configured by opts.
func New(opts Options) (*Generator, error) {
	switch opts.Tag {
	case "":
		opts.Tag = "json"
	case "json", "yaml":
	default:
		return nil, fmt.Errorf("unsupported tag %q: want json or yaml", opts.Tag)
	}
	if opts.Module != "" && !moduleName.MatchString(opts.Module) {
		return nil, fmt.Errorf("invalid module name %q", opts.Module)
	}
	return &Generator{opts: opts, fset: token.NewFileSet()}, nil
}

// AddFile parses the Go source src and records its declarations. Files in
// the same directory with the same package clause form one package; name is
// also used in diagnostics.
func (g *Generator) AddFile(name string, src []byte) error {
	af, err := parser.ParseFile(g.fset, name, src, parser.ParseComments|parser.SkipObjectResolution)
	if err != nil {
		return err
	}
	p := g.pkg(filepath.Dir(name), af.Name.Name)
	f := &file{pkg: p, imports: map[string]string{}}
	for _, is := range af.Imports {
		path, _ := strconv.Unquote(is.Path.Value)
		local := importName(path)
		if is.Name != nil {
			local = is.Name.Name
		}
		f.imports[local] = path
	}
	for _, d := range af.Decls {
		gd, ok := d.(*ast.GenDecl)
		if !ok {
			continue
		}
		switch gd.Tok {
		case token.TYPE:
			for _, spec := range gd.Specs {
				ts := spec.(*ast.TypeSpec)
				if p.types[ts.Name.Name] != nil {
					return fmt.Errorf("%s: %s redeclared", g.fset.Position(ts.Pos()), ts.Name.Name)
				}
				doc := ts.Doc
				if doc == nil && len(gd.Specs) == 1 {
					doc = gd.Doc
				}
				d := &decl{
					name:    ts.Name.Name,
					pkg:     p,
					file:    f,
					expr:    ts.Type,
					doc:     doc.Text(),
					pos:     ts.Pos(),
					generic: ts.TypeParams != nil,
				}
				p.types[d.name] = d
				p.decls = append(p.decls, d)
			}
		case token.CONST:
			for _, spec := range gd.Specs {
				vs := spec.(*ast.ValueSpec)
				typ, ok := vs.Type.(*ast.Ident)
				if !ok {
					continue
				}
				for _, v := range vs.Values {
					lit, ok := v.(*ast.BasicLit)
					if !ok || lit.Kind != token.STRING {
						continue
					}
					s, err := strconv.Unquote(lit.Value)
					if err == nil && !slices.Contains(p.enums[typ.Name], s) {
						p.enums[typ.Name] = append(p.enums[typ.Name], s)
					}
				}
			}
		}
	}
	return nil
}

func (g *Generator) pkg(dir, name string) *pkg {
	for _, p := range g.pkgs {
		if p.dir == dir && p.name == name {
			return p
		}
	}
	p := &pkg{dir: dir, name: name, types: map[string]*decl{}, enums: map[string][]string{}}
	g.pkgs = append(g.pkgs, p)
	return p
}

// lookup finds the declaration named "Name" or "pkg.Name".
func (g *Generator) lookup(s string) (*decl, error) {
	q, name, ok := strings.Cut(s, ".")
	if !ok {
		q, name = "", s
	}
	var found []*decl
	for _, p := range g.pkgs {
		if d := p.types[name]; d != nil && (q == "" || p.name == q) {
			found = append(found, d)
		}
	}
	switch len(found) {
	case 0:
		return nil, fmt.Errorf("type %s not found", s)
	case 1:
		return found[0], nil
	}
	return nil, fmt.Errorf("type %s is ambiguous; qualify it as pkg.%s", s, name)
}

// find returns the declaration of name in a package other than from that
// matches the import path.
func (g *Generator) find(from *pkg, importPath, name string) *decl {
	for _, p := range g.pkgs {
		d := p.types[name]
		if d == nil || p == from {
			continue
		}
		if p.name == importName(importPath) || filepath.Base(p.dir) == path.Base(importPath) {
			return d
		}
	}
	return nil
}

// resolve returns the declaration that the type name e refers to, if any.
func (g *Generator) resolve(f *file, e ast.Expr) *decl {
	switch e := e.(type) {
	case *ast.ParenExpr:
		return g.resolve(f, e.X)
	case *ast.StarExpr:
		return g.resolve(f, e.X)
	case *ast.Ident:
		return f.pkg.types[e.Name]
	case *ast.SelectorExpr:
		if q, ok := e.X.(*ast.Ident); ok {
			return g.find(f.pkg, f.importPath(q.Name), e.Sel.Name)
		}
	}
	return nil
}

// structOf returns the struct type that d declares, directly or through
// other named types, and the declaration that spells it out.
func (g *Generator) structOf(d *decl) (*decl, *ast.StructType) {
	for i := 0; d != nil && i < 10; i++ {
		if st, ok := ast.Unparen(d.expr).(*ast.StructType); ok {
			return d, st
		}
		d = g.resolve(d.file, d.expr)
	}
	return nil, nil
}

func (f *file) importPath(local string) string {
	if p, ok := f.imports[local]; ok {
		return p
	}
	return local
}

// importName guesses the package name of an import path.
func importName(importPath string) string {
	elems := strings.Split(importPath, "/")
	name := elems[len(elems)-1]
	if len(elems) > 1 && isMajorVersion(name) {
		name = elems[len(elems)-2]
	}
	name, _, _ = strings.Cut(name, ".")
	name = strings.TrimPrefix(name, "go-")
	return strings.TrimSuffix(name, "-go")
}

func isMajorVersion(s string) bool {
	if len(s) < 2 || s[0] != 'v' {
		return false
	}
	_, err := strconv.Atoi(s[1:])
	return err == nil
}

// Generate writes a Pkl module for the collected declarations to w.
func (g *Generator) Generate(w io.Writer) error {
	s := &state{
		Generator: g,
		defs:      map[*decl]*def{},
		anon:      map[*ast.StructType]*decl{},
		warned:    map[string]bool{},
	}
	var (
		props     []prop
		mod       = g.opts.Module
		modDoc    string
		hasModule = mod != ""
	)
	if g.opts.Root != "" {
		root, err := g.lookup(g.opts.Root)
		if err != nil {
			return err
		}
		owner, st := g.structOf(root)
		if st == nil {
			return fmt.Errorf("%s is not a struct type", g.opts.Root)
		}
		props = s.fields(owner, st)
		for _, p := range props {
			if p.name == "output" {
				return fmt.Errorf("%s: property %q clashes with Pkl's module output", g.fset.Position(p.pos), p.name)
			}
		}
		modDoc = root.doc
		if mod == "" {
			mod = root.name
		}
		hasModule = true
	} else {
		for _, p := range g.pkgs {
			for _, d := range p.decls {
				if _, st := g.structOf(d); st != nil && ast.IsExported(d.name) && !d.generic {
					s.use(d)
				}
			}
		}
		if len(s.queue) == 0 {
			return errors.New("no exported struct types found")
		}
	}
	s.drain()

	decls := slices.Clone(s.queue)
	slices.SortFunc(decls, func(a, b *decl) int { return cmp.Compare(a.pos, b.pos) })

	// Module properties hide the types of the same name in the whole module,
	// so hidden built-in types are referenced through an import of pkl:base.
	taken := map[string]string{}
	for _, n := range builtins {
		taken[n] = "a built-in type"
	}
	hidden := map[string]bool{}
	for _, p := range props {
		if taken[p.name] != "" {
			hidden[p.name] = true
		}
		taken[p.name] = "a module property"
	}
	if len(hidden) > 0 {
		if taken["base"] != "" {
			return errors.New(`property "base" clashes with the import of pkl:base`)
		}
		taken["base"] = "the import of pkl:base"
		for _, p := range props {
			qualify(p.typ, hidden)
		}
		for _, def := range s.defs {
			if def.alias != nil {
				qualify(def.alias, hidden)
			}
			for _, p := range def.props {
				qualify(p.typ, hidden)
			}
		}
	}
	for _, d := range decls {
		name := d.name
		for i := 2; taken[name] != ""; i++ {
			name = d.name + strconv.Itoa(i)
		}
		if name != d.name {
			s.warnf(d.pos, "type %s renamed to %s; the name is taken by %s", d.name, name, taken[d.name])
		}
		taken[name] = "type " + d.pkg.name + "." + d.name
		d.pklName = name
	}

	var b strings.Builder
	b.WriteString("// Code generated by pkl-go-generator. DO NOT EDIT.\n")
	if hasModule {
		b.WriteString("\n")
		writeDoc(&b, "", modDoc)
		fmt.Fprintf(&b, "module %s\n", mod)
	}
	if len(hidden) > 0 {
		b.WriteString("\nimport \"pkl:base\"\n")
	}
	if len(props) > 0 {
		b.WriteString("\n")
		writeProps(&b, "", props)
	}
	for _, d := range decls {
		b.WriteString("\n")
		writeDoc(&b, "", d.doc)
		def := s.defs[d]
		if def.alias != nil {
			fmt.Fprintf(&b, "typealias %s = %s\n", ident(d.pklName), def.alias)
			continue
		}
		fmt.Fprintf(&b, "class %s {", ident(d.pklName))
		if len(def.props) > 0 {
			b.WriteString("\n")
			writeProps(&b, "  ", def.props)
		}
		b.WriteString("}\n")
	}
	_, err := io.WriteString(w, b.String())
	return err
}
