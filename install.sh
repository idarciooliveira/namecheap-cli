#!/bin/sh
# Install ncli from GitHub Releases.
#
#   curl -fsSL https://raw.githubusercontent.com/idarciooliveira/namecheap-cli/main/install.sh | sh
#
# Options (env vars):
#   NCLI_VERSION      tag to install, e.g. v0.1.0 (default: latest)
#   NCLI_INSTALL_DIR  where to put the binary (default: ~/.local/bin)
set -eu

REPO="idarciooliveira/namecheap-cli"
BIN="ncli"

err() { echo "ncli install: $*" >&2; exit 1; }
info() { echo "$*" >&2; }

if command -v curl >/dev/null 2>&1; then
  fetch() { curl -fsSL "$1" -o "$2"; }
  resolve_latest() { curl -fsSLI -o /dev/null -w '%{url_effective}' "https://github.com/$REPO/releases/latest"; }
elif command -v wget >/dev/null 2>&1; then
  fetch() { wget -qO "$2" "$1"; }
  resolve_latest() { wget -S --spider "https://github.com/$REPO/releases/latest" 2>&1 | sed -n 's/^ *[Ll]ocation: *//p' | tail -1 | tr -d '\r'; }
else
  err "curl or wget is required"
fi

case "$(uname -s)" in
  Linux) os=linux ;;
  Darwin) os=darwin ;;
  *) err "unsupported OS $(uname -s); on Windows use install.ps1" ;;
esac

case "$(uname -m)" in
  x86_64 | amd64) arch=amd64 ;;
  arm64 | aarch64) arch=arm64 ;;
  *) err "unsupported CPU $(uname -m)" ;;
esac

tag="${NCLI_VERSION:-}"
if [ -z "$tag" ]; then
  url=$(resolve_latest) || err "could not reach GitHub"
  tag="${url##*/}"
  case "$tag" in v*) ;; *) err "could not find the latest release" ;; esac
fi
version="${tag#v}"

archive="${BIN}_${version}_${os}_${arch}.tar.gz"
base="https://github.com/$REPO/releases/download/$tag"

tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT

info "Downloading $BIN $tag ($os/$arch)..."
fetch "$base/$archive" "$tmp/$archive" || err "download failed: $base/$archive"
fetch "$base/checksums.txt" "$tmp/checksums.txt" || err "download failed: checksums.txt"

expected=$(awk -v f="$archive" '$2 == f { print $1 }' "$tmp/checksums.txt")
[ -n "$expected" ] || err "$archive not listed in checksums.txt"
if command -v sha256sum >/dev/null 2>&1; then
  actual=$(sha256sum "$tmp/$archive" | awk '{print $1}')
else
  actual=$(shasum -a 256 "$tmp/$archive" | awk '{print $1}')
fi
[ "$expected" = "$actual" ] || err "checksum mismatch for $archive"

tar -xzf "$tmp/$archive" -C "$tmp" "$BIN"

dir="${NCLI_INSTALL_DIR:-$HOME/.local/bin}"
mkdir -p "$dir" || err "cannot create $dir"
install -m 0755 "$tmp/$BIN" "$dir/$BIN" 2>/dev/null || { cp "$tmp/$BIN" "$dir/$BIN" && chmod 0755 "$dir/$BIN"; } || err "cannot write to $dir"

info "Installed $("$dir/$BIN" --version) to $dir/$BIN"
case ":$PATH:" in
  *":$dir:"*) ;;
  *) info ""
     info "$dir is not on your PATH. Add this to your shell profile:"
     info "  export PATH=\"$dir:\$PATH\"" ;;
esac
info ""
info "Next: ncli auth login"
