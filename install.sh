#!/bin/sh
# Install macup from GitHub releases.
#
#   curl -fsSL https://raw.githubusercontent.com/Hinkolas/macup/main/install.sh | sh
#
# Environment:
#   MACUP_VERSION      release tag to install (default: latest), e.g. v0.1.0
#   MACUP_INSTALL_DIR  target directory (default: ~/.local/bin)

set -eu

REPO="Hinkolas/macup"
INSTALL_DIR="${MACUP_INSTALL_DIR:-$HOME/.local/bin}"

err() {
	echo "macup install: $*" >&2
	exit 1
}

[ "$(uname -s)" = "Darwin" ] || err "macup only supports macOS"

case "$(uname -m)" in
arm64 | aarch64) arch="arm64" ;;
x86_64 | amd64) arch="amd64" ;;
*) err "unsupported architecture: $(uname -m)" ;;
esac

version="${MACUP_VERSION:-}"
if [ -z "$version" ]; then
	# The /releases/latest page redirects to /releases/tag/<tag>
	latest_url=$(curl -fsSLI -o /dev/null -w '%{url_effective}' "https://github.com/$REPO/releases/latest") ||
		err "could not determine the latest release"
	version="${latest_url##*/}"
	case "$version" in
	v*) ;;
	*) err "no published release found" ;;
	esac
fi

asset="macup_darwin_${arch}.tar.gz"
base_url="https://github.com/$REPO/releases/download/$version"

tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT INT TERM

echo "Downloading macup $version ($arch)..."
curl -fsSL -o "$tmp/$asset" "$base_url/$asset" || err "download failed: $base_url/$asset"
curl -fsSL -o "$tmp/checksums.txt" "$base_url/checksums.txt" || err "download failed: $base_url/checksums.txt"

(
	cd "$tmp"
	grep " $asset\$" checksums.txt | shasum -a 256 -c - >/dev/null
) || err "checksum verification failed"

tar -xzf "$tmp/$asset" -C "$tmp" macup
mkdir -p "$INSTALL_DIR"
install -m 755 "$tmp/macup" "$INSTALL_DIR/macup"

echo "Installed macup $version to $INSTALL_DIR/macup"

case ":$PATH:" in
*":$INSTALL_DIR:"*) ;;
*)
	echo
	echo "$INSTALL_DIR is not on your PATH. Add it with:"
	echo "  echo 'export PATH=\"$INSTALL_DIR:\$PATH\"' >> ~/.zshrc && source ~/.zshrc"
	;;
esac
