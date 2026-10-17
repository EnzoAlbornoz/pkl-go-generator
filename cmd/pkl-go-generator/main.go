// Command pkl-go-generator generates a Pkl schema from Go struct types.
//
// Usage:
//
//	pkl-go-generator [-R] [-f type]... [-m module] [-o file] [-r type] [-t tag] [source ...]
//	pkl-go-generator -V
//
// Each source is a Go file or a directory of them, an import path with an
// optional "@version" suffix, or the URL of a Go file or of a GitHub
// directory or file. "-" or no source at all reads a single file from
// standard input. Remote packages, and the packages that their types refer
// to, are downloaded with the go command. The schema is written to standard
// output, and diagnostics to standard error.
package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"runtime/debug"
	"strings"

	"github.com/EnzoAlbornoz/pkl-go-generator/internal/remote"
	"github.com/EnzoAlbornoz/pkl-go-generator/pkg/pklgen"
)

const name = "pkl-go-generator"

// version is set by release builds with -ldflags "-X main.version=v1.2.3".
var version string

func main() {
	os.Exit(run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
}

// run executes the command and returns its exit status: 0 on success, 1 on
// failure and 2 on invalid usage.
func run(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet(name, flag.ContinueOnError)
	flags.SetOutput(stderr)
	flags.Usage = func() {
		fmt.Fprintf(stderr, "usage: %s [-R] [-f type]... [-m module] [-o file] [-r type] [-t tag] [source ...]\n", name)
		fmt.Fprintf(stderr, "       %s -V\n", name)
		flags.PrintDefaults()
	}
	recursive := flags.Bool("R", false, "descend into subdirectories")
	printVersion := flags.Bool("V", false, "print the version and exit")
	var types []string
	flags.Func("f", "generate only struct `type` and the types it uses; may be repeated", func(s string) error {
		types = append(types, s)
		return nil
	})
	module := flags.String("m", "", "set the module `name` (default: the root type)")
	out := flags.String("o", "", "write to `file` instead of standard output")
	root := flags.String("r", "", "make the fields of struct `type` module properties")
	tag := flags.String("t", "json", "use the `tag` struct tag for field names: json or yaml")
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if *printVersion {
		fmt.Fprintf(stdout, "%s %s\n", name, buildVersion())
		return 0
	}

	warn := func(msg string) { fmt.Fprintf(stderr, "%s: warning: %s\n", name, msg) }
	var loader remote.Loader
	defer loader.Close()
	g, err := pklgen.New(pklgen.Options{
		Tag:    *tag,
		Root:   *root,
		Types:  types,
		Module: *module,
		Import: loader.Import,
		Warn:   warn,
	})
	if err != nil {
		fmt.Fprintf(stderr, "%s: %v\n", name, err)
		return 2
	}
	if err := generate(g, &loader, flags.Args(), *recursive, *out, stdin, stdout, warn); err != nil {
		fmt.Fprintf(stderr, "%s: %v\n", name, err)
		return 1
	}
	return 0
}

// buildVersion returns the version set at link time or, for a build by
// "go install module@version", the module version.
func buildVersion() string {
	if version != "" {
		return version
	}
	if info, ok := debug.ReadBuildInfo(); ok && info.Main.Version != "" {
		return info.Main.Version
	}
	return "(devel)"
}

// source is a command-line argument: a local path, a remote package, or a
// file fetched from a URL.
type source struct {
	arg string
	pkg *remote.Package
	src []byte
}

func generate(g *pklgen.Generator, loader *remote.Loader, args []string, recursive bool, out string, stdin io.Reader, stdout io.Writer, warn func(string)) error {
	if len(args) == 0 {
		args = []string{"-"}
	}
	// Download remote sources before adding any, so that types are
	// generated in the order of the arguments.
	var (
		sources          []source
		queries, imports []string
	)
	for _, arg := range args {
		s := source{arg: arg}
		if _, err := os.Stat(arg); err != nil && arg != "-" {
			if p, ok := remote.Parse(arg); ok {
				if err := p.Resolve(); err != nil {
					return err
				}
				s.pkg = &p
				queries = append(queries, p.Path+"@"+p.Version)
			} else if u, err := url.Parse(arg); err == nil && (u.Scheme == "https" || u.Scheme == "http") {
				if !strings.HasSuffix(u.Path, ".go") {
					return fmt.Errorf("%s: not a Go file or a GitHub package", arg)
				}
				if s.src, err = remote.Fetch(arg); err != nil {
					return err
				}
				paths, _ := remote.Imports(s.src) // AddFile reports syntax errors
				imports = append(imports, paths...)
			}
		}
		sources = append(sources, s)
	}
	if len(queries) > 0 {
		if err := loader.Get(queries...); err != nil {
			return err
		}
	}
	if len(imports) > 0 {
		if err := loader.Get(imports...); err != nil {
			warn(fmt.Sprintf("cannot download imports: %v", err))
		}
	}

	seen := map[string]bool{}
	for _, s := range sources {
		var err error
		switch {
		case s.pkg != nil:
			var files []string
			if s.pkg.File != "" {
				files = append(files, s.pkg.File)
			}
			err = g.AddPackage(s.pkg.Path, files...)
		case s.src != nil:
			err = g.AddFile(s.arg, s.src)
		default:
			err = add(g, s.arg, recursive, stdin, seen)
		}
		if err != nil {
			return err
		}
	}
	var b strings.Builder
	if err := g.Generate(&b); err != nil {
		return err
	}
	if out != "" {
		return os.WriteFile(out, []byte(b.String()), 0o644)
	}
	_, err := io.WriteString(stdout, b.String())
	return err
}

// add adds the Go files at path, the files of a directory, or standard
// input for "-".
func add(g *pklgen.Generator, path string, recursive bool, stdin io.Reader, seen map[string]bool) error {
	if path == "-" {
		if seen[path] {
			return nil
		}
		seen[path] = true
		src, err := io.ReadAll(stdin)
		if err != nil {
			return err
		}
		return g.AddFile("<stdin>", src)
	}
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return addFile(g, path, seen)
	}
	return filepath.WalkDir(path, func(p string, e fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if e.IsDir() {
			if p != path && (!recursive || ignored(e.Name()) || e.Name() == "testdata" || e.Name() == "vendor") {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasSuffix(p, ".go") && !strings.HasSuffix(p, "_test.go") && !ignored(e.Name()) {
			return addFile(g, p, seen)
		}
		return nil
	})
}

func addFile(g *pklgen.Generator, path string, seen map[string]bool) error {
	key, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	if seen[key] {
		return nil
	}
	seen[key] = true
	src, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return g.AddFile(path, src)
}

// ignored reports whether the go command ignores a file or directory name.
func ignored(name string) bool {
	return strings.HasPrefix(name, ".") || strings.HasPrefix(name, "_")
}
