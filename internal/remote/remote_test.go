package remote

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/EnzoAlbornoz/pkl-go-generator/internal/remote/remotetest"
)

func TestParse(t *testing.T) {
	tests := []struct {
		in   string
		want Package
	}{
		{"github.com/o/r/pkg", Package{Path: "github.com/o/r/pkg", Version: "latest"}},
		{"github.com/o/r/pkg@v1.2.3", Package{Path: "github.com/o/r/pkg", Version: "v1.2.3"}},
		{"k8s.io/api/core/v1@v0.31.0", Package{Path: "k8s.io/api/core/v1", Version: "v0.31.0"}},
		{"gopkg.in/yaml.v3", Package{Path: "gopkg.in/yaml.v3", Version: "latest"}},
		{"https://github.com/o/r", Package{Path: "github.com/o/r", Version: "latest", repo: "o/r"}},
		{"https://github.com/o/r.git", Package{Path: "github.com/o/r", Version: "latest", repo: "o/r"}},
		{"https://github.com/o/r/tree/main/pkg/config", Package{Path: "github.com/o/r/pkg/config", Version: "main", repo: "o/r", dir: "pkg/config"}},
		{"https://github.com/o/r/blob/v1.0.0/pkg/config/types.go", Package{Path: "github.com/o/r/pkg/config", Version: "v1.0.0", File: "types.go", repo: "o/r", dir: "pkg/config"}},
		{"https://github.com/o/r/blob/main/types.go", Package{Path: "github.com/o/r", Version: "main", File: "types.go", repo: "o/r"}},
		{"https://raw.githubusercontent.com/o/r/main/pkg/types.go", Package{Path: "github.com/o/r/pkg", Version: "main", File: "types.go", repo: "o/r", dir: "pkg"}},
		{"https://raw.githubusercontent.com/o/r/refs/heads/dev/pkg/types.go", Package{Path: "github.com/o/r/pkg", Version: "dev", File: "types.go", repo: "o/r", dir: "pkg"}},
	}
	for _, tt := range tests {
		if got, ok := Parse(tt.in); !ok || got != tt.want {
			t.Errorf("Parse(%q) = %+v, %t; want %+v", tt.in, got, ok, tt.want)
		}
	}
	for _, in := range []string{
		"https://github.com/o",
		"https://github.com/o/r/blob/main/README.md",
		"https://github.com/o/r/issues/1",
		"https://raw.githubusercontent.com/o/r/main/go.mod",
		"https://example.com/x/config.go",
		"config.go",
		"pkg/config",
		"./github.com/o/r",
		"../x.y/z",
		`C:\x.y\z`,
		"encoding/json",
		"example.com/x/config.go",
	} {
		if got, ok := Parse(in); ok {
			t.Errorf("Parse(%q) = %+v, want no package", in, got)
		}
	}
}

func TestResolve(t *testing.T) {
	files := map[string]string{
		"/kubernetes/api/v0.31.0/go.mod": "// generated\n\nmodule k8s.io/api\n\ngo 1.22\n",
		"/o/multi/main/tools/go.mod":     "module \"example.com/multi/tools\"\n",
		"/o/v2/HEAD/go.mod":              "module github.com/o/v2/v2\n",
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if src, ok := files[r.URL.Path]; ok {
			w.Write([]byte(src))
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()
	defer func(s string) { rawGitHub = s }(rawGitHub)
	rawGitHub = srv.URL

	tests := map[string]string{
		"https://github.com/kubernetes/api/blob/v0.31.0/apps/v1/types.go": "k8s.io/api/apps/v1",
		"https://github.com/o/multi/tree/main/tools/cmd/x":                "example.com/multi/tools/cmd/x",
		"https://github.com/o/v2/tree/HEAD/pkg":                           "github.com/o/v2/v2/pkg",
		"https://github.com/o/v2":                                         "github.com/o/v2/v2",
		"https://github.com/o/gopath/tree/main/pkg":                       "github.com/o/gopath/pkg",
		"k8s.io/api/apps/v1":                                              "k8s.io/api/apps/v1",
	}
	for in, want := range tests {
		p, ok := Parse(in)
		if !ok {
			t.Fatalf("Parse(%q) failed", in)
		}
		if err := p.Resolve(); err != nil || p.Path != want {
			t.Errorf("%s: resolved to %s, %v; want %s", in, p.Path, err, want)
		}
	}
}

func TestImports(t *testing.T) {
	src := `package p

import (
	"encoding/json"
	"example.com/x"
	"k8s.io/api/core/v1"
	y "gopkg.in/yaml.v3"
)
`
	paths, err := Imports([]byte(src))
	if err != nil {
		t.Fatal(err)
	}
	if got, want := strings.Join(paths, " "), "example.com/x k8s.io/api/core/v1 gopkg.in/yaml.v3"; got != want {
		t.Errorf("got %s, want %s", got, want)
	}
}

func TestFetch(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/types.go" {
			http.NotFound(w, r)
			return
		}
		w.Write([]byte("package types\n"))
	}))
	defer srv.Close()
	if src, err := Fetch(srv.URL + "/types.go"); err != nil || string(src) != "package types\n" {
		t.Errorf("got %q, %v", src, err)
	}
	if _, err := Fetch(srv.URL + "/missing.go"); err == nil || !strings.Contains(err.Error(), "404") {
		t.Errorf("got error %v, want 404", err)
	}
}

// modules are an application that requires an older version of a library.
var modules = []remotetest.Module{
	{
		Path:    "example.com/app",
		Version: "v1.0.0",
		Files: map[string]string{
			"go.mod": "module example.com/app\n\ngo 1.21\n\nrequire example.com/lib v1.2.0\n",
			"config/config.go": `package config

import (
	"time"

	"example.com/lib/types"
)

type Config struct {
	Server  types.Server  ` + "`json:\"server\"`" + `
	Timeout time.Duration ` + "`json:\"timeout\"`" + `
}
`,
			"config/ignored.go": "//go:build ignore\n\npackage config\n\ntype Config struct{}\n",
		},
	},
	{
		Path:    "example.com/lib",
		Version: "v1.2.0",
		Files: map[string]string{
			"go.mod":         "module example.com/lib\n\ngo 1.21\n",
			"types/types.go": "package types\n\ntype Server struct {\n\tPort int `json:\"port\"`\n}\n",
		},
	},
	{
		Path:    "example.com/lib",
		Version: "v1.3.0",
		Files: map[string]string{
			"go.mod":         "module example.com/lib\n\ngo 1.21\n",
			"types/types.go": "package types\n\ntype Server struct {\n\tAddress string `json:\"address\"`\n}\n",
		},
	},
}

func TestLoader(t *testing.T) {
	remotetest.Serve(t, modules...)
	var l Loader
	defer l.Close()
	if files, err := l.Import("example.com/app/config"); err != nil || files != nil {
		t.Fatalf("Import before Get = %v, %v", files, err)
	}
	if err := l.Get("example.com/app/config@v1.0.0"); err != nil {
		t.Fatal(err)
	}
	importSrc := func(path string) string {
		t.Helper()
		files, err := l.Import(path)
		if err != nil {
			t.Fatal(err)
		}
		var b strings.Builder
		for _, f := range files {
			b.WriteString("// " + f.Name + "\n" + string(f.Src))
		}
		return b.String()
	}
	if src := importSrc("example.com/app/config"); !strings.HasPrefix(src, "// example.com/app/config/config.go\n") || strings.Contains(src, "ignored.go") {
		t.Errorf("app/config:\n%s", src)
	}
	if src := importSrc("example.com/lib/types"); !strings.Contains(src, "Port int") {
		t.Errorf("lib/types is not the required v1.2.0:\n%s", src)
	}
	if src := importSrc("time"); src != "" {
		t.Errorf("time is imported:\n%s", src)
	}
	if err := l.Get("example.com/lib/types@latest"); err != nil {
		t.Fatal(err)
	}
	if src := importSrc("example.com/lib/types"); !strings.Contains(src, "Address string") {
		t.Errorf("lib/types is not the latest v1.3.0:\n%s", src)
	}
	if err := l.Get("example.com/missing/pkg"); err == nil || !strings.Contains(err.Error(), "example.com/missing") {
		t.Errorf("got error %v, want one about example.com/missing", err)
	}
}
