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

// settingsSchema generates a module from the Settings type of this file.
func settingsSchema(t *testing.T, opts Options) string {
	t.Helper()
	src, err := os.ReadFile("pkl_test.go")
	if err != nil {
		t.Fatal(err)
	}
	opts.Root = "Settings"
	opts.Warn = func(msg string) { t.Error(msg) }
	g, err := New(opts)
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
	return schema.String()
}

// checkSettings decodes out, rejecting unknown fields, and compares the
// result with the values that settingsPkl sets.
func checkSettings(t *testing.T, out string) {
	t.Helper()
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
}

func TestRoundTrip(t *testing.T) {
	schema := settingsSchema(t, Options{})
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "Settings.pkl"), schema)

	writeFile(t, filepath.Join(dir, "app.pkl"), settingsPkl)
	out, err := pkl(t, dir, "eval", "-f", "json", "app.pkl")
	if err != nil {
		t.Fatalf("%v\nschema:\n%s", err, schema)
	}
	checkSettings(t, out)

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

// openBasePkl extends the module generated from Settings with a parameter
// for configurations to set, and a subclass of the open class Upstream.
const openBasePkl = `extends "Settings.pkl"

hidden env: "dev" | "prod"

debug = env == "dev"

class Host extends Upstream {
  hidden host: String
  url = "http://\(host)"
}
`

// TestOpenPkl checks that Pkl modules can extend the open module and
// classes, and that the configurations amending them still decode.
func TestOpenPkl(t *testing.T) {
	schema := settingsSchema(t, Options{OpenModule: true, OpenTypes: []string{"Upstream"}})
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "Settings.pkl"), schema)
	writeFile(t, filepath.Join(dir, "Base.pkl"), openBasePkl)

	app := strings.NewReplacer(
		`amends "Settings.pkl"`, `amends "Base.pkl"`,
		`debug = true`, `env = "dev"`,
		`upstream { url = "http://up"; weight = 0.5 }`, `upstream = new Host { host = "up"; weight = 0.5 }`,
	).Replace(settingsPkl)
	writeFile(t, filepath.Join(dir, "app.pkl"), app)
	out, err := pkl(t, dir, "eval", "-f", "json", "app.pkl")
	if err != nil {
		t.Fatalf("%v\nschema:\n%s", err, schema)
	}
	checkSettings(t, out)

	// The classes not named in OpenTypes stay closed.
	writeFile(t, filepath.Join(dir, "Base.pkl"), openBasePkl+"\nclass Note extends SettingsExtra {}\n")
	_, err = pkl(t, dir, "eval", "-f", "json", "app.pkl")
	if want := "Cannot extend non-open class"; err == nil || !strings.Contains(err.Error(), want) {
		t.Errorf("got error %v, want %q", err, want)
	}
}

// typeMapPkl amends testdata/typemap.pkl.
const typeMapPkl = `amends "schema.pkl"

id = "123e4567-e89b-12d3-a456-426614174000"
port = "http"
ports = "80-443"
timeout = 5_000_000_000
String = "s"
`

// TestTypeMapPkl checks that the Pkl types of the type map constrain
// values, also where module properties hide the types they refer to.
func TestTypeMapPkl(t *testing.T) {
	src, err := os.ReadFile(filepath.Join("testdata", "typemap.pkl"))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "schema.pkl"), string(src))
	writeFile(t, filepath.Join(dir, "app.pkl"), typeMapPkl)
	if _, err := pkl(t, dir, "eval", "-f", "json", "app.pkl"); err != nil {
		t.Fatal(err)
	}
	for _, tt := range []struct{ old, new, want string }{
		{`id = "123e4567-e89b-12d3-a456-426614174000"`, `id = "123"`, "`length == 36` violated"},
		{`port = "http"`, `port = true`, "Expected value of type"},
		{`ports = "80-443"`, `ports = "80"`, "`matches(Regex(#\"\\d+-\\d+\"#))` violated"},
		{`timeout = 5_000_000_000`, `timeout = -1`, "`this >= 0` violated"},
	} {
		writeFile(t, filepath.Join(dir, "bad.pkl"), strings.Replace(typeMapPkl, tt.old, tt.new, 1))
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
