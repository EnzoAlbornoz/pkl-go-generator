#!/bin/sh
# Build the release archive of pkl-go-generator for one platform.
#
# usage: scripts/dist.sh version goos goarch
#
# The archive goes to dist/, named so that packslip infers its platform,
# such as dist/pkl-go-generator-1.2.3-darwin-arm64.tar.gz. Windows archives
# are zip files, the others gzipped tarballs. The executable is statically
# linked and sits at the root of the archive. The path of the archive is
# printed.
set -eu

if [ $# -ne 3 ]; then
	echo "usage: $0 version goos goarch" >&2
	exit 2
fi
version=${1#v} goos=$2 goarch=$3
name=pkl-go-generator

case $goarch in
amd64) arch=x64 ;;
*) arch=$goarch ;;
esac
exe=$name
if [ "$goos" = windows ]; then
	exe=$name.exe
fi

build=$(mktemp -d)
trap 'rm -rf "$build"' EXIT
CGO_ENABLED=0 GOOS=$goos GOARCH=$goarch go build -trimpath \
	-ldflags "-s -w -X main.version=v$version" \
	-o "$build/$exe" ./cmd/$name
chmod 755 "$build/$exe"

mkdir -p dist
archive=$PWD/dist/$name-$version-$goos-$arch
if [ "$goos" = windows ]; then
	archive=$archive.zip
	rm -f "$archive"
	(cd "$build" && zip -q "$archive" "$exe")
else
	archive=$archive.tar.gz
	tar -czf "$archive" -C "$build" "$exe"
fi
echo "dist/${archive##*/}"
