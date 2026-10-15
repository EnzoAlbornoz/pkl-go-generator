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
	for _, opts := range []Options{{Tag: "toml"}, {Module: "a-b"}, {Module: "a..b"}} {
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
