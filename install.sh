#!/bin/sh
#
# wyrd installer.
#
#   curl -fsSL https://raw.githubusercontent.com/kgatilin/wyrd/main/install.sh | sh
#
# Downloads a prebuilt binary from a GitHub release, verifies its sha256
# against the release's checksums.txt, and installs it. This is the supported
# way in: `go install` cannot build the full wyrd, because the review UI is
# embedded from web/dist, which is a build artifact and not in the module zip.
#
# Options, with the equivalent environment variable in brackets:
#
#   --version <tag>   install a specific release instead of the latest  [WYRD_VERSION]
#   --dir <path>      install into this directory                       [WYRD_INSTALL_DIR]
#   --check           install wyrd-check instead of wyrd
#   --all             install both binaries
#   --help            print this and exit
#
# Piping into sh takes the same options after `-s --`:
#
#   curl -fsSL <url> | sh -s -- --all --dir /usr/local/bin

set -eu

REPO=kgatilin/wyrd
VERSION="${WYRD_VERSION:-}"
DIR="${WYRD_INSTALL_DIR:-$HOME/.local/bin}"
BINARIES=wyrd

say() { printf '%s\n' "$*"; }
die() { printf 'install.sh: %s\n' "$*" >&2; exit 1; }

# Spelled out rather than parsed off the header comment: when this script is
# piped into sh, $0 is not a readable file.
usage() {
	cat <<'USAGE'
wyrd installer.

  curl -fsSL https://raw.githubusercontent.com/kgatilin/wyrd/main/install.sh | sh

Downloads a prebuilt binary from a GitHub release, verifies its sha256 against
the release's checksums.txt, and installs it.

Options, with the equivalent environment variable in brackets:

  --version <tag>   install a specific release instead of the latest  [WYRD_VERSION]
  --dir <path>      install into this directory                       [WYRD_INSTALL_DIR]
  --check           install wyrd-check instead of wyrd
  --all             install both binaries
  --help            print this and exit

Piping into sh takes the same options after `-s --`:

  curl -fsSL <url> | sh -s -- --all --dir /usr/local/bin
USAGE
}

while [ $# -gt 0 ]; do
	case "$1" in
	--version) [ $# -ge 2 ] || die "--version needs a tag"; VERSION="$2"; shift 2 ;;
	--version=*) VERSION="${1#--version=}"; shift ;;
	--dir) [ $# -ge 2 ] || die "--dir needs a path"; DIR="$2"; shift 2 ;;
	--dir=*) DIR="${1#--dir=}"; shift ;;
	--check) BINARIES=wyrd-check; shift ;;
	--all) BINARIES="wyrd wyrd-check"; shift ;;
	--help | -h) usage; exit 0 ;;
	*) die "unknown option: $1 (try --help)" ;;
	esac
done

command -v curl >/dev/null 2>&1 || die "curl is required"
command -v tar >/dev/null 2>&1 || die "tar is required"

# --- platform -------------------------------------------------------------
# Release assets are built for linux and darwin on amd64 and arm64. WSL
# reports itself as Linux, which is correct: the linux build is the right one.
os=$(uname -s | tr '[:upper:]' '[:lower:]')
arch=$(uname -m)

case "$os" in
linux | darwin) ;;
*) die "unsupported OS '$os' — releases cover linux and darwin; build from source instead" ;;
esac

case "$arch" in
x86_64 | amd64) arch=amd64 ;;
aarch64 | arm64) arch=arm64 ;;
*) die "unsupported architecture '$arch' — releases cover amd64 and arm64; build from source instead" ;;
esac

# --- version --------------------------------------------------------------
# /releases/latest redirects to /releases/tag/<version>, so the tag can be read
# off the final URL. That costs no API call, which matters: the unauthenticated
# GitHub API allows 60 requests an hour per IP and CI runners share one.
if [ -z "$VERSION" ]; then
	url=$(curl -fsSLI -o /dev/null -w '%{url_effective}' \
		"https://github.com/$REPO/releases/latest" 2>/dev/null) ||
		die "could not reach github.com to resolve the latest release"
	VERSION="${url##*/tag/}"
	case "$VERSION" in
	"" | *//*) die "no published release found for $REPO" ;;
	esac
fi

base="https://github.com/$REPO/releases/download/$VERSION"

tmp=$(mktemp -d 2>/dev/null || mktemp -d -t wyrd)
trap 'rm -rf "$tmp"' EXIT INT TERM

# --- checksums ------------------------------------------------------------
# Verification is not optional: this script pipes a downloaded executable
# straight onto the user's PATH. If no sha256 tool exists, say so and stop
# rather than installing something unverified.
if command -v sha256sum >/dev/null 2>&1; then
	sha256() { sha256sum "$1" | cut -d' ' -f1; }
elif command -v shasum >/dev/null 2>&1; then
	sha256() { shasum -a 256 "$1" | cut -d' ' -f1; }
else
	die "need sha256sum or shasum to verify the download"
fi

curl -fsSL "$base/checksums.txt" -o "$tmp/checksums.txt" 2>/dev/null ||
	die "release $VERSION has no checksums.txt — it may predate the release pipeline; pick another with --version"

# --- install --------------------------------------------------------------
mkdir -p "$DIR" || die "cannot create $DIR"
[ -w "$DIR" ] || die "$DIR is not writable — rerun with --dir <path>, or with sudo"

for bin in $BINARIES; do
	tarball="${bin}_${VERSION}_${os}_${arch}.tar.gz"

	say "downloading $tarball"
	curl -fsSL "$base/$tarball" -o "$tmp/$tarball" 2>/dev/null ||
		die "release $VERSION has no asset $tarball"

	want=$(grep " $tarball\$" "$tmp/checksums.txt" | cut -d' ' -f1) ||
		die "$tarball is not listed in checksums.txt"
	[ -n "$want" ] || die "$tarball is not listed in checksums.txt"

	got=$(sha256 "$tmp/$tarball")
	[ "$got" = "$want" ] ||
		die "checksum mismatch for $tarball (expected $want, got $got)"

	tar -xzf "$tmp/$tarball" -C "$tmp" "$bin" ||
		die "could not extract $bin from $tarball"

	cp "$tmp/$bin" "$DIR/$bin.tmp$$" && chmod 0755 "$DIR/$bin.tmp$$" &&
		mv -f "$DIR/$bin.tmp$$" "$DIR/$bin" ||
		die "could not install $bin into $DIR"

	say "installed $DIR/$bin ($VERSION)"
done

# --- PATH -----------------------------------------------------------------
case ":${PATH}:" in
*":$DIR:"*) ;;
*)
	say ""
	say "$DIR is not on your PATH. Add it:"
	say "    export PATH=\"\$PATH:$DIR\""
	;;
esac

say ""
say "Next: cd into a Go repo and run 'wyrd daemon start'."
