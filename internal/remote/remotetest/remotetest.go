// Package remotetest serves Go modules to the go command from a directory
// laid out like a module proxy, for tests that download packages.
package remotetest

import (
	"archive/zip"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// A Module is a version of a module and its files, including go.mod.
type Module struct {
	Path, Version string
	Files         map[string]string
}

// Serve makes the go command download the modules, and only them, for the
// rest of the test, into a module cache of its own. It skips the test if the
// go command is missing.
func Serve(t testing.TB, modules ...Module) {
	t.Helper()
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go command not found in PATH")
	}
	dir := t.TempDir()
	for _, m := range modules {
		vdir := filepath.Join(dir, filepath.FromSlash(m.Path), "@v")
		if err := os.MkdirAll(vdir, 0o755); err != nil {
			t.Fatal(err)
		}
		info := fmt.Sprintf(`{"Version":%q,"Time":"2024-01-01T00:00:00Z"}`, m.Version)
		write(t, filepath.Join(vdir, m.Version+".info"), info)
		write(t, filepath.Join(vdir, m.Version+".mod"), m.Files["go.mod"])
		writeZip(t, filepath.Join(vdir, m.Version+".zip"), m)
		list, _ := os.ReadFile(filepath.Join(vdir, "list"))
		write(t, filepath.Join(vdir, "list"), string(list)+m.Version+"\n")
	}
	t.Setenv("GOPROXY", "file:///"+strings.TrimPrefix(filepath.ToSlash(dir), "/"))
	t.Setenv("GOSUMDB", "off")
	t.Setenv("GOFLAGS", "-modcacherw")
	t.Setenv("GOMODCACHE", t.TempDir())
	t.Setenv("GOTOOLCHAIN", "local")
}

func write(t testing.TB, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func writeZip(t testing.TB, path string, m Module) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	zw := zip.NewWriter(f)
	for name, content := range m.Files {
		w, err := zw.Create(m.Path + "@" + m.Version + "/" + name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
}
