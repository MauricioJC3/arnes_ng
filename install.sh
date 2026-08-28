#!/bin/sh
# Instalador de arnes para Linux y macOS.
#   curl -fsSL https://raw.githubusercontent.com/MauricioJC3/arnes_ng/main/install.sh | sh
#
# Variables opcionales:
#   ARNES_INSTALL_DIR   dónde poner el binario (default: ~/.local/bin)
#   ARNES_VERSION       tag a instalar (default: el último release)
set -eu

REPO="MauricioJC3/arnes_ng"
BIN="arnes"

os=$(uname -s | tr '[:upper:]' '[:lower:]')
case "$os" in
  linux | darwin) ;;
  *) echo "SO no soportado: $os (en Windows usá install.ps1)" >&2; exit 1 ;;
esac

arch=$(uname -m)
case "$arch" in
  x86_64 | amd64) arch=amd64 ;;
  aarch64 | arm64) arch=arm64 ;;
  *) echo "arquitectura no soportada: $arch" >&2; exit 1 ;;
esac

asset="${BIN}-${os}-${arch}"
if [ "${ARNES_VERSION:-}" = "" ]; then
  url="https://github.com/${REPO}/releases/latest/download/${asset}"
else
  url="https://github.com/${REPO}/releases/download/${ARNES_VERSION}/${asset}"
fi

dir="${ARNES_INSTALL_DIR:-$HOME/.local/bin}"
mkdir -p "$dir"
dest="${dir}/${BIN}"

echo "bajando ${asset}…"
tmp=$(mktemp)
if command -v curl >/dev/null 2>&1; then
  curl -fSL "$url" -o "$tmp"
elif command -v wget >/dev/null 2>&1; then
  wget -qO "$tmp" "$url"
else
  echo "hace falta curl o wget" >&2; exit 1
fi

chmod +x "$tmp"
mv "$tmp" "$dest"
echo "instalado en ${dest}"

case ":$PATH:" in
  *":$dir:"*) ;;
  *) printf '\nagregá esto a tu shell rc (~/.bashrc, ~/.zshrc):\n  export PATH="%s:$PATH"\n' "$dir" ;;
esac

"$dest" --version || true
