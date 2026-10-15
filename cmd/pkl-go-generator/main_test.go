package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/EnzoAlbornoz/pkl-go-generator/internal/remote/remotetest"
)

func runCmd(stdin string, args ...string) (code int, stdout, stderr string) {
	var out, errOut strings.Builder
	code = run(args, strings.NewReader(stdin), &out, &errOut)
	return code, out.String(), errOut.String()
}

func TestStdin(t *testing.T) {
	src := "package p\n\ntype T struct {\n\tMaxConns int `yaml:\"max_conns\"`\n\tName string\n}\n"
	want := "class T {\n  max_conns: Int\n  name: String\n}\n"
	for _, args := range [][]string{{"-t", "yaml"}, {"-t", "yaml", "-"}} {
		code, out, errOut := runCmd(src, args...)
		if code != 0 || !strings.HasSuffix(out, want) {
			t.Errorf("%v: exit %d, stderr %q, stdout:\n%s", args, code, errOut, out)
		}
	}
}

func TestOutputFile(t *testing.T) {
	dir := t.TempDir()
	in := filepath.Join(dir, "a.go")
	write(t, in, "package a\n\n// T is documented.\ntype T struct{ A int `json:\"a\"` }\n")
	out := filepath.Join(dir, "T.pkl")
	code, stdout, stderr := runCmd("", "-r", "T", "-o", out, in)
	if code != 0 || stdout != "" || stderr != "" {
		t.Fatalf("exit %d, stdout %q, stderr %q", code, stdout, stderr)
	}
	got, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	if want := "\n/// T is documented.\nmodule T\n\na: Int\n"; !strings.HasSuffix(string(got), want) {
		t.Errorf("got:\n%s", got)
	}
}

func TestDirectories(t *testing.T) {
	dir := t.TempDir()
	for name, typ := range map[string]string{
		"a.go":          "A",
		"a_test.go":     "ATest",
		"_b.go":         "B",
		"sub/c.go":      "C",
		"sub/.d/d.go":   "D",
		"testdata/e.go": "E",
		"vendor/f.go":   "F",
	} {
		write(t, filepath.Join(dir, name), "package p\n\ntype "+typ+" struct{}\n")
	}
	tests := []struct {
		args []string
		want string
	}{
		{[]string{dir}, "A"},
		{[]string{"-R", dir}, "A C"},
		{[]string{"-R", dir, dir, filepath.Join(dir, "a.go")}, "A C"},
		{[]string{filepath.Join(dir, "a_test.go")}, "ATest"},
	}
	for _, tt := range tests {
		code, out, stderr := runCmd("", tt.args...)
		if code != 0 {
			t.Errorf("%v: exit %d: %s", tt.args, code, stderr)
			continue
		}
		var got []string
		for _, line := range strings.Split(out, "\n") {
			if name, ok := strings.CutPrefix(line, "class "); ok {
				got = append(got, strings.TrimSuffix(name, " {}"))
			}
		}
		if strings.Join(got, " ") != tt.want {
			t.Errorf("%v: got classes %v, want %s", tt.args, got, tt.want)
		}
	}
}

func TestExitStatus(t *testing.T) {
	dir := t.TempDir()
	notStruct := filepath.Join(dir, "n.go")
	write(t, notStruct, "package n\n\ntype N int\n")
	tests := []struct {
		args []string
		code int
		want string
	}{
		{[]string{"-h"}, 0, "usage: pkl-go-generator"},
		{[]string{"-x"}, 2, "usage: pkl-go-generator"},
		{[]string{"-t", "toml"}, 2, `pkl-go-generator: unsupported tag "toml"`},
		{[]string{filepath.Join(dir, "missing.go")}, 1, "pkl-go-generator: "},
		{[]string{notStruct}, 1, "pkl-go-generator: no exported struct types found"},
	}
	for _, tt := range tests {
		code, out, stderr := runCmd("", tt.args...)
		if code != tt.code || out != "" || !strings.Contains(stderr, tt.want) {
			t.Errorf("%v: exit %d, stdout %q, stderr %q; want exit %d and %q", tt.args, code, out, stderr, tt.code, tt.want)
		}
	}
}

func TestVersion(t *testing.T) {
	defer func(v string) { version = v }(version)
	version = "v1.2.3"
	code, out, stderr := runCmd("", "-V")
	if code != 0 || out != "pkl-go-generator v1.2.3\n" || stderr != "" {
		t.Errorf("exit %d, stdout %q, stderr %q", code, out, stderr)
	}
}

func TestWarnings(t *testing.T) {
	src := "package p\n\ntype T struct{ C chan int }\n"
	code, out, stderr := runCmd(src)
	if code != 0 || !strings.Contains(out, "class T {}") {
		t.Fatalf("exit %d, stdout:\n%s", code, out)
	}
	if want := "pkl-go-generator: warning: <stdin>:3:16: field C cannot be encoded; skipping\n"; stderr != want {
		t.Errorf("stderr %q, want %q", stderr, want)
	}
}

func TestRemote(t *testing.T) {
	remotetest.Serve(t,
		remotetest.Module{
			Path:    "example.com/app",
			Version: "v1.0.0",
			Files: map[string]string{
				"go.mod": "module example.com/app\n\ngo 1.21\n\nrequire example.com/lib v1.0.0\n",
				"config/config.go": "package config\n\nimport \"example.com/lib/types\"\n\n" +
					"// Config is the app configuration.\ntype Config struct {\n" +
					"\tServer types.Server `json:\"server\"`\n\tLevel  Level        `json:\"level\"`\n}\n",
				"config/level.go": "package config\n\ntype Level string\n\n" +
					"const (\n\tDebug Level = \"debug\"\n\tInfo  Level = \"info\"\n)\n\ntype Other struct{}\n",
			},
		},
		remotetest.Module{
			Path:    "example.com/lib",
			Version: "v1.0.0",
			Files: map[string]string{
				"go.mod":         "module example.com/lib\n\ngo 1.21\n",
				"types/types.go": "package types\n\ntype Server struct {\n\tPort int `json:\"port\"`\n}\n\ntype Unused struct{}\n",
			},
		},
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("package remote\n\nimport \"example.com/lib/types\"\n\ntype Remote struct {\n\tServer types.Server `json:\"server\"`\n}\n"))
	}))
	defer srv.Close()

	const header = "// Code generated by pkl-go-generator. DO NOT EDIT.\n"
	const server = "\nclass Server {\n  port: Int\n}\n"
	const level = "\ntypealias Level = \"debug\" | \"info\"\n"
	tests := []struct {
		args []string
		want string
	}{
		{
			[]string{"-r", "Config", "example.com/app/config@v1.0.0"},
			header + "\n/// Config is the app configuration.\nmodule Config\n\nserver: Server\nlevel: Level\n" + level + server,
		},
		{
			[]string{"example.com/app/config"},
			header + "\n/// Config is the app configuration.\nclass Config {\n  server: Server\n  level: Level\n}\n" +
				level + "\nclass Other {}\n" + server,
		},
		{
			[]string{srv.URL + "/remote.go"},
			header + "\nclass Remote {\n  server: Server\n}\n" + server,
		},
	}
	for _, tt := range tests {
		code, out, stderr := runCmd("", tt.args...)
		if code != 0 || out != tt.want || stderr != "" {
			t.Errorf("%v: exit %d, stderr %q, stdout:\n%s\nwant:\n%s", tt.args, code, stderr, out, tt.want)
		}
	}

	for _, tt := range []struct {
		arg, want string
	}{
		{"example.com/app/missing", "example.com/app/missing"},
		{srv.URL + "/README.md", "not a Go file or a GitHub package"},
	} {
		code, out, stderr := runCmd("", tt.arg)
		if code != 1 || out != "" || !strings.Contains(stderr, tt.want) {
			t.Errorf("%s: exit %d, stdout %q, stderr %q; want %q", tt.arg, code, out, stderr, tt.want)
		}
	}
}

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
