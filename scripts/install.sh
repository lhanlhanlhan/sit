#!/usr/bin/env bash
set -euo pipefail

REPO="${SIT_REPO:-lhanlhanlhan/sit}"
INSTALL_DIR="${SIT_INSTALL_DIR:-$HOME/bin}"
BIN_NAME="sit"

need() {
  if ! command -v "$1" >/dev/null 2>&1; then
    echo "sit installer: missing required command: $1" >&2
    exit 1
  fi
}

detect_asset() {
  os="$(uname -s)"
  arch="$(uname -m)"

  case "$os/$arch" in
    Linux/x86_64 | Linux/amd64)
      echo "sit_linux_amd64"
      ;;
    Darwin/arm64 | Darwin/aarch64)
      echo "sit_darwin_arm64"
      ;;
    *)
      echo "sit installer: unsupported platform: $os/$arch" >&2
      exit 1
      ;;
  esac
}

need curl
need mktemp
need sed
need tr

ASSET="$(detect_asset)"
API_URL="https://api.github.com/repos/${REPO}/releases/latest"

echo "sit installer: resolving latest release for ${REPO}"
RELEASE_JSON="$(curl -fsSL "${API_URL}")"
TAG="$(printf '%s' "${RELEASE_JSON}" | sed -n 's/.*"tag_name":[[:space:]]*"\([^"]*\)".*/\1/p' | head -n 1)"

if [ -z "${TAG}" ]; then
  echo "sit installer: failed to resolve latest release tag" >&2
  exit 1
fi

URL="https://github.com/${REPO}/releases/download/${TAG}/${ASSET}"
TMP="$(mktemp)"

cleanup() {
  rm -f "${TMP}"
}
trap cleanup EXIT

echo "sit installer: downloading ${ASSET} from ${TAG}"
curl -fL "${URL}" -o "${TMP}"
chmod +x "${TMP}"

mkdir -p "${INSTALL_DIR}"
mv -f "${TMP}" "${INSTALL_DIR}/${BIN_NAME}"

VERSION="$("${INSTALL_DIR}/${BIN_NAME}" version | tr -d '\r')"
echo "sit installer: installed ${VERSION} to ${INSTALL_DIR}/${BIN_NAME}"
