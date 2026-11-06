#!/bin/sh
# Installs a rehome release into ~/.local/bin: curl -fsSL <raw url>/install.sh | sh
# REHOME_VERSION=v0.1.0-rc.1 picks a tag, including a prerelease; the default is the latest stable release.
set -eu

repo="rchehd/rehome"
dest="${REHOME_BIN_DIR:-$HOME/.local/bin}"
version="${REHOME_VERSION:-}"

case "$(uname -s)" in
  Linux) os=linux ;;
  Darwin) os=darwin ;;
  *) echo "unsupported OS: $(uname -s)" >&2; exit 1 ;;
esac

case "$(uname -m)" in
  x86_64 | amd64) arch=amd64 ;;
  aarch64 | arm64) arch=arm64 ;;
  *) echo "unsupported architecture: $(uname -m)" >&2; exit 1 ;;
esac

fetch() {
  if command -v curl >/dev/null 2>&1; then
    curl -fsSL "$1" -o "$2"
  elif command -v wget >/dev/null 2>&1; then
    wget -qO "$2" "$1"
  else
    echo "need curl or wget" >&2
    exit 1
  fi
}

sha256() {
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "$1" | cut -d' ' -f1
  else
    shasum -a 256 "$1" | cut -d' ' -f1
  fi
}

archive="rehome_${os}_${arch}.tar.gz"
base="https://github.com/$repo/releases/latest/download"

if [ -n "$version" ]; then
  base="https://github.com/$repo/releases/download/$version"
fi
tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

fetch "$base/$archive" "$tmp/$archive"
fetch "$base/checksums.txt" "$tmp/checksums.txt"

expected="$(grep " $archive\$" "$tmp/checksums.txt" | cut -d' ' -f1)"

if [ -z "$expected" ] || [ "$expected" != "$(sha256 "$tmp/$archive")" ]; then
  echo "checksum mismatch for $archive" >&2
  exit 1
fi

tar -xzf "$tmp/$archive" -C "$tmp"
mkdir -p "$dest"
install -m 0755 "$tmp/rehome" "$dest/rehome"
echo "installed rehome to $dest/rehome"

case ":$PATH:" in
  *":$dest:"*) ;;
  *) echo "add $dest to your PATH" ;;
esac

case "${SHELL##*/}" in
  zsh) echo "tab completion: echo 'source <(rehome completion zsh)' >> ~/.zshrc" ;;
  bash) echo "tab completion: echo 'source <(rehome completion bash)' >> ~/.bashrc" ;;
  fish) echo "tab completion: rehome completion fish > ~/.config/fish/completions/rehome.fish" ;;
esac
