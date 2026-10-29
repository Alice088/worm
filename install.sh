#!/usr/bin/env bash
set -euo pipefail

REPO="${WORM_REPO:-Alice088/worm}"
BIN_DIR="${WORM_BIN_DIR:-$HOME/.local/bin}"
VERSION="${WORM_VERSION:-latest}"
ACTION="install"
PURGE=0
YES=0

usage() {
	cat <<'EOF'
worm installer

Usage:
  curl -fsSL https://raw.githubusercontent.com/Alice088/worm/master/install.sh | bash
  curl -fsSL https://raw.githubusercontent.com/Alice088/worm/master/install.sh | bash -s -- --update
  curl -fsSL https://raw.githubusercontent.com/Alice088/worm/master/install.sh | bash -s -- --uninstall

Options:
  --update              install the latest release (default)
  --uninstall           remove worm and worm-server from the install dir
  --purge               with --uninstall also remove ~/.worm config
  --version TAG         install a specific release tag (e.g. v1.2.3)
  --dir DIR             install directory (default: ~/.local/bin)
  --yes, -y             do not ask for confirmation
  -h, --help            show this help
EOF
}

log() { printf '%s\n' "$*"; }
fail() { printf 'worm: %s\n' "$*" >&2; exit 1; }

while [ $# -gt 0 ]; do
	case "$1" in
		--update) ACTION="install"; shift ;;
		--uninstall|--remove) ACTION="uninstall"; shift ;;
		--purge) PURGE=1; shift ;;
		--version) [ $# -ge 2 ] || fail "--version needs a tag"; VERSION="$2"; shift 2 ;;
		--dir) [ $# -ge 2 ] || fail "--dir needs a path"; BIN_DIR="$2"; shift 2 ;;
		--yes|-y) YES=1; shift ;;
		-h|--help) usage; exit 0 ;;
		*) fail "unknown option: $1 (see --help)" ;;
	esac
done

bin_dir_expanded="${BIN_DIR/#\~/$HOME}"

confirm() {
	[ "$YES" -eq 1 ] && return 0
	[ -t 0 ] || return 1
	printf '%s [y/N] ' "$1"
	read -r answer
	case "$answer" in
		y|Y|yes|YES) return 0 ;;
		*) return 1 ;;
	esac
}

uninstall() {
	local removed=0
	for name in worm worm-server; do
		if [ -f "$bin_dir_expanded/$name" ]; then
			rm -f "$bin_dir_expanded/$name"
			log "removed $bin_dir_expanded/$name"
			removed=1
		fi
	done
	[ "$removed" -eq 1 ] || log "nothing to remove in $bin_dir_expanded"
	if [ "$PURGE" -eq 1 ]; then
		if confirm "remove config directory $HOME/.worm?"; then
			rm -rf "$HOME/.worm"
			log "removed $HOME/.worm"
		fi
	fi
}

detect_platform() {
	os="$(uname -s | tr '[:upper:]' '[:lower:]')"
	case "$os" in
		linux|darwin) ;;
		*) fail "unsupported OS: $os" ;;
	esac
	arch="$(uname -m)"
	case "$arch" in
		x86_64|amd64) arch="amd64" ;;
		aarch64|arm64) arch="arm64" ;;
		*) fail "unsupported architecture: $arch" ;;
	esac
}

latest_tag() {
	local url tag
	url="$(curl -fsSLI -o /dev/null -w '%{url_effective}' "https://github.com/$REPO/releases/latest")" \
		|| fail "cannot reach github releases"
	tag="${url##*/}"
	case "$tag" in
		v*) printf '%s\n' "$tag"; return 0 ;;
	esac
	tag="$(curl -fsSL "https://api.github.com/repos/$REPO/releases/latest" \
		| sed -n 's/.*"tag_name"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' | head -n 1)"
	[ -n "$tag" ] || fail "cannot resolve the latest release tag"
	printf '%s\n' "$tag"
}

install_release() {
	detect_platform
	command -v curl >/dev/null 2>&1 || fail "curl is required"

	local tag="$VERSION"
	[ "$tag" = "latest" ] && tag="$(latest_tag)"
	[ -n "$tag" ] || fail "cannot resolve the latest release tag"

	local version="${tag#v}"
	local asset="worm_${version}_${os}_${arch}.tar.gz"
	local base="${WORM_ASSET_BASE:-https://github.com/$REPO/releases/download/$tag}"
	WORK="$(mktemp -d)"
	trap cleanup EXIT

	log "downloading $asset ($tag)"
	curl -fsSL --retry 3 -o "$WORK/$asset" "$base/$asset" \
		|| fail "release asset not found: $base/$asset"
	curl -fsSL --retry 3 -o "$WORK/checksums.txt" "$base/checksums.txt" \
		|| fail "checksums.txt not found for $tag"

	verify_checksum "$WORK/checksums.txt" "$asset" "$WORK/$asset"

	tar -xzf "$WORK/$asset" -C "$WORK" worm
	[ -f "$WORK/worm" ] || fail "archive does not contain worm"

	mkdir -p "$bin_dir_expanded"
	install -m 0755 "$WORK/worm" "$bin_dir_expanded/worm"
	log "installed $bin_dir_expanded/worm ($tag)"

	if tar -tzf "$WORK/$asset" | grep -qx 'worm-server'; then
		tar -xzf "$WORK/$asset" -C "$WORK" worm-server
		install -m 0755 "$WORK/worm-server" "$bin_dir_expanded/worm-server"
		log "installed $bin_dir_expanded/worm-server"
	fi

	case ":$PATH:" in
		*":$bin_dir_expanded:"*) ;;
		*) log "add $bin_dir_expanded to PATH: export PATH=\"$bin_dir_expanded:\$PATH\"" ;;
	esac
	log "run: worm"
}

cleanup() {
	[ -n "${WORK:-}" ] && rm -rf "$WORK"
	WORK=""
}

verify_checksum() {
	local sums="$1" asset="$2" file="$3" expected actual
	expected="$(awk -v name="$asset" '$2 == name || $2 == "*" name { print tolower($1); exit }' "$sums")"
	[ -n "$expected" ] || fail "no checksum for $asset"
	if command -v sha256sum >/dev/null 2>&1; then
		actual="$(sha256sum "$file" | awk '{print tolower($1)}')"
	else
		actual="$(shasum -a 256 "$file" | awk '{print tolower($1)}')"
	fi
	[ "$expected" = "$actual" ] || fail "checksum mismatch for $asset"
}

case "$ACTION" in
	uninstall) uninstall ;;
	install) install_release ;;
esac
