package pklgen

import (
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

// These tests run the pkl command, and are skipped if it is not in PATH.

func pkl(t *testing.T, dir string, args ...string) (string, error) {
	t.Helper()
	bin, err := exec.LookPath("pkl")
	if err != nil {
		t.Skip("pkl not found in PATH")
	}
	cmd := exec.Command(bin, args...)
	cmd.Dir = dir
	out, err := cmd.Output()
	if exit := (*exec.ExitError)(nil); errors.As(err, &exit) {
		err = errors.New(string(exit.Stderr))
	}
	return string(out), err
}

// checkPkl evaluates the type of every property and typealias in
// schema.pkl, which fails if a type does not resolve.
const checkPkl = `import "pkl:reflect"
import "schema.pkl"

local mod = reflect.Module(schema)
local classes = mod.classes.values.toList().add(mod.moduleClass)
types = classes.flatMap((c) -> c.properties.values.toList().map((p) -> p.type))
  + mod.typeAliases.values.toList().map((t) -> t.referent)
count = types.length
`

func TestGoldenPkl(t *testing.T) {
	for _, tt := range goldenTests {
		t.Run(tt.name, func(t *testing.T) {
			src, err := os.ReadFile(filepath.Join("testdata", tt.name+".pkl"))
			if err != nil {
				t.Fatal(err)
			}
			dir := t.TempDir()
			writeFile(t, filepath.Join(dir, "schema.pkl"), string(src))
			writeFile(t, filepath.Join(dir, "check.pkl"), checkPkl)
			if out, err := pkl(t, dir, "format", "--diff-name-only", "schema.pkl"); err != nil {
				t.Errorf("not formatted: %s %v", out, err)
			}
			if _, err := pkl(t, dir, "eval", "-x", "count", "check.pkl"); err != nil {
				t.Errorf("types do not resolve: %v", err)
			}
		})
	}
}

// Settings is the input of TestRoundTrip, which parses this file.
type Settings struct {
	Name     string            `json:"name"`
	Port     uint16            `json:"port,omitempty"`
	Debug    *bool             `json:"debug"`
	Retries  int               `json:"retries,string"`
	Level    LogLevel          `json:"level"`
	Timeout  time.Duration     `json:"timeout"`
	Tags     []string          `json:"tags"`
	Limits   map[int]int       `json:"limits"`
	Labels   map[string]string `json:"labels,omitempty"`
	Upstream Upstream          `json:"upstream"`
	Backup   *Upstream         `json:"backup,omitempty"`
	Extra    struct {
		Note string `json:"note"`
	} `json:"extra"`
	Common
	Secret string `json:"-"`
}

type LogLevel string

const (
	LogInfo LogLevel = "info"
	LogWarn LogLevel = "warn"
)

type Upstream struct {
	URL    string  `json:"url"`
	Weight float64 `json:"weight"`
}

type Common struct {
	Region string `json:"region"`
}

const settingsPkl = `amends "Settings.pkl"

name = "svc"
debug = true
retries = "3"
level = "warn"
timeout = 5_000_000_000
tags { "a"; "b" }
limits { ["1"] = 10 }
upstream { url = "http://up"; weight = 0.5 }
extra { note = "hi" }
region = "eu"
`

func TestRoundTrip(t *testing.T) {
	src, err := os.ReadFile("pkl_test.go")
	if err != nil {
		t.Fatal(err)
	}
	g, err := New(Options{Root: "Settings", Warn: func(msg string) { t.Error(msg) }})
	if err != nil {
		t.Fatal(err)
	}
	if err := g.AddFile("pkl_test.go", src); err != nil {
		t.Fatal(err)
	}
	var schema strings.Builder
	if err := g.Generate(&schema); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "Settings.pkl"), schema.String())

	writeFile(t, filepath.Join(dir, "app.pkl"), settingsPkl)
	out, err := pkl(t, dir, "eval", "-f", "json", "app.pkl")
	if err != nil {
		t.Fatalf("%v\nschema:\n%s", err, schema.String())
	}
	dec := json.NewDecoder(strings.NewReader(out))
	dec.DisallowUnknownFields()
	var got Settings
	if err := dec.Decode(&got); err != nil {
		t.Fatalf("%v\njson:\n%s", err, out)
	}
	debug := true
	want := Settings{
		Name:     "svc",
		Debug:    &debug,
		Retries:  3,
		Level:    LogWarn,
		Timeout:  5 * time.Second,
		Tags:     []string{"a", "b"},
		Limits:   map[int]int{1: 10},
		Upstream: Upstream{URL: "http://up", Weight: 0.5},
		Common:   Common{Region: "eu"},
	}
	want.Extra.Note = "hi"
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got  %+v\nwant %+v", got, want)
	}

	// The schema rejects what Go would not decode, or would encode
	// differently.
	for _, tt := range []struct{ old, new, want string }{
		{`level = "warn"`, `level = "loud"`, "Expected value of type"},
		{`debug = true`, `port = 70000`, "isBetween"},
		{`retries = "3"`, `retries = 3`, "Expected value of type `String`"},
		{`["1"] = 10`, `[1] = 10`, "Expected value of type `String`"},
		{`weight = 0.5`, `weight = 1`, "Expected value of type `Float`"},
		{`note = "hi"`, `note = "hi"; more = 1`, "Cannot find property `more`"},
		{`name = "svc"`, `name = null`, "Expected value of type `String`"},
		{`name = "svc"`, ``, "Tried to read property `name`"},
	} {
		writeFile(t, filepath.Join(dir, "bad.pkl"), strings.Replace(settingsPkl, tt.old, tt.new, 1))
		_, err := pkl(t, dir, "eval", "-f", "json", "bad.pkl")
		if err == nil || !strings.Contains(err.Error(), tt.want) {
			t.Errorf("%s: got error %v, want %q", tt.new, err, tt.want)
		}
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
