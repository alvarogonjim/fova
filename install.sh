#!/bin/sh
# install.sh — fova one-line installer (v0.5 SPECS §20 #5).
#
# Usage:
#   curl -fsSL https://fova.dev/install | sh
#
# Environment overrides:
#   FOVA_INSTALL_DIR  install destination (default: ~/.local/bin)
#   FOVA_VERSION      pin to a specific release tag (default: latest)
#
# Safety rails:
#   * set -eu and pipefail so any failure aborts the install.
#   * Refuses to overwrite a `fova` binary that already lives outside
#     FOVA_INSTALL_DIR (e.g. a Homebrew install at /opt/homebrew/bin/fova).
#   * Verifies the archive's SHA256 against the release's checksums.txt before
#     extracting anything.
#   * Cleans up its temp dir on success, failure, or interrupt.

set -eu
# pipefail is bash/zsh/dash-on-most-distros; guard the set in case the shell
# happens to be a strict POSIX `sh` that lacks the option.
# shellcheck disable=SC3040
(set -o pipefail 2>/dev/null) && set -o pipefail

REPO_OWNER="alvarogonjim"
REPO_NAME="fova"
INSTALL_DIR="${FOVA_INSTALL_DIR:-$HOME/.local/bin}"
VERSION="${FOVA_VERSION:-}"

# ----- helpers ---------------------------------------------------------------

die() { printf 'install.sh: %s\n' "$*" >&2; exit 1; }
info() { printf '==> %s\n' "$*"; }
warn() { printf 'warning: %s\n' "$*" >&2; }

need() { command -v "$1" >/dev/null 2>&1 || die "missing required tool: $1"; }

# ----- preflight -------------------------------------------------------------

need uname
need mkdir
need rm
need tar
need install
# Either curl or wget is fine.
if command -v curl >/dev/null 2>&1; then
  fetch() { curl -fsSL -o "$2" "$1"; }
  fetch_stdout() { curl -fsSL "$1"; }
elif command -v wget >/dev/null 2>&1; then
  fetch() { wget -qO "$2" "$1"; }
  fetch_stdout() { wget -qO - "$1"; }
else
  die "need curl or wget on PATH"
fi
# Either sha256sum (Linux) or shasum -a 256 (macOS) is fine.
if command -v sha256sum >/dev/null 2>&1; then
  sha256() { sha256sum "$1" | awk '{print $1}'; }
elif command -v shasum >/dev/null 2>&1; then
  sha256() { shasum -a 256 "$1" | awk '{print $1}'; }
else
  die "need sha256sum or shasum on PATH"
fi

# ----- OS / arch -------------------------------------------------------------

uname_s=$(uname -s)
case "$uname_s" in
  Darwin) OS=darwin ;;
  Linux)  OS=linux  ;;
  *)      die "unsupported OS: $uname_s (fova ships darwin/linux/windows; Windows users see the README)" ;;
esac

uname_m=$(uname -m)
case "$uname_m" in
  x86_64|amd64)        ARCH=amd64 ;;
  arm64|aarch64)       ARCH=arm64 ;;
  *)                   die "unsupported architecture: $uname_m" ;;
esac

info "platform: ${OS}/${ARCH}"

# ----- refuse to clobber an out-of-tree fova ---------------------------------

existing=$(command -v fova 2>/dev/null || true)
if [ -n "$existing" ]; then
  case "$existing" in
    "$INSTALL_DIR"/*)
      warn "replacing existing $existing"
      ;;
    *)
      die "refusing to clobber an existing fova at $existing (outside $INSTALL_DIR). Uninstall it first, or set FOVA_INSTALL_DIR to a directory of your choice."
      ;;
  esac
fi

# ----- resolve the version tag ----------------------------------------------

if [ -z "$VERSION" ]; then
  info "fetching latest release tag"
  api="https://api.github.com/repos/${REPO_OWNER}/${REPO_NAME}/releases/latest"
  VERSION=$(fetch_stdout "$api" | sed -n 's/.*"tag_name": *"\([^"]*\)".*/\1/p' | head -n1)
  [ -n "$VERSION" ] || die "could not parse a tag from $api — is the network reachable and the repo public?"
fi
# Strip a leading `v` from the version-in-archive-name (goreleaser drops it).
version_bare=${VERSION#v}

info "installing fova ${VERSION}"

# ----- download and verify ---------------------------------------------------

archive_name="fova_${version_bare}_${OS}_${ARCH}.tar.gz"
release_base="https://github.com/${REPO_OWNER}/${REPO_NAME}/releases/download/${VERSION}"
archive_url="${release_base}/${archive_name}"
checksums_url="${release_base}/checksums.txt"

tmpdir=$(mktemp -d 2>/dev/null || mktemp -d -t fova-install)
cleanup() { rm -rf "$tmpdir"; }
trap cleanup EXIT INT TERM

info "downloading $archive_url"
fetch "$archive_url" "$tmpdir/$archive_name"

info "downloading checksums.txt"
fetch "$checksums_url" "$tmpdir/checksums.txt"

info "verifying SHA256"
expected=$(awk -v name="$archive_name" '$2 == name || $2 == "*"name { print $1 }' "$tmpdir/checksums.txt" | head -n1)
[ -n "$expected" ] || die "no checksum entry for $archive_name in checksums.txt"
actual=$(sha256 "$tmpdir/$archive_name")
[ "$expected" = "$actual" ] || die "checksum mismatch: expected $expected, got $actual"

# ----- extract and install ---------------------------------------------------

info "extracting"
tar -xzf "$tmpdir/$archive_name" -C "$tmpdir"
[ -f "$tmpdir/fova" ] || die "archive did not contain a fova binary"

mkdir -p "$INSTALL_DIR"
install -m 0755 "$tmpdir/fova" "$INSTALL_DIR/fova"
info "installed $INSTALL_DIR/fova"

# ----- PATH hint -------------------------------------------------------------

case ":${PATH:-}:" in
  *":$INSTALL_DIR:"*) : ;;  # already on PATH
  *)
    cat <<EOF

fova is installed at $INSTALL_DIR/fova, but that directory is not on
your PATH. Add it by appending the following to your shell's rc file:

    export PATH="$INSTALL_DIR:\$PATH"

Then re-open the shell, or run \`source <rc-file>\`.

EOF
    ;;
esac

"$INSTALL_DIR/fova" version || warn "could not run \`fova version\` (binary present but not executable on this PATH)"
