#!/usr/bin/env bash
set -euo pipefail

REPO="${WORM_REPO:-Alice088/worm}"
BIN_DIR="${WORM_BIN_DIR:-$HOME/.local/bin}"
VERSION="${WORM_VERSION:-latest}"
ACTION="install"
PURGE=0
YES=0
ADD_PATH=1

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
  --no-path             do not add the install directory to the shell rc
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
		--no-path) ADD_PATH=0; shift ;;
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

fetch() {
	local url="$1" out="$2" attempt=1 waited=0
	while :; do
		curl -fsSL -o "$out" "$url" 2>/dev/null && return 0
		[ "$attempt" -ge 12 ] && return 1
		if [ "$waited" -eq 0 ]; then
			log "release is still publishing, waiting…"
			waited=1
		fi
		sleep 5
		attempt=$((attempt + 1))
	done
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
	[ -n "$tag" ] || fail "no releases published yet: https://github.com/$REPO/releases"
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
	fetch "$base/$asset" "$WORK/$asset" \
		|| fail "release asset not found: $base/$asset"
	fetch "$base/checksums.txt" "$WORK/checksums.txt" \
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

	ensure_path
	log "run: worm"
}

shell_rc() {
	case "${SHELL##*/}" in
		zsh) printf '%s\n' "$HOME/.zshrc" ;;
		bash) printf '%s\n' "$HOME/.bashrc" ;;
		*) printf '%s\n' "$HOME/.profile" ;;
	esac
}

ensure_path() {
	case ":$PATH:" in
		*":$bin_dir_expanded:"*) return 0 ;;
	esac
	[ "$ADD_PATH" -eq 1 ] || return 0
	local rc
	rc="$(shell_rc)"
	grep -qsF 'worm installer' "$rc" && return 0
	if printf '\n# worm installer\nexport PATH="%s:$PATH"\n' "$bin_dir_expanded" >>"$rc" 2>/dev/null; then
		log "added $bin_dir_expanded to PATH in $rc (restart the shell or source it)"
	else
		log "add $bin_dir_expanded to PATH: export PATH=\"$bin_dir_expanded:\$PATH\""
	fi
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
