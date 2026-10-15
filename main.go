// Command pkl-go-generator generates a Pkl schema from Go struct types.
//
// Usage:
//
//	pkl-go-generator [-R] [-m module] [-o file] [-r type] [-t tag] [path ...]
//
// Each path is a Go source file or a directory of them; "-" or no path at
// all reads a single file from standard input. The schema is written to
// standard output, and diagnostics to standard error.
package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/EnzoAlbornoz/pkl-go-generator/pklgen"
)

const name = "pkl-go-generator"

func main() {
	os.Exit(run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
}

// run executes the command and returns its exit status: 0 on success, 1 on
// failure and 2 on invalid usage.
func run(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet(name, flag.ContinueOnError)
	flags.SetOutput(stderr)
	flags.Usage = func() {
		fmt.Fprintf(stderr, "usage: %s [-R] [-m module] [-o file] [-r type] [-t tag] [path ...]\n", name)
		flags.PrintDefaults()
	}
	recursive := flags.Bool("R", false, "descend into subdirectories")
	module := flags.String("m", "", "set the module `name` (default: the root type)")
	out := flags.String("o", "", "write to `file` instead of standard output")
	root := flags.String("r", "", "make the fields of struct `type` module properties")
	tag := flags.String("t", "json", "name fields after the struct `tag` json or yaml")
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}

	g, err := pklgen.New(pklgen.Options{
		Tag:    *tag,
		Root:   *root,
		Module: *module,
		Warn:   func(msg string) { fmt.Fprintf(stderr, "%s: warning: %s\n", name, msg) },
	})
	if err != nil {
		fmt.Fprintf(stderr, "%s: %v\n", name, err)
		return 2
	}
	if err := generate(g, flags.Args(), *recursive, *out, stdin, stdout); err != nil {
		fmt.Fprintf(stderr, "%s: %v\n", name, err)
		return 1
	}
	return 0
}

func generate(g *pklgen.Generator, paths []string, recursive bool, out string, stdin io.Reader, stdout io.Writer) error {
	if len(paths) == 0 {
		paths = []string{"-"}
	}
	seen := map[string]bool{}
	for _, p := range paths {
		if err := add(g, p, recursive, stdin, seen); err != nil {
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
