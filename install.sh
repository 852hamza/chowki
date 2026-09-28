#!/bin/sh
# Installs the chowki binary from a GitHub release, after checking it
# against the release's checksums.txt.
#
#   curl -fsSL https://github.com/852hamza/chowki/raw/main/install.sh | sh
#
# Settings, all optional, as environment variables:
#   CHOWKI_VERSION       the version to install, such as v0.1.0; the latest by default
#   CHOWKI_INSTALL_DIR   the folder for the binary; /usr/local/bin when you can write
#                        to it, ~/.local/bin otherwise
#   CHOWKI_RELEASES_URL  where releases are downloaded from, for a mirror
set -eu

releases=${CHOWKI_RELEASES_URL:-https://github.com/852hamza/chowki/releases}

fail() {
	echo "install.sh: $*" >&2
	exit 1
}

need() {
	command -v "$1" >/dev/null 2>&1 || fail "this script needs $1; install it and try again"
}
need curl
need tar
need uname

case $(uname -s) in
Linux) os=linux ;;
Darwin) os=darwin ;;
*) fail "$(uname -s) isn't supported by this script; on Windows, download the zip from $releases" ;;
esac
case $(uname -m) in
x86_64 | amd64) arch=amd64 ;;
aarch64 | arm64) arch=arm64 ;;
*) fail "the $(uname -m) processor has no release; build from source instead" ;;
esac

if command -v sha256sum >/dev/null 2>&1; then
	sha256() { sha256sum "$1" | cut -d ' ' -f 1; }
elif command -v shasum >/dev/null 2>&1; then
	sha256() { shasum -a 256 "$1" | cut -d ' ' -f 1; }
else
	fail "this script needs sha256sum or shasum to check the download"
fi

version=${CHOWKI_VERSION:-}
if [ -z "$version" ]; then
	# The latest release redirects to its tag, which needs no API token.
	latest=$(curl -fsSLI -o /dev/null -w '%{url_effective}' "$releases/latest") ||
		fail "couldn't reach $releases"
	version=${latest##*/}
	case $version in
	v*) ;;
	*) fail "couldn't find the latest release at $releases" ;;
	esac
fi
case $version in
v*) ;;
*) version=v$version ;;
esac

archive="chowki_${version#v}_${os}_${arch}.tar.gz"
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT INT TERM

echo "Downloading chowki $version for $os/$arch"
curl -fsSL -o "$tmp/$archive" "$releases/download/$version/$archive" ||
	fail "couldn't download $archive of $version; check that the version exists"
curl -fsSL -o "$tmp/checksums.txt" "$releases/download/$version/checksums.txt" ||
	fail "couldn't download the checksums of $version"

want=$(awk -v f="$archive" '$2 == f { print $1 }' "$tmp/checksums.txt")
[ -n "$want" ] || fail "checksums.txt has no line for $archive"
got=$(sha256 "$tmp/$archive")
[ "$got" = "$want" ] || fail "the checksum of $archive doesn't match checksums.txt; don't use it"

tar -xzf "$tmp/$archive" -C "$tmp" chowki || fail "$archive has no chowki binary"

dir=${CHOWKI_INSTALL_DIR:-}
if [ -z "$dir" ]; then
	if [ -w /usr/local/bin ]; then
		dir=/usr/local/bin
	else
		dir="$HOME/.local/bin"
	fi
fi
mkdir -p "$dir"
# Install through a temporary name, so that a running chowki keeps its file.
cp "$tmp/chowki" "$dir/.chowki.new"
chmod 0755 "$dir/.chowki.new"
mv -f "$dir/.chowki.new" "$dir/chowki"

echo "Installed $("$dir/chowki" version | head -n 1) in $dir/chowki"
case ":$PATH:" in
*":$dir:"*) ;;
*) echo "Add $dir to your PATH to run chowki by name." ;;
esac
echo "Next: chowki init, then see https://852hamza.github.io/chowki/docs/get-started/quickstart"
