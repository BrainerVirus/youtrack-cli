#!/bin/sh
# Install ytrack, the unofficial YouTrack CLI, on Linux or macOS without a
# package manager or sudo:
#
#   curl -fsSL https://raw.githubusercontent.com/BrainerVirus/youtrack-cli/main/install.sh | sh
#
# It downloads the release archive for this OS and CPU, checks its SHA-256
# against the release's checksums file, checks that file's cosign signature
# when cosign is installed, puts ytrack in ~/.local/bin and installs shell
# completions for your shell ($SHELL: zsh, bash or fish).
#
# Environment:
#   YTRACK_VERSION      release tag to install, e.g. v0.3.0 (default: latest)
#   YTRACK_INSTALL_DIR  target directory (default: ~/.local/bin)
#   YTRACK_NO_COMPLETIONS=1  skip installing shell completions
#   YTRACK_RELEASES_URL, YTRACK_RELEASES_API_URL
#                       release download and API roots, for mirrors and tests
#
# Later upgrades: `ytrack upgrade`.
set -eu

repo="BrainerVirus/youtrack-cli"
releases_url="${YTRACK_RELEASES_URL:-https://github.com/$repo/releases}"
api_url="${YTRACK_RELEASES_API_URL:-https://api.github.com/repos/$repo}"
install_dir="${YTRACK_INSTALL_DIR:-$HOME/.local/bin}"
signer_issuer="https://token.actions.githubusercontent.com"
signer_identity='^https://github\.com/BrainerVirus/youtrack-cli/\.github/workflows/release\.yml@refs/tags/v[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.]+)?$'

say() { printf '%s\n' "$*" >&2; }
fail() {
	say "ytrack install: $*"
	exit 1
}

fetch() { # fetch URL FILE
	if command -v curl >/dev/null 2>&1; then
		curl -fsSL --proto '=https,http' --retry 2 -o "$2" "$1"
	elif command -v wget >/dev/null 2>&1; then
		wget -q -O "$2" "$1"
	else
		fail "curl or wget is required"
	fi
}

sha256() {
	if command -v sha256sum >/dev/null 2>&1; then
		sha256sum "$1" | cut -d ' ' -f 1
	elif command -v shasum >/dev/null 2>&1; then
		shasum -a 256 "$1" | cut -d ' ' -f 1
	else
		fail "sha256sum or shasum is required to verify the download"
	fi
}

detect_os() {
	case "$(uname -s)" in
	Linux) echo linux ;;
	Darwin) echo darwin ;;
	MINGW* | MSYS* | CYGWIN* | Windows_NT) fail "on Windows, use install.ps1 (irm .../install.ps1 | iex)" ;;
	*) fail "unsupported operating system: $(uname -s)" ;;
	esac
}

detect_arch() {
	arch="$(uname -m)"
	# A shell under Rosetta reports x86_64 on Apple silicon; prefer the native build.
	if [ "$1" = darwin ] && [ "$arch" = x86_64 ] && [ "$(sysctl -n sysctl.proc_translated 2>/dev/null || echo 0)" = 1 ]; then
		arch=arm64
	fi
	case "$arch" in
	x86_64 | amd64) echo amd64 ;;
	aarch64 | arm64) echo arm64 ;;
	*) fail "unsupported CPU architecture: $arch" ;;
	esac
}

# zsh_fpath_dir prints the first directory in zsh's $fpath (as a
# non-interactive zsh sees it) that is under $HOME and writable, if any.
zsh_fpath_dir() {
	command -v zsh >/dev/null 2>&1 || return 0
	zsh -c 'print -rl -- $fpath' 2>/dev/null | while IFS= read -r d; do
		case "$d" in
		"$HOME"/*)
			if [ -d "$d" ] && [ -w "$d" ]; then
				printf '%s\n' "$d"
				break
			fi
			;;
		esac
	done
}

# install_completions ARCHIVE DIR copies the completion script for the
# user's shell from the release archive. It never fails the install.
install_completions() {
	[ "${YTRACK_NO_COMPLETIONS:-}" = 1 ] && return 0
	shell_name="$(basename "${SHELL:-sh}")"
	data_home="${XDG_DATA_HOME:-$HOME/.local/share}"
	found_fpath=""
	case "$shell_name" in
	zsh)
		file=_ytrack
		found_fpath="$(zsh_fpath_dir)"
		dest_dir="${found_fpath:-$data_home/zsh/site-functions}"
		dest="$dest_dir/_ytrack"
		;;
	bash)
		file=ytrack.bash
		dest_dir="$data_home/bash-completion/completions"
		dest="$dest_dir/ytrack"
		;;
	fish)
		file=ytrack.fish
		dest_dir="${XDG_CONFIG_HOME:-$HOME/.config}/fish/completions"
		dest="$dest_dir/ytrack.fish"
		;;
	*)
		say ""
		say "Shell completions: see 'ytrack completion --help' for $shell_name."
		return 0
		;;
	esac
	if ! tar -xzf "$1" -C "$2" "completions/$file" 2>/dev/null || [ ! -s "$2/completions/$file" ]; then
		say ""
		say "This release has no $shell_name completions; generate them with 'ytrack completion $shell_name'."
		return 0
	fi
	if ! { mkdir -p "$dest_dir" && cp "$2/completions/$file" "$dest"; } 2>/dev/null; then
		say ""
		say "Could not write $dest; generate completions with 'ytrack completion $shell_name'."
		return 0
	fi
	say ""
	say "Installed $shell_name completions to $dest"
	case "$shell_name" in
	zsh)
		if [ -n "$found_fpath" ]; then
			say "That directory is on zsh's \$fpath, so new zsh sessions load them."
		else
			say "Enable them: add these lines to ~/.zshrc, before any other compinit call:"
			say "  fpath=(\"$dest_dir\" \$fpath)"
			say "  autoload -Uz compinit && compinit"
		fi
		;;
	bash)
		say "bash-completion loads them in new bash sessions. Without bash-completion, add to ~/.bashrc:"
		say "  [ -r \"$dest\" ] && . \"$dest\""
		;;
	fish)
		say "fish loads them in new fish sessions."
		;;
	esac
}

main() {
	os="$(detect_os)"
	arch="$(detect_arch "$os")"

	tmp="$(mktemp -d 2>/dev/null || mktemp -d -t ytrack)"
	trap 'rm -rf "$tmp"' EXIT INT TERM

	tag="${YTRACK_VERSION:-}"
	if [ -z "$tag" ]; then
		fetch "$api_url/releases/latest" "$tmp/latest.json" || fail "cannot find the latest release"
		tag="$(tr ',' '\n' <"$tmp/latest.json" | sed -n 's/.*"tag_name"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' | head -n 1)"
	fi
	case "$tag" in v*) ;; *) tag="v$tag" ;; esac
	printf '%s\n' "$tag" | grep -Eq '^v[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.]+)?$' || fail "invalid release tag: '$tag'"
	version="${tag#v}"

	archive="ytrack_${version}_${os}_${arch}.tar.gz"
	checksums="ytrack_${version}_checksums.txt"
	base="$releases_url/download/$tag"

	say "Downloading ytrack $version for $os/$arch..."
	fetch "$base/$checksums" "$tmp/$checksums" || fail "cannot download $checksums for $tag"
	if command -v cosign >/dev/null 2>&1; then
		fetch "$base/$checksums.sigstore.json" "$tmp/$checksums.sigstore.json" ||
			fail "cannot download the signature $checksums.sigstore.json for $tag"
		cosign verify-blob \
			--bundle "$tmp/$checksums.sigstore.json" \
			--certificate-oidc-issuer "$signer_issuer" \
			--certificate-identity-regexp "$signer_identity" \
			"$tmp/$checksums" >/dev/null 2>&1 ||
			fail "the signature of $checksums does not verify; nothing was installed"
		say "Verified the cosign signature of $checksums."
	else
		say "cosign not found; skipping the signature check (the SHA-256 is still checked)."
	fi

	fetch "$base/$archive" "$tmp/$archive" || fail "cannot download $archive (no build for $os/$arch in $tag?)"
	want="$(awk -v f="$archive" '$2 == f || $2 == "*" f { print $1; exit }' "$tmp/$checksums")"
	[ -n "$want" ] || fail "$checksums does not list $archive"
	got="$(sha256 "$tmp/$archive")"
	[ "$got" = "$want" ] || fail "checksum mismatch for $archive (got $got, want $want); nothing was installed"

	mkdir -p "$tmp/x"
	tar -xzf "$tmp/$archive" -C "$tmp/x" ytrack || fail "$archive does not contain ytrack"

	mkdir -p "$install_dir" || fail "cannot create $install_dir"
	# Copy next to the target, then rename: the swap is atomic.
	cp "$tmp/x/ytrack" "$install_dir/.ytrack.new" || fail "cannot write to $install_dir"
	chmod 755 "$install_dir/.ytrack.new"
	mv -f "$install_dir/.ytrack.new" "$install_dir/ytrack"

	say "Installed ytrack $version to $install_dir/ytrack"
	install_completions "$tmp/$archive" "$tmp/x"
	case ":${PATH:-}:" in
	*":$install_dir:"*) ;;
	*)
		say ""
		say "$install_dir is not on your PATH. Add it, e.g. in ~/.profile or your shell's rc file:"
		say "  export PATH=\"$install_dir:\$PATH\""
		;;
	esac
}

main "$@"
