#!/bin/sh

set -eu

repository="abcdlsj/gnar"
install_dir="${GNAR_INSTALL_DIR:-${HOME}/.local/bin}"
version="${GNAR_VERSION:-latest}"

fail() {
    printf 'gnar: %s\n' "$1" >&2
    exit 1
}

command -v curl >/dev/null 2>&1 || fail "curl is required to install gnar"
command -v tar >/dev/null 2>&1 || fail "tar is required to install gnar"

case "$(uname -s)" in
    Darwin)
        system="apple-darwin"
        [ "$(uname -m)" = "arm64" ] || fail "unsupported macOS architecture: $(uname -m); only Apple Silicon is supported"
        ;;
    Linux) system="unknown-linux-musl" ;;
    *) fail "unsupported operating system: $(uname -s); only macOS and Linux are supported" ;;
esac

case "$(uname -m)" in
    x86_64 | amd64) architecture="x86_64" ;;
    arm64 | aarch64) architecture="aarch64" ;;
    *) fail "unsupported Linux architecture: $(uname -m); only x86_64 and arm64 are supported" ;;
esac

target="${architecture}-${system}"
archive="gnar-${target}.tar.gz"

if [ "$version" = "latest" ]; then
    download_url="https://github.com/${repository}/releases/latest/download/${archive}"
else
    case "$version" in
        v*) tag="$version" ;;
        *) tag="v${version}" ;;
    esac
    download_url="https://github.com/${repository}/releases/download/${tag}/${archive}"
fi

temporary_dir="$(mktemp -d 2>/dev/null || mktemp -d -t gnar)"
trap 'rm -rf "$temporary_dir"' EXIT HUP INT TERM

printf 'Downloading gnar for %s...\n' "$target"
curl --proto '=https' --proto-redir '=https' --tlsv1.2 \
    --fail --location --silent --show-error \
    --output "${temporary_dir}/${archive}" "$download_url" || \
    fail "download failed; check that the requested release supports ${target}"
curl --proto '=https' --proto-redir '=https' --tlsv1.2 \
    --fail --location --silent --show-error \
    --output "${temporary_dir}/${archive}.sha256" "${download_url}.sha256" || \
    fail "checksum download failed"

expected_checksum="$(awk 'NR == 1 { print $1 }' "${temporary_dir}/${archive}.sha256")"
if command -v sha256sum >/dev/null 2>&1; then
    actual_checksum="$(sha256sum "${temporary_dir}/${archive}" | awk '{ print $1 }')"
elif command -v shasum >/dev/null 2>&1; then
    actual_checksum="$(shasum -a 256 "${temporary_dir}/${archive}" | awk '{ print $1 }')"
else
    fail "sha256sum or shasum is required to verify the download"
fi

[ "$expected_checksum" = "$actual_checksum" ] || fail "download checksum does not match"

tar -xzf "${temporary_dir}/${archive}" -C "$temporary_dir"
mkdir -p "$install_dir" || fail "cannot create ${install_dir}"
install -m 755 "${temporary_dir}/gnar" "${install_dir}/gnar" || \
    fail "cannot write to ${install_dir}; set GNAR_INSTALL_DIR to a writable directory"

printf 'Installed gnar to %s/gnar\n' "$install_dir"
case ":${PATH}:" in
    *":${install_dir}:"*) ;;
    *) printf 'Add %s to PATH before running gnar.\n' "$install_dir" ;;
esac
