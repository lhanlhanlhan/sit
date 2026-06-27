#!/usr/bin/env bash
# Serve the static Manager GUI from localhost so it has a stable browser origin.
set -euo pipefail

cd "$(dirname "$0")/.."

PORT="${SIT_WEB_PORT:-45101}"
HOST="${SIT_WEB_HOST:-127.0.0.1}"
URL="http://localhost:${PORT}/login.html"

echo "sit web: serving ./web at ${URL}"
echo "sit web: set SIT_WEB_PORT=xxxx to use a different port"
cd web
python3 -m http.server "${PORT}" --bind "${HOST}"
