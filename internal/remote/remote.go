// Package remote downloads Go packages named by import paths or URLs.
package remote

import (
	"bytes"
	"encoding/json"
	"fmt"
	"go/parser"
	"go/token"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/EnzoAlbornoz/pkl-go-generator/pkg/pklgen"
)

// A Package names a remote package, and optionally one of its files.
type Package struct {
	Path    string // import path
	Version string // module query, such as "latest", "v1.2.3" or a branch
	File    string // base name of a file, or "" for the whole package

	repo, dir string // GitHub repository and directory in it, for Resolve
}

// rawGitHub serves the files of GitHub repositories.
var rawGitHub = "https://raw.githubusercontent.com"

// Resolve sets the import path of a package named by a GitHub URL from the
// nearest go.mod file in the repository, since modules can have a path
// other than their repository, such as k8s.io/api or github.com/o/r/v2.
func (p *Package) Resolve() error {
	if p.repo == "" {
		return nil
	}
	ref := p.Version
	if ref == "latest" {
		ref = "HEAD"
	}
	for dir := p.dir; ; dir = path.Dir(dir) {
		if dir == "." {
			dir = ""
		}
		src, status, err := get(rawGitHub + "/" + path.Join(p.repo, ref, dir, "go.mod"))
		if status == http.StatusNotFound && dir != "" {
			continue
		}
		if status == http.StatusNotFound {
			return nil // not a module; keep the repository path
		}
		if err != nil {
			return err
		}
		mod := modulePath(src)
		if mod == "" {
			return fmt.Errorf("%s/go.mod: no module path", path.Join(p.repo, dir))
		}
		p.Path = path.Join(mod, strings.TrimPrefix(strings.TrimPrefix(p.dir, dir), "/"))
		return nil
	}
}

// modulePath returns the module path that a go.mod file declares.
func modulePath(gomod []byte) string {
	for _, line := range strings.Split(string(gomod), "\n") {
		if f := strings.Fields(line); len(f) > 1 && f[0] == "module" {
			if p, err := strconv.Unquote(f[1]); err == nil {
				return p
			}
			return f[1]
		}
	}
	return ""
}

// Parse returns the package that s names: an import path with an optional
// "@version" suffix, or a GitHub URL of a directory or a .go file. It
// reports false for anything else.
func Parse(s string) (Package, bool) {
	if strings.HasPrefix(s, "https://") || strings.HasPrefix(s, "http://") {
		return parseGitHub(s)
	}
	p, version, _ := strings.Cut(s, "@")
	host, rest, _ := strings.Cut(p, "/")
	if rest == "" || isStandard(host) || strings.HasPrefix(host, ".") ||
		strings.ContainsAny(p, `\:`) || strings.HasSuffix(p, ".go") {
		return Package{}, false
	}
	if version == "" {
		version = "latest"
	}
	return Package{Path: p, Version: version}, true
}

// parseGitHub parses URLs of the forms
//
//	https://github.com/OWNER/REPO
//	https://github.com/OWNER/REPO/tree/REF/DIR
//	https://github.com/OWNER/REPO/blob/REF/DIR/FILE.go
//	https://raw.githubusercontent.com/OWNER/REPO/REF/DIR/FILE.go
//
// where REF is a branch, tag or commit, and DIR is the package directory.
func parseGitHub(s string) (Package, bool) {
	u, err := url.Parse(s)
	if err != nil {
		return Package{}, false
	}
	elems := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(elems) < 2 {
		return Package{}, false
	}
	repo := "github.com/" + elems[0] + "/" + strings.TrimSuffix(elems[1], ".git")
	ref, rest := "latest", elems[2:]
	switch u.Host {
	case "github.com", "www.github.com":
		if len(rest) > 0 {
			if len(rest) < 2 || rest[0] != "tree" && rest[0] != "blob" {
				return Package{}, false
			}
			blob := rest[0] == "blob"
			ref, rest = rest[1], rest[2:]
			if blob && (len(rest) == 0 || !strings.HasSuffix(rest[len(rest)-1], ".go")) {
				return Package{}, false
			}
		}
	case "raw.githubusercontent.com":
		if len(rest) > 2 && rest[0] == "refs" {
			rest = rest[2:] // refs/heads/NAME or refs/tags/NAME
		}
		if len(rest) < 2 || !strings.HasSuffix(rest[len(rest)-1], ".go") {
			return Package{}, false
		}
		ref, rest = rest[0], rest[1:]
	default:
		return Package{}, false
	}
	p := Package{Version: ref, repo: elems[0] + "/" + strings.TrimSuffix(elems[1], ".git")}
	if n := len(rest); n > 0 && strings.HasSuffix(rest[n-1], ".go") {
		p.File, rest = rest[n-1], rest[:n-1]
	}
	p.dir = path.Join(rest...)
	p.Path = path.Join(repo, p.dir)
	return p, true
}

var client = &http.Client{Timeout: time.Minute}

// Fetch returns the body of a successful GET request for a URL.
func Fetch(url string) ([]byte, error) {
	body, _, err := get(url)
	return body, err
}

// get returns the body and status of a GET request, and an error unless
// it succeeds.
func get(url string) ([]byte, int, error) {
	resp, err := client.Get(url)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, resp.StatusCode, fmt.Errorf("GET %s: %s", url, resp.Status)
	}
	body, err := io.ReadAll(resp.Body)
	return body, resp.StatusCode, err
}

// Imports returns the import paths of the packages outside the standard
// library that a Go source file imports.
func Imports(src []byte) ([]string, error) {
	f, err := parser.ParseFile(token.NewFileSet(), "", src, parser.ImportsOnly)
	if err != nil {
		return nil, err
	}
	var paths []string
	for _, is := range f.Imports {
		p, _ := strconv.Unquote(is.Path.Value)
		if host, _, _ := strings.Cut(p, "/"); !isStandard(host) {
			paths = append(paths, p)
		}
	}
	return paths, nil
}

// isStandard reports whether an import path that starts with elem belongs
// to the standard library, whose paths have no domain name.
func isStandard(elem string) bool {
	return !strings.Contains(elem, ".")
}

// A Loader downloads packages with the go command, which honors the GOPROXY,
// GOPRIVATE and related settings, and reads their source files. Its zero
// value is ready to use, and it creates a temporary module on first use;
// Close removes it.
type Loader struct {
	dir  string
	pkgs map[string]*listed // by import path
}

type listed struct {
	ImportPath, Name, Dir string
	GoFiles, CgoFiles     []string
	Standard              bool
}

// Get downloads packages, given as import paths with optional "@version"
// suffixes, along with the packages they import, at the versions that
// their modules require.
func (l *Loader) Get(queries ...string) error {
	if l.dir == "" {
		dir, err := os.MkdirTemp("", "pkl-go-generator-")
		if err != nil {
			return err
		}
		l.dir, l.pkgs = dir, map[string]*listed{}
		if err := l.goCmd(nil, "mod", "init", "pkl-go-generator.local/remote"); err != nil {
			return err
		}
	}
	if err := l.goCmd(nil, append([]string{"get"}, queries...)...); err != nil {
		return err
	}
	args := []string{"list", "-e", "-deps", "-mod=mod", "-json=ImportPath,Name,Dir,GoFiles,CgoFiles,Standard"}
	for _, q := range queries {
		p, _, _ := strings.Cut(q, "@")
		args = append(args, p)
	}
	var out bytes.Buffer
	if err := l.goCmd(&out, args...); err != nil {
		return err
	}
	for dec := json.NewDecoder(&out); dec.More(); {
		var p listed
		if err := dec.Decode(&p); err != nil {
			return err
		}
		if !p.Standard {
			l.pkgs[p.ImportPath] = &p
		}
	}
	return nil
}

// Import returns the source files of a downloaded package, named after its
// import path, or none if the package is unknown or in the standard library.
// The build constraints of the host select the files.
func (l *Loader) Import(importPath string) ([]pklgen.File, error) {
	p := l.pkgs[importPath]
	if p == nil {
		return nil, nil
	}
	var files []pklgen.File
	for _, name := range slices.Concat(p.GoFiles, p.CgoFiles) {
		src, err := os.ReadFile(filepath.Join(p.Dir, name))
		if err != nil {
			return nil, err
		}
		files = append(files, pklgen.File{Name: importPath + "/" + name, Src: src})
	}
	return files, nil
}

// Close removes the temporary module. The downloads stay in the module
// cache.
func (l *Loader) Close() error {
	if l.dir == "" {
		return nil
	}
	err := os.RemoveAll(l.dir)
	l.dir = ""
	return err
}

func (l *Loader) goCmd(stdout io.Writer, args ...string) error {
	var stderr bytes.Buffer
	cmd := exec.Command("go", args...)
	cmd.Dir = l.dir
	cmd.Env = append(os.Environ(), "GOWORK=off")
	cmd.Stdout = stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		if msg := strings.TrimSpace(stderr.String()); msg != "" {
			return fmt.Errorf("go %s: %s", args[0], msg)
		}
		return fmt.Errorf("go %s: %w", args[0], err)
	}
	return nil
}
