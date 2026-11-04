#!/usr/bin/env bash
# Install sysmap from GitHub Releases into ~/.local/bin by default.
# Usage:
#   curl -fsSL https://raw.githubusercontent.com/towry/sysmap/main/install.sh | bash
#   SYSMAP_VERSION=v1.0.0 bash install.sh
#   PREFIX=/usr/local bash install.sh
set -euo pipefail

REPO="${SYSMAP_REPO:-towry/sysmap}"
PREFIX="${PREFIX:-${HOME}/.local}"
BIN_DIR="${SYSMAP_BIN_DIR:-${PREFIX}/bin}"
VERSION="${SYSMAP_VERSION:-latest}"
BINARY_NAME="sysmap"

die() {
	echo "install.sh: $*" >&2
	exit 1
}

need() {
	command -v "$1" >/dev/null 2>&1 || die "need $1"
}

os_name() {
	case "$(uname -s)" in
	Linux) echo linux ;;
	Darwin) echo darwin ;;
	*) die "unsupported OS: $(uname -s). Supported: linux, darwin." ;;
	esac
}

arch_name() {
	case "$(uname -m)" in
	x86_64 | amd64) echo amd64 ;;
	arm64 | aarch64) echo arm64 ;;
	*) die "unsupported architecture: $(uname -m). Supported: amd64, arm64." ;;
	esac
}

download() {
	local url="$1"
	local dest="$2"
	if ! curl -fsSL --retry 3 --retry-delay 1 -o "${dest}" "${url}"; then
		die "failed to download ${url}"
	fi
}

need curl
need tar
need mktemp
need uname

OS="$(os_name)"
ARCH="$(arch_name)"
if [[ -z "${VERSION}" ]]; then
	VERSION="latest"
fi
if [[ "${VERSION}" != "latest" ]]; then
	case "${VERSION}" in
	v*) ;;
	*) VERSION="v${VERSION}" ;;
	esac
fi

ARCHIVE="${BINARY_NAME}_${OS}_${ARCH}.tar.gz"
if [[ "${VERSION}" == "latest" ]]; then
	BASE="https://github.com/${REPO}/releases/latest/download"
else
	BASE="https://github.com/${REPO}/releases/download/${VERSION}"
fi
TMP="$(mktemp -d)"
trap 'rm -rf "${TMP}"' EXIT

echo "installing ${BINARY_NAME} ${VERSION} (${OS}/${ARCH}) to ${BIN_DIR}"
download "${BASE}/${ARCHIVE}" "${TMP}/${ARCHIVE}"
download "${BASE}/checksums.txt" "${TMP}/checksums.txt"

if command -v sha256sum >/dev/null 2>&1; then
	(cd "${TMP}" && sha256sum -c --ignore-missing checksums.txt)
elif command -v shasum >/dev/null 2>&1; then
	want="$(awk -v f="${ARCHIVE}" '$2 == f { print $1; exit }' "${TMP}/checksums.txt")"
	[[ -n "${want}" ]] || die "checksums.txt has no entry for ${ARCHIVE}"
	got="$(shasum -a 256 "${TMP}/${ARCHIVE}" | awk '{ print $1 }')"
	[[ "${got}" == "${want}" ]] || die "checksum mismatch for ${ARCHIVE}"
else
	die "need sha256sum or shasum to verify the download"
fi

tar -xzf "${TMP}/${ARCHIVE}" -C "${TMP}"
[[ -f "${TMP}/${BINARY_NAME}" ]] || die "archive ${ARCHIVE} did not contain ${BINARY_NAME}"

mkdir -p "${BIN_DIR}"
install -m 0755 "${TMP}/${BINARY_NAME}" "${BIN_DIR}/${BINARY_NAME}"

echo "installed ${BIN_DIR}/${BINARY_NAME}"
"${BIN_DIR}/${BINARY_NAME}" --version

case ":${PATH}:" in
*:"${BIN_DIR}":*) ;;
*)
	echo "add ${BIN_DIR} to PATH, for example:"
	echo "  export PATH=\"${BIN_DIR}:\$PATH\""
	;;
esac
