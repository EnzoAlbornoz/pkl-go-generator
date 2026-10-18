package pklgen

import (
	"errors"
	"flag"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

var update = flag.Bool("update", false, "rewrite the golden files")

// goldenTests generate testdata/NAME.pkl and testdata/NAME.warnings from the
// Go files under testdata/NAME.
var goldenTests = []struct {
	name string
	opts Options
}{
	{"json", Options{}},
	{"yaml", Options{Tag: "yaml"}},
	{"root", Options{Root: "Config"}},
	{"multi", Options{Module: "example.Shop"}},
	{"typemap", Options{Root: "Config", TypeMap: map[string]string{
		"github.com/google/uuid.UUID": "String(length == 36)",
		"app.IntOrString":             "Int | String",
		"app.Range":                   `String(matches(Regex(#"\d+-\d+"#)))`,
		"time.Duration":               "Int(this >= 0)",
	}}},
}

func TestGolden(t *testing.T) {
	for _, tt := range goldenTests {
		t.Run(tt.name, func(t *testing.T) {
			out, warnings, err := generateDir(tt.opts, filepath.Join("testdata", tt.name))
			if err != nil {
				t.Fatal(err)
			}
			golden(t, filepath.Join("testdata", tt.name+".pkl"), out)
			golden(t, filepath.Join("testdata", tt.name+".warnings"), warnings)
		})
	}
}

// generateDir generates a module from the Go files under dir.
func generateDir(opts Options, dir string) (out, warnings string, err error) {
	var w strings.Builder
	opts.Warn = func(msg string) { w.WriteString(msg + "\n") }
	g, err := New(opts)
	if err != nil {
		return "", "", err
	}
	err = filepath.WalkDir(dir, func(path string, e fs.DirEntry, err error) error {
		if err != nil || e.IsDir() || !strings.HasSuffix(path, ".go") {
			return err
		}
		src, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return g.AddFile(filepath.ToSlash(path), src)
	})
	if err != nil {
		return "", "", err
	}
	var b strings.Builder
	err = g.Generate(&b)
	return b.String(), w.String(), err
}

func golden(t *testing.T, path, got string) {
	t.Helper()
	if *update {
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if got != string(want) {
		t.Errorf("%s differs; got:\n%s", path, got)
	}
}

func TestGenerateErrors(t *testing.T) {
	tests := []struct {
		name string
		opts Options
		src  string
		want string
	}{
		{"no structs", Options{}, "type T int", "no exported struct types"},
		{"root not found", Options{Root: "Missing"}, "type T struct{}", "type Missing not found"},
		{"root not struct", Options{Root: "T"}, "type T int", "T is not a struct type"},
		{"root wrong package", Options{Root: "q.T"}, "type T struct{}", "type q.T not found"},
		{"output property", Options{Root: "T"}, "type T struct{ Output string `json:\"output\"` }", "clashes with Pkl's module output"},
		{"type not found", Options{Types: []string{"T", "Missing"}}, "type T struct{}", "type Missing not found"},
		{"type not struct", Options{Types: []string{"T"}}, "type T int", "T is not a struct type"},
		{"type generic", Options{Types: []string{"T"}}, "type T[E any] struct{ E E }", "T is generic"},
		{"type marshaled", Options{Types: []string{"T"}}, "type T struct{}\nfunc (T) MarshalText() ([]byte, error)", "T has a custom marshaler; it maps to String"},
		{"redeclared", Options{}, "type T struct{}\ntype T struct{}", "T redeclared"},
		{"syntax", Options{}, "type T struct{", "expected"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g, err := New(tt.opts)
			if err != nil {
				t.Fatal(err)
			}
			err = g.AddFile("p.go", []byte("package p\n"+tt.src))
			if err == nil {
				err = g.Generate(new(strings.Builder))
			}
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Errorf("got error %v, want %q", err, tt.want)
			}
		})
	}
}

func TestNewErrors(t *testing.T) {
	for _, opts := range []Options{
		{Tag: "toml"},
		{Module: "a-b"},
		{Module: "a..b"},
		{TypeMap: map[string]string{"UUID": "String"}},
		{TypeMap: map[string]string{".T": "String"}},
		{TypeMap: map[string]string{"p.1T": "String"}},
		{TypeMap: map[string]string{"p.T": " "}},
		{TypeMap: map[string]string{"p.T": "String("}},
	} {
		if _, err := New(opts); err == nil {
			t.Errorf("New(%+v) succeeded", opts)
		}
	}
}

func TestRootQualified(t *testing.T) {
	g, err := New(Options{Root: "b.T", Module: "x.Y"})
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range []struct{ name, src string }{
		{"a/a.go", "package a\ntype T struct{ A int `json:\"a\"` }"},
		{"b/b.go", "package b\ntype T struct{ B int `json:\"b\"` }"},
	} {
		if err := g.AddFile(f.name, []byte(f.src)); err != nil {
			t.Fatal(err)
		}
	}
	var b strings.Builder
	if err := g.Generate(&b); err != nil {
		t.Fatal(err)
	}
	if want := "module x.Y\n\nb: Int\n"; !strings.HasSuffix(b.String(), want) {
		t.Errorf("got:\n%s\nwant suffix:\n%s", b.String(), want)
	}
}

func TestTypes(t *testing.T) {
	files := []struct{ name, src string }{
		{"a/a.go", `package a

import "example.com/b"

type T struct {
	U U ` + "`json:\"u\"`" + `
}

type U struct {
	N int ` + "`json:\"n\"`" + `
}

type V struct {
	T b.T ` + "`json:\"t\"`" + `
}

type W struct{}

type Alias T
`},
		{"b/b.go", `package b

type T struct {
	S string ` + "`json:\"s\"`" + `
}

type Unused struct{}
`},
	}
	const header = "// Code generated by pkl-go-generator. DO NOT EDIT.\n"
	tests := []struct {
		name    string
		opts    Options
		want    string
		wantErr string
	}{
		{
			name: "dependencies",
			opts: Options{Types: []string{"a.T"}},
			want: header + "\nclass T {\n  u: U\n}\n\nclass U {\n  n: Int\n}\n",
		},
		{
			name: "several in source order",
			opts: Options{Types: []string{"V", "U", "U"}},
			want: header + "\nclass U {\n  n: Int\n}\n\nclass V {\n  t: T\n}\n\nclass T {\n  s: String\n}\n",
		},
		{
			name: "named struct type",
			opts: Options{Types: []string{"Alias"}},
			want: header + "\nclass T {\n  u: U\n}\n\nclass U {\n  n: Int\n}\n\ntypealias Alias = T\n",
		},
		{
			name: "with root",
			opts: Options{Root: "a.T", Types: []string{"W"}},
			want: header + "\nmodule T\n\nu: U\n\nclass U {\n  n: Int\n}\n\nclass W {}\n",
		},
		{
			name:    "ambiguous",
			opts:    Options{Types: []string{"T"}},
			wantErr: "type T is ambiguous; qualify it as pkg.T",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g, err := New(tt.opts)
			if err != nil {
				t.Fatal(err)
			}
			for _, f := range files {
				if err := g.AddFile(f.name, []byte(f.src)); err != nil {
					t.Fatal(err)
				}
			}
			var b strings.Builder
			err = g.Generate(&b)
			if tt.wantErr != "" {
				if err == nil || err.Error() != tt.wantErr {
					t.Errorf("got error %v, want %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if b.String() != tt.want {
				t.Errorf("got:\n%s\nwant:\n%s", b.String(), tt.want)
			}
		})
	}
}

func TestTypeMap(t *testing.T) {
	const header = "// Code generated by pkl-go-generator. DO NOT EDIT.\n"
	tests := []struct {
		name     string
		opts     Options
		src      string
		want     string
		warnings string
	}{
		{
			// The stand-in for lib.ID, used before lib is imported, also
			// serves the ID that lib.Box refers to.
			name: "one alias per type",
			opts: Options{TypeMap: map[string]string{"example.com/lib.ID": "String"}},
			src: "import \"example.com/lib\"\n\n" +
				"type T struct {\n\tA lib.ID  `json:\"a\"`\n\tB lib.Box `json:\"b\"`\n}\n",
			want: header + "\nclass T {\n  a: ID\n  b: Box\n}\n\ntypealias ID = String\n\nclass Box {\n  id: ID\n}\n",
		},
		{
			name: "names the Pkl type uses",
			opts: Options{TypeMap: map[string]string{"app.Window": "Listing<Duration>"}},
			src: "type T struct {\n\tWait Window   `json:\"wait\"`\n\tLast Duration `json:\"last\"`\n}\n\n" +
				"type Window struct{}\n\ntype Duration struct{}\n",
			want:     header + "\nclass T {\n  wait: Window\n  last: Duration2\n}\n\ntypealias Window = Listing<Duration>\n\nclass Duration2 {}\n",
			warnings: "app.go:10:6: type Duration renamed to Duration2; the name is taken by a built-in type\n",
		},
		{
			name: "selected despite its marshaler",
			opts: Options{Types: []string{"T"}, TypeMap: map[string]string{"app.T": `"a" | "b"`}},
			src:  "type T struct{}\n\nfunc (T) MarshalText() ([]byte, error)\n",
			want: header + "\ntypealias T = \"a\" | \"b\"\n",
		},
		{
			name:     "generic without mapping",
			opts:     Options{},
			src:      "type T struct {\n\tP Pair[int] `json:\"p\"`\n}\n\ntype Pair[E any] struct{ A, B E }\n",
			want:     header + "\nclass T {\n  p: Any\n}\n",
			warnings: "app.go:4:4: generic type Pair[int]; using Any\n",
		},
	}
	lib := []File{{"example.com/lib/lib.go", []byte("package lib\n\ntype ID [16]byte\n\ntype Box struct {\n\tID ID `json:\"id\"`\n}\n")}}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var warnings strings.Builder
			tt.opts.Warn = func(msg string) { warnings.WriteString(msg + "\n") }
			tt.opts.Import = func(path string) ([]File, error) {
				if path == "example.com/lib" {
					return lib, nil
				}
				return nil, nil
			}
			g, err := New(tt.opts)
			if err != nil {
				t.Fatal(err)
			}
			if err := g.AddFile("app.go", []byte("package app\n\n"+tt.src)); err != nil {
				t.Fatal(err)
			}
			var b strings.Builder
			if err := g.Generate(&b); err != nil {
				t.Fatal(err)
			}
			if b.String() != tt.want {
				t.Errorf("got:\n%s\nwant:\n%s", b.String(), tt.want)
			}
			if warnings.String() != tt.warnings {
				t.Errorf("warnings %q, want %q", warnings.String(), tt.warnings)
			}
		})
	}
}

func TestPklType(t *testing.T) {
	hidden := map[string]bool{"String": true, "Duration": true, "Inner": true}
	tests := []struct {
		in, names, out string
	}{
		{"String", "String", "base.String"},
		{"Int | String?", "Int String", "Int | base.String?"},
		{"Listing<String(!isEmpty)>(length > 0)", "Listing String", "Listing<base.String(!isEmpty)>(length > 0)"},
		{"String(this is Duration)", "String Duration", "base.String(this is base.Duration)"},
		{`String(matches(Regex(#"[A-Z]"#)))`, "String", `base.String(matches(Regex(#"[A-Z]"#)))`},
		{`"String" | "Int"`, "", `"String" | "Int"`},
		{`String(this != "a\"String")`, "String", `base.String(this != "a\"String")`},
		{`String(this != #"a"String"#)`, "String", `base.String(this != #"a"String"#)`},
		{"String(this != \"\"\"\nString\n\"\"\")", "String", "base.String(this != \"\"\"\nString\n\"\"\")"},
		{"Outer.Inner", "Outer", "Outer.Inner"},
		{"`String`", "", "`String`"},
		{"Int(isBetween(0, 1_000))", "Int", "Int(isBetween(0, 1_000))"},
	}
	for _, tt := range tests {
		pt, err := parsePklType(tt.in)
		if err != nil {
			t.Errorf("parsePklType(%q): %v", tt.in, err)
			continue
		}
		if got := strings.Join(pt.names(), " "); got != tt.names {
			t.Errorf("parsePklType(%q) names %q, want %q", tt.in, got, tt.names)
		}
		if got := pt.render(hidden); got != tt.out {
			t.Errorf("parsePklType(%q) renders %s, want %s", tt.in, got, tt.out)
		}
	}
	for _, in := range []string{"", " ", "String(", "String)", `String(this == "a)`, `String(this == #"a"))`, "`a"} {
		if _, err := parsePklType(in); err == nil {
			t.Errorf("parsePklType(%q) succeeded", in)
		}
	}
}

func TestImport(t *testing.T) {
	pkgs := map[string][]File{
		"example.com/app/config": {
			{"example.com/app/config/config.go", []byte(`package config

import (
	"example.com/broken"
	"example.com/lib/v2"
	"example.com/unused"
	"k8s.io/api/apps/v1"
	core "k8s.io/api/core/v1"
)

type Config struct {
	Server lib.Server    ` + "`json:\"server\"`" + `
	Pod    core.Pod      ` + "`json:\"pod\"`" + `
	Deploy v1.Deployment ` + "`json:\"deploy\"`" + `
	Extra  Extra         ` + "`json:\"extra\"`" + `
	Broken broken.Thing  ` + "`json:\"broken\"`" + `
}
`)},
			{"example.com/app/config/extra.go", []byte("package config\n\ntype Extra struct{ N int `json:\"n\"` }\n\ntype NotGenerated struct{}\n")},
		},
		"example.com/lib/v2":  {{"example.com/lib/v2/lib.go", []byte("package lib\n\ntype Server struct{ Port int `json:\"port\"` }\n\ntype Unused struct{}\n")}},
		"k8s.io/api/core/v1":  {{"k8s.io/api/core/v1/types.go", []byte("package v1\n\ntype Pod struct{ Name string `json:\"name\"` }\n")}},
		"k8s.io/api/apps/v1":  {{"k8s.io/api/apps/v1/types.go", []byte("package v1\n\ntype Deployment struct{ Replicas *int32 `json:\"replicas\"` }\n")}},
		"example.com/unused":  {{"example.com/unused/unused.go", []byte("package unused\n")}},
		"example.com/missing": nil,
	}
	var imported, warnings []string
	g, err := New(Options{
		Import: func(path string) ([]File, error) {
			imported = append(imported, path)
			if path == "example.com/broken" {
				return nil, errors.New("no network")
			}
			return pkgs[path], nil
		},
		Warn: func(msg string) { warnings = append(warnings, msg) },
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := g.AddPackage("example.com/app/config", "missing.go"); err == nil {
		t.Error("AddPackage succeeded with a missing file")
	}
	if err := g.AddPackage("example.com/missing"); err == nil {
		t.Error("AddPackage succeeded with a missing package")
	}
	if err := g.AddPackage("example.com/app/config", "config.go"); err != nil {
		t.Fatal(err)
	}
	var b strings.Builder
	if err := g.Generate(&b); err != nil {
		t.Fatal(err)
	}
	want := `// Code generated by pkl-go-generator. DO NOT EDIT.

class Config {
  server: Server
  pod: Pod
  deploy: Deployment
  extra: Extra
  broken: Any
}

class Extra {
  n: Int
}

class Server {
  port: Int
}

class Pod {
  name: String
}

class Deployment {
  replicas: Int32?
}
`
	if b.String() != want {
		t.Errorf("got:\n%s\nwant:\n%s", b.String(), want)
	}
	wantImported := "example.com/app/config example.com/missing example.com/lib/v2 k8s.io/api/core/v1 k8s.io/api/apps/v1 example.com/broken"
	if got := strings.Join(imported, " "); got != wantImported {
		t.Errorf("imported %s\nwant     %s", got, wantImported)
	}
	wantWarnings := "import example.com/broken: no network | example.com/app/config/config.go:16:9: unknown type broken.Thing; using Any"
	if got := strings.Join(warnings, " | "); got != wantWarnings {
		t.Errorf("warnings %s\nwant     %s", got, wantWarnings)
	}
}

func TestIdent(t *testing.T) {
	tests := map[string]string{
		"name":     "name",
		"_x$1":     "_x$1",
		"ünï":      "ünï",
		"api-key":  "`api-key`",
		"1st":      "`1st`",
		"foo.bar":  "`foo.bar`",
		"class":    "`class`",
		"default":  "default",
		"MaxConns": "MaxConns",
	}
	for in, want := range tests {
		if got := ident(in); got != want {
			t.Errorf("ident(%q) = %s, want %s", in, got, want)
		}
	}
}

func TestPklString(t *testing.T) {
	tests := map[string]string{
		`plain`:     `"plain"`,
		`a"b`:       `"a\"b"`,
		`\(x)`:      `"\\(x)"`,
		"tab\there": `"tab\there"`,
		"nl\n":      `"nl\n"`,
		"bell\a":    `"bell\u{7}"`,
	}
	for in, want := range tests {
		if got := pklString(in); got != want {
			t.Errorf("pklString(%q) = %s, want %s", in, got, want)
		}
	}
}

func TestImportNames(t *testing.T) {
	tests := map[string]string{
		"time":                       "time",
		"example.com/shop/model":     "model",
		"example.com/shop/model/v2":  "model v2",
		"k8s.io/api/core/v1":         "core v1",
		"gopkg.in/yaml.v3":           "yaml",
		"github.com/go-chi/chi":      "chi",
		"github.com/apple/pkl-go":    "pkl",
		"github.com/x/go-playground": "playground",
	}
	for in, want := range tests {
		if got := strings.Join(importNames(in), " "); got != want {
			t.Errorf("importNames(%q) = %s, want %s", in, got, want)
		}
	}
}
