# pkl-go-generator

Generate [Pkl](https://pkl-lang.org) schemas from Go struct types.

Property names follow your `json` or `yaml` struct tags, so the JSON or YAML
that Pkl renders from the schema decodes straight into your Go types.

## Install

Each [release](https://github.com/EnzoAlbornoz/pkl-go-generator/releases) has
archives for macOS on Apple silicon, Linux x64 and Windows x64. The release
workflow signs them with [packslip](https://packslip.dev), so packslip or
[mise](https://mise.jdx.dev) can install a verified release:

```sh
packslip install github.com/EnzoAlbornoz/pkl-go-generator
mise use -g packslip:github.com/EnzoAlbornoz/pkl-go-generator
```

To check a downloaded archive against the release's `packslip.sigstore.json`:

```sh
packslip verify packslip.sigstore.json \
  --identity-prefix https://github.com/EnzoAlbornoz/pkl-go-generator/ \
  --issuer https://token.actions.githubusercontent.com \
  --artifact pkl-go-generator-1.2.3-linux-x64.tar.gz
```

On other platforms, or to build from source, use Go:

```sh
go install github.com/EnzoAlbornoz/pkl-go-generator/cmd/pkl-go-generator@latest
```

## Usage

```
pkl-go-generator [-R] [-f type]... [-m module] [-map gotype=pkltype]... [-o file] [-r type] [-t tag] [source ...]
pkl-go-generator -V
```

Each source is a Go file or a directory of them, or a remote package: an
import path or a URL, described [below](#remote-packages). With no source, or
with `-`, the command reads one file from standard input. The schema is written
to standard output, and warnings and errors go to standard error. The exit
status is 0 on success, 1 on failure and 2 on invalid usage.

| Option                | Meaning                                                        |
| --------------------- | -------------------------------------------------------------- |
| `-R`                  | Descend into subdirectories.                                   |
| `-f type`             | Generate only struct `type` and the types it uses. Repeatable. |
| `-m module`           | Set the module name. Defaults to the root type.                |
| `-map gotype=pkltype` | Generate Go type `gotype` as Pkl type `pkltype`. Repeatable.   |
| `-o file`             | Write to `file` instead of standard output.                    |
| `-r type`             | Make the fields of struct `type` module properties.            |
| `-t tag`              | Use the `json` (default) or `yaml` struct tag for field names. |
| `-V`                  | Print the version and exit.                                    |

Without `-r` or `-f`, every exported struct becomes a class. With `-r`, the
fields of that one struct become module properties, and only the types it uses
are generated. The result is a module that a configuration file can amend. With
`-f`, only the named structs and the types they use are generated, as classes;
repeat it to name several, as in `-f Server -f Client`. Given with `-r`, these
classes are generated next to the module properties. Use the form `pkg.Type`
when several packages declare the type.

Directories are read the way the `go` command reads them: `_test.go` files and
names starting with `.` or `_` are skipped, and so are `testdata` and `vendor`
when descending.

## Remote packages

A source that is not a local path can name a package to download:

```sh
# An import path, at the latest version or at a given one.
pkl-go-generator k8s.io/api/apps/v1@v0.31.0

# A GitHub directory or file, at a branch, tag or commit.
pkl-go-generator -r Deployment https://github.com/kubernetes/api/blob/v0.31.0/apps/v1/types.go

# Any other URL of a Go file.
pkl-go-generator https://example.com/config.go
```

A URL of a file generates the structs of that file only, but the rest of its
package is read too. For GitHub URLs, the import path comes from the nearest
`go.mod` in the repository, so modules hosted under another path, such as
`k8s.io/api` above, work.

The types of remote packages may refer to types of other packages. These are
downloaded too, at the versions that the module requires, and only the types
that are referred to are generated from them. The example above generates
`ObjectMeta` from `k8s.io/apimachinery` and `PodSpec` from `k8s.io/api/core/v1`,
but no other types of those packages. The imports of a file fetched from a
plain URL are downloaded at their latest versions.

Downloads go through the `go` command, which must be installed. It honors your
`GOPROXY`, `GOPRIVATE` and related settings, verifies checksums, keeps the
modules in its cache for later runs, and selects files by build constraints.
The imports of local sources are not downloaded: they are resolved only against
the other sources given.

## Example

Given `config.go`:

```go
package config

import "time"

// Config configures the server.
type Config struct {
	// Addr is the listen address.
	Addr    string        `yaml:"addr"`
	Timeout time.Duration `yaml:"timeout,omitempty"`
	Level   Level         `yaml:"level"`
	TLS     *TLS          `yaml:"tls"`
	Users   []User        `yaml:"users"`
}

type Level string

const (
	Debug Level = "debug"
	Info  Level = "info"
)

type TLS struct {
	Cert string `yaml:"cert"`
	Key  string `yaml:"key"`
}

type User struct {
	Name  string   `yaml:"name"`
	Roles []string `yaml:"roles,omitempty"`
}
```

`pkl-go-generator -t yaml -r Config -o Config.pkl config.go` writes:

```pkl
// Code generated by pkl-go-generator. DO NOT EDIT.

/// Config configures the server.
module Config

/// Addr is the listen address.
addr: String

timeout: String?
level: Level
tls: TLS?
users: Listing<User>

typealias Level = "debug" | "info"

class TLS {
  cert: String
  key: String
}

class User {
  name: String
  roles: Listing<String>?
}
```

A configuration then amends the schema:

```pkl
amends "Config.pkl"

addr = ":8443"
level = "info"
tls {
  cert = "/etc/tls/cert.pem"
  key = "/etc/tls/key.pem"
}
users {
  new { name = "alice"; roles { "admin" } }
}
```

and `pkl eval -f yaml app.pkl` renders YAML that `yaml.Unmarshal` reads into
`Config`. Pkl rejects a configuration that misses `addr`, sets `level` to
`"loud"`, or misspells a property.

To keep the schema up to date, add a directive next to the type:

```go
//go:generate pkl-go-generator -t yaml -r Config -o Config.pkl config.go
```

## Mapping

### Struct tags

| Go                                       | Pkl                                               |
| ---------------------------------------- | ------------------------------------------------- |
| `json:"name"`, `yaml:"name"`             | Property `name`, quoted with backticks if needed. |
| No name in the tag                       | The field name. With `-t yaml`, lowercased.       |
| `json:"-"`, `yaml:"-"`                   | No property.                                      |
| `omitempty`, `omitzero`, pointer         | Nullable type, `T?`. Null is omitted on output.   |
| `json:",string"` on a bool or number     | `String`.                                         |
| Embedded struct (json), `,inline`        | Its fields are flattened in.                      |
| Embedded struct without `,inline` (yaml) | A property named after the lowercased type.       |

Properties of other fields have no default, so Pkl requires them to be set:
Go always writes them out. When several fields map to the same name, the least
embedded one wins, as it does in Go.

### Types

| Go                                   | Pkl                                                 |
| ------------------------------------ | --------------------------------------------------- |
| `bool`, `string`                     | `Boolean`, `String`                                 |
| `int`, `int64`, `int8` … `int32`     | `Int`, `Int`, `Int8` … `Int32`                      |
| `uint`, `uint64`, `uint8` … `uint32` | `UInt`, `UInt`, `UInt8` … `UInt32`                  |
| `float32`, `float64`                 | `Float`                                             |
| `[]byte`                             | `String`                                            |
| `[]T`, `[N]T`                        | `Listing<T>`                                        |
| `map[K]V`                            | `Mapping<String, V>` (json), `Mapping<K, V>` (yaml) |
| `any`, interfaces                    | `Any`                                               |
| `time.Time`                          | `String`                                            |
| `net.IP`, `netip.Addr` and friends   | `String`                                            |
| `time.Duration`                      | `Int` nanoseconds (json), `String` (yaml)           |
| `json.Number`, `json.RawMessage`     | `Number`, `Any`                                     |
| Named struct                         | `class`                                             |
| Anonymous struct                     | `class` named after the parent type and field       |
| Other named type                     | `typealias`                                         |
| `type T string` with typed constants | `typealias T = "a" \| "b"`                          |
| Type with a `MarshalText` method     | `String`                                            |
| Type with a `MarshalJSON` method     | `Any` with `-t json`                                |
| Type with a `MarshalYAML` method     | `Any` with `-t yaml`                                |

Types that cannot be found become `Any`, and channels, functions, complex
numbers and `error` are skipped; both cases print a warning. A type whose name is already taken, by a Pkl type it would shadow,
by a module property or by a type of another package, gets a numeric suffix,
also with a warning.

### Overriding types

`-map gotype=pkltype` replaces the Pkl type of a Go type. Use it for types that
the rules above get wrong, such as types with a custom marshaler, and for types
of packages that are not read, such as the imports of local files:

```sh
pkl-go-generator -r Config \
  -map 'github.com/google/uuid.UUID=String(length == 36)' \
  -map 'k8s.io/apimachinery/pkg/util/intstr.IntOrString=Int | String' \
  config.go
```

```pkl
id: UUID
port: IntOrString?

typealias UUID = String(length == 36)

typealias IntOrString = Int | String
```

Name a Go type by its import path, a dot and its name. Types declared in local
files, or in a file fetched from a URL other than GitHub, have no known import
path, so name them by their package name instead, as in `config.Level`.

Each mapped type that is used becomes a `typealias`, so that a pointer makes
the whole type nullable. The Pkl type is copied as written: it should describe
what the Go type encodes to, and it may use the Pkl standard library but not the
generated types. A mapping takes precedence over the rules above. Repeat `-map`
for each type; for the same type, the last one wins.

## Limitations

Types are read from source without type checking. Marshaler methods are
recognized only when they are declared on the type itself, not when they are
promoted from an embedded field, and generic types map to `Any`. A type that
customizes only decoding, with `UnmarshalJSON` or `UnmarshalYAML`, keeps its
fields, as such methods usually just fill in defaults. Map such types with
`-map`, or edit the generated schema, when these rules do not fit.

## Library

The generator is also available as a Go package:

```go
import "github.com/EnzoAlbornoz/pkl-go-generator/pkg/pklgen"

g, err := pklgen.New(pklgen.Options{Tag: "yaml", Root: "Config"})
if err != nil {
	return err
}
if err := g.AddFile("config.go", src); err != nil {
	return err
}
return g.Generate(os.Stdout)
```

To resolve types from other packages, set `Options.Import` to a function that
returns the files of a package by import path; `AddPackage` then adds a whole
package by its import path. See [`pklgen`](https://pkg.go.dev/github.com/EnzoAlbornoz/pkl-go-generator/pkg/pklgen).

## Development

```sh
go test ./...                  # the Pkl tests run when pkl is in PATH
go test ./pkg/pklgen -update   # rewrite the golden files in pkg/pklgen/testdata
```

## Releasing

Push a version tag from `main`:

```sh
git tag -a v1.2.3 -m v1.2.3
git push origin v1.2.3
```

The [release workflow](.github/workflows/release.yml) runs the tests on the
tagged commit and builds an archive per platform with `scripts/dist.sh`. A job
with no write access then signs the archives with packslip, and a last job
publishes the GitHub release with the archives, `SHA256SUMS`,
`packslip.sigstore.json` and notes generated from the changes. A tag with a
suffix, such as `v1.2.3-rc.1`, publishes a prerelease.

To build an archive locally, run `scripts/dist.sh 1.2.3 darwin arm64`; it needs
`zip` for Windows archives.

After the first release, run `packslip pin packslip.sigstore.json` on its
bundle and publish the printed signer fingerprint here, so that users can pass
it to `packslip install --pin` and `packslip verify --pin`.
