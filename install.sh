#!/usr/bin/env bash
#
# nutshell installer for Linux and macOS.
#
#   curl -fsSL https://github.com/mhrlife/nutshell/releases/latest/download/install.sh | bash
#
# It picks the build for this machine, verifies its checksum, drops the binary
# in ~/.nutshell/bin and puts that directory on your PATH. Windows has its own
# installer, install.ps1.
set -euo pipefail

REPO="mhrlife/nutshell"
APP="nutshell"

BOLD=$'\033[1m'
DIM=$'\033[2m'
RED=$'\033[0;31m'
GREEN=$'\033[0;32m'
YELLOW=$'\033[0;33m'
NC=$'\033[0m'

if [ ! -t 1 ]; then
	BOLD=""
	DIM=""
	RED=""
	GREEN=""
	YELLOW=""
	NC=""
fi

info() { printf '%s\n' "$1"; }
step() { printf '%s==>%s %s\n' "$GREEN" "$NC" "$1"; }
warn() { printf '%swarning:%s %s\n' "$YELLOW" "$NC" "$1" >&2; }
die() {
	printf '%serror:%s %s\n' "$RED" "$NC" "$1" >&2
	exit 1
}

usage() {
	cat <<EOF
nutshell installer

Usage: install.sh [options]

Options:
  -v, --version <version>  Install a specific version (e.g. v0.1.0)
  -d, --dir <path>         Install directory (default: \$HOME/.nutshell/bin)
      --no-modify-path     Don't touch your shell config
      --force              Reinstall even if this version is already present
  -h, --help               Show this help

Environment:
  NUTSHELL_VERSION       Same as --version
  NUTSHELL_INSTALL_DIR   Same as --dir

Examples:
  curl -fsSL https://github.com/$REPO/releases/latest/download/install.sh | bash
  curl -fsSL https://github.com/$REPO/releases/latest/download/install.sh | bash -s -- --version v0.1.0
  NUTSHELL_INSTALL_DIR=/usr/local/bin ./install.sh
EOF
}

requested_version="${NUTSHELL_VERSION:-}"
install_dir="${NUTSHELL_INSTALL_DIR:-$HOME/.nutshell/bin}"
modify_path=true
force=false

while [ $# -gt 0 ]; do
	case "$1" in
	-h | --help)
		usage
		exit 0
		;;
	-v | --version)
		[ -n "${2:-}" ] || die "--version needs a value"
		requested_version="$2"
		shift 2
		;;
	-d | --dir)
		[ -n "${2:-}" ] || die "--dir needs a value"
		install_dir="$2"
		shift 2
		;;
	--no-modify-path)
		modify_path=false
		shift
		;;
	--force)
		force=true
		shift
		;;
	*)
		die "unknown option '$1' (try --help)"
		;;
	esac
done

for tool in curl tar; do
	command -v "$tool" >/dev/null 2>&1 || die "'$tool' is required but not installed"
done

# --- what are we running on? ------------------------------------------------

case "$(uname -s)" in
Linux*) os=linux ;;
Darwin*) os=darwin ;;
MINGW* | MSYS* | CYGWIN* | Windows*)
	die "this is the Unix installer; on Windows run:
  irm https://github.com/$REPO/releases/latest/download/install.ps1 | iex"
	;;
*) die "unsupported operating system: $(uname -s)" ;;
esac

case "$(uname -m)" in
x86_64 | amd64) arch=amd64 ;;
aarch64 | arm64) arch=arm64 ;;
*) die "unsupported architecture: $(uname -m)" ;;
esac

# A shell under Rosetta reports x86_64 on Apple silicon; install the native
# build instead.
if [ "$os" = darwin ] && [ "$arch" = amd64 ] &&
	[ "$(sysctl -n sysctl.proc_translated 2>/dev/null || echo 0)" = 1 ]; then
	arch=arm64
fi

# --- which version? ---------------------------------------------------------

if [ -n "$requested_version" ]; then
	version="$requested_version"
else
	step "Looking up the latest release"
	# /releases/latest redirects to /releases/tag/<tag>; reading the redirect
	# avoids the GitHub API and its unauthenticated rate limit.
	latest_url=$(curl -fsSLI -o /dev/null -w '%{url_effective}' \
		"https://github.com/$REPO/releases/latest") ||
		die "could not reach github.com"
	version="${latest_url##*/}"
fi

case "$version" in
v*) ;;
*) version="v$version" ;;
esac

[ "$version" != "vreleases" ] || die "$REPO has no releases yet"

if [ "$force" = false ] && command -v "$APP" >/dev/null 2>&1; then
	installed=$("$APP" --version 2>/dev/null | awk '{print $1}' || true)
	if [ "$installed" = "$version" ]; then
		info "${BOLD}$APP $version${NC} is already installed at $(command -v "$APP")"
		info "${DIM}Pass --force to reinstall.${NC}"
		exit 0
	fi
fi

# --- download and verify ----------------------------------------------------

archive="${APP}_${version#v}_${os}_${arch}.tar.gz"
base_url="https://github.com/$REPO/releases/download/$version"

tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT

step "Downloading $archive"
curl -fL --progress-bar -o "$tmp/$archive" "$base_url/$archive" ||
	die "no build for $os/$arch in $version — see https://github.com/$REPO/releases"

if curl -fsSL -o "$tmp/checksums.txt" "$base_url/checksums.txt" 2>/dev/null; then
	if command -v sha256sum >/dev/null 2>&1; then
		actual=$(sha256sum "$tmp/$archive" | awk '{print $1}')
	elif command -v shasum >/dev/null 2>&1; then
		actual=$(shasum -a 256 "$tmp/$archive" | awk '{print $1}')
	else
		actual=""
		warn "no sha256sum or shasum; skipping checksum verification"
	fi

	if [ -n "$actual" ]; then
		expected=$(awk -v f="$archive" '$2 == f || $2 == "*" f {print $1}' "$tmp/checksums.txt")
		[ -n "$expected" ] || die "$archive is missing from checksums.txt"
		[ "$actual" = "$expected" ] || die "checksum mismatch for $archive — refusing to install"
		info "${DIM}checksum ok${NC}"
	fi
else
	warn "could not fetch checksums.txt; skipping verification"
fi

# --- install ----------------------------------------------------------------

tar -xzf "$tmp/$archive" -C "$tmp"
[ -f "$tmp/$APP" ] || die "$archive did not contain a $APP binary"

mkdir -p "$install_dir" 2>/dev/null ||
	die "cannot create $install_dir — pick another with --dir, or re-run with sudo"

if [ ! -w "$install_dir" ]; then
	die "$install_dir is not writable — pick another with --dir, or re-run with sudo"
fi

install -m 755 "$tmp/$APP" "$install_dir/$APP"

# Gatekeeper refuses unsigned binaries that carry the quarantine flag; the
# release builds are unsigned, so clear it.
if [ "$os" = darwin ]; then
	xattr -d com.apple.quarantine "$install_dir/$APP" 2>/dev/null || true
fi

step "Installed $APP $version to $install_dir/$APP"

# --- PATH -------------------------------------------------------------------

on_path=false
case ":$PATH:" in
*":$install_dir:"*) on_path=true ;;
esac

add_to_rc() {
	local file=$1 line=$2

	if [ -f "$file" ] && grep -Fqx "$line" "$file"; then
		return 0
	fi

	if [ -e "$file" ] && [ ! -w "$file" ]; then
		return 1
	fi

	printf '\n# nutshell\n%s\n' "$line" >>"$file" || return 1
	step "Added $install_dir to your PATH in $file"
}

if [ "$on_path" = false ] && [ "$modify_path" = true ]; then
	shell_name=$(basename "${SHELL:-sh}")
	xdg="${XDG_CONFIG_HOME:-$HOME/.config}"

	case "$shell_name" in
	fish)
		rc_files="$xdg/fish/config.fish"
		path_line="fish_add_path $install_dir"
		;;
	zsh)
		rc_files="${ZDOTDIR:-$HOME}/.zshrc ${ZDOTDIR:-$HOME}/.zshenv"
		path_line="export PATH=\"$install_dir:\$PATH\""
		;;
	*)
		rc_files="$HOME/.bashrc $HOME/.bash_profile $HOME/.profile"
		path_line="export PATH=\"$install_dir:\$PATH\""
		;;
	esac

	rc_target=""

	for file in $rc_files; do
		if [ -f "$file" ]; then
			rc_target="$file"
			break
		fi
	done

	# Nothing to append to: create the first candidate rather than give up.
	if [ -z "$rc_target" ]; then
		rc_target="${rc_files%% *}"
		mkdir -p "$(dirname "$rc_target")" 2>/dev/null || true
	fi

	add_to_rc "$rc_target" "$path_line" || {
		warn "could not write $rc_target; add this line yourself:"
		info "  $path_line"
	}
fi

if [ -n "${GITHUB_ACTIONS:-}" ] && [ -n "${GITHUB_PATH:-}" ]; then
	printf '%s\n' "$install_dir" >>"$GITHUB_PATH"
fi

# --- what's left to do ------------------------------------------------------

info ""
info "${BOLD}nutshell $version${NC}"
info ""

if [ "$on_path" = false ]; then
	info "  Open a new terminal (or run: ${BOLD}export PATH=\"$install_dir:\$PATH\"${NC})"
fi

if ! command -v claude >/dev/null 2>&1; then
	info "  Install the agent CLI:   ${BOLD}npm i -g @anthropic-ai/claude-code${NC}"
fi

if [ -z "${OPENROUTER_API_KEY:-}" ]; then
	info "  Set a key for voice:     ${BOLD}export OPENROUTER_API_KEY=sk-or-...${NC}"
	info "  ${DIM}Without it nutshell still works with typed questions.${NC}"
fi

info "  Then, in any project:    ${BOLD}nutshell${NC}"
info ""
